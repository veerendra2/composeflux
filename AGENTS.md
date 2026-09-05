# ComposeFlux — Agent Guidelines

## Project Overview

ComposeFlux is a Go application that implements a GitOps reconciliation loop for Docker Compose stacks. It polls a Git repository, detects changes via Git diff and Compose project dependency trees, and deploys/prunes stacks using the native Docker Compose SDK.

**Module**: `github.com/veerendra2/composeflux`  
**Go version**: 1.26 (CGO enabled — Bitwarden SDK uses cgo FFI into Rust)

---

## Repository Layout

```
cmd/composeflux/        # CLI subcommands (run, sync) setup via kong
cmd/playground/         # Dev scratch area
internal/reconcile/     # Core reconciliation loop, Git sync, health checks, prune logic
pkg/dockercompose/      # Docker & Compose SDK wrapper
pkg/gitrepo/            # Git repository client (go-git wrapper)
pkg/localsecrets/       # Local encrypted secret providers (Age; optional)
pkg/remotesecrets/      # Remote secret providers (Bitwarden, Infisical; optional)
docs/                   # MkDocs documentation
docs/internal/          # Internal notes excluded from the published MkDocs site
```

---

## Build, Lint, Test Commands

The project uses [Task](https://taskfile.dev/) (`Taskfile.yml`).

```bash
task build          # Compile binary to dist/composeflux (injects version via ldflags)
task run            # go run ./cmd/composeflux (runs vet first)
task fmt            # go fmt ./...
task vet            # go vet ./...
task lint           # golangci-lint run --timeout 3m
task test           # go test ./...
task security       # govulncheck ./...
task all            # fmt + lint + vet + security + test (full local CI)
task build-docker   # docker build -t composeflux .
task install        # Install govulncheck and golangci-lint
```

### Running a Single Test

```bash
# Run one test function in a specific package
go test -v ./internal/reconcile/... -run TestFunctionName

# Run all tests in a package
go test ./pkg/localsecrets/...

# Run with race detector
go test -race ./...
```

> No `*_test.go` files exist yet. Use Go's standard `testing` package; `testify` is available as a transitive dependency.

---

## Coding Instructions & Guidelines

### Agreed Design Decisions

These policies were confirmed with the maintainer during the September 2026 review.
Preserve them in future changes; do not reopen them unless a new requirement or concrete conflict arises.

- **Keep the implementation small**: Do not change the project folder structure. Split functions only for actual reuse or independently testable logic, not merely to reduce function length. Avoid speculative abstractions and cosmetic refactors.
- **Failure handling**: An error returned by the initial `GitSync` must fail startup and exit. During periodic reconciliation, log failures, skip the affected work, and keep the daemon running. Existing per-stack warning-and-continue behavior is intentional.
- **Outcome logging**: Report deployed stacks, deployment failures, load failures, and intentionally skipped stacks separately. Use warning severity when any stack fails. Do not describe a one-shot run with skipped failures as an unconditional success; logging changes must not introduce retries or alter exit behavior.
- **Retries**: Keep the existing pending-Git retry mechanism for now. Do not add another retry mechanism, retry queue, or persistence layer without explicit approval. The existing optional health-reconciliation policy is unchanged.
- **Manual recovery is intentional**: A crash after Git updates the checkout but before deployment completes can leave healthy stacks on older configuration. Likewise, an image pull can succeed while deployment fails. Operators may correct the Compose configuration and run `composeflux sync`; do not introduce applied-revision persistence or automatic image-deployment retries to cover these cases.
- **Git checkout ownership (#54)**: Git is the source of truth. Before startup checkout or upstream reset, discard local changes to tracked files and warn with their paths. Preserve unrelated untracked files; do not add `git clean` or unrestricted go-git `HardReset`/forced checkout. Reset changed tracked paths explicitly, then use normal checkout or `MergeReset` for the target revision. Do not stash, commit, or merge application-written local changes.
- **Stack configuration failures**: A missing optional `stack.yml` is acceptable. A malformed or unreadable existing file must stop that reconciliation pass rather than substitute empty environment variables or startup order. Initial-sync errors exit; periodic errors are logged without exiting.
- **Infisical partial results**: Keep successful paths when another configured secret path fails, and log a warning for each failed path. Fail if every path fails. This best-effort policy is intentional.
- **Suspension source of truth**: Read `composeflux.health.suspend=true` from the loaded Git Compose project, not from stale running-container labels. Any active service with that label suspends the entire stack: skip Git deployment (including force sync), health recovery, and image updates. Suspend also blocks the entire periodic Docker resource-prune pass.
- **Suspension lifecycle**: Adding the label in Git does not require redeploying it onto running containers. Removing the label in Git resumes management once the change is pulled. Deleting the stack from Git explicitly requests removal, so `PruneStacks` still removes it even if its old containers carry a suspension label.
- **Dependency boundaries**: Retain missing in-repository bind sources so Git deletions of a file or directory contents still match. Do not start tracking external host paths. Directory-secret caching must avoid repeated decryption while reapplying cached values to every source-load environment that visits that directory.
- **Validate before side effects**: Require `GIT_INTERVAL > 0`, nonnegative optional health/prune intervals, and a valid nonempty image-update cron expression before initializing clients.
- **Project identity**: Reject loaded projects whose Compose name differs from the stack directory name, before building or deploying them. Custom names are not a supported feature. Do not add automatic renaming or custom-name support as a cleanup.
- **Cron lifecycle**: Skip overlapping image-update jobs with the existing cron wrapper and wait for active jobs before returning from `Run` and closing clients. Recheck cancellation after acquiring the reconciliation mutex, before starting image-update work. Waiting may delay shutdown if a provider call is stuck; a timeout must not close a native client still in use.
- **SDK cancellation limits**: Bitwarden calls do not accept a context. The pinned Infisical v0.8.0 constructor context controls token-refresh lifecycle, not individual secret HTTP requests. Do not claim that passing a context to the constructor enforces request deadlines, or wrap blocking SDK calls in abandoned goroutines.
- **Tests are deferred**: Do not add test cases until the maintainer explicitly resumes that work. Continue using the existing formatting, vet, lint, and build tooling for code changes.

Build-argument dependency tracking is explicitly deferred to [issue #74](https://github.com/veerendra2/composeflux/issues/74);
do not implement it as part of the current cleanup. Remote-request timeout changes still require implementation approval.

### Implementation Principles

- **Surgical & Simple**: Keep diffs minimal, concise, readable, and production-ready. Avoid unneeded abstractions, interfaces with single implementations, or premature options/scaffolding.
- **Preserve Functionality**: Never break existing behavior or CLI contracts. Ensure changes do not introduce regressions.
- **Verify Edge Cases & Docs**: Always cross-check edge cases against official library/platform documentation (e.g., `go-git`, `docker/compose`, `filepath.Rel` traversal behavior, OS path separators).
- **Bug & Leak Prevention**:
  - Always clean up resources (`defer cancel()`, `defer file.Close()`, `defer ticker.Stop()`).
  - Watch for goroutine leaks, unbounded memory allocation, and unhandled errors in loop select cases.
  - Guard nil references, map lookups, and pointer dereferences.

### Formatting

- Format with `go fmt ./...` before committing.
- Run `go vet ./...` and `task lint` on modified code.

### Import Organization

Three groups separated by blank lines:

```go
import (
    // 1. Standard library
    "context"
    "fmt"
    "log/slog"

    // 2. Third-party
    "github.com/alecthomas/kong"
    mobyClient "github.com/moby/moby/client"

    // 3. Internal module
    "github.com/veerendra2/composeflux/internal/reconcile"
    "github.com/veerendra2/composeflux/pkg/remotesecrets"
)
```

- Use aliases only to resolve conflicts (e.g. `mobyClient`, `dockerconfigtypes`, `infisical`).
- No dot imports.

### Naming Conventions

| Element | Convention | Example |
|---|---|---|
| Variables, fields | `camelCase` | `stackPath`, `gitInterval` |
| Exported functions/methods | `PascalCase` | `New()`, `Deploy()`, `GitSync()` |
| Unexported functions | `camelCase` | `discoverComposeStack()`, `isManagedStack()` |
| Structs / Types | `PascalCase` | `Reconciler`, `StackConfig` |
| Interfaces | `PascalCase` | `Client` |
| Exported constants | `PascalCase` | `LabelManaged`, `LabelDeployedAt` |
| Unexported constants | `camelCase` | `appName` |
| Source files | `snake_case.go` | `deploy.go`, `config.go`, `bitwarden.go` |
| Receiver names | Short (1–2 letters) | `r` for `*Reconciler`, `c` for `*client` |
| Type aliases | `PascalCase` | `type StackStateMap map[string]StackInfo` |

### Interfaces and Dependency Injection

- Each integration package (`localsecrets`, `remotesecrets`, `gitrepo`, `dockercompose`) exposes an exported `Client` interface and an unexported concrete struct.
- `Reconciler` holds interface types only — never concrete implementations.

```go
type Client interface {
    Deploy(ctx context.Context, project *types.Project) error
}
type client struct { /* unexported */ }
```

### Structs and Configuration

- `kong` struct tags carry CLI flag name, env var, default, and help text in one place.
- Embed shared config with `embed:""` to avoid duplication across subcommands.
- Use `yaml` struct tags (library: `go.yaml.in/yaml/v4`) for file-based config (`stack.yml`).

### Error Handling

In order of precedence:

```go
// 1. Wrap with context
return nil, fmt.Errorf("failed to clone: %w", err)

// 2. Sentinel comparison
if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) { ... }

// 3. Log structured, then return
slog.Error("Failed to create client", "provider", name, "error", err)
return nil, err

// 4. Warn and continue in reconciliation loops (non-fatal)
if err := r.Deploy(ctx, project); err != nil {
    slog.Warn("Failed to deploy stack", "stack_name", name, "error", err)
    continue
}

// 5. Fatal only at cmd layer
ctx.FatalIfErrorf(ctx.Run())
```

- Never `panic` in `internal/` or `pkg/`.
- Return cleanup funcs alongside errors from initializers (see `InitClients` pattern).

### Logging

- Use `log/slog` throughout — not `logrus` or `fmt.Println`.
- Structured key-value pairs: `slog.Info("message", "key", value)`.
- Levels: `Debug` for internals, `Info` for lifecycle events, `Warn` for recoverable failures, `Error` for non-recoverable.
- The Docker SDK uses logrus internally; it is bridged to slog in `pkg/dockercompose/logger.go` — do not add new logrus dependencies.

### Context Usage

- `context.Context` is always the first parameter on I/O functions.
- Bound external calls with `context.WithTimeout`; always `defer cancel()`.
- Handle `ctx.Done()` in long-running loops (graceful shutdown via `signal.NotifyContext`).

### Concurrency

- `reconcileMu sync.Mutex` serializes `GitSync`, `UpdateImages`, `ReconcileHealth`, and `PruneResources` to prevent concurrent reconciliation work — lock it as the first action in each method.
- `sync.Mutex` / `sync.RWMutex` zero values are ready to use; do not initialise them explicitly in `New()`.

### Constructor Pattern

- Every package exposes `New(...)` returning `(Interface, error)` or `(*Type, error)`.
- Constructors perform all initialisation: auth, connection setup, validation.

---

## CI / GitHub Actions

- **`ci.yml`**: Runs `golangci-lint` on all pull requests. Run `task lint` locally before opening a PR.
- **`release.yml`**: Builds and pushes multi-arch images (`linux/amd64`, `linux/arm64`) to `ghcr.io` on semver tags (`v*.*.*`).

Internal engineering and research notes belong under `docs/internal/`. Keep that directory covered by MkDocs `exclude_docs`; do not add its files to site navigation.

## Docker / Build Notes

- CGO is enabled (`CGO_ENABLED=1`) for the Bitwarden SDK (Rust FFI).
- Multi-stage Dockerfile: `golang:1.26` builder → `gcr.io/distroless/static-debian13` final image.
- Version info injected at link time via `-ldflags` (git tag, commit SHA, branch, build date).
- Local dev: `task compose` expects a local `compose-dev.yml` file, which is not tracked in this repository.

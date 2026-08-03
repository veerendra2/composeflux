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
pkg/secrets/            # Secrets manager integrations (Bitwarden, Infisical) — optional
pkg/source/             # Git client (go-git wrapper)
docs/                   # MkDocs documentation
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
go test ./pkg/secrets/...

# Run with race detector
go test -race ./...
```

> No `*_test.go` files exist yet. Use Go's standard `testing` package; `testify` is available as a transitive dependency.

---

## Coding Instructions & Guidelines

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
    "github.com/veerendra2/composeflux/pkg/secrets"
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

- Each integration package (`secrets`, `source`, `dockercompose`) exposes an exported `Client` interface and an unexported concrete struct.
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

- `reconcileMu sync.Mutex` serializes `GitSync` and `UpdateImages` to prevent concurrent execution and partial deployments — lock it as the first action in both methods.
- `sync.Mutex` / `sync.RWMutex` zero values are ready to use; do not initialise them explicitly in `New()`.

### Constructor Pattern

- Every package exposes `New(...)` returning `(Interface, error)` or `(*Type, error)`.
- Constructors perform all initialisation: auth, connection setup, validation.

---

## CI / GitHub Actions

- **`ci.yml`**: Runs `golangci-lint` on all pull requests. Run `task lint` locally before opening a PR.
- **`release.yml`**: Builds and pushes multi-arch images (`linux/amd64`, `linux/arm64`) to `ghcr.io` on semver tags (`v*.*.*`).

## Docker / Build Notes

- CGO is enabled (`CGO_ENABLED=1`) for the Bitwarden SDK (Rust FFI).
- Multi-stage Dockerfile: `golang:1.26` builder → `gcr.io/distroless/static-debian13` final image.
- Version info injected at link time via `-ldflags` (git tag, commit SHA, branch, build date).
- Local dev: `task compose` runs the app via `compose.yml`.


# ComposeFlux — Agent Guidelines

## Project Overview

ComposeFlux is a Go application that implements a GitOps reconciliation loop for Docker Compose stacks. It polls a Git repository, detects changes via SHA256 checksums stored as container labels, and deploys/prunes stacks using the native Docker Compose SDK.

**Module**: `github.com/veerendra2/composeflux`  
**Go version**: 1.26.0 (CGO enabled — Bitwarden SDK uses cgo FFI into Rust)

---

## Repository Layout

```
cmd/composeflux/        # Main binary entry point (CLI setup via kong)
cmd/prune-playground/   # Dev scratch binary
internal/reconcile/     # Core reconciliation logic (private to module)
pkg/dockercompose/      # Docker Compose SDK wrapper (exported)
pkg/secrets/            # Secrets manager integrations (Bitwarden, Infisical)
pkg/source/             # Git client (go-git)
docs/                   # MkDocs documentation
```

- `cmd/` — executable `main` packages; each subdirectory is a separate binary
- `internal/` — packages that cannot be imported outside this module
- `pkg/` — exported, independently reusable packages; one concern per package

---

## Build, Lint, Test Commands

The project uses [Task](https://taskfile.dev/) as its build runner (`Taskfile.yml`).

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
task serve-docs     # mkdocs serve (Python-based; requires pyenv via .envrc)
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

> Note: No `*_test.go` files exist yet. When writing tests, use Go's standard `testing` package. The `testify` package is available as a transitive dependency if needed.

---

## Code Style Guidelines

### Formatting

- All code must be formatted with `gofmt` / `go fmt ./...` before committing.
- No custom linter config file (`.golangci.yml`) exists; defaults apply.
- Line length is not explicitly enforced — follow idiomatic Go (avoid very long lines).

### Import Organization

Use the standard Go three-group style, separated by blank lines:

```go
import (
    // 1. Standard library
    "context"
    "fmt"
    "log/slog"

    // 2. Third-party
    "github.com/alecthomas/kong"
    dockerClient "github.com/docker/docker/client"

    // 3. Internal module
    "github.com/veerendra2/composeflux/internal/reconcile"
    "github.com/veerendra2/composeflux/pkg/secrets"
)
```

- Use import aliases only to resolve name conflicts (e.g., `dockerClient`, `infisical`).
- Do not use dot imports (`. "pkg"`).

### Naming Conventions

| Element | Convention | Example |
|---|---|---|
| Variables, fields | `camelCase` | `stackPath`, `gitInterval` |
| Exported functions/methods | `PascalCase` | `New()`, `Deploy()`, `CacheGet()` |
| Unexported functions | `camelCase` | `projectChecksum()`, `discoverComposeStack()` |
| Structs / Types | `PascalCase` | `Reconciler`, `StackConfig`, `CommonConfig` |
| Interfaces | `PascalCase` | `Client` |
| Exported constants | `PascalCase` | `LabelManagedBy`, `SSHUser` |
| Unexported constants | `camelCase` | `appName` |
| Source files | `snake_case.go` | `deploy.go`, `cache.go`, `bitwarden.go` |
| Receiver names | Short (1–2 letters) | `r` for `*Reconciler`, `c` for `*client` |
| Type aliases | `PascalCase` | `type StackStateMap map[string]StackInfo` |

### Interfaces and Dependency Injection

- Define a `Client` interface in each integration package (`secrets`, `source`, `dockercompose`).
- Keep concrete implementation structs **unexported** (`client`, `bitwardenClient`).
- `Reconciler` holds interface types — never concrete implementations.
- This pattern is mandatory; it enables testability without changing call sites.

```go
// Correct — exported interface, unexported implementation
type Client interface {
    Deploy(ctx context.Context, project *types.Project) error
}

type client struct { /* ... */ }
```

### Structs and Configuration

- Use `kong` struct tags for CLI flags, env vars, defaults, and help text in one place.
- Embed shared config with `embed:""` tag to avoid duplication across subcommands.
- Use `yaml` struct tags for file-based config (`stack.yml`).

```go
type Config struct {
    StackPath string `name:"stack-path" env:"STACK_PATH" required:"" group:"Reconciler Options:"`
}
```

### Error Handling

Follow these patterns in order of precedence:

**1. Wrap errors with context using `%w`:**
```go
return nil, fmt.Errorf("failed to clone: %w", err)
```

**2. Use `errors.Is()` for sentinel comparison:**
```go
if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) { ... }
```

**3. Log with structured slog, then return:**
```go
if err != nil {
    slog.Error("Failed to create client", "provider", name, "error", err)
    return nil, err
}
```

**4. Warn and continue inside reconciliation loops (non-fatal):**
```go
if err := r.Deploy(ctx, project); err != nil {
    slog.Warn("Failed to deploy stack", "stack_name", name, "error", err)
    continue
}
```

**5. Fatal exit only at the `cmd` layer via kong:**
```go
ctx.FatalIfErrorf(ctx.Run())
```

- Never use `panic` in library code (`internal/`, `pkg/`).
- Return cleanup functions alongside errors from initializers (see `InitClients` pattern).

### Logging

- Use `log/slog` (stdlib) throughout — **not** `logrus` or `fmt.Println`.
- Pass structured key-value pairs: `slog.Info("message", "key", value)`.
- Log at appropriate levels: `Debug` for internals, `Info` for lifecycle events, `Warn` for recoverable failures, `Error` for non-recoverable failures.
- The Docker SDK uses logrus internally; it is bridged to slog via `pkg/dockercompose/logger.go` — do not add new logrus dependencies.

### Context Usage

- Thread `context.Context` as the first parameter through all I/O operations.
- Use `context.WithTimeout` for bounded external calls:
```go
vCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
defer cancel()
```
- Handle `ctx.Done()` in long-running loops (graceful shutdown via `signal.NotifyContext`).

### Concurrency

- Protect shared mutable state with `sync.RWMutex` (see `cache.go`).
- Explicitly nil cached slices after use (`CacheClear`) to release memory.
- Use `signal.NotifyContext` for OS signal handling in the daemon loop.

### Constructor Pattern

- Every package exposes a `New(ctx, ...)` constructor that returns `(Type, error)` or `(Interface, error)`.
- Constructors handle all initialization including auth, connection setup, and validation.

---

## CI / GitHub Actions

- **`.github/workflows/ci.yml`**: Runs `golangci-lint` on all pull requests.
- **`.github/workflows/release.yml`**: Builds and pushes multi-arch Docker images (`linux/amd64`, `linux/arm64`) to `ghcr.io` on semver tags (`v*.*.*`).
- All PRs must pass linting before merge. Run `task lint` locally before opening a PR.

---

## Docker / Build Notes

- CGO is enabled (`CGO_ENABLED=1`) for the Bitwarden SDK (Rust FFI).
- The release Dockerfile uses a multi-stage build: `golang:1.26.0` builder → `gcr.io/distroless/static-debian13` final image.
- Version info is injected at link time via `-ldflags` (git tag, commit SHA, branch, build user/date).
- Local dev: `task compose` runs the app via `compose-dev.yml`.

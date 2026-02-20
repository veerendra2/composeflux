# ComposeFlux – Copilot Instructions

ComposeFlux is a GitOps continuous deployment daemon for Docker Compose, written in Go. It polls a Git repository, detects changed stacks via SHA-256 hashing, and deploys them using the Docker Compose SDK (no shell exec).

## Build, Test & Lint

All tasks use [Task](https://taskfile.dev/) (`Taskfile.yml`).

```bash
task build          # compile to dist/composeflux
task test           # go vet + go test ./...
task lint           # golangci-lint (requires: task install)
task fmt            # gofmt all files
task vet            # go vet only
task security       # govulncheck ./...
task all            # fmt + lint + vet + security + test
task run            # go run with debug logging
task install        # install golangci-lint and govulncheck
```

Run a single test:
```bash
go test ./internal/reconcile/... -run TestFunctionName -v
```

**CGO is required** (`CGO_ENABLED=1` is set in Taskfile). The Bitwarden SDK calls into a Rust library via cgo. On Linux, `musl-gcc` is needed; on macOS, Xcode CLT suffice.

## Architecture

### Package Layout

```
cmd/composeflux/      CLI entry point (kong), two subcommands: run (daemon) and sync (one-shot)
internal/reconcile/   Core reconciliation logic
pkg/secrets/          Secrets manager clients (Bitwarden, Infisical)
pkg/source/           Git source client (go-git, SSH only)
pkg/dockercompose/    Docker Compose SDK wrapper
```

### Reconciliation Flow

`Reconciler.Run()` → `Reconciler.Sync()` on a ticker (default 5m):

1. **Pull** – `source.Client.Pull()` fetches the Git repo  
2. **Config** – reads `stack.yml` (optional) from `<clone-path>/<stack-path>/stack.yml` for `startup_order` and shared `envs`  
3. **Secrets** – fetches all secrets from the provider, stores them in `r.cache` as `KEY=VALUE` strings  
4. **Discover** – scans each subdirectory under `<stack-path>` for compose files; each directory = one stack  
5. **Hash compare** – computes SHA-256 of the marshaled compose YAML and compares against the `compose.stack.hash` container label  
6. **Deploy** – runs `docker compose up` for changed/new stacks, respecting `startup_order`  
7. **Prune** – removes running stacks with the `compose.stack.managed-by=composeflux` label that are no longer in the Git repo  
8. **Clear cache** – wipes `r.cache` after each sync cycle

### Client Interfaces

Each pkg defines an interface, allowing the reconciler to be decoupled and testable:

- `secrets.Client` – `Get(id)`, `FetchAll()`, `Close()`
- `source.Client` – `Pull(ctx)`, `HasUpdates(ctx)`, `Path()`
- `dockercompose.Client` – `LoadProject`, `Up`, `Down`, `List`, `Ps`, `Pull`, `Restart`, `Version`

### CLI / Config

Uses [kong](https://github.com/alecthomas/kong) for CLI flag + env var parsing. Struct tags drive both:
- `cmd/composeflux/common.go` – `CommonConfig` wires all sub-configs together
- Every config field has a matching `env:` tag (e.g. `SECRETS_PROVIDER`, `GIT_REPO_URL`, `GIT_INTERVAL`)

## Key Conventions

### Stack Discovery
Each immediate subdirectory of `<STACK_PATH>` that contains a compose file (`compose.yaml`, `compose.yml`, `docker-compose.yml`, etc.) is treated as one independent stack. Override files (`compose.override.yml`, etc.) are automatically appended.

### Hash-based Change Detection
`projectChecksum()` marshals the fully-loaded `types.Project` to YAML and SHA-256s it. The hash is stored on every container as `compose.stack.hash`. Only stacks with a hash mismatch (or no matching running stack) are redeployed.

### Managed-by Label
ComposeFlux only prunes and tracks stacks whose containers carry `compose.stack.managed-by=composeflux`. Stacks not managed by ComposeFlux are never touched.

### Secrets Cache
Secrets (and `envs` from `stack.yml`) are accumulated into `r.cache []string` as `KEY=VALUE` entries and passed as environment to `LoadProject`. The cache is fully replaced on each sync—never appended across cycles.

### SSH Deploy Key
Git access is SSH-only. The deploy key can be pre-placed at `GIT_SSH_KEY_PATH` (default `/.ssh/composeflux_id_rsa`) or fetched from the secrets manager at startup via `GIT_DEPLOY_KEY_SECRET_REF`. `GIT_SSH_KEY_NAME` is a deprecated alias.

### Logging
Uses `log/slog` throughout. Docker SDK's logrus output is silenced and bridged to slog via `slogHook` in `pkg/dockercompose/logger.go`. Log format/level are controlled by `LOG_FORMAT`, `LOG_LEVEL`, `LOG_ADD_SOURCE` env vars.

### stack.yml Schema
```yaml
startup_order:   # ordered list of stack directory names to deploy first
  - infra
  - app
envs:            # shared env vars injected into all stacks during this sync
  MY_VAR: value
```

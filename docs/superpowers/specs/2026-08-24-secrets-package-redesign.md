# Secrets Package Redesign

**Date:** 2026-08-24
**Branch:** `36-offlinegit-secrets-management`

## Context

ComposeFlux currently has two secrets packages with inconsistent naming and no clear interface contract:
- `pkg/secretsmanager` — external remote providers (Bitwarden, Infisical)
- `pkg/agesecrets` — local encrypted files (age)

The reconciler holds both as separate concrete types, and the external secrets manager carries a deprecation warning (to be removed). The goal is to:
1. Rename both packages to be semantically clear and symmetrical
2. Give each package a clean `Client` interface (matching the pattern of `pkg/source` and `pkg/dockercompose`)
3. Keep them separate — they are different in call signature, they should not share a common interface
4. Remove the deprecation warning on `remotesecrets` — external secrets remain first-class
5. `localsecrets` supports multiple offline backends (age now, sops later) via the same interface — provider is implicit from file extension

---

## Package Renames

| Old | New | Role |
|---|---|---|
| `pkg/secretsmanager` | `pkg/remotesecrets` | Remote API-based providers (Bitwarden, Infisical) |
| `pkg/agesecrets` | `pkg/localsecrets` | Local encrypted file providers (age, future: sops) |

---

## Interfaces

### `pkg/localsecrets`

```go
type Client interface {
    // Decrypt scans dir for secret files, decrypts all found files,
    // and returns merged env vars and the file paths for change tracking.
    Decrypt(dir string) (map[string]*string, []string, error)
}
```

- `FindFiles` (extension-based scanning) is a private implementation detail
- The age backend scans for `*.age`, a future sops backend scans for `*.sops.yaml` etc.
- Provider is selected at construction time (passphrase → age)
- Returns file paths so the reconciler can add them to `deps.FilePaths` for git change detection

### `pkg/remotesecrets`

```go
type Client interface {
    // FetchAll retrieves all secrets from the remote provider.
    FetchAll() (map[string]*string, error)
    // Get retrieves a single secret by key/ID (used for deploy key fetch).
    Get(id string) (string, error)
    Close()
}
```

- Mirrors current `secretsmanager.Client` shape, return type of `FetchAll` changes from `[]Secret` to `map[string]*string` (removes the intermediate `Secret` struct)

---

## CLI Flag Changes (breaking)

### `remotesecrets` (was `secretsmanager`)

| Old flag | New flag | Old env | New env |
|---|---|---|---|
| `--secrets-provider` | `--remote-secrets-provider` | `SECRETS_PROVIDER` | `REMOTE_SECRETS_PROVIDER` |
| `--bitwarden-*` | unchanged | `BITWARDEN_*` | unchanged |
| `--infisical-*` | unchanged | `INFISICAL_*` | unchanged |

### `localsecrets` (was `agesecrets`)

| Old flag | New flag | Old env | New env |
|---|---|---|---|
| `--age-passphrase` | unchanged | `AGE_PASSPHRASE` | unchanged |

Age-specific flags stay as-is. Future sops support adds its own flags. No `--local-secrets-provider` selector — provider is implicit from file extension.

---

## Reconciler Changes

### `internal/reconcile/reconcile.go`

```go
type Reconciler struct {
    lClient localsecrets.Client   // local encrypted files
    rClient remotesecrets.Client  // remote API providers (nil if not configured)
    gClient source.Client
    dClient dockercompose.Client
    ...
}

func New(cfg Config, lClient localsecrets.Client, rClient remotesecrets.Client, gClient source.Client, dClient dockercompose.Client) (*Reconciler, error)
```

### `internal/reconcile/discover.go`

`loadSharedSecrets()` changes:
- Calls `r.rClient.FetchAll()` (if not nil) — returns `map[string]*string` directly, no conversion loop
- Calls `r.lClient.Decrypt(stacksRootDir)` for root shared age files

`decryptAgeEnvs()` is replaced by direct calls to `r.lClient.Decrypt(dir)` — the reconciler no longer orchestrates find + decrypt separately.

### `cmd/composeflux/common.go`

```go
type CommonConfig struct {
    RemoteSecrets remotesecrets.Config `embed:""`
    LocalSecrets  localsecrets.Config  `embed:""`
    ...
}
```

Remove deprecation warning on remote secrets provider. Remove `Secret` struct from `remotesecrets` (callers receive `map[string]*string` directly).

---

## `pkg/localsecrets` Internal Structure

```
pkg/localsecrets/
  client.go      — Client interface + New() constructor + Config struct
  age.go         — ageClient struct implementing Client (moved from pkg/agesecrets)
```

`New(cfg Config) Client` — constructs an `ageClient` for now. When sops is added, `Config` grows a provider selector or file-extension dispatch happens inside `Decrypt`.

---

## Files to Change

| File | Change |
|---|---|
| `pkg/agesecrets/` → `pkg/localsecrets/` | Rename package, restructure into `client.go` + `age.go`, expose `Client` interface |
| `pkg/secretsmanager/` → `pkg/remotesecrets/` | Rename package, change `FetchAll` return to `map[string]*string`, remove `Secret` struct |
| `internal/reconcile/reconcile.go` | Rename fields `sClient`→`rClient`, `ageClient`→`lClient`; update `New` signature |
| `internal/reconcile/discover.go` | Replace `decryptAgeEnvs` calls with `r.lClient.Decrypt(dir)`; update `loadSharedSecrets` |
| `cmd/composeflux/common.go` | Rename config fields, remove deprecation warning, update client construction |

---

## Verification

1. `go build ./...` and `go vet ./...` — clean compile after all renames
2. `go run ./cmd/composeflux/... sync --help` — verify new flag names appear correctly
3. Local integration test with `--age-passphrase` and local bare repo — `docker exec nginx env` shows correctly interpolated secrets
4. Verify `--remote-secrets-provider` flag name in help output

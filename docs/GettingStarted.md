# Getting Started

Deploy ComposeFlux and manage Docker Compose stacks via GitOps.

## Prerequisites

- Docker with Compose v2+
- Git repository with Compose stacks
- Secrets manager: **Bitwarden** or **Infisical** (optional, for secrets injection and deploy key fetching)
- SSH key for Git access (store in secrets manager or mount as volume)

## Environment Variables

### Required

| Variable       | Description                                                   |
| -------------- | ------------------------------------------------------------- |
| `GIT_REPO_URL` | Git repository SSH URL (e.g., `git@github.com:user/repo.git`) |
| `STACK_PATH`   | Path to stacks directory in repo (relative to repo root)      |

### Optional - Secrets Provider

| Variable           | Description                                            |
| ------------------ | ------------------------------------------------------ |
| `SECRETS_PROVIDER` | Secrets manager: `bitwarden` or `infisical` (optional) |

**Bitwarden (when `SECRETS_PROVIDER=bitwarden`):**

| Variable                    | Description                  | Default                                |
| --------------------------- | ---------------------------- | -------------------------------------- |
| `BITWARDEN_ACCESS_TOKEN`    | Machine account access token |                                        |
| `BITWARDEN_ORGANIZATION_ID` | Organization ID              |                                        |
| `BITWARDEN_PROJECT_ID`      | Project ID                   |                                        |
| `BITWARDEN_API_URL`         | Bitwarden API URL            | `https://vault.bitwarden.com/api`      |
| `BITWARDEN_IDENTITY_URL`    | Bitwarden Identity URL       | `https://vault.bitwarden.com/identity` |

**Infisical (when `SECRETS_PROVIDER=infisical`):**

| Variable                  | Description                                                       | Default                     |
| ------------------------- | ----------------------------------------------------------------- | --------------------------- |
| `INFISICAL_CLIENT_ID`     | Universal Auth client ID                                          |                             |
| `INFISICAL_CLIENT_SECRET` | Universal Auth client secret                                      |                             |
| `INFISICAL_ENVIRONMENT`   | Environment slug (e.g., `prod`)                                   |                             |
| `INFISICAL_PROJECT_ID`    | Project ID                                                        |                             |
| `INFISICAL_SITE_URL`      | Infisical site URL                                                | `https://app.infisical.com` |
| `INFISICAL_SECRET_PATH`   | Secret path in Infisical project. Supports comma-separated paths (e.g., `/generic,/apps/prod`). If the same secret exists in multiple paths, the last path's value takes precedence. | `/`                         |

### Optional

| Variable                    | Description                                                                                                                   | Default                    |
| --------------------------- | ----------------------------------------------------------------------------------------------------------------------------- | -------------------------- |
| `GIT_DEPLOY_KEY_SECRET_REF` | Deploy key secret reference (name or ID) in secrets manager (See [Deploy Key Secret Reference](#deploy-key-secret-reference)) |                            |
| `GIT_SSH_KEY_PATH`          | SSH key path inside container                                                                                                 | `/.ssh/composeflux_id_rsa` |
| `GIT_CLONE_PATH`            | Local clone directory                                                                                                         | `/opt/compose-stack`       |
| `GIT_INTERVAL`              | Git sync interval                                                                                                             | `5m`                       |
| `IMAGE_UPDATE_SCHEDULE`     | Cron expression for Docker image update checks, e.g. `0 3 * * *`. Empty = disabled.                                           | `""`                       |
| `GIT_BRANCH`                | Git branch to track                                                                                                           | `main`                     |
| `CONFIG_FILE`               | Stack config file name (see [Stack Configuration](Introduction.md#stack-configuration))                                       | `stack.yml`                |
| `LOG_LEVEL`                 | Log level (`debug`/`info`/`warn`/`error`)                                                                                     | `info`                     |
| `LOG_FORMAT`                | Log format (`console`/`json`)                                                                                                 | `console`                  |
| `LOG_ADD_SOURCE`            | Add source location to logs                                                                                                   | `false`                    |
| `REMOVE_ORPHANS`            | Remove orphan containers during deploy                                                                                        | `true`                     |
| `PRUNE_RESOURCES`           | Prune all unused Docker resources during cleanup                                                                              | `true`                     |
| `METRICS_ADDR`              | Prometheus metrics listen address. Empty to disable.                                                                          | `:9090`                    |

!!! warning

    When `PRUNE_RESOURCES=true`, pruning removes **all unused Docker resources** including images, containers, volumes, networks, and build cache. Any image not used by a running container will be deleted.

## Commands

ComposeFlux supports two commands:

```bash
Usage: composeflux <command> [flags]

A GitOps continuous deployment tool for Docker Compose.

Flags:
  -h, --help                    Show context-sensitive help.
      --log-format="console"    Set the output format of the logs. Must be "console" or "json" ($LOG_FORMAT).
      --log-level=INFO          Set the log level. Must be "DEBUG", "INFO", "WARN" or "ERROR" ($LOG_LEVEL).
      --log-add-source          Whether to add source file and line number to log records ($LOG_ADD_SOURCE).
      --version                 Print version information and exit

Commands:
  run     Run ComposeFlux in daemon mode (continuous reconciliation)
  sync    Perform a one-shot sync and deploy

Run "composeflux <command> --help" for more information on a command.
```

- **`run`** - Daemon mode with continuous reconciliation (default). Performs an initial sync at startup, then checks the
  Git repository for changes at configured intervals (default: 5 minutes).
- **`sync`** - One-shot sync and deploy. Manually triggers immediate synchronization. Useful when you update secrets in
  your secrets manager but haven't made Git changes. See
  [Hash-Based Change Detection](Introduction.md#hash-based-change-detection).

```bash
# Daemon mode (initial sync at startup, then checks Git every 5 minutes)
composeflux run

# One-shot mode - manually trigger sync
composeflux sync
```

**Important**: After the initial startup sync, the `run` command fetches secrets and deploys changes only when Git
updates are detected. If you update secrets in your secrets manager without changing anything in Git, run
`composeflux sync` manually to apply updated secrets. See
[Hash-Based Change Detection](Introduction.md#hash-based-change-detection).

## Deploy ComposeFlux

**1. Set up Secrets Manager:**

- [Bitwarden Setup Guide](how-to-guides/Bitwarden.md)
- [Infisical Setup Guide](how-to-guides/Infisical.md)

**2. Configure Git Access:**

- [GitHub Deploy Keys Setup](how-to-guides/GithubDeployKeys.md) - Recommended for secure read-only access

**3. Create `.env` file:**

```bash
# Required - Common
GIT_REPO_URL=git@github.com:user/stacks-repo.git
STACK_PATH=stacks

# Optional - Choose a secrets provider (omit to run without secrets):

# Option A: Bitwarden
SECRETS_PROVIDER=bitwarden
GIT_DEPLOY_KEY_SECRET_REF=aaaaaaa-bbbbb-bbbb-cccc-ddddd
BITWARDEN_ACCESS_TOKEN=your-access-token
BITWARDEN_ORGANIZATION_ID=your-org-id
BITWARDEN_PROJECT_ID=your-project-id

# Option B: Infisical
# SECRETS_PROVIDER=infisical
# GIT_DEPLOY_KEY_SECRET_REF=SSH_PRIVATE_KEY
# INFISICAL_CLIENT_ID=your-client-id
# INFISICAL_CLIENT_SECRET=your-client-secret
# INFISICAL_ENVIRONMENT=prod
# INFISICAL_PROJECT_ID=your-project-id
```

**4. Create `compose.yml`:**

```yaml
services:
  composeflux:
    image: ghcr.io/veerendra2/composeflux:latest
    container_name: composeflux
    restart: unless-stopped

    environment:
      # Git Configuration
      GIT_REPO_URL: ${GIT_REPO_URL}
      STACK_PATH: ${STACK_PATH}
      # GIT_INTERVAL: 5m              # Sync interval
      # GIT_BRANCH: main

      # Secrets Manager - Bitwarden (optional)
      # SECRETS_PROVIDER: ${SECRETS_PROVIDER}
      # GIT_DEPLOY_KEY_SECRET_REF: ${GIT_DEPLOY_KEY_SECRET_REF}
      # BITWARDEN_ACCESS_TOKEN: ${BITWARDEN_ACCESS_TOKEN}
      # BITWARDEN_ORGANIZATION_ID: ${BITWARDEN_ORGANIZATION_ID}
      # BITWARDEN_PROJECT_ID: ${BITWARDEN_PROJECT_ID}

      # Secrets Manager - Infisical (comment out Bitwarden above if using this)
      # SECRETS_PROVIDER: infisical
      # GIT_DEPLOY_KEY_SECRET_REF: ${GIT_DEPLOY_KEY_SECRET_REF}
      # INFISICAL_CLIENT_ID: ${INFISICAL_CLIENT_ID}
      # INFISICAL_CLIENT_SECRET: ${INFISICAL_CLIENT_SECRET}
      # INFISICAL_ENVIRONMENT: ${INFISICAL_ENVIRONMENT}
      # INFISICAL_PROJECT_ID: ${INFISICAL_PROJECT_ID}
      # INFISICAL_SITE_URL: https://app.infisical.com

      # Logging
      # LOG_LEVEL: info
      # LOG_FORMAT: console           # console or json

      # Metrics
      # METRICS_ADDR: ":9090"         # Prometheus metrics endpoint, empty to disable

    ports:
      - "9090:9090" # Prometheus metrics

    volumes:
      - /var/run/docker.sock:/var/run/docker.sock

      # Optional: Custom SSH known_hosts
      # - ./ssh_known_hosts:/etc/ssh/ssh_known_hosts:ro

      # Optional: Mount local SSH key instead of fetching from secrets manager
      # - ~/.ssh/id_rsa:/.ssh/composeflux_id_rsa:ro
```

### Mount SSH Key

If you prefer to mount your SSH key directly instead of storing it in the secrets manager:

1. Leave `SECRETS_PROVIDER` unset (or omit it entirely)
2. Mount your SSH key to the container at `GIT_SSH_KEY_PATH` location (default: `/.ssh/composeflux_id_rsa`)

### Deploy Key Secret Reference

ComposeFlux can fetch your SSH deploy key from the secrets manager during startup, so it can clone private repositories
without mounting a local key.

How `GIT_DEPLOY_KEY_SECRET_REF` works:

- Requires `SECRETS_PROVIDER` to be set
- When set to a value (e.g., `SSH_PRIVATE_KEY` or a Bitwarden secret ID), ComposeFlux fetches that secret from your
  secrets manager
- **Bitwarden**: Uses it as the secret ID to fetch (see
  [Bitwarden Add Secrets](how-to-guides/Bitwarden.md#2-add-secrets))
- **Infisical**: Uses it as the secret key name to fetch
- The fetched content must be your SSH private key
- When left empty (default), skips fetch and uses mounted key at `GIT_SSH_KEY_PATH`

Example:

```yaml
environment:
  GIT_DEPLOY_KEY_SECRET_REF: "" # Disable fetch from secrets manager
  # GIT_SSH_KEY_PATH: /.ssh/composeflux_id_rsa  # Optional: custom path

volumes:
  - ~/.ssh/id_rsa:/.ssh/composeflux_id_rsa:ro
```

**5. Start ComposeFlux:**

```bash
# Default: Run in daemon mode (continuous reconciliation)
docker compose up -d
docker compose logs -f
```

## Verify Deployment

```bash
# Check logs
docker compose logs -f

# List managed stacks (should show containers with composeflux label)
docker ps --filter "label=composeflux.managed=true"

# List all compose projects
docker compose ls
```

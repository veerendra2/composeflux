# Getting Started

Install ComposeFlux on a Docker host and connect it to a Git repository of Compose stacks.

## Prerequisites

- A Linux host with Docker Engine and access to the daemon socket at `/var/run/docker.sock`.
- The Docker Compose plugin on the host that starts ComposeFlux, for the `docker compose` commands in this guide.
  ComposeFlux itself does not require the plugin at runtime.
- A Git repository reachable over SSH that contains the Compose stacks.
- Read access to the repository through an SSH key. The key can be mounted into the container or stored in a supported
  secrets manager.
- Optional: an Age passphrase or Bitwarden/Infisical credentials for secrets.

See [GitHub Deploy Keys](how-to-guides/GithubDeployKeys.md) for creating a read-only key.

## Quick start

The following configuration deploys every stack under `stacks/` in the repository.

1. Create a `.env` file:

    ```bash
    GIT_REPO_URL=git@github.com:user/stacks-repo.git
    STACK_PATH=stacks
    ```

2. Create `compose.yml`:

    ```yaml
    services:
      composeflux:
        image: ghcr.io/veerendra2/composeflux:latest
        container_name: composeflux
        restart: unless-stopped
        environment:
          GIT_REPO_URL: ${GIT_REPO_URL}
          STACK_PATH: ${STACK_PATH}
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock
          - ~/.ssh/composeflux_deploy:/.ssh/composeflux_id_rsa:ro
          - compose-stack:/opt/compose-stack

    volumes:
      compose-stack:
    ```

3. Start ComposeFlux:

    ```bash
    docker compose up -d
    docker compose logs -f
    ```

The mounted key must be readable by the container and must not have a passphrase. The named volume keeps the Git clone
between restarts. If the initial sync fails, the process exits with the error in the log.

The SSH deploy key can also be fetched from a remote secrets manager (Bitwarden or Infisical) by setting
`GIT_DEPLOY_KEY_SECRET_REF`. For Bitwarden the value is the secret ID; for Infisical it is the secret key name. See
[GitHub Deploy Keys](how-to-guides/GithubDeployKeys.md#store-the-key-in-a-secrets-manager).

!!! note

    When `GIT_DEPLOY_KEY_SECRET_REF` is set, remove the read-only key mount from the Compose file. ComposeFlux writes
    the fetched key to `GIT_SSH_KEY_PATH`, and a read-only mount makes that write fail at startup.

## Deploy with Ansible

The [`ansible-role-composeflux-deploy`](https://github.com/veerendra2/ansible-role-composeflux-deploy) role installs the
same deployment on a target host.

```bash
ansible-galaxy role install veerendra2.composeflux_deploy
ansible-galaxy collection install community.docker   # required by the role
```

```yaml
- hosts: servers
  become: true
  roles:
    - role: veerendra2.composeflux_deploy
      vars:
        composeflux_git_repo_url: "git@github.com:user/infra-repo.git"
        composeflux_stack_path: "servers/myhost"

        # Provide either a local key or a secrets manager reference.
        composeflux_ssh_key_local_path: "~/.ssh/composeflux_deploy"
        # composeflux_ssh_key_secret_ref: "SSH_PRIVATE_KEY"

        # Optional secrets.
        composeflux_age_passphrase: "{{ vault_age_passphrase }}"
        # composeflux_infisical_client_id: "{{ vault_infisical_client_id }}"
        # composeflux_infisical_client_secret: "{{ vault_infisical_client_secret }}"
        # composeflux_infisical_environment: "prod"
        # composeflux_infisical_project_id: "project-id"
```

```bash
ansible-playbook playbook.yml
ansible-playbook playbook.yml --tags sync   # force a reconciliation now
```

Role variables map to the environment variables below. See the
[role defaults](https://github.com/veerendra2/ansible-role-composeflux-deploy/blob/main/defaults/main.yml) for the full
list.

## Options

Every option has an environment variable and a command-line flag. Flags apply to both `run` and `sync`.

### Core

| Environment variable        | Flag                      | Default     | Description                                                                                                                                |
| --------------------------- | ------------------------- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `GIT_REPO_URL`              | `--repo-url`              | required    | SSH URL of the Git repository.                                                                                                             |
| `STACK_PATH`                | `--stack-path`            | required    | Directory in the repository that contains the stacks.                                                                                      |
| `CONFIG_FILE`               | `--config-file`           | `stack.yml` | Path of the shared stack configuration file, relative to `STACK_PATH`.                                                                     |
| `GIT_INTERVAL`              | `--git-interval`          | `5m`        | Git polling interval. Must be greater than zero.                                                                                           |
| `HEALTH_RECONCILE_INTERVAL` | `--health-interval`       | `0`         | Health reconciliation interval. `0` disables it.                                                                                           |
| `IMAGE_UPDATE_SCHEDULE`     | `--image-update-schedule` | empty       | Cron expression for image update checks. Empty disables it.                                                                                |
| `PRUNE_INTERVAL`            | `--prune-interval`        | `24h`       | Docker resource prune interval. `0` disables it. See [Resource pruning](Overview.md#resource-pruning) before enabling it on a shared host. |

### Git

| Environment variable        | Flag                      | Default                    | Description                                                                           |
| --------------------------- | ------------------------- | -------------------------- | ------------------------------------------------------------------------------------- |
| `GIT_BRANCH`                | `--branch`                | `main`                     | Branch to track.                                                                      |
| `GIT_SSH_KEY_PATH`          | `--ssh-key-path`          | `/.ssh/composeflux_id_rsa` | SSH private key path inside the container.                                            |
| `GIT_DEPLOY_KEY_SECRET_REF` | `--deploy-key-secret-ref` | empty                      | Secret name or ID in the configured secrets manager. Requires Bitwarden or Infisical. |
| `GIT_CLONE_PATH`            | `--clone-path`            | `/opt/compose-stack`       | Local clone directory.                                                                |

### Offline secrets

| Environment variable | Flag               | Default | Description                                                   |
| -------------------- | ------------------ | ------- | ------------------------------------------------------------- |
| `AGE_PASSPHRASE`     | `--age-passphrase` | empty   | Passphrase for `*.age` files. Empty disables offline secrets. |

### Bitwarden

| Environment variable        | Flag                          | Default                                | Description                   |
| --------------------------- | ----------------------------- | -------------------------------------- | ----------------------------- |
| `BITWARDEN_ACCESS_TOKEN`    | `--bitwarden-access-token`    | required with Bitwarden                | Machine account access token. |
| `BITWARDEN_ORGANIZATION_ID` | `--bitwarden-organization-id` | required with Bitwarden                | Organization ID.              |
| `BITWARDEN_PROJECT_ID`      | `--bitwarden-project-id`      | required with Bitwarden                | Project ID.                   |
| `BITWARDEN_API_URL`         | `--bitwarden-api-url`         | `https://vault.bitwarden.com/api`      | API URL.                      |
| `BITWARDEN_IDENTITY_URL`    | `--bitwarden-identity-url`    | `https://vault.bitwarden.com/identity` | Identity URL.                 |

### Infisical

| Environment variable      | Flag                        | Default                     | Description                                                                        |
| ------------------------- | --------------------------- | --------------------------- | ---------------------------------------------------------------------------------- |
| `INFISICAL_CLIENT_ID`     | `--infisical-client-id`     | required with Infisical     | Universal Auth client ID.                                                          |
| `INFISICAL_CLIENT_SECRET` | `--infisical-client-secret` | required with Infisical     | Universal Auth client secret.                                                      |
| `INFISICAL_ENVIRONMENT`   | `--infisical-environment`   | required with Infisical     | Environment slug, for example `prod`.                                              |
| `INFISICAL_PROJECT_ID`    | `--infisical-project-id`    | required with Infisical     | Project ID.                                                                        |
| `INFISICAL_SECRET_PATH`   | `--infisical-secret-path`   | `/`                         | Secret path. Comma-separated paths are allowed; later paths override earlier ones. |
| `INFISICAL_SITE_URL`      | `--infisical-site-url`      | `https://app.infisical.com` | Site URL.                                                                          |

### Docker and logging

| Environment variable | Flag               | Default   | Description                                                       |
| -------------------- | ------------------ | --------- | ----------------------------------------------------------------- |
| `REMOVE_ORPHANS`     | `--remove-orphans` | `true`    | Remove containers that are not defined in the Compose project.    |
| `LOG_LEVEL`          | `--log-level`      | `INFO`    | `DEBUG`, `INFO`, `WARN`, or `ERROR`. Values are case-insensitive. |
| `LOG_FORMAT`         | `--log-format`     | `console` | `console` or `json`.                                              |
| `LOG_ADD_SOURCE`     | `--log-add-source` | `false`   | Include the source file and line in log records.                  |

### Commands

| Command            | Description                                                              |
| ------------------ | ------------------------------------------------------------------------ |
| `composeflux run`  | Daemon mode. Polls Git and reconciles continuously. This is the default. |
| `composeflux sync` | Performs one forced reconciliation, then exits.                          |

To run a one-shot sync against a running container:

```bash
docker exec composeflux /composeflux sync
```

ComposeFlux validates the configuration before starting. `GIT_INTERVAL` must be greater than zero, health and prune
intervals must not be negative, and a non-empty `IMAGE_UPDATE_SCHEDULE` must be a valid cron expression. Invalid values
stop startup.

## Full Compose example

The example below lists every option. Uncomment only the options in use.

```yaml
services:
  composeflux:
    image: ghcr.io/veerendra2/composeflux:latest
    container_name: composeflux
    restart: unless-stopped
    environment:
      # Required
      GIT_REPO_URL: ${GIT_REPO_URL}
      STACK_PATH: ${STACK_PATH}

      # Git
      # GIT_BRANCH: main
      # GIT_INTERVAL: 5m
      # GIT_CLONE_PATH: /opt/compose-stack
      # GIT_SSH_KEY_PATH: /.ssh/composeflux_id_rsa
      # GIT_DEPLOY_KEY_SECRET_REF: ""

      # Stack configuration
      # CONFIG_FILE: stack.yml

      # Offline secrets
      # AGE_PASSPHRASE: ${AGE_PASSPHRASE}

      # Bitwarden Secrets Manager
      # BITWARDEN_ACCESS_TOKEN: ${BITWARDEN_ACCESS_TOKEN}
      # BITWARDEN_ORGANIZATION_ID: ${BITWARDEN_ORGANIZATION_ID}
      # BITWARDEN_PROJECT_ID: ${BITWARDEN_PROJECT_ID}
      # BITWARDEN_API_URL: https://vault.bitwarden.com/api
      # BITWARDEN_IDENTITY_URL: https://vault.bitwarden.com/identity

      # Infisical Secrets Manager
      # INFISICAL_CLIENT_ID: ${INFISICAL_CLIENT_ID}
      # INFISICAL_CLIENT_SECRET: ${INFISICAL_CLIENT_SECRET}
      # INFISICAL_ENVIRONMENT: prod
      # INFISICAL_PROJECT_ID: ${INFISICAL_PROJECT_ID}
      # INFISICAL_SECRET_PATH: /
      # INFISICAL_SITE_URL: https://app.infisical.com

      # Reconciliation and pruning
      # HEALTH_RECONCILE_INTERVAL: 5m
      # IMAGE_UPDATE_SCHEDULE: "0 3 * * *"
      # PRUNE_INTERVAL: 24h

      # Docker
      # REMOVE_ORPHANS: "true"

      # Logging
      # LOG_LEVEL: INFO
      # LOG_FORMAT: console
      # LOG_ADD_SOURCE: "false"

    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      # Local key only. Omit when GIT_DEPLOY_KEY_SECRET_REF is set.
      - ~/.ssh/composeflux_deploy:/.ssh/composeflux_id_rsa:ro
      - compose-stack:/opt/compose-stack

volumes:
  compose-stack:
```

## Verify

```bash
# Follow logs. The initial sync reports deployed stacks and any failures.
docker compose logs -f

# List managed containers.
docker ps --filter "label=composeflux.managed=true"

# List Compose projects known to Docker.
docker compose ls
```

A successful sync logs a `Stack deployment summary` with counts for deployed, failed, and skipped stacks.

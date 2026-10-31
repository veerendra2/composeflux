# Overview

ComposeFlux is a GitOps continuous deployment tool for Docker Compose, reconciles docker stacks from Git to host

## Introduction

ComposeFlux reconciles docker stacks under specific path (`STACK_PATH`) in a Git repository using deploy key as
authentication. As ComposeFlux is standalone and there are no central management or UI or DB, if you want to manage some
stacks on a host, you deploy ComposeFlux in that host and configure a path (`STACK_PATH`), to tell ComposeFlux deploy
stacks from that path.

## Structure

You organize your docker compose stacks in organized folders (Ideally per server) with `STACK_PATH`, lets look how to
organize

### Stack Path

Stack path (`STACK_PATH`) configuration tells ComposeFlux, where to look for stacks, the stacks has to be flat,
ComposeFlux continues looks for drifts in the directory and reconcile stacks on the path.

Below image illustrate, multiple standalone ComposeFlux runs on servers pointing same Git repostory with difference
`STACK_PATH`

![Stack Path Example](assets/composeflux-overview.png)

### Stacks

A stack is docker compose directory where you put docker compose file and its related configs(or age vault), for example
below

```
stacks/                  STACK_PATH
├── stack.yml            optional shared configuration
├── traefik/
│   └── compose.yml      optional stack secrets
├── nextcloud/
│   └── compose.yml
└── jellyfin/
    └── docker-compose.yml
```

During reconciliation, ComposeFlux lists directories in the `STACK_PATH` and then looks for looks for `compose.yaml`,
`compose.yml`, `docker-compose.yml`, or `docker-compose.yaml`, plus a matching override file. Directories without a
Compose file are ignored. Discovery is one level deep; nested directories are not scanned unless compose file
[modularized or contains include](https://docs.docker.com/reference/compose-file/include/) of other compose file
different location in the repo

Caveat: Dont set different `name:` in docker compose file other than directory name. ComposeFlux assumes, the directory
name is the docker compose project name

## Stack configuration

An optional `stack.yml` in `STACK_PATH` controls deployment order and defines environment variables shared by all
stacks.

```yaml
startup_order:
  - traefik

envs:
  DOMAIN: homeserver.local
  TZ: America/New_York
  ENVIRONMENT: production
```

- `startup_order` lists stacks that must deploy first. Other stacks deploy after them in no guaranteed order.
- `envs` are available for Compose interpolation in every stack.
- Names in `startup_order` must match directory names. Unknown names are reported in the log and skipped.
- A missing `stack.yml` is allowed. A malformed or unreadable file stops the current reconciliation pass.

## Background Operations

### Health Reconciliation

Set `HEALTH_RECONCILE_INTERVAL` to enable periodic health checks (disabled by default). ComposeFlux redeploys any
managed stack with an unhealthy container. It does not pull Git for this recovery.

A stack is healthy when one of these is true for a container in the stack:

- It is running and its health check is not `unhealthy`.
- It exited with code `0` and has the `composeflux.init: "true"` label.

Containers in `restarting`, `paused`, or `dead` state are unhealthy. A stack is unhealthy when any of its containers is
unhealthy. After three consecutive failed deploys, ComposeFlux stops trying the stack until the next successful Git sync
or image update.

### Automatic image updates

Set `IMAGE_UPDATE_SCHEDULE` to a cron expression to check for new images. ComposeFlux compares the local and remote
image digests for each stack. When a newer digest is available, it pulls the images and redeploys the stack. Images
built locally or pinned by digest are skipped.

Add `composeflux.image-update.exclude: "true"` to any service to exclude the stack. If a scheduled check is still
running, the next invocation is skipped.

### Resource pruning

`PRUNE_INTERVAL` defaults to `24h`. Set it to `0` to disable pruning.

!!! warning Pruning applies to the whole Docker host, not only to ComposeFlux stacks. It removes all unused volumes, all
dangling images, and all build cache. Containers and networks are not pruned. Set `PRUNE_INTERVAL=0` if other workloads
on the host keep unused volumes that must be retained.

Pruning runs only when every stack under `STACK_PATH` is healthy, and not suspended. Otherwise the cycle is skipped and
a warning is logged.

## Secrets

ComposeFlux supports two secret sources. They can be used together.

### Offline Secrets

> At the moment, ComposeFlux supports [age](https://github.com/FiloSottile/age) with passphrase only. It can
> automatically picks .age extention file and decrypts with AGE_PASSPHRASE (--age-passphrase) option

ComposeFlux can exports secrets from env vault file while bring up the docker compose stacks, you can set environmental
vars in docker compose config to pick up.

- If the .age file is inside of the stack directory, the secrets in the vault file belongs to that stack
- If the .age file is in root of the `STACK_PATH`, it belongs to all stacks in that stack path i.e. shared secrets

![Offline Vaults](./assets/offline-secrets.png)

For example, create

```bash
cat .env_secrets
TOKEN=my-token

# Encrypt
age -p -o .env.secrets.age .env_secrets

cat .env.secrets.age
age-encryption.org/v1
-> scrypt zZIcf60TjTWPvmc9fxJyXQ 18
hbPYsea19K1Wteq4Ysa1DYzP76dfldoMhKahFzxaHc8
--- jPSiPnOjUVYs6LRKpzHFFdavh17X3/ZCv4bsnR0kK6c
�����^�����8��q�#�n��&��5�o�׋�L9�\/k&�ϐ��

rm .env_secrets

tree -a servers/wg1-ams/wg-easy/
servers/wg1-ams/wg-easy/
├── .env.secrets.age
├── compose.override.yml
└── compose.yml

1 directory, 3 files
```

Add env in docker compose file, to docker compose interporate the env

```yaml
name: wg-easy

services:
  wg-easy:
    image: ghcr.io/wg-easy/wg-easy:latest
    environment:
      TOKEN: ${TOKEN} # <----- ENV from the .env.secrets.age, decrypted and exports while bring up the stack
...
```

See [Offline Secrets with Age](how-to-guides/AgeSecrets.md).

### Remote secrets

Configure Bitwarden Secrets Manager or Infisical. Only one remote provider can be configured at a time. Values are
fetched whenever ComposeFlux reconciles a stack. After changing a secret in the provider, run `composeflux sync` to
apply it.

![ComposeFlux Remote Secrets Manager](./assets/composeflux-secrets-manager.png)

ComposeFlux does not inject every secret into every container. A value reaches a container only when the Compose service
references it:

```yaml
services:
  app:
    image: myapp:latest
    environment:
      DATABASE_PASSWORD: ${DATABASE_PASSWORD}
```

- [Bitwarden Secrets Manager (How-to Guide)](how-to-guides/Bitwarden.md)
- [Infisical Secrets Manager (How-to Guide)](how-to-guides/Infisical.md)

## Git checkout ownership

!!! warning

    Git is the source of truth for the checkout at `GIT_CLONE_PATH`. Before startup checkout and before each sync, ComposeFlux discards local changes to tracked files and logs the affected paths. This includes changes written by containers through bind mounts. Untracked files are preserved, but a file required by the target revision can be replaced. Keep mutable application data outside tracked files.

## Labels

ComposeFlux adds `composeflux.managed`, `composeflux.version`, and `composeflux.deployed-at` labels to every service
during deployment. Use the managed label to list stacks:

```bash
docker ps --filter "label=composeflux.managed=true"
```

Along with labels set by ComposeFlux, you can also set labels to docker compose stacks to controll reconcilation

### Suspend

`composeflux.health.suspend`

When you set this label to any service in docker compose file, ComposeFlux ignores reconcilation i.e. no Git deployment,
health recovery, or image updates, and resource pruning is skipped.

Example

```yaml
---
name: actualbudget
services:
  actualbudget:
    image: docker.io/actualbudget/actual-server:latest
    container_name: actualbudget
    restart: unless-stopped
    labels:
      - composeflux.health.suspend=true
    hostname: actualbudget
```

### Init

`composeflux.init`

When you enable [Health reconciliation](#health-reconciliation), and if you have
[one short container](https://docs.docker.com/compose/how-tos/init-containers) in docker compose stack, to make
ComposeFlux aware of this one-short container and prevent thinks container is down, you can add this label

Example

```yaml
---
name: valkey
services:
  init-change-ownership: #<---- One short container
    image: busybox:latest
    labels:
      - composeflux.init=true #<----- Make Health checks success, even this container exits (with exit code 0)
    volumes:
      - valkey_data:/tmp/change-ownership
    environment:
      PUID: ${PUID}
      PGID: ${PGID}
    command: chown -R ${PUID}:${PGID} /tmp/change-ownership

  valkey:
    image: valkey/valkey:8-alpine
    container_name: valkey
    depends_on:
      init-change-ownership:
        condition: service_completed_successfully
    volumes:
      - valkey_data:/data
    user: ${PUID}:${PGID}
    hostname: valkey
volumes:
  valkey_data:
    name: valkey_data
```

### Exclude Image Update

`composeflux.image-update.exclude`

You can exclude, image updates using this label

Example

```yaml
---
name: actualbudget
services:
  actualbudget:
    image: docker.io/actualbudget/actual-server:latest
    container_name: actualbudget
    restart: unless-stopped
    labels:
      - composeflux.image-update.exclude=true
    hostname: actualbudget
```

## Limitations

- Stack discovery is one level deep.
- No central controller or multi-server orchestration.
- No rolling updates or zero-downtime deployments.
- No built-in monitoring or alerting.

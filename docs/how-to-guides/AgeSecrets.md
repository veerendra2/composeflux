# Age Encrypted Secrets Setup

Set up offline encrypted secrets management for ComposeFlux using [age](https://github.com/FiloSottile/age).

ComposeFlux natively supports decrypting `*.age` encrypted dotenv files directly in your Git repository at runtime without writing plaintext secrets to disk or calling external third-party secret manager APIs.

## Overview

- Encrypted `.env.age` (or any `*.age`) files are committed directly to your Git repository.
- Secrets are decrypted in memory using a passphrase (`--age-passphrase` or `AGE_PASSPHRASE`) and injected directly into container environments.
- When `*.age` files are updated in Git, ComposeFlux detects the change and triggers an automatic redeploy.

## Secret Hierarchy & Layering

ComposeFlux supports layered secrets:

1. **Shared Root Secrets**: Place `*.age` files at the root of your `STACK_PATH` directory (e.g. `stacks/shared.env.age`). These variables are injected into **all** stacks.
2. **Stack-Specific Secrets**: Place `*.age` files in the stack directory next to `compose.yml` (e.g. `stacks/nextcloud/secrets.env.age`). These apply only to that stack and override matching keys from shared root secrets.
3. **Included Subdirectory Secrets**: If a compose file uses `include` directives targeting other directories, any `*.age` files located in those included directories are also loaded and mapped as dependencies.

### Directory Structure Example

```
your-stacks-repo/
└── stacks/                       ← STACK_PATH
    ├── stack.yml
    ├── shared.env.age            ← Applied to all stacks
    ├── traefik/
    │   ├── compose.yml
    │   └── traefik.env.age       ← Applied to traefik stack only
    └── nextcloud/
        ├── compose.yml
        └── secrets.env.age       ← Applied to nextcloud stack only
```

## How to Encrypt Secret Files

### 1. Install `age` CLI

Install the `age` CLI tool on your local machine:

```bash
# macOS (Homebrew)
brew install age

# Linux (Debian/Ubuntu)
apt install age

# Arch Linux
pacman -S age

# Go
go install filippo.io/age/cmd/...@latest
```

### 2. Create Plaintext Dotenv File

Create your secret environment file (e.g., `.env.secret`):

```bash
DATABASE_PASSWORD=supersecretpassword
API_KEY=1234567890abcdef
JWT_SECRET=supersecretjwtkey
```

### 3. Encrypt with Passphrase

Encrypt the file with a passphrase using `age -p`. We recommend ASCII armor (`-a`) so the ciphertext is stored in text format:

```bash
# Encrypt with armor (-a) and passphrase (-p)
age -p -a -o secrets.env.age .env.secret
```

Enter your secure passphrase when prompted.

Delete the unencrypted plaintext file after encryption:
```bash
rm .env.secret
```

### 4. Commit Encrypted File to Git

Commit the `*.age` file to your Git repository:

```bash
git add secrets.env.age
git commit -m "Add encrypted secrets for nextcloud"
git push
```

## Configuring ComposeFlux

Provide the passphrase to ComposeFlux via the `AGE_PASSPHRASE` environment variable or `--age-passphrase` flag.

### Compose Example

```yaml
services:
  composeflux:
    image: ghcr.io/veerendra2/composeflux:latest
    container_name: composeflux
    restart: unless-stopped
    environment:
      GIT_REPO_URL: git@github.com:user/stacks-repo.git
      STACK_PATH: stacks
      AGE_PASSPHRASE: "your-secure-passphrase"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ~/.ssh/id_rsa:/.ssh/composeflux_id_rsa:ro
```

## Usage in Compose Stacks

Decrypted keys from `*.age` files are automatically injected into each container's environment in the stack without needing manual `environment:` entries in your compose file.

```yaml
services:
  app:
    image: myapp:latest
    # All decrypted secrets from *.age files are injected into this container environment automatically.
```

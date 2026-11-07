# GitHub Deploy Keys

A deploy key gives ComposeFlux read-only SSH access to the stacks repository. The same approach works with other Git
providers.

## Generate a key pair

```bash
ssh-keygen -t ed25519 -C "composeflux-deploy-key" -f ~/.ssh/composeflux_deploy
```

This creates the private key `~/.ssh/composeflux_deploy` and the public key `~/.ssh/composeflux_deploy.pub`. Do not
set a passphrase; ComposeFlux runs unattended.

## Add the public key to GitHub

1. Copy the public key:

    ```bash
    cat ~/.ssh/composeflux_deploy.pub
    ```

2. Open the repository on GitHub.
3. Go to **Settings**, then **Deploy keys**, and click **Add deploy key**.
4. Paste the key and leave write access disabled.

![GitHub deploy key](../assets/github-deploy-key-config.png)

## Provide the private key to ComposeFlux

Use one of the following options.

### Mount the key

Mount the private key at `GIT_SSH_KEY_PATH`. The default path is `/.ssh/composeflux_id_rsa`.

```yaml
services:
  composeflux:
    image: ghcr.io/veerendra2/composeflux:latest
    environment:
      GIT_REPO_URL: git@github.com:user/repo.git
      STACK_PATH: stacks
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ~/.ssh/composeflux_deploy:/.ssh/composeflux_id_rsa:ro
      - compose-stack:/opt/compose-stack

volumes:
  compose-stack:
```

### Store the key in a secrets manager

This option requires Bitwarden or Infisical credentials. Set `GIT_DEPLOY_KEY_SECRET_REF` to the secret reference:

- **Bitwarden**: the secret ID.
- **Infisical**: the secret key name, for example `SSH_PRIVATE_KEY`.

At startup, ComposeFlux fetches the value and writes it to `GIT_SSH_KEY_PATH` with mode `0600`. When the reference is
empty, ComposeFlux uses the key already present at that path.

Do not also mount the key at `GIT_SSH_KEY_PATH`: a read-only mount makes the write fail at startup.

```yaml
environment:
  GIT_REPO_URL: git@github.com:user/repo.git
  STACK_PATH: stacks
  GIT_DEPLOY_KEY_SECRET_REF: SSH_PRIVATE_KEY
  INFISICAL_CLIENT_ID: ${INFISICAL_CLIENT_ID}
  INFISICAL_CLIENT_SECRET: ${INFISICAL_CLIENT_SECRET}
  INFISICAL_ENVIRONMENT: prod
  INFISICAL_PROJECT_ID: ${INFISICAL_PROJECT_ID}
```

See [Bitwarden](Bitwarden.md#2-add-secrets) or [Infisical](Infisical.md#3-add-secrets) for adding the secret.

## Trust a self-hosted Git host

The image trusts `github.com` and `gitlab.com` because their host keys are added to
`/etc/ssh/ssh_known_hosts` at build time. Any other SSH host, such as a self-hosted Gitea, Forgejo, or GitLab, or a server
on a non-default port, must be added. Without an entry, the clone fails with `knownhosts: key is unknown`.

Generate the file on the Docker host, because the distroless image does not include `ssh-keyscan`:

```bash
ssh-keyscan git.example.com >> ~/.ssh/composeflux_known_hosts
```

Do not use `-t` to limit the key type. An entry with only one host key type can fail with `knownhosts: key mismatch`,
because go-git negotiates a different type. For a non-default SSH port, scan with `-p 2222` and clone with
`ssh://git@git.example.com:2222/group/repo.git`.

Give the file to the container in one of two ways.

### Mount over the default path

```yaml
volumes:
  - ~/.ssh/composeflux_known_hosts:/etc/ssh/ssh_known_hosts:ro
```

This replaces the built-in file, so add `github.com` and `gitlab.com` entries too if those repositories are used.

### Point SSH_KNOWN_HOSTS at the file

```yaml
environment:
  SSH_KNOWN_HOSTS: /config/known_hosts
volumes:
  - ~/.ssh/composeflux_known_hosts:/config/known_hosts:ro
```

When `SSH_KNOWN_HOSTS` is set, go-git uses only that file and ignores `/etc/ssh/ssh_known_hosts`. The file must
therefore contain every host you connect to, including the well-known providers.

Verify the host fingerprint out of band before trusting a scanned key.

## Test access

```bash
ssh -i ~/.ssh/composeflux_deploy -T git@github.com
```

A successful test prints a message that the key authenticated but does not provide shell access.

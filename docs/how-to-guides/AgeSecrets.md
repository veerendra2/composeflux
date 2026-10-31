# Offline Secrets with Age

ComposeFlux can decrypt [age](https://github.com/FiloSottile/age) encrypted files stored in the Git repository and use
their values for Compose interpolation. Decryption happens in memory; plaintext is not written to disk.

## How it works

- ComposeFlux reads `*.age` files directly from the stack root, each stack directory, and directories pulled in through
  `include` or `extends`. Subdirectories are not scanned recursively.
- Each file must be a dotenv file encrypted with a passphrase (`age -p`). ASCII-armored and binary files are both
  supported.
- Values are merged in this order: stack root, stack directory, included directory. A later value overrides an earlier
  one. Files in the same directory are merged in filename order.
- Offline secrets also override remote secrets. A stack-directory value takes precedence over a stack-root value, which
  takes precedence over a value fetched from Bitwarden or Infisical.
- A `*.age` file must be a regular file. Symbolic links are rejected.
- Values are available only for Compose interpolation. A container receives a value only when its service references it,
  for example `DATABASE_PASSWORD: ${DATABASE_PASSWORD}`.
- Committing a change to a `*.age` file triggers a redeploy of the affected stacks.

## Directory layout

```
stacks/                     STACK_PATH
├── stack.yml
├── shared.env.age          applies to every stack
├── traefik/
│   ├── compose.yml
│   └── traefik.env.age     applies to traefik only
└── nextcloud/
    ├── compose.yml
    └── secrets.env.age     applies to nextcloud only
```

## Encrypt a file

1. Install the age CLI:

    ```bash
    # Debian or Ubuntu
    apt install age

    # macOS
    brew install age
    ```

2. Create a dotenv file with the values to encrypt:

    ```bash
    DATABASE_PASSWORD=supersecretpassword
    API_KEY=1234567890abcdef
    ```

3. Encrypt the file with a passphrase. The `-a` option stores the ciphertext as text:

    ```bash
    age -p -a -o stacks/nextcloud/secrets.env.age secrets.env
    ```

4. Remove the plaintext file:

    ```bash
    rm secrets.env
    ```

5. Commit and push the encrypted file:

    ```bash
    git add stacks/nextcloud/secrets.env.age
    git commit -m "Add nextcloud secrets"
    git push
    ```

## Configure ComposeFlux

Set `AGE_PASSPHRASE` (or the `--age-passphrase` flag) to the passphrase used during encryption. When it is empty,
offline secrets are disabled and no `*.age` files are read.

```yaml
services:
  composeflux:
    image: ghcr.io/veerendra2/composeflux:latest
    environment:
      GIT_REPO_URL: git@github.com:user/stacks-repo.git
      STACK_PATH: stacks
      AGE_PASSPHRASE: ${AGE_PASSPHRASE}
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ~/.ssh/composeflux_deploy:/.ssh/composeflux_id_rsa:ro
      - compose-stack:/opt/compose-stack

volumes:
  compose-stack:
```

Keep the passphrase in a protected environment variable or a local `.env` file. Do not commit it to the Compose file or
the stacks repository.

## Use the values

Reference the keys in a stack's Compose file:

```yaml
services:
  app:
    image: myapp:latest
    environment:
      DATABASE_PASSWORD: ${DATABASE_PASSWORD}
      API_KEY: ${API_KEY}
```

Keys that are not referenced are not passed to containers.

## Edit an encrypted file

The [`agedit`](https://gist.github.com/veerendra2/35c0b81980e81fcb11a86e6cde2f9508) helper decrypts a file into a
temporary file, opens it in `vim`, and re-encrypts it only when the content changed.

```bash
mkdir -p ~/.local/bin
curl -fsSL https://gist.githubusercontent.com/veerendra2/35c0b81980e81fcb11a86e6cde2f9508/raw -o ~/.local/bin/agedit
chmod +x ~/.local/bin/agedit

agedit stacks/nextcloud/secrets.env.age
```

The script requires `age` and `vim`. It writes binary age files, which ComposeFlux supports.

## Troubleshooting

| Symptom                                       | Cause                                                                                                                 |
| --------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `failed to decrypt age file`                  | Wrong `AGE_PASSPHRASE`, or the file was encrypted to an age identity instead of a passphrase.                         |
| `age secret file must not be a symbolic link` | Replace the link with the encrypted file itself.                                                                      |
| A file is not loaded                          | The file is nested in a subdirectory, is not located next to a Compose file or stack root, or does not end in `.age`. |
| A variable is empty in a container            | The service does not reference the variable in its Compose configuration.                                             |

A decryption failure stops the current reconciliation pass. Fix the passphrase or the file, then wait for the next sync
or run `composeflux sync`.

<p align="center">
  <img src="./assets/composeflux-banner.png" alt="ComposeFlux" width="400">
</p>

<p align="center">
  <strong>A lightweight GitOps continuous deployment tool for Docker Compose.</strong>
</p>

<p align="center">
  <a href="https://github.com/veerendra2/composeflux">GitHub Repository</a>
</p>

ComposeFlux automates Docker Compose deployments using GitOps principles. It watches a Git repository, detects changes,
and deploys the affected stacks without manual intervention.

## Features

| Feature | Description |
| --- | --- |
| GitOps driven | Deployments follow the Git repository |
| Change detection | Only stacks affected by a commit are redeployed |
| Lightweight and headless | Single container, no UI, no database, no control plane |
| Pure Go implementation | Native Docker Compose SDK, no shell execution |
| Offline secrets | `*.age` files in the repository are decrypted in memory |
| Remote secrets | Optional Bitwarden Secrets Manager and Infisical support |
| Automatic image updates | Scheduled registry checks redeploy stacks when newer images are available |
| Flexible configuration | Deployment order and shared environment variables |

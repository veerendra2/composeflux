<p align="center">
  <img src="./docs/assets/composeflux-banner.png" alt="ComposeFlux" width="400">
</p>

<p align="center">
  <strong>A lightweight GitOps continuous deployment tool for Docker Compose.</strong>
</p>

<p align="center">
  <a href="https://veerendra2.github.io/composeflux/Overvie">Overview</a> •
  <a href="https://veerendra2.github.io/composeflux/GettingStarted/">Getting Started</a> •
  <a href="https://github.com/veerendra2/composeflux/releases">Releases</a> •
  <a href="Architecture Diagram">Releases</a>
</p>

<p align="center">
<img src="https://img.shields.io/github/go-mod/go-version/veerendra2/composeflux?style=flat&logo=go&logoColor=white" alt="Go">
<img src="https://img.shields.io/github/license/veerendra2/composeflux?style=flat" alt="License">
<img src="https://img.shields.io/github/stars/veerendra2/composeflux?style=flat&logo=github" alt="Stars">
<img src="https://img.shields.io/github/forks/veerendra2/composeflux?style=flat&logo=github" alt="Forks">
<a href="https://github.com/veerendra2/composeflux/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/veerendra2/composeflux/ci.yml?style=flat&logo=githubactions&logoColor=white&label=CI" alt="CI"></a>
<a href="https://github.com/veerendra2/composeflux/releases/latest"><img src="https://img.shields.io/github/v/release/veerendra2/composeflux?style=flat&logo=github" alt="Release"></a>
<a href="https://ghcr.io/veerendra2/composeflux"><img src="https://img.shields.io/badge/ghcr.io-amd64%20%7C%20arm64-blue?style=flat&logo=docker&logoColor=white" alt="Docker"></a>
<img src="https://img.shields.io/badge/Image%20Size-~50%20MB-blue?style=flat&logo=docker&logoColor=white" alt="Docker image size">
</p>

> **Currently in beta.** Expect heavy refactoring and breaking changes as the project evolves toward v1.0. See
> [open v1.x.x issues](https://github.com/veerendra2/composeflux/issues?q=is%3Aissue%20state%3Aopen%20label%3Av1.x.x)
> for tracking progress toward the first stable release.

ComposeFlux automates Docker Compose deployments using GitOps principles. It watches a Git repository, detects changes,
and deploys the affected Docker stacks without manual intervention.

---

## Features

| Feature                  | Description                                                                                   |
| ------------------------ | --------------------------------------------------------------------------------------------- |
| GitOps driven            | Deployments follow the Git repository                                                         |
| Change detection         | Only stacks affected by a commit are redeployed                                               |
| Lightweight and headless | Single container, no UI, no database, no control plane                                        |
| Pure Go implementation   | Native Docker [Compose SDK](https://docs.docker.com/compose/compose-sdk/), no shell execution |
| Offline secrets          | `*.age` files in the repository are decrypted in memory                                       |
| Remote secrets           | Optional Bitwarden Secrets Manager and Infisical support                                      |
| Automatic image updates  | Scheduled registry checks redeploy stacks when newer images are available                     |
| Flexible configuration   | Deployment order and shared environment variables                                             |

## Blog Posts

- [GitOps for Homeservers (Part 1) - My Homeservers, Ansible, and the Pain Points](https://veerendra2.github.io/gitops-for-homeservers-part1)
- [GitOps for Homeservers (Part 2) — Searching for the Right Tool](https://veerendra2.github.io/gitops-for-homeservers-part2)
- [GitOps for Homeservers (Part 3) — ComposeFlux: A Lightweight GitOps Tool](https://veerendra2.github.io/gitops-for-homeservers-part3)
- [How I Manage My Homeservers with GitOps and Docker Compose](https://medium.com/p/1da41b3680a4) (Medium)

## Contributing

See the [Development Guide](https://veerendra2.github.io/composeflux/Development/) for build instructions and project
layout.

## License

MIT. See [LICENSE](LICENSE).

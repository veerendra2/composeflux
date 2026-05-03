# ComposeFlux

> :warning: _Currently in beta_

_A GitOps continuous deployment tool for Docker Compose_

![ComposeFlux](./docs/assets/composeflux-banner.png)

<p align="center">
<img src="https://img.shields.io/github/go-mod/go-version/veerendra2/composeflux?style=flat&logo=go&logoColor=white" alt="Go">
<img src="https://img.shields.io/github/license/veerendra2/composeflux?style=flat" alt="License">
<img src="https://img.shields.io/github/stars/veerendra2/composeflux?style=flat&logo=github" alt="Stars">
<img src="https://img.shields.io/github/forks/veerendra2/composeflux?style=flat&logo=github" alt="Forks">
<a href="https://github.com/veerendra2/composeflux/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/veerendra2/composeflux/ci.yml?style=flat&logo=githubactions&logoColor=white&label=CI" alt="CI"></a>
<a href="https://github.com/veerendra2/composeflux/releases/latest"><img src="https://img.shields.io/github/v/release/veerendra2/composeflux?style=flat&logo=github" alt="Release"></a>
<a href="https://ghcr.io/veerendra2/composeflux"><img src="https://img.shields.io/badge/ghcr.io-amd64%20%7C%20arm64-blue?style=flat&logo=docker&logoColor=white" alt="Docker"></a>
<a href="https://veerendra2.github.io/composeflux/"><img src="https://img.shields.io/badge/Documentation-blue?style=flat&logo=readthedocs&logoColor=white" alt="Docs"></a>
</p>

ComposeFlux automates Docker Compose deployments using GitOps principles. Monitor your Git repository, detect changes,
and automatically deploy your Docker stacks—all without manual intervention.

---

## Features

| Feature | Description |
| ------- | ----------- |
| GitOps Driven | Automatic deployment from Git repository |
| Smart Change Detection | Hash-based detection deploys only changed stacks |
| Pure Go Implementation | Native Docker [Compose SDK](https://docs.docker.com/compose/compose-sdk/) without shell execution |
| Secrets Management | Optional Bitwarden Secrets Manager and Infisical support |
| Automatic Image Updates | Scheduled registry checks redeploy stacks when newer images are available |
| Flexible Configuration | Startup order and shared environment variables |
| Prometheus Metrics | Built-in metrics endpoint for deployment, image update, and prune observability |
| Simple & Headless | No UI, no backend database |

## Blog Posts

- [GitOps for Homeservers (Part 1) — My Homeservers, Ansible, and the Pain Points](https://veerendra2.github.io/gitops-for-homeservers-part1)
- [GitOps for Homeservers (Part 2) — Searching for the Right Tool](https://veerendra2.github.io/gitops-for-homeservers-part2)
- [GitOps for Homeservers (Part 3) — ComposeFlux: A Lightweight GitOps Tool](https://veerendra2.github.io/gitops-for-homeservers-part3)
- [How I Manage My Homeservers with GitOps and Docker Compose](https://medium.com/p/1da41b3680a4) (Medium)

## Contributing

Contributions are welcome! Please see our [Development Guide](https://veerendra2.github.io/composeflux/Development/) for
details.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

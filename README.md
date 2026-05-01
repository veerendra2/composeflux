# ComposeFlux

> :warning: _Currently in beta_

_A GitOps continuous deployment tool for Docker Compose_

![ComposeFlux](./docs/assets/composeflux-banner.png)

<p align="center">
<img src="https://img.shields.io/github/go-mod/go-version/veerendra2/composeflux?style=flat&logo=go&logoColor=white" alt="Go">
<img src="https://img.shields.io/github/license/veerendra2/composeflux?style=flat" alt="License">
<img src="https://img.shields.io/github/stars/veerendra2/composeflux?style=flat&logo=github" alt="Stars">
<img src="https://img.shields.io/github/forks/veerendra2/composeflux?style=flat&logo=github" alt="Forks">
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

## Contributing

Contributions are welcome! Please see our [Development Guide](https://veerendra2.github.io/composeflux/Development/) for
details.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

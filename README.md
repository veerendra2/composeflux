# ComposeFlux

> _\*Currently in beta_

_A GitOps continuous deployment tool for Docker Compose_

![ComposeFlux](./docs/assets/composeflux-banner.png)

ComposeFlux automates Docker Compose deployments using GitOps principles. Monitor your Git repository, detect changes, and automatically deploy your Docker stacks—all without manual intervention.

---

## Features

- **GitOps Driven** - Automatic deployment from Git repository
- **Smart Change Detection** - Hash-based detection deploys only changed stacks
- **Pure Go Implementation** - Native Docker [Compose SDK](https://docs.docker.com/compose/compose-sdk/) without shell execution
- **Secrets Management** - Integrated Bitwarden Secrets Manager and Infisical support
- **Flexible Configuration** - Startup order and shared environment variables
- **Simple & Headless** - No UI, no backend database

## Documentation

- 📖 [**Introduction**](https://veerendra2.github.io/composeflux/Introduction/)
- 🚀 [**Getting Started**](https://veerendra2.github.io/composeflux/GettingStarted/)

## Contributing

Contributions are welcome! Please see our [Development Guide](https://veerendra2.github.io/composeflux/Development/) for details.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

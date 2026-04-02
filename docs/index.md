# ComposeFlux

_A GitOps continuous deployment tool for Docker Compose_

![ComposeFlux](./assets/composeflux-banner.png)

ComposeFlux automates Docker Compose deployments using GitOps principles. Monitor your Git repository, detect changes, and automatically deploy your Docker stacks—all without manual intervention.

---

## Features

- **GitOps Driven** - Automatic deployment from Git repository
- **Smart Change Detection** - Hash-based detection deploys only changed stacks
- **Pure Go Implementation** - Native Docker Compose SDK without shell execution
- **Secrets Management** - Integrated Bitwarden Secrets Manager and Infisical support
- **Flexible Configuration** - Startup order and shared environment variables
- **Automatic Image Updates** - Scheduled registry checks redeploy stacks when newer images are available
- **Simple & Headless** - No UI, no backend database

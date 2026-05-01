# ComposeFlux

_A GitOps continuous deployment tool for Docker Compose_

![ComposeFlux](./assets/composeflux-banner.png)

ComposeFlux automates Docker Compose deployments using GitOps principles. Monitor your Git repository, detect changes,
and automatically deploy your Docker stacks—all without manual intervention.

---

## Features

| Feature | Description |
| ------- | ----------- |
| GitOps Driven | Automatic deployment from Git repository |
| Smart Change Detection | Hash-based detection deploys only changed stacks |
| Pure Go Implementation | Native Docker Compose SDK without shell execution |
| Secrets Management | Optional Bitwarden Secrets Manager and Infisical support |
| Automatic Image Updates | Scheduled registry checks redeploy stacks when newer images are available |
| Flexible Configuration | Startup order and shared environment variables |
| Prometheus Metrics | Built-in metrics endpoint for deployment, image update, and prune observability |
| Grafana Dashboard | Pre-built dashboard for deployment and image update visibility |
| Simple & Headless | No UI, no backend database |

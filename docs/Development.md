# Development Guide

## Prerequisites

- Go 1.26+
- Docker
- [Task](https://taskfile.dev/)

**Platform Requirements:**

- Linux: `musl-gcc`
- macOS: Xcode Command Line Tools

> _CGO must be enabled (`CGO_ENABLED=1`, already set in `Taskfile.yml`) because ComposeFlux uses the Bitwarden Go SDK,
> which calls into the Bitwarden Rust SDK via FFI using cgo. See
> [Bitwarden SDK Go instructions](https://github.com/bitwarden/sdk-go/blob/main/INSTRUCTIONS.md)._

## Quick Setup

```bash
git clone https://github.com/veerendra2/composeflux.git
cd composeflux
go mod download
task install    # Install dev tools
task build
./dist/composeflux --help
```

### MkDocs Local Setup

If you use direnv and pyenv, an [.envrc](../.envrc) is already configured. Otherwise, set up a Python virtual
environment manually:

```bash
python3 -m venv venv/
source venv/bin/activate
```

Install MkDocs dependencies:

```bash
pip install mkdocs-material
```

Serve documentation locally:

```bash
task docs    # or task serve-docs
```

## Available Tasks

```bash
task: Available tasks for this project:
* all:                Run comprehensive checks;  format, lint, security and test
* build:              Build the application binary for the current platform
* build-docker:       Build Docker image
* compose:            Run the application in development mode using Docker Compose
* fmt:                Formats all Go source files
* install:            Install required tools and dependencies
* lint:               Run static analysis and code linting using golangci-lint
* run:                Runs the main application
* security:           Run security vulnerability scan
* serve-docs:         Serve MkDocs documentation locally with live reload      (aliases: docs)
* test:               Runs all tests in the project                            (aliases: tests)
* vet:                Examines Go source code and reports suspicious constructs
```

## Resources

- [Docker Compose SDK](https://github.com/docker/compose)
- [Bitwarden Go SDK](https://github.com/bitwarden/sdk-go)
- [Infisical Go SDK](https://infisical.com/docs/sdks/languages/go)
- [Go Git Examples](https://github.com/go-git/go-git/tree/main/_examples)
- [MkDocs Docs](https://squidfunk.github.io/mkdocs-material/getting-started/)

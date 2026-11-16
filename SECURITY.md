# Security Policy

## Supported Versions

ComposeFlux is pre-1.0. Security fixes are released only for the
[latest release](https://github.com/veerendra2/composeflux/releases/latest).

## Reporting a Vulnerability

Please do not report security vulnerabilities through public issues, discussions, or pull requests.

Report them privately through
[GitHub private vulnerability reporting](https://github.com/veerendra2/composeflux/security/advisories/new). Include:

- the affected version or commit
- steps to reproduce, or a proof of concept
- the impact you expect

You should receive a response within 7 days. Once the issue is confirmed, a fix is released and a GitHub security
advisory is published, crediting you unless you prefer otherwise.

## Scope

ComposeFlux has access to the Docker socket, the Git deploy key, and decrypted secrets. Reports about the following are
especially welcome:

- secrets (age, Bitwarden, Infisical, deploy keys) leaking into logs, files, or containers
- Compose files or paths escaping the repository or stack root
- running unintended workloads from Git content

Vulnerabilities in dependencies such as Docker, Compose, or age should be reported to those projects first.

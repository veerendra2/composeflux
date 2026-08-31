# Docker Swarm Compatibility

> This is an internal research note. It is excluded from the published MkDocs site.

## Summary

ComposeFlux is not currently Docker Swarm compatible for cluster-wide deployment. It can run on a Swarm manager, but
its Docker Compose SDK calls create standalone containers on the connected Docker Engine. They do not create Swarm
services or distribute workloads across nodes.

The proposed model is still valuable: one ComposeFlux instance on a manager could reconcile the whole cluster while
Swarm handles placement, replicas, rolling updates, and task recovery. Supporting that model requires a Swarm-specific
runtime implementation; it cannot be enabled by changing a Compose SDK option.

## Current Execution Model

ComposeFlux uses the native Compose SDK through [`pkg/dockercompose`](../../pkg/dockercompose/compose.go):

```text
Git repository -> Compose project -> Compose SDK Up -> standalone containers
```

The SDK's `Up`, `Down`, `Ps`, `List`, and `Restart` operations manage Compose projects and containers. Swarm instead
uses stacks, services, and tasks:

```text
Git repository -> Swarm stack -> services -> tasks scheduled across nodes
```

Docker requires `docker stack deploy` or the Swarm service APIs for the second model.

## Compatibility Matrix

| Aspect | Status | Notes |
| --- | --- | --- |
| Git polling and synchronization | Compatible | Independent from the Docker runtime. |
| File-change and dependency detection | Compatible | Compose files, environment files, secrets, bind mounts, and build contexts can still trigger reconciliation. |
| Stack directory discovery | Compatible | The existing source layout can remain. |
| Remote and local secret retrieval | Partially compatible | Fetching and Age decryption are reusable, but values are currently interpolation inputs rather than managed Swarm secrets. |
| Compose loading and interpolation | Partially compatible | ComposeFlux loads the current Compose specification, while `docker stack deploy` uses the legacy Compose v3 format. Modern features require validation or pre-rendering. |
| Deploy and update | Incompatible | Compose `Up` creates containers, not Swarm services. |
| Stack removal | Incompatible | Compose `Down` must be replaced with stack or service removal. |
| Restart | Incompatible | Swarm requires a forced service update, normally using its rolling-update policy. |
| State and health | Incompatible | Current health checks inspect Compose containers. Swarm health must inspect services, desired and running replicas, tasks, rejected tasks, and update state. |
| Management and suspend labels | Incompatible | Current labels are read from Compose containers. Swarm management should use service labels. |
| Image update detection | Incompatible | Current logic compares the registry digest with an image installed on the connected manager. Swarm services use registry-resolved image digests across workers. |
| Local builds | Incompatible | `docker stack deploy` ignores `build`; images must be built and pushed to a registry before deployment. |
| Startup order | Partially compatible | Stacks can be submitted sequentially, but ComposeFlux must wait for Swarm service convergence before continuing. |
| Resource pruning | Incompatible | Current volume, image, and build-cache pruning is Engine-local and guarded by Compose-container health. It is not cluster-aware. |
| Replicas, placement, and rolling updates | New Swarm capability | A Swarm backend can support the Compose `deploy` specification. |
| Overlay networking and routing mesh | New Swarm capability | Available when applications are deployed as Swarm services. |
| Volumes and bind mounts | Requires operator care | Local volumes are node-specific. Bind paths must exist on every eligible node; stateful services need placement constraints or shared storage. |

## Secrets

ComposeFlux can continue retrieving and decrypting secrets before deployment. Delivery to workloads needs a separate
decision:

- Environment interpolation technically works, but values used as environment variables become part of the service
  specification.
- Native Swarm secrets are encrypted in the Raft log and exposed only to assigned service tasks.
- Swarm secrets are immutable. Rotation requires creating a new secret, updating the services, and removing the old
  secret when it is no longer referenced.

A Swarm backend should convert suitable ComposeFlux secret values into versioned Swarm secrets instead of automatically
placing every value into service environment variables.

## Images and Builds

Swarm workers need access to the same image through a registry. A local image built on the manager is not distributed to
other nodes.

For Swarm stacks, ComposeFlux would need to either:

1. Require services to reference prebuilt registry images; or
2. Build and push tagged images before updating the stack.

Image-update checks should compare the registry digest with the digest in the Swarm service specification, not with the
manager's local image cache.

## Health and Reconciliation

Swarm already maintains the desired replica count and replaces failed tasks. ComposeFlux health reconciliation should
therefore observe the orchestrator rather than repeatedly redeploying a stack after an individual container exits.

Relevant state includes:

- Desired replicas versus running replicas
- Pending, rejected, failed, and shutdown tasks
- Service update and rollback status
- Health-check failures during the update monitor period

Stack deployment is asynchronous by default. Startup ordering requires an explicit convergence wait with a timeout.

## Running ComposeFlux in the Swarm

One ComposeFlux service can control the cluster when it:

- Runs on a manager using a `node.role == manager` placement constraint
- Has access to the manager Docker socket or a secured manager API
- Runs with one replica
- Can access its Git credentials, secrets-provider credentials, and Age passphrase after rescheduling
- Uses a prebuilt image available from a registry

The current mutex coordinates reconciliation only within one process. Multiple ComposeFlux replicas could deploy
concurrently unless leader election or a distributed lock is added. Access to the manager Docker socket also gives the
container cluster-administrator privileges and must be treated accordingly.

## Is There a Go SDK for `docker stack deploy`?

There is no supported high-level Go SDK equivalent to the Docker Compose SDK's `Compose.Up` method.

Docker implements `docker stack deploy` inside `github.com/docker/cli/cli/command/stack`, but its orchestration functions
such as `runDeploy`, `deployCompose`, and `deployServices` are unexported. They are CLI implementation details rather than
a reusable stack-deployment API.

The CLI implementation performs these operations:

1. Load a legacy Compose configuration
2. Convert it into Swarm networks, secrets, configs, and service specifications
3. Inspect the existing stack resources
4. Create or update services through the Moby client
5. Optionally prune removed services
6. Optionally wait for service convergence

### Available Building Blocks

- **Moby Go client:** Exposes `ServiceCreate`, `ServiceUpdate`, `ServiceList`, `ServiceRemove`, task, network, secret, and
  config APIs. This is the supported low-level Engine client.
- **Docker CLI conversion packages:** Contain useful Compose-to-Swarm conversion logic, but are part of the CLI
  implementation and do not form a stable stack SDK.
- **Docker CLI executable:** Running `docker stack deploy` gives exact CLI behavior, but ComposeFlux's distroless image
  would need to include and execute the Docker CLI binary.

Copying Docker's unexported stack implementation into ComposeFlux is not recommended because it would create a
maintenance fork of Docker's deployment logic.

### Recommended Investigation

Start with a small technical spike comparing two approaches:

1. Render a Swarm-compatible stack file and invoke the Docker CLI for behavioral parity.
2. Use Docker's conversion building blocks plus the Moby client for a native Go implementation.

The spike must test modern ComposeFlux features such as multiple files, interpolation, `include`, `extends`, local and
remote secrets, configs, registry authentication, pruning, and convergence. The result should determine whether the CLI
dependency is safer than maintaining native orchestration code.

## Official References

- [Deploy a stack to a swarm](https://docs.docker.com/engine/swarm/stack-deploy/)
- [`docker stack deploy`](https://docs.docker.com/reference/cli/docker/stack/deploy/)
- [Docker CLI stack implementation](https://github.com/docker/cli/blob/v29.7.2/cli/command/stack/deploy_composefile.go)
- [Moby Go client](https://pkg.go.dev/github.com/moby/moby/client)
- [How Swarm services work](https://docs.docker.com/engine/swarm/how-swarm-mode-works/services/)
- [Compose Deploy Specification](https://docs.docker.com/reference/compose-file/deploy/)
- [Docker Swarm secrets](https://docs.docker.com/engine/swarm/secrets/)
- [Deploy services to a swarm](https://docs.docker.com/engine/swarm/services/)

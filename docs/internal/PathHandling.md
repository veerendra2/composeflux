# Repository Path Handling

> This is an internal implementation note. It is excluded from the published MkDocs site.

## Summary

ComposeFlux confines its Git and Compose source files to the cloned repository. Runtime dependencies may reference
files or directories outside the repository, but external dependencies are not monitored for Git changes.

External build contexts and Dockerfiles are currently allowed. This is intentional for now, even though changes to
those external paths cannot trigger reconciliation.

## Path Behavior

| Compose path | Allowed outside repository | Monitored by GitSync | Notes |
| --- | --- | --- | --- |
| Service `env_file` | Yes | No | Supports environment files managed on the ComposeFlux host. |
| Top-level `configs.*.file` | Yes | No | The file must be available to the Compose client at deployment time. |
| Top-level `secrets.*.file` | Yes | No | The file must be available to the Compose client at deployment time. |
| Bind-mount source | Yes | No | The source must exist on the Docker host where the container runs. |
| Build context | Yes | No | Builds can use it, but external changes do not trigger a rebuild. |
| Dockerfile | Yes | No | Builds can use it, but external changes do not trigger a rebuild. |
| Compose file | No | Not applicable | Local Compose source files must remain inside the repository. |
| Local `include.path` | No | Not applicable | Local included Compose files must remain inside the repository. |
| `include.project_directory` | No | Not applicable | The included project directory must remain inside the repository. |
| Include-specific `env_file` | No | Not applicable | This file participates in loading an included Compose project. |
| Local `extends.file` | No | Not applicable | Local extended Compose files must remain inside the repository. |
| Stack directory | No | Not applicable | Every discovered stack must remain inside the configured repository root. |
| ComposeFlux stack config | No | Not applicable | The configuration path must remain inside the configured stack root. |

Paths inside the repository are monitored when ComposeFlux recognizes them as dependencies. A matching Git change can
trigger stack deployment, and changes within an in-repository build context can also trigger an image rebuild.

## Boundary Enforcement

ComposeFlux rejects repository escapes for paths that control project discovery and Compose source loading. It checks
both the lexical path and its symlink-resolved path, preventing a symlink inside the repository from resolving outside
the repository.

The boundary is enforced for:

- The configured stack root and stack directories
- The ComposeFlux stack configuration
- Local Compose files
- Local Compose `include` files and their project directories
- Include-specific environment files
- Local Compose `extends` files

Runtime dependencies follow a different policy. ComposeFlux passes them to the Compose SDK but excludes paths outside
the repository from its Git dependency map. They are therefore allowed but cannot trigger GitSync when modified on the
host.

## Fields Not Currently Inspected

ComposeFlux does not currently add the following path-bearing Compose fields to its dependency map:

- `build.additional_contexts`
- `label_file`
- `credential_spec.file`
- `develop.watch.path`
- Host device paths

Some fields do not affect the normal reconciliation workflow. For example, ComposeFlux does not run Compose Watch, so
`develop.watch.path` is not an active dependency. Local additional build contexts are build inputs, however, and changes
to them are not currently detected.

## Operational Considerations

- An external path must be mounted or otherwise available inside the ComposeFlux container when the Compose SDK reads
  it.
- A bind-mount source is interpreted by the Docker daemon and must exist on the Docker host.
- Updating an external file does not cause a Git commit and therefore does not trigger GitSync.
- A force sync, health reconciliation, or another tracked stack change may still deploy a project that references an
  external dependency.
- A successful deployment does not prove that every external build input was rebuilt. When rebuilding is not selected,
  Compose can reuse an existing image.

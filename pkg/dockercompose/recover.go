package dockercompose

import (
	"context"
	"log/slog"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	mobyClient "github.com/moby/moby/client"
)

// tempNameSeparator joins the truncated container ID with the canonical name in
// the temporary name Compose assigns to a replacement container.
const tempNameSeparator = "_"

// recoverReplaced repairs containers left under the temporary name Compose uses
// while recreating a container. Compose creates the replacement container as
// "<first 12 characters of the replaced container ID>_<canonical name>", then
// stops and removes the old container, and finally renames the replacement. It
// has no rollback, so an interrupted plan leaves the replacement behind under
// its temporary name. The next recreation then derives the same temporary name
// from the surviving container and fails with "container name is already in
// use" on every attempt, which no retry can break.
//
// A leftover is renamed to its canonical name when that name is free, which
// restores exactly the state the interrupted plan aimed for. When the canonical
// container still exists, the leftover is the stale duplicate and is removed.
func (c *client) recoverReplaced(ctx context.Context, project *types.Project) {
	result, err := c.docker.ContainerList(ctx, mobyClient.ContainerListOptions{
		All:     true,
		Filters: mobyClient.Filters{}.Add("label", api.ProjectLabel+"="+strings.ToLower(project.Name)),
	})
	if err != nil {
		slog.Warn("Failed to list containers for recovery", "stack_name", project.Name, "error", err)
		return
	}

	type leftover struct {
		id        string
		name      string
		canonical string
	}

	var leftovers []leftover
	inUse := make(map[string]struct{}, len(result.Items))
	for _, ctr := range result.Items {
		name := containerName(ctr.Names)
		canonical := canonicalContainerName(project, ctr.Labels[api.ServiceLabel], ctr.Labels[api.ContainerNumberLabel])
		if name == "" || canonical == "" {
			continue
		}
		if name == canonical {
			inUse[canonical] = struct{}{}
			continue
		}
		if prefix, found := strings.CutSuffix(name, tempNameSeparator+canonical); found && isContainerIDPrefix(prefix) {
			leftovers = append(leftovers, leftover{id: ctr.ID, name: name, canonical: canonical})
		}
	}

	for _, item := range leftovers {
		if _, taken := inUse[item.canonical]; !taken {
			if _, err := c.docker.ContainerRename(ctx, item.id, mobyClient.ContainerRenameOptions{NewName: item.canonical}); err != nil {
				slog.Warn("Failed to rename container left by an interrupted recreate", "stack_name", project.Name, "container", item.name, "error", err)
				continue
			}
			inUse[item.canonical] = struct{}{}
			slog.Info("Renamed container left by an interrupted recreate", "stack_name", project.Name, "container", item.name, "new_name", item.canonical)
			continue
		}
		if _, err := c.docker.ContainerRemove(ctx, item.id, mobyClient.ContainerRemoveOptions{Force: true}); err != nil {
			slog.Warn("Failed to remove container left by an interrupted recreate", "stack_name", project.Name, "container", item.name, "error", err)
			continue
		}
		slog.Warn("Removed container left by an interrupted recreate", "stack_name", project.Name, "container", item.name, "reason", "canonical container still running", "canonical_name", item.canonical)
	}
}

// containerName returns the canonical name of a container, ignoring the link
// aliases Docker reports alongside it.
func containerName(names []string) string {
	for _, name := range names {
		if strings.LastIndex(name, "/") == 0 {
			return strings.TrimPrefix(name, "/")
		}
	}
	return ""
}

// canonicalContainerName resolves the name Compose assigns to a container of a
// loaded project, mirroring Compose's own naming rules.
func canonicalContainerName(project *types.Project, serviceName, number string) string {
	service, ok := project.Services[serviceName]
	if !ok || number == "" {
		return ""
	}
	if service.ContainerName != "" {
		return service.ContainerName
	}
	return strings.Join([]string{project.Name, serviceName, number}, api.Separator)
}

// isContainerIDPrefix reports whether value is the truncated container ID
// Compose uses as the temporary name prefix (12 lowercase hexadecimal digits).
func isContainerIDPrefix(value string) bool {
	if len(value) != 12 {
		return false
	}
	for _, char := range value {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}

package dockercompose

import (
	"context"
	"log/slog"

	"github.com/moby/moby/api/types/container"
	mobyClient "github.com/moby/moby/client"
)

func (c *client) ListContainers(ctx context.Context, labels []string) ([]container.Summary, error) {
	f := mobyClient.Filters{}
	for _, l := range labels {
		f.Add("label", l)
	}

	res, err := c.docker.ContainerList(ctx, mobyClient.ContainerListOptions{
		All:     true,
		Filters: f,
	})
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}

func (c *client) Prune(ctx context.Context) {
	// Container and Network prune skipped intentionally — these resources cannot be safely
	// filtered by composeflux label, and pruning unmanaged stopped containers/networks is unsafe.
	// _, err := c.docker.ContainerPrune(ctx, mobyClient.ContainerPruneOptions{})
	// _, err := c.docker.NetworkPrune(ctx, mobyClient.NetworkPruneOptions{})

	// Volume Prune
	if _, err := c.docker.VolumePrune(ctx, mobyClient.VolumePruneOptions{}); err != nil {
		slog.Warn("Failed to prune volumes", "error", err)
	}

	// Image Prune — only dangling (untagged) images; tagged images can't be filtered by our label
	pruneAllFilter := mobyClient.Filters{}
	pruneAllFilter.Add("dangling", "true")
	if _, err := c.docker.ImagePrune(ctx, mobyClient.ImagePruneOptions{Filters: pruneAllFilter}); err != nil {
		slog.Warn("Failed to prune images", "error", err)
	}

	// Build Cache
	if _, err := c.docker.BuildCachePrune(ctx, mobyClient.BuildCachePruneOptions{All: true}); err != nil {
		slog.Warn("Failed to prune build cache", "error", err)
	}
}

func (c *client) Version(ctx context.Context) ([]any, error) {
	serverVersion, err := c.docker.ServerVersion(ctx, mobyClient.ServerVersionOptions{})
	if err != nil {
		return []any{}, err
	}
	clientVersion := c.docker.ClientVersion()

	return []any{
		"server_engine", serverVersion.Version,
		"server_api", serverVersion.APIVersion,
		"client_api", clientVersion,
	}, nil
}

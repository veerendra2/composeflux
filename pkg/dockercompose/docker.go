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
	f := mobyClient.Filters{}

	// Container Prune
	_, err := c.docker.ContainerPrune(ctx, mobyClient.ContainerPruneOptions{Filters: f})
	if err != nil {
		slog.Warn("Failed to prune containers", "error", err)
	}

	// Network Prune
	_, err = c.docker.NetworkPrune(ctx, mobyClient.NetworkPruneOptions{Filters: f})
	if err != nil {
		slog.Warn("Failed to prune networks", "error", err)
	}

	// Volume Prune
	_, err = c.docker.VolumePrune(ctx, mobyClient.VolumePruneOptions{Filters: f})
	if err != nil {
		slog.Warn("Failed to prune volumes", "error", err)
	}

	// Image Prune
	// Note: We only prune dangling (untagged) images here to be safe. Downloaded images
	// don't carry our label, so we can't filter by it.
	pruneAllFilter := mobyClient.Filters{}
	pruneAllFilter.Add("dangling", "true")
	_, err = c.docker.ImagePrune(ctx, mobyClient.ImagePruneOptions{Filters: pruneAllFilter})
	if err != nil {
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

package dockercompose

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/docker/go-units"
	mobyClient "github.com/moby/moby/client"
)

// Prune removes unused Docker resources from the whole host.
// Containers are not pruned: container prune would delete exited-0 init containers.
func (c *client) Prune(ctx context.Context) error {
	var errs []error
	var reclaimed uint64
	var imageRecords, volumes, networks, cacheEntries int

	// All unused images, tagged or untagged. Docker keeps any image a container uses.
	imageResult, err := c.docker.ImagePrune(ctx, mobyClient.ImagePruneOptions{
		Filters: mobyClient.Filters{}.Add("dangling", "false"),
	})
	if err != nil {
		slog.Warn("Failed to prune images", "error", err)
		errs = append(errs, fmt.Errorf("failed to prune images: %w", err))
	} else {
		// One record per untagged tag, deleted image, and removed layer; the response
		// does not distinguish image IDs from layer IDs, so only the total is known.
		for _, d := range imageResult.Report.ImagesDeleted {
			if d.Deleted != "" {
				imageRecords++
			}
		}
		reclaimed += imageResult.Report.SpaceReclaimed
	}

	// Anonymous volumes only. Named volumes hold stack data.
	volumeResult, err := c.docker.VolumePrune(ctx, mobyClient.VolumePruneOptions{})
	if err != nil {
		slog.Warn("Failed to prune volumes", "error", err)
		errs = append(errs, fmt.Errorf("failed to prune volumes: %w", err))
	} else {
		volumes = len(volumeResult.Report.VolumesDeleted)
		reclaimed += volumeResult.Report.SpaceReclaimed
	}

	networkResult, err := c.docker.NetworkPrune(ctx, mobyClient.NetworkPruneOptions{})
	if err != nil {
		slog.Warn("Failed to prune networks", "error", err)
		errs = append(errs, fmt.Errorf("failed to prune networks: %w", err))
	} else {
		networks = len(networkResult.Report.NetworksDeleted)
	}

	buildResult, err := c.docker.BuildCachePrune(ctx, mobyClient.BuildCachePruneOptions{All: true})
	if err != nil {
		slog.Warn("Failed to prune build cache", "error", err)
		errs = append(errs, fmt.Errorf("failed to prune build cache: %w", err))
	} else {
		cacheEntries = len(buildResult.Report.CachesDeleted)
		reclaimed += buildResult.Report.SpaceReclaimed
	}

	attrs := []any{
		"image_records_deleted", imageRecords,
		"volumes_deleted", volumes,
		"networks_deleted", networks,
		"build_cache_entries_deleted", cacheEntries,
		"space_reclaimed", units.HumanSize(float64(reclaimed)),
		"failed_resources", len(errs),
	}
	if len(errs) == 0 {
		slog.Info("Pruned Docker resources", attrs...)
	} else {
		slog.Warn("Docker resource prune finished with errors", attrs...)
	}

	return errors.Join(errs...)
}

// Version returns structured Docker engine and API version attributes for logging.
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

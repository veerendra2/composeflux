package reconcile

import (
	"context"
	"log/slog"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/veerendra2/composeflux/internal/metrics"
)

// UpdateImages checks all discovered stacks for Docker image updates and redeploys any that have new images.
func (r *Reconciler) UpdateImages(ctx context.Context) error {
	r.reconcileMu.Lock()
	defer r.reconcileMu.Unlock()

	envs, _, err := r.loadEnvAndConfig()
	if err != nil {
		return err
	}

	composeCfgs, err := r.discoverComposeStack(envs)
	if err != nil {
		slog.Error("Failed to discover compose stacks for image update check", "error", err)
		return err
	}

	for _, composeCfg := range composeCfgs {
		project, err := r.dClient.LoadProject(ctx, composeCfg)
		if err != nil {
			slog.Warn("Skipping stack, failed to load project for image check", "path", composeCfg.WorkingDir, "error", err)
			continue
		}

		if hasImageUpdateExcludeLabel(project) {
			slog.Info("Stack has image update excluded, skipping", "stack_name", project.Name)
			continue
		}

		hasUpdate, err := r.dClient.HasImageUpdates(ctx, project)
		if err != nil {
			slog.Warn("Failed to check image updates", "stack_name", project.Name, "error", err)
			continue
		}

		if !hasUpdate {
			slog.Debug("All images up to date", "stack_name", project.Name)
			continue
		}

		metrics.ImageUpdatesTotal.WithLabelValues(project.Name).Inc()

		if err := r.dClient.Pull(ctx, project); err != nil {
			slog.Warn("Failed to pull updated images, skipping redeploy", "stack_name", project.Name, "error", err)
			metrics.ImageUpdateFailuresTotal.WithLabelValues(project.Name).Inc()
			continue
		}

		if err := r.Deploy(ctx, project); err != nil {
			slog.Warn("Failed to redeploy stack after image update", "stack_name", project.Name, "error", err)
			metrics.ImageUpdateFailuresTotal.WithLabelValues(project.Name).Inc()
			continue
		}

		r.healthFailCounts[project.Name] = 0
		slog.Info("Stack redeployed after image update", "stack_name", project.Name)
	}

	return nil
}

// hasImageUpdateExcludeLabel checks if any service in the project has the image update exclude label.
func hasImageUpdateExcludeLabel(project *types.Project) bool {
	for _, svc := range project.Services {
		if excludeValue, ok := svc.Labels[LabelImageUpdateExclude]; ok && excludeValue == "true" {
			return true
		}
	}
	return false
}

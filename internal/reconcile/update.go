package reconcile

import (
	"context"
	"errors"
	"log/slog"

	"github.com/compose-spec/compose-go/v2/types"
)

// UpdateImages checks all discovered stacks for Docker image updates and redeploys any that have new images.
func (r *Reconciler) UpdateImages(ctx context.Context) error {
	r.reconcileMu.Lock()
	defer r.reconcileMu.Unlock()

	repoPath, stackRoot, err := r.sourceRoots()
	if err != nil {
		return err
	}
	globalEnvs, _, _, err := r.loadStackConfig(stackRoot)
	if err != nil {
		return err
	}

	composeCfgs, err := discoverComposeStacks(stackRoot, globalEnvs)
	if err != nil {
		slog.Error("Failed to discover compose stacks for image update check", "error", err)
		return err
	}

	sharedSecrets, _, err := r.loadSharedSecrets(stackRoot)
	if err != nil {
		return err
	}

	for _, composeCfg := range composeCfgs {
		loaded, err := r.loadProjectWithSecrets(ctx, repoPath, composeCfg, sharedSecrets)
		if err != nil {
			if errors.Is(err, errLocalSecrets) {
				return err
			}
			slog.Warn("Skipping stack, failed to load project for image check", "path", composeCfg.WorkingDir, "error", err)
			continue
		}

		if hasImageUpdateExcludeLabel(loaded.project) {
			slog.Info("Stack has image update excluded, skipping", "stack_name", loaded.project.Name)
			continue
		}

		hasUpdate, err := r.dClient.HasImageUpdates(ctx, loaded.project)
		if err != nil {
			slog.Warn("Failed to check image updates", "stack_name", loaded.project.Name, "error", err)
			continue
		}

		if !hasUpdate {
			slog.Debug("All images up to date", "stack_name", loaded.project.Name)
			continue
		}

		if err := r.dClient.Pull(ctx, loaded.project); err != nil {
			slog.Warn("Failed to pull updated images, skipping redeploy", "stack_name", loaded.project.Name, "error", err)
			continue
		}

		if err := r.Deploy(ctx, loaded.project); err != nil {
			slog.Warn("Failed to redeploy stack after image update", "stack_name", loaded.project.Name, "error", err)
			continue
		}

		r.healthFailCounts[loaded.project.Name] = 0
		slog.Info("Stack redeployed after image update", "stack_name", loaded.project.Name)
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

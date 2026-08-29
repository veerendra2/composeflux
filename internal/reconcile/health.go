package reconcile

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
)

const maxHealthReconcileAttempts = 3

// ReconcileHealth redeploys unhealthy managed stacks up to the retry limit.
func (r *Reconciler) ReconcileHealth(ctx context.Context) error {
	r.reconcileMu.Lock()
	defer r.reconcileMu.Unlock()

	stackStatuses, err := r.getStackStates(ctx)
	if err != nil {
		return err
	}

	var toReconcile []string

	for stackName, status := range stackStatuses {
		if !status.Healthy && !status.Suspend {
			toReconcile = append(toReconcile, stackName)
		}
	}

	if len(toReconcile) > 0 {
		globalEnvs, _, err := r.loadStackConfig()
		if err != nil {
			return err
		}

		sharedSecrets, _, err := r.loadSharedSecrets()
		if err != nil {
			slog.Warn("Failed to load shared secrets for health reconcile", "error", err)
		}

		for _, stackName := range toReconcile {
			if r.healthFailCounts[stackName] >= maxHealthReconcileAttempts {
				slog.Warn("Max health reconcile attempts reached, skipping stack",
					"stack_name", stackName, "attempts", r.healthFailCounts[stackName])
				continue
			}

			stackPath := filepath.Join(r.gClient.Path(), r.stackPath, stackName)
			stat, err := os.Stat(stackPath)
			if err != nil || !stat.IsDir() {
				r.healthFailCounts[stackName]++
				slog.Warn("Stack path not found or not a directory", "stack_name", stackName, "stack_path", stackPath, "error", err)
				continue
			}

			composeCfg, err := buildComposeConfig(stackPath, globalEnvs)
			if err != nil {
				r.healthFailCounts[stackName]++
				slog.Warn("Ignoring directory without valid compose files", "stack_dir_name", stackName, "error", err)
				continue
			}

			loaded, err := r.loadProjectWithSecrets(ctx, composeCfg, sharedSecrets)
			if err != nil {
				r.healthFailCounts[stackName]++
				slog.Warn("Skipping, failed to load project with secrets", "path", composeCfg.WorkingDir, "error", err)
				continue
			}

			if err := r.Deploy(ctx, loaded.project); err != nil {
				r.healthFailCounts[stackName]++
				slog.Warn("Failed to deploy the stack", "stack_name", stackName,
					"attempt", r.healthFailCounts[stackName], "error", err)
				continue
			}

			r.healthFailCounts[stackName] = 0
		}
	}

	return nil
}

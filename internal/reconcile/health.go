package reconcile

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
)

const maxHealthReconcileAttempts = 3

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
		envs, _, err := r.loadEnvAndConfig()
		if err != nil {
			return err
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

			composeCfg, err := r.buildComposeConfig(stackPath, envs)
			if err != nil {
				r.healthFailCounts[stackName]++
				slog.Warn("Ignoring directory without valid compose files", "stack_dir_name", stackName, "error", err)
				continue
			}

			project, err := r.dClient.LoadProject(ctx, composeCfg)
			if err != nil {
				r.healthFailCounts[stackName]++
				slog.Warn("Skipping, failed to load project", "path", composeCfg.WorkingDir, "error", err)
				continue
			}

			if err := r.Deploy(ctx, project); err != nil {
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

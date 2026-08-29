package reconcile

import (
	"context"
	"errors"
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
		repoPath, stackRoot, err := r.sourceRoots()
		if err != nil {
			return err
		}
		globalEnvs, _, err := r.loadStackConfig(stackRoot)
		if err != nil {
			return err
		}

		sharedSecrets, _, err := r.loadSharedSecrets(stackRoot)
		if err != nil {
			return err
		}

		for _, stackName := range toReconcile {
			if r.healthFailCounts[stackName] >= maxHealthReconcileAttempts {
				slog.Warn("Max health reconcile attempts reached, skipping stack",
					"stack_name", stackName, "attempts", r.healthFailCounts[stackName])
				continue
			}

			stackPath := filepath.Clean(filepath.Join(stackRoot, stackName))
			if !pathWithinRoot(stackRoot, stackPath) {
				r.healthFailCounts[stackName]++
				slog.Warn("Stack path is outside the configured stack root", "stack_name", stackName, "stack_path", stackPath)
				continue
			}
			stat, err := os.Stat(stackPath)
			if err != nil || !stat.IsDir() {
				r.healthFailCounts[stackName]++
				slog.Warn("Stack path not found or not a directory", "stack_name", stackName, "stack_path", stackPath, "error", err)
				continue
			}
			stackPath, err = resolvePathWithinRoot(stackRoot, stackPath)
			if err != nil {
				r.healthFailCounts[stackName]++
				slog.Warn("Stack path resolves outside the configured stack root", "stack_name", stackName, "stack_path", stackPath, "error", err)
				continue
			}

			composeCfg, err := buildComposeConfig(stackPath, globalEnvs)
			if err != nil {
				r.healthFailCounts[stackName]++
				slog.Warn("Ignoring directory without valid compose files", "stack_dir_name", stackName, "error", err)
				continue
			}

			loaded, err := r.loadProjectWithSecrets(ctx, repoPath, composeCfg, sharedSecrets)
			if err != nil {
				if errors.Is(err, errLocalSecrets) {
					return err
				}
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

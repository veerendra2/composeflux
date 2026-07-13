// ReconcileHealth periodically checks all managed stacks and redeploys any that are unhealthy.
package reconcile

import (
	"context"
	"log/slog"
	"path/filepath"
)

// ReconcileHealth checks all composeflux-managed stacks and redeploys any that are unhealthy
// and not suspended. A stack is unhealthy if its status is not Running or any container is
// in the exited or dead state. Suspended stacks (composeflux.suspend=true on any container)
// are skipped entirely.
func (r *Reconciler) ReconcileHealth(ctx context.Context) error {
	r.reconcileMu.Lock()
	defer r.reconcileMu.Unlock()

	envs, _, err := r.loadEnvAndConfig()
	if err != nil {
		return err
	}

	srcStacks, err := r.discoverComposeStack(envs)
	if err != nil {
		return err
	}

	runningStacks, err := r.dClient.List(ctx)
	if err != nil {
		return err
	}

	// Build a map from stack name to status for O(1) lookup
	statusMap := make(map[string]string, len(runningStacks))
	for _, s := range runningStacks {
		statusMap[s.Name] = s.Status
	}

	for _, src := range srcStacks {
		stackName := filepath.Base(src.WorkingDir)

		_, exists := statusMap[stackName]
		if !exists {
			// Stack not deployed yet, git sync will handle it
			continue
		}

		containers, err := r.dClient.Ps(ctx, stackName)
		if err != nil {
			slog.Warn("Failed to list containers for stack during health reconcile", "stack_name", stackName, "error", err)
			continue
		}

		if isStackSuspended(containers) {
			slog.Debug("Stack is suspended, skipping health reconcile", "stack_name", stackName)
			continue
		}

		if isStackHealthy(containers) {
			continue
		}

		slog.Info("Stack is unhealthy, redeploying", "stack_name", stackName)

		project, err := r.dClient.LoadProject(ctx, src)
		if err != nil {
			slog.Warn("Failed to load project for unhealthy stack, skipping", "stack_name", stackName, "error", err)
			continue
		}

		if err := r.Deploy(ctx, project); err != nil {
			slog.Warn("Failed to redeploy unhealthy stack", "stack_name", stackName, "error", err)
			continue
		}

		slog.Info("Successfully redeployed unhealthy stack", "stack_name", stackName)
	}

	return nil
}

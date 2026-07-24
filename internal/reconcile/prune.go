package reconcile

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/veerendra2/composeflux/internal/metrics"
	"github.com/veerendra2/composeflux/pkg/dockercompose"
)

func (r *Reconciler) PruneResources(ctx context.Context) error {
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

	stackStatuses, err := r.getStackStates(ctx)
	if err != nil {
		return err
	}

	// We skip pruning if any stack is missing from Docker, unhealthy, or suspended
	// See https://github.com/veerendra2/composeflux/issues/31
	for _, src := range srcStacks {
		stackName := filepath.Base(src.WorkingDir)
		status, exists := stackStatuses[stackName]
		if !exists {
			slog.Warn("Skipping prune", "reason", "stack not running in docker", "stack_name", stackName)
			return nil
		}
		if !status.Healthy {
			slog.Warn("Skipping prune", "reason", "unhealthy stack", "stack_name", stackName)
			return nil
		}
		if status.Suspend {
			slog.Warn("Skipping prune", "reason", "suspended stack", "stack_name", stackName)
			return nil
		}
	}

	slog.Info("Pruning unused Docker resources")
	r.dClient.Prune(ctx)
	return nil
}

// Prune deletes the running stacks which are not in the source repository
func (r *Reconciler) PruneStacks(ctx context.Context, srcStack []dockercompose.ComposeConfig) error {
	runningStack, err := r.dClient.List(ctx)
	if err != nil {
		return err
	}

	// Create a map of source stack names
	srcStackNames := make(map[string]bool)
	for _, src := range srcStack {
		srcStackNames[filepath.Base(src.WorkingDir)] = true
	}

	// Find managed stacks that are not present in source (Git Repo)
	var prunedStacks []string
	for _, stack := range runningStack {

		containers, err := r.dClient.Ps(ctx, stack.Name)
		if err != nil {
			slog.Error("Failed to list containers for stack", "stack_name", stack.Name, "error", err)
			continue
		}

		// Ignore the stack if it's not managed by composeflux
		if !isManagedStack(containers) {
			continue
		}

		// Delete stack which is not in source
		if !srcStackNames[stack.Name] {
			if err := r.dClient.Down(ctx, stack.Name); err != nil {
				slog.Warn("Failed to prune stack", "stack_name", stack.Name, "error", err)
				continue
			}
			metrics.StacksPrunedTotal.WithLabelValues(stack.Name).Inc()
			prunedStacks = append(prunedStacks, stack.Name)
		}
	}

	if len(prunedStacks) > 0 {
		slog.Info("Pruned stacks", "count", len(prunedStacks), "stack_names", strings.Join(prunedStacks, ","))
	}

	return nil
}

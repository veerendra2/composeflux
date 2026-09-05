package reconcile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/veerendra2/composeflux/pkg/dockercompose"
)

// PruneResources removes unused Docker resources when every source stack is healthy.
func (r *Reconciler) PruneResources(ctx context.Context) error {
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

	srcStacks, err := discoverComposeStacks(stackRoot, globalEnvs)
	if err != nil {
		return err
	}

	stackStatuses, err := r.getStackStates(ctx)
	if err != nil {
		return err
	}

	sharedSecrets, _, err := r.loadSharedSecrets(stackRoot)
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
		loaded, err := r.loadProjectWithSecrets(ctx, repoPath, src, sharedSecrets)
		if err != nil {
			return fmt.Errorf("failed to load stack %s for pruning: %w", stackName, err)
		}
		if hasProjectLabel(loaded.project, LabelSuspend) {
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
	var pruneErrors []error
	for _, stack := range runningStack {
		if srcStackNames[stack.Name] {
			continue
		}

		containers, err := r.dClient.Ps(ctx, stack.Name)
		if err != nil {
			slog.Error("Failed to list containers for stack", "stack_name", stack.Name, "error", err)
			pruneErrors = append(pruneErrors, fmt.Errorf("failed to list containers for stack %s: %w", stack.Name, err))
			continue
		}

		// Ignore the stack if it's not managed by composeflux
		if !isManagedStack(containers) {
			continue
		}

		if err := r.dClient.Down(ctx, stack.Name); err != nil {
			slog.Warn("Failed to prune stack", "stack_name", stack.Name, "error", err)
			pruneErrors = append(pruneErrors, fmt.Errorf("failed to prune stack %s: %w", stack.Name, err))
			continue
		}
		prunedStacks = append(prunedStacks, stack.Name)
	}

	if len(prunedStacks) > 0 {
		slog.Info("Pruned stacks", "count", len(prunedStacks), "stack_names", strings.Join(prunedStacks, ","))
	}

	return errors.Join(pruneErrors...)
}

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

// PruneResources removes unused Docker resources from the host.
func (r *Reconciler) PruneResources(ctx context.Context) error {
	r.reconcileMu.Lock()
	defer r.reconcileMu.Unlock()

	return r.dClient.Prune(ctx)
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
			slog.Warn("Failed to list containers for stack", "stack_name", stack.Name, "error", err)
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

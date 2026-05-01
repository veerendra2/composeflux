package reconcile

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/veerendra2/composeflux/internal/metrics"
	"github.com/veerendra2/composeflux/pkg/dockercompose"
)

// isManagedStack checks if the stack is managed by composeflux via container labels.
func isManagedStack(containers []api.ContainerSummary) bool {
	return len(containers) > 0 && containers[0].Labels[LabelManaged] == ManagedValue
}

// Prune deletes the running stacks which are not in source repo
func (r *Reconciler) Prune(ctx context.Context, srcStack []dockercompose.ComposeConfig) error {
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
			slog.Error("Failed to get stack", "error", err)
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

	// Prune unused Docker resources (containers, images, volumes, networks, build cache)
	if r.pruneImages {
		r.dClient.Prune(ctx)
	}

	return nil
}

type StackStateMap map[string]StackInfo

type StackInfo struct {
	Hash string
}

// getStackStates returns a StackStateMap keyed by stack name containing each stack's hash
func (r *Reconciler) getStackStates(ctx context.Context) (StackStateMap, error) {
	stackStateMap := make(StackStateMap)
	stacks, err := r.dClient.List(ctx)
	if err != nil {
		return stackStateMap, err
	}

	for _, stack := range stacks {
		containers, err := r.dClient.Ps(ctx, stack.Name)
		if err != nil {
			slog.Error("Failed to get stack", "error", err)
			continue
		}

		// Ignore the stack if it's not managed by composeflux
		if !isManagedStack(containers) {
			continue
		}

		containerHash := ""
		if hash, ok := containers[0].Labels[LabelStackHash]; ok {
			containerHash = hash
		}

		stackStateMap[stack.Name] = StackInfo{
			Hash: containerHash,
		}
	}
	return stackStateMap, nil
}

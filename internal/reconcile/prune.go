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
			slog.Error("Failed to get stack containers", "stack_name", stack.Name, "error", err)
			continue
		}

		// Ignore the stack if it's not managed by composeflux
		if !isManagedStack(containers) {
			continue
		}

		// Safety check: ensure containers array is not empty before accessing
		containerHash := ""
		if len(containers) > 0 {
			if hash, ok := containers[0].Labels[LabelStackHash]; ok {
				containerHash = hash
			}
		}

		stackStateMap[stack.Name] = StackInfo{
			Hash: containerHash,
		}
	}
	return stackStateMap, nil
}

// pruneDeletedStacks removes stacks that are managed by ComposeFlux but no longer exist in Git
func (r *Reconciler) pruneDeletedStacks(ctx context.Context, gitStacks []dockercompose.ComposeConfig) {
	runningStacks, err := r.dClient.List(ctx)
	if err != nil {
		slog.Error("Failed to list running stacks for pruning", "error", err)
		return
	}

	// Create a map of Git stack names for quick lookup
	// Use actual project names from loaded projects, not directory names
	gitStackNames := make(map[string]bool)
	for _, composeCfg := range gitStacks {
		// Load project to get the actual project name (could differ from directory name)
		project, err := r.dClient.LoadProject(ctx, composeCfg)
		if err != nil {
			slog.Warn("Failed to load project for pruning check, using directory name as fallback",
				"path", composeCfg.WorkingDir, "error", err)
			// Fallback to directory name if project can't be loaded
			gitStackNames[filepath.Base(composeCfg.WorkingDir)] = true
			continue
		}
		gitStackNames[project.Name] = true
	}

	// Find and remove stacks that are managed by ComposeFlux but no longer in Git
	var prunedStacks []string
	for _, stack := range runningStacks {
		containers, err := r.dClient.Ps(ctx, stack.Name)
		if err != nil {
			slog.Error("Failed to get stack containers", "stack_name", stack.Name, "error", err)
			continue
		}

		// Skip stacks not managed by ComposeFlux
		if !isManagedStack(containers) {
			continue
		}

		// Remove stack if it's not in Git anymore
		if !gitStackNames[stack.Name] {
			if err := r.dClient.Down(ctx, stack.Name); err != nil {
				slog.Warn("Failed to remove deleted stack", "stack_name", stack.Name, "error", err)
				continue
			}
			metrics.StacksPrunedTotal.WithLabelValues(stack.Name).Inc()
			prunedStacks = append(prunedStacks, stack.Name)
		}
	}

	if len(prunedStacks) > 0 {
		slog.Info("Removed stacks deleted from Git", "count", len(prunedStacks), "stack_names", strings.Join(prunedStacks, ","))
	}
}

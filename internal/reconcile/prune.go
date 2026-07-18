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

const (
	StateRunning = "running"
	StateExited  = "exited"
	Unhealthy    = "unhealthy"
)

// isManagedStack checks if the stack is managed by composeflux via container labels.
func isManagedStack(containers []api.ContainerSummary) bool {
	return len(containers) > 0 && containers[0].Labels != nil && containers[0].Labels[LabelManaged] == ValueTrue
}

func (r *Reconciler) PruneResources(ctx context.Context) error {
	r.reconcileMu.Lock()
	defer r.reconcileMu.Unlock()

	stackStatuses, err := r.getStackStates(ctx)
	if err != nil {
		return err
	}

	for stackName, status := range stackStatuses {
		if !status.Healthy {
			slog.Warn("Stack is unhealth, skip pruning", "stack_name", stackName)
			return nil
		}
		if status.Suspend {
			slog.Warn("Stack is suspended, skip pruning", "stack_name", stackName)
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

type StackStateMap map[string]StackInfo

type StackInfo struct {
	Hash       string
	Healthy    bool
	Suspend    bool
	WorkingDir string
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
			slog.Error("Failed to list containers for stack", "stack_name", stack.Name, "error", err)
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

		workingDir := ""
		if dir, ok := containers[0].Labels[LabelDockerComposeWorkingDir]; ok {
			workingDir = dir
		}

		stackHealthy := true
		stackSuspend := false
		for _, container := range containers {
			if !isContainerHealthy(container) {
				slog.Debug("Container is not healthy", "stack_name", stack.Name, "container", container.Name,
					"exit_code", container.ExitCode, "status", container.State, "container_health", container.Health,
				)
				stackHealthy = false
			}
			if container.Labels[LabelSuspend] == ValueTrue {
				stackSuspend = true
			}
		}

		stackStateMap[stack.Name] = StackInfo{
			Hash:       containerHash,
			Healthy:    stackHealthy,
			Suspend:    stackSuspend,
			WorkingDir: workingDir,
		}
	}
	return stackStateMap, nil
}

func isContainerHealthy(container api.ContainerSummary) bool {
	if container.ExitCode == 0 && container.State == StateRunning {
		return true
	} else if container.Labels[LabelInit] == ValueTrue {
		return true
	} else if container.Health != Unhealthy {
		return true
	}

	return false
}

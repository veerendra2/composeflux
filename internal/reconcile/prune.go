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
	return len(containers) > 0 && containers[0].Labels != nil && containers[0].Labels[LabelManaged] == ManagedValue
}

// isStackSuspended returns true if any container in the stack has composeflux.suspend=true.
// The label is set via the Docker CLI (not compose YAML) to pause reconciliation during
// operations like database backups where containers are intentionally stopped.
func isStackSuspended(containers []api.ContainerSummary) bool {
	for _, c := range containers {
		if c.Labels[LabelSuspend] == "true" {
			return true
		}
	}
	return false
}

// isStackHealthy returns true if no container is crashed or dead.
// Exited containers with exit code 0 are healthy (completed successfully, like init containers).
// Exited containers with non-zero exit code are unhealthy (crashed).
// restarting is intentionally not treated as unhealthy — Docker restart policy is working.
func isStackHealthy(containers []api.ContainerSummary) bool {
	if len(containers) == 0 {
		return false // No containers = unhealthy
	}

	hasRunningContainer := false

	for _, c := range containers {
		// Dead containers are always unhealthy
		if c.State == "dead" {
			return false
		}
		// Exited with non-zero exit code = crashed (unhealthy)
		if c.State == "exited" && c.ExitCode != 0 {
			return false
		}

		// Track if at least one container is NOT exited
		if c.State != "exited" {
			hasRunningContainer = true
		}
	}

	// If ALL containers are exited (even with code 0) → unhealthy (stack is down)
	if !hasRunningContainer {
		return false
	}

	return true
}

// allManagedStacksHealthy returns true only if every deployed managed stack is healthy
// and none has the suspend label set. Any suspended stack causes an immediate false return.
func (r *Reconciler) allManagedStacksHealthy(ctx context.Context) (bool, error) {
	allContainers, err := r.dClient.ListAllContainers(ctx)
	if err != nil {
		return false, err
	}

	// Filter for managed containers
	var managedContainers []api.ContainerSummary
	for _, c := range allContainers {
		if c.Labels != nil && c.Labels[LabelManaged] == ManagedValue {
			managedContainers = append(managedContainers, c)
		}
	}

	if len(managedContainers) == 0 {
		return true, nil // No managed containers, considered healthy
	}

	// Group containers by project name
	projectGroups := r.groupContainersByProject(managedContainers)

	for stackName, containers := range projectGroups {
		if isStackSuspended(containers) {
			slog.Info("Stack is suspended, skipping prune", "stack_name", stackName)
			return false, nil
		}

		if !isStackHealthy(containers) {
			slog.Info("Stack is not healthy, skipping prune", "stack_name", stackName)
			return false, nil
		}
	}

	return true, nil
}

// PruneDockerResources prunes unused Docker resources (images, volumes, build cache) only
// when all composeflux-managed stacks are healthy and none are suspended.
func (r *Reconciler) PruneDockerResources(ctx context.Context) error {
	r.reconcileMu.Lock()
	defer r.reconcileMu.Unlock()

	healthy, err := r.allManagedStacksHealthy(ctx)
	if err != nil {
		return err
	}
	if !healthy {
		slog.Info("Skipping Docker resource prune: not all managed stacks are healthy")
		return nil
	}

	slog.Info("All managed stacks healthy, pruning unused Docker resources")
	r.dClient.Prune(ctx)
	return nil
}

// Prune deletes the running stacks which are not in the source repository
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

		stackStateMap[stack.Name] = StackInfo{
			Hash: containerHash,
		}
	}
	return stackStateMap, nil
}

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

// isStackHealthy returns true if the stack status is Running and no container is exited or dead.
// restarting is intentionally not treated as unhealthy — Docker restart policy is working.
func isStackHealthy(stackStatus string, containers []api.ContainerSummary) bool {
	if stackStatus != "Running" {
		return false
	}
	for _, c := range containers {
		if c.State == "exited" || c.State == "dead" {
			return false
		}
	}
	return true
}

// allManagedStacksHealthy returns true only if every stack discovered from Git is healthy
// and none has the suspend label set. Any suspended stack causes an immediate false return.
func (r *Reconciler) allManagedStacksHealthy(ctx context.Context, srcStacks []dockercompose.ComposeConfig) (bool, error) {
	runningStacks, err := r.dClient.List(ctx)
	if err != nil {
		return false, err
	}

	// Build a map from stack name to status for O(1) lookup
	statusMap := make(map[string]string, len(runningStacks))
	for _, s := range runningStacks {
		statusMap[s.Name] = s.Status
	}

	for _, src := range srcStacks {
		stackName := filepath.Base(src.WorkingDir)

		containers, err := r.dClient.Ps(ctx, stackName)
		if err != nil {
			slog.Warn("Failed to list containers for stack during health check", "stack_name", stackName, "error", err)
			return false, nil
		}

		if isStackSuspended(containers) {
			slog.Info("Stack is suspended, skipping prune", "stack_name", stackName)
			return false, nil
		}

		status := statusMap[stackName]
		if !isStackHealthy(status, containers) {
			slog.Info("Stack is not healthy, skipping prune", "stack_name", stackName, "status", status)
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

	envs, _, err := r.loadEnvAndConfig()
	if err != nil {
		return err
	}

	srcStacks, err := r.discoverComposeStack(envs)
	if err != nil {
		return err
	}

	healthy, err := r.allManagedStacksHealthy(ctx, srcStacks)
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

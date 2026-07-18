package reconcile

import (
	"context"
	"log/slog"

	"github.com/docker/compose/v5/pkg/api"
	dockertypes "github.com/docker/docker/api/types/container"
)

// ReconcileHealth checks all composeflux-managed containers and redeploys unhealthy stacks
func (r *Reconciler) ReconcileHealth(ctx context.Context) error {
	r.reconcileMu.Lock()
	defer r.reconcileMu.Unlock()

	// Step 1: Get all containers
	allContainers, err := r.dClient.ListAllContainers(ctx)
	if err != nil {
		slog.Error("Failed to list all containers for health reconcile", "error", err)
		return err
	}

	// Filter for managed containers
	var managedContainers []api.ContainerSummary
	for _, c := range allContainers {
		if c.Labels != nil && c.Labels[LabelManaged] == ManagedValue {
			managedContainers = append(managedContainers, c)
		}
	}

	if len(managedContainers) == 0 {
		return nil // No managed containers, nothing to do
	}

	// Step 2: Group containers by project name
	projectGroups := r.groupContainersByProject(managedContainers)

	// Step 3: Check health for each project and build reconciliation map
	toReconcile := make(map[string]string) // projectName -> workingDir

	for projectName, containers := range projectGroups {
		// Skip if suspended
		if isStackSuspended(containers) {
			slog.Debug("Stack is suspended, skipping health reconcile", "project", projectName)
			continue
		}

		// Check if unhealthy using stack-level heuristic
		if !isStackHealthy(containers) {
			// Get working directory from first container's label
			workingDir := containers[0].Labels["com.docker.compose.project.working_dir"]
			if workingDir == "" {
				slog.Warn("Container missing working_dir label", "project", projectName)
				continue
			}
			toReconcile[projectName] = workingDir
			slog.Info("Stack is unhealthy, will redeploy", "project", projectName)
		}
	}

	// Step 4: Reconcile unhealthy stacks
	for projectName, workingDir := range toReconcile {
		if err := r.reconcileStack(ctx, projectName, workingDir); err != nil {
			slog.Warn("Failed to reconcile stack", "project", projectName, "error", err)
			continue
		}
		slog.Info("Successfully reconciled stack", "project", projectName)
	}

	return nil
}

// groupContainersByProject groups containers by com.docker.compose.project label
func (r *Reconciler) groupContainersByProject(containers []dockertypes.Summary) map[string][]dockertypes.Summary {
	groups := make(map[string][]dockertypes.Summary)
	for _, c := range containers {
		project := c.Labels["com.docker.compose.project"]
		if project == "" {
			continue
		}
		groups[project] = append(groups[project], c)
	}
	return groups
}

// reconcileStack loads project from working directory and runs docker compose up
func (r *Reconciler) reconcileStack(ctx context.Context, projectName, workingDir string) error {
	// Build ComposeConfig from working directory
	envs, _, err := r.loadEnvAndConfig()
	if err != nil {
		slog.Warn("Failed to load environment and config for stack reconciliation", "project", projectName, "error", err)
		// We can still try to deploy without envs, as we did in Reconciler before
	}

	composeCfg, err := r.buildComposeConfig(workingDir, envs)
	if err != nil {
		return err
	}

	project, err := r.dClient.LoadProject(ctx, composeCfg)
	if err != nil {
		return err
	}

	return r.Deploy(ctx, project)
}

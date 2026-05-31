// Sync changes from upstream Git repository and deploy updated/new stacks
package reconcile

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/veerendra2/composeflux/internal/metrics"
)

// SyncImages checks all discovered stacks for Docker image updates and redeploys any that have new images.
func (r *Reconciler) SyncImages(ctx context.Context) error {
	r.reconcileMu.Lock()
	defer r.reconcileMu.Unlock()

	envs, _, err := r.loadEnvAndConfig()
	if err != nil {
		return err
	}

	composeCfgs, err := r.discoverComposeStack(envs)
	if err != nil {
		slog.Error("Failed to discover compose stacks for image update check", "error", err)
		return err
	}

	for _, composeCfg := range composeCfgs {
		project, err := r.dClient.LoadProject(ctx, composeCfg)
		if err != nil {
			slog.Warn("Skipping stack, failed to load project for image check", "path", composeCfg.WorkingDir, "error", err)
			continue
		}

		hasUpdate, err := r.dClient.HasImageUpdates(ctx, project)
		if err != nil {
			slog.Warn("Failed to check image updates", "stack_name", project.Name, "error", err)
			continue
		}

		if !hasUpdate {
			slog.Debug("All images up to date", "stack_name", project.Name)
			continue
		}

		metrics.ImageUpdatesTotal.WithLabelValues(project.Name).Inc()

		if err := r.dClient.Pull(ctx, project); err != nil {
			slog.Warn("Failed to pull updated images, skipping redeploy", "stack_name", project.Name, "error", err)
			metrics.ImageUpdateFailuresTotal.WithLabelValues(project.Name).Inc()
			continue
		}

		if err := r.Deploy(ctx, project); err != nil {
			slog.Warn("Failed to redeploy stack after image update", "stack_name", project.Name, "error", err)
			metrics.ImageUpdateFailuresTotal.WithLabelValues(project.Name).Inc()
			continue
		}

		slog.Info("Stack redeployed after image update", "stack_name", project.Name)
	}

	// Prune stacks which are not in Git repo
	if err := r.Prune(ctx, composeCfgs); err != nil {
		slog.Error("Failed to prune stacks", "error", err)
	}

	return nil
}

// Sync pulls changes from the Git repository and deploys stacks which are changed or new
func (r *Reconciler) Sync(ctx context.Context) error {
	r.reconcileMu.Lock()
	defer r.reconcileMu.Unlock()

	if err := r.gClient.Pull(ctx); err != nil {
		return err
	}

	envs, startupOrder, err := r.loadEnvAndConfig()
	if err != nil {
		return err
	}

	// Discover compose stacks
	composeCfgs, err := r.discoverComposeStack(envs)
	if err != nil {
		return err
	}

	// Validate StartupOrder directories and log warning if not exists
	for _, stackName := range startupOrder {
		startupItemDir := filepath.Join(r.gClient.Path(), r.stackPath, stackName)
		if _, err := os.Stat(startupItemDir); os.IsNotExist(err) {
			slog.Warn("Stack directory in startup_order not found",
				"startup_order_item", stackName,
				"expected_path", startupItemDir)
		}
	}

	// Get current running stacks info
	currentStackMap, err := r.getStackStates(ctx)
	if err != nil {
		return err
	}

	// Store projects to deploy
	// Map of Stack name -> loaded Project
	toDeploy := make(map[string]*types.Project)

	// Check hash and determine which stacks are changed and deploy those
	for _, composeCfg := range composeCfgs {
		project, err := r.dClient.LoadProject(ctx, composeCfg)
		if err != nil {
			slog.Warn("Skipping, failed to load project", "path", composeCfg.WorkingDir, "error", err)
			continue
		}

		sourceHash, err := projectChecksum(project)
		if err != nil {
			slog.Warn("Failed to calculate project checksum, deploying stack anyway",
				"stack_name", project.Name, "error", err)
			// Deploy anyway if hash calculation fails
			toDeploy[project.Name] = project
			continue
		}

		if stackInfo, exists := currentStackMap[project.Name]; exists {
			if stackInfo.Hash != sourceHash {
				slog.Info("Stack hash changed, redeploying", "stack_name", project.Name)
				toDeploy[project.Name] = project
			}
		} else {
			slog.Info("New stack detected", "stack_name", project.Name)
			toDeploy[project.Name] = project
		}
	}

	// Create slice to arrange stack array according to StartupOrder
	// defined in the stack config
	deployOrder := []string{}

	for _, stackName := range startupOrder {
		// Add StartupOrder first, if the stack in StartupOrder is also in toDeploy
		if _, exists := toDeploy[stackName]; exists {
			deployOrder = append(deployOrder, stackName)
		}
	}

	// Add remaining stacks (not in StartupOrder)
	for stackName := range toDeploy {
		// Only add if not already in deployOrder
		if !slices.Contains(deployOrder, stackName) {
			deployOrder = append(deployOrder, stackName)
		}
	}

	if len(deployOrder) > 0 {
		slog.Info("Deploying stacks", "count", len(deployOrder), "order", strings.Join(deployOrder, ","))
	}

	for _, name := range deployOrder {
		metrics.DeploymentsTotal.WithLabelValues(name).Inc()
		if err := r.Deploy(ctx, toDeploy[name]); err != nil {
			slog.Warn("Failed to deploy the stack", "stack_name", name, "error", err)
			metrics.DeploymentFailuresTotal.WithLabelValues(name).Inc()
			continue
		}
		slog.Info("Successfully deployed the stack", "stack_name", name)
	}

	// Prune stacks which are not in the Git repository
	if err := r.Prune(ctx, composeCfgs); err != nil {
		slog.Error("Failed to prune stacks", "error", err)
	}

	return nil
}

// Sync changes from upstream Git repository and deploy updated/new stacks
package reconcile

import (
	"context"
	"log/slog"
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
		metrics.DeploymentsTotal.WithLabelValues(project.Name).Inc()
		if err := r.Deploy(ctx, project); err != nil {
			slog.Warn("Failed to redeploy stack after image update", "stack_name", project.Name, "error", err)
			metrics.ImageUpdateFailuresTotal.WithLabelValues(project.Name).Inc()
			metrics.DeploymentFailuresTotal.WithLabelValues(project.Name).Inc()
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

	envs, cfg, err := r.loadEnvAndConfig()
	if err != nil {
		return err
	}

	// Discover compose stacks
	composeCfgs, err := r.discoverComposeStack(envs)
	if err != nil {
		return err
	}

	// Validate StartupOrder items exist in discovered stacks
	if cfg != nil {
		discovered := make(map[string]bool)
		for _, c := range composeCfgs {
			discovered[filepath.Base(c.WorkingDir)] = true
		}
		for _, name := range cfg.StartupOrder {
			if !discovered[name] {
				slog.Warn("Stack in startup_order not found in discovered stacks", "startup_order_item", name)
			}
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
			slog.Warn("Failed to get hash from the running stack, picking for deployment anyways",
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

	var startupOrder []string
	if cfg != nil {
		startupOrder = cfg.StartupOrder
	}
	deployOrder := orderStacks(toDeploy, startupOrder)

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

// orderStacks arranges stack names according to startupOrder, followed by remaining stacks in alphabetical order.
func orderStacks(toDeploy map[string]*types.Project, startupOrder []string) []string {
	var deployOrder []string
	seen := make(map[string]bool)

	for _, name := range startupOrder {
		if _, exists := toDeploy[name]; exists && !seen[name] {
			deployOrder = append(deployOrder, name)
			seen[name] = true
		}
	}

	var remaining []string
	for name := range toDeploy {
		if !seen[name] {
			remaining = append(remaining, name)
		}
	}
	slices.Sort(remaining)
	return append(deployOrder, remaining...)
}

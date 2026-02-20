// Sync changes from upstream Git repository and deploy updated/new stacks
package reconcile

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/veerendra2/composeflux/pkg/dockercompose"
)

// Sync pulls changes from Git repo and deploys stacks which are changed and new
func (r *Reconciler) Sync(ctx context.Context) error {
	if err := r.gClient.Pull(ctx); err != nil {
		return err
	}

	// Read stack config file in the Git repo
	configPath := filepath.Join(r.gClient.Path(), r.stackPath, r.configFile)
	var cfg *StackConfig

	if _, err := os.Stat(configPath); err == nil {
		slog.Debug("Found stack config", "path", configPath)
		cfg, err = Load(configPath)
		if err != nil {
			return err
		}

		// Validate StartupOrder directories exist
		if cfg != nil && len(cfg.StartupOrder) > 0 {
			for _, stackName := range cfg.StartupOrder {
				startupItemDir := filepath.Join(r.gClient.Path(), r.stackPath, stackName)

				if _, err := os.Stat(startupItemDir); os.IsNotExist(err) {
					slog.Warn("Stack directory in startup_order not found",
						"startup_order_item", stackName,
						"expected_path", startupItemDir)
				}
			}
		}
	} else {
		slog.Warn("Stack config not found in the Git repo", "path", configPath)
		slog.Warn("Continuing stacks deployment without stack config")
	}

	// Load secrets into cache
	if err := r.CacheLoadSecrets(); err != nil {
		return err
	}

	// Add environmental variables from stack config to cache
	if cfg != nil {
		if len(cfg.Envs) > 0 {
			slog.Info("Adding env vars to cache", "count", len(cfg.Envs))
			r.CacheSet(cfg.Envs)
		}
	}

	// Discover compose stacks
	composeCfgs, err := r.discoverComposeStack()
	if err != nil {
		return err
	}

	// Get current running stacks info
	currentStackMap, err := r.getStackStates(ctx)
	if err != nil {
		return err
	}

	// Store stack configs to deploy
	// Map of Stack name -> Compose config
	toDeployConfigs := make(map[string]dockercompose.ComposeConfig)

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
			toDeployConfigs[project.Name] = composeCfg
			continue
		}

		if stackInfo, exists := currentStackMap[project.Name]; exists {
			if stackInfo.Hash != sourceHash {
				slog.Info("Stack hash changed, redeploying", "stack_name", project.Name)
				toDeployConfigs[project.Name] = composeCfg
			}
		} else {
			slog.Info("New stack detected", "stack_name", project.Name)
			toDeployConfigs[project.Name] = composeCfg
		}
	}

	// Create slice to arrange stack array according to StartupOrder
	// defined in the stack config
	deployOrder := []string{}
	if cfg != nil && len(cfg.StartupOrder) > 0 {
		for _, stackName := range cfg.StartupOrder {
			// Add StartupOrder first, if the stack in StartupOrder is also in toDeployConfigs
			if _, exists := toDeployConfigs[stackName]; exists {
				deployOrder = append(deployOrder, stackName)
			}
		}
	}

	// Add remaining stacks (not in StartupOrder)
	for stackName := range toDeployConfigs {
		// Only add if not already in deployOrder
		if !slices.Contains(deployOrder, stackName) {
			deployOrder = append(deployOrder, stackName)
		}
	}

	slog.Info("Deploying stacks", "count", len(deployOrder),
		"order", strings.Join(deployOrder, ","))

	for _, stackName := range deployOrder {
		if stackCfg, exists := toDeployConfigs[stackName]; exists {
			project, err := r.dClient.LoadProject(ctx, stackCfg)
			if err != nil {
				slog.Warn("Failed to load project for deployment", "stack_name", stackName, "error", err)
				continue
			}

			if err := r.Deploy(ctx, project); err != nil {
				slog.Warn("Failed to deploy the stack", "stack_name", stackName, "error", err)
				continue
			}
			slog.Info("Successfully deployed the stack", "stack_name", stackName)
		}
	}

	// Prune stacks which are not in Git repo
	if err := r.Prune(ctx, composeCfgs); err != nil {
		slog.Error("Failed to prune stacks", "error", err)
	}

	slog.Debug("Clearing cache")
	r.CacheClear()

	return nil
}

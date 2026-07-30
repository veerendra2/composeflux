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
	"github.com/veerendra2/composeflux/pkg/dockercompose"
)

// GitSync pulls changes from the Git repository and deploys stacks which are changed or new
func (r *Reconciler) GitSync(ctx context.Context) error {
	r.reconcileMu.Lock()
	defer r.reconcileMu.Unlock()

	changedFiles, err := r.gClient.Pull(ctx)
	if err != nil {
		return err
	}

	repoPath := r.gClient.Path()

	// Convert relative git changed files to absolute cleaned paths
	changedPathMap := make(map[string]struct{})
	for _, f := range changedFiles {
		absPath := filepath.Clean(filepath.Join(repoPath, f))
		changedPathMap[absPath] = struct{}{}
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

		// Check if stack needs deployment
		stackInfo, exists := currentStackMap[project.Name]
		if !exists {
			slog.Info("New stack detected", "stack_name", project.Name)
			toDeploy[project.Name] = project
		} else if !stackInfo.Healthy && !stackInfo.Suspend {
			slog.Info("Unhealthy stack detected", "stack_name", project.Name)
			toDeploy[project.Name] = project
		} else if len(changedFiles) > 0 {
			// Stack is running, check if any changed file in git overlaps with stack's dependency tree
			deps := dockercompose.GetDependencyPaths(project)
			hasMatch := false
			sep := string(filepath.Separator)

			for changedPath := range changedPathMap {
				for _, dep := range deps {
					// Only consider dependency paths that are inside the git repository
					rel, err := filepath.Rel(repoPath, dep)
					if err != nil || strings.HasPrefix(rel, "..") {
						continue
					}

					depPrefix := dep
					if !strings.HasSuffix(depPrefix, sep) {
						depPrefix += sep
					}

					if changedPath == dep || strings.HasPrefix(changedPath, depPrefix) {
						hasMatch = true
						slog.Debug("Changed dependency file detected in stack", "stack_name", project.Name, "file", changedPath, "dir", dep)
						break
					}
				}
				if hasMatch {
					break
				}
			}
			if hasMatch {
				toDeploy[project.Name] = project
			}
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
		if err := r.Deploy(ctx, toDeploy[name]); err != nil {
			slog.Warn("Failed to deploy the stack", "stack_name", name, "error", err)
			continue
		}
		slog.Info("Successfully deployed the stack", "stack_name", name)
	}

	// Reset health fail counters — Git sync is the authoritative source of truth
	clear(r.healthFailCounts)

	// Prune stacks which are not in the Git repository
	if err := r.PruneStacks(ctx, composeCfgs); err != nil {
		slog.Error("Failed to prune stacks", "error", err)
	}

	return nil
}

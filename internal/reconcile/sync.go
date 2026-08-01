// Sync changes from upstream Git repository and deploy updated/new stacks
package reconcile

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/veerendra2/composeflux/pkg/dockercompose"
)

// GitSync pulls changes from the Git repository and deploys stacks which are changed or new.
// If force is true, all non-suspended stacks are deployed regardless of git diff.
func (r *Reconciler) GitSync(ctx context.Context, force bool) error {
	r.reconcileMu.Lock()
	defer r.reconcileMu.Unlock()

	// Reclaim heap memory allocations from ASTs and Git tree diffs back to the OS when GitSync exits
	defer func() {
		runtime.GC()
		debug.FreeOSMemory()
	}()

	changedFiles, err := r.gClient.Pull(ctx)
	if err != nil {
		return err
	}

	repoPath, err := filepath.Abs(filepath.Clean(r.gClient.Path()))
	if err != nil {
		return err
	}

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
		startupItemDir := filepath.Join(repoPath, r.stackPath, stackName)
		if _, err := os.Stat(startupItemDir); errors.Is(err, os.ErrNotExist) {
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

		deps := dockercompose.GetDependencyPaths(project)
		sep := string(filepath.Separator)

		// Validate dependency paths and filter in-repository dependencies
		defaultEnvPath := filepath.Join(project.WorkingDir, ".env")
		var inRepoFileDeps []string
		var inRepoDirDeps []string

		for _, dep := range deps.FilePaths {
			rel, err := filepath.Rel(repoPath, dep)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+sep) {
				// Path is outside the repository (e.g. host bind mounts /media/...).
				// Skip os.Stat since host filesystems are not mounted inside ComposeFlux container.
				continue
			}

			// Path is inside the Git repository clone directory — check if it exists on disk
			fi, err := os.Stat(dep)
			if errors.Is(err, os.ErrNotExist) && dep != defaultEnvPath {
				slog.Warn("Dependency path does not exist", "stack_name", project.Name, "path", dep)
			}

			if err == nil && fi.IsDir() {
				inRepoDirDeps = append(inRepoDirDeps, dep)
			} else {
				inRepoFileDeps = append(inRepoFileDeps, dep)
			}
		}

		var inRepoBuildContexts []string
		for _, ctxDir := range deps.BuildContexts {
			rel, err := filepath.Rel(repoPath, ctxDir)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+sep) {
				continue
			}

			if _, err := os.Stat(ctxDir); errors.Is(err, os.ErrNotExist) {
				slog.Warn("Build context directory does not exist", "stack_name", project.Name, "path", ctxDir)
			}

			inRepoBuildContexts = append(inRepoBuildContexts, ctxDir)
		}

		// Check if stack needs deployment
		stackInfo, exists := currentStackMap[project.Name]
		if !exists {
			slog.Info("New stack detected", "stack_name", project.Name)
			toDeploy[project.Name] = project
		} else if !stackInfo.Healthy && !stackInfo.Suspend {
			slog.Info("Unhealthy stack detected", "stack_name", project.Name)
			toDeploy[project.Name] = project
		} else if force && !stackInfo.Suspend {
			toDeploy[project.Name] = project
		} else if len(changedFiles) > 0 {
			// Stack is running, check if any changed file in git overlaps with stack's dependency tree
			hasMatch := false

			for changedPath := range changedPathMap {
				// 1. Check exact file or path equality match for file dependencies
				for _, dep := range inRepoFileDeps {
					if changedPath == dep {
						hasMatch = true
						slog.Debug("Changed dependency file detected in stack", "stack_name", project.Name, "file", changedPath, "dep", dep)
						break
					}
				}
				if hasMatch {
					break
				}

				// 2. Check directory bind mounts (pre-computed directory dependencies)
				for _, dirDep := range inRepoDirDeps {
					dirPrefix := dirDep
					if !strings.HasSuffix(dirPrefix, sep) {
						dirPrefix += sep
					}
					if changedPath == dirDep || strings.HasPrefix(changedPath, dirPrefix) {
						hasMatch = true
						slog.Debug("Changed file in volume directory detected in stack", "stack_name", project.Name, "file", changedPath, "dir", dirDep)
						break
					}
				}
				if hasMatch {
					break
				}

				// 3. Check recursive match for build contexts
				for _, ctxDir := range inRepoBuildContexts {
					ctxPrefix := ctxDir
					if !strings.HasSuffix(ctxPrefix, sep) {
						ctxPrefix += sep
					}

					if changedPath == ctxDir || strings.HasPrefix(changedPath, ctxPrefix) {
						hasMatch = true
						slog.Debug("Changed file in build context detected in stack", "stack_name", project.Name, "file", changedPath, "build_context", ctxDir)
						break
					}
				}
				if hasMatch {
					break
				}
			}
			if hasMatch {
				slog.Info("Changed stack detected", "stack_name", project.Name)
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

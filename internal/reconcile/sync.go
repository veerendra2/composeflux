// Sync changes from upstream Git repository and deploy updated/new stacks.
package reconcile

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/veerendra2/composeflux/pkg/dockercompose"
)

type loadedStack struct {
	project *types.Project
	build   bool
}

type syncState struct {
	repoPath          string
	currentStacks     StackStateMap
	changedPaths      map[string]struct{}
	force             bool
	sharedSecrets     map[string]*string
	sharedSecretFiles []string
	sharedSecretDir   string
}

// GitSync pulls changes from the Git repository and deploys stacks which are changed or new.
// If force is true, all non-suspended stacks are deployed regardless of git diff.
func (r *Reconciler) GitSync(ctx context.Context, force bool) error {
	r.reconcileMu.Lock()
	defer r.reconcileMu.Unlock()

	changedFiles, err := r.gClient.Pull(ctx)
	if err != nil {
		return err
	}
	repoPath, stackRoot, err := r.sourceRoots()
	if err != nil {
		return err
	}
	changedPaths := absolutePaths(repoPath, changedFiles)

	globalEnv, startupOrder, err := r.loadStackConfig(stackRoot)
	if err != nil {
		return err
	}
	composeCfgs, err := discoverComposeStacks(stackRoot, globalEnv)
	if err != nil {
		return err
	}
	warnMissingStartupOrder(stackRoot, startupOrder)

	currentStacks, err := r.getStackStates(ctx)
	if err != nil {
		return err
	}
	sharedSecrets, sharedSecretFiles, err := r.loadSharedSecrets(stackRoot)
	if err != nil {
		return err
	}
	var sharedSecretDir string
	if r.lClient != nil {
		sharedSecretDir = stackRoot
	}

	toDeploy, err := r.selectStacks(ctx, composeCfgs, syncState{
		repoPath:          repoPath,
		currentStacks:     currentStacks,
		changedPaths:      changedPaths,
		force:             force,
		sharedSecrets:     sharedSecrets,
		sharedSecretFiles: sharedSecretFiles,
		sharedSecretDir:   sharedSecretDir,
	})
	if err != nil {
		return err
	}
	r.deployStacks(ctx, toDeploy, deploymentOrder(toDeploy, startupOrder))

	clear(r.healthFailCounts)
	if err := r.PruneStacks(ctx, composeCfgs); err != nil {
		slog.Error("Failed to prune stacks", "error", err)
	}
	return nil
}

// selectStacks loads each source stack and selects those requiring deployment or rebuilding.
func (r *Reconciler) selectStacks(
	ctx context.Context,
	composeCfgs []dockercompose.ComposeConfig,
	state syncState,
) (map[string]loadedStack, error) {
	selected := make(map[string]loadedStack)
	for _, composeCfg := range composeCfgs {
		loaded, err := r.loadProjectWithSecrets(ctx, state.repoPath, composeCfg, state.sharedSecrets)
		if err != nil {
			if errors.Is(err, errLocalSecrets) {
				return nil, err
			}
			slog.Warn("Skipping, failed to load project with secrets", "path", composeCfg.WorkingDir, "error", err)
			continue
		}

		secretDirs := append([]string(nil), loaded.localSecretDirs...)
		if state.sharedSecretDir != "" {
			secretDirs = append(secretDirs, state.sharedSecretDir)
		}
		extraFiles := append(loaded.localSecretFiles, state.sharedSecretFiles...)
		extraFiles = append(extraFiles, loaded.sources.dependencyFiles...)
		extraFiles = append(extraFiles, loaded.sources.envFiles...)
		dependencies := buildStackDependencies(state.repoPath, loaded.project, extraFiles, secretDirs, loaded.sources.optionalFiles)
		if r.lClient != nil {
			dependencies.isLocalSecret = r.lClient.IsSecretFile
		}

		stackInfo, exists := state.currentStacks[loaded.project.Name]
		if exists && stackInfo.Suspend {
			slog.Debug("Skipping suspended stack", "stack_name", loaded.project.Name)
			continue
		}

		impact := changeImpact{}
		switch {
		case !exists:
			slog.Info("New stack detected", "stack_name", loaded.project.Name)
			impact = changeImpact{deploy: true, build: true}
		case !stackInfo.Healthy:
			slog.Info("Unhealthy stack detected", "stack_name", loaded.project.Name)
			impact = changeImpact{deploy: true, build: true}
		case state.force:
			impact = changeImpact{deploy: true, build: true}
		case len(state.changedPaths) > 0:
			impact = dependencies.impact(state.changedPaths)
			if impact.deploy {
				slog.Info("Changed stack detected", "stack_name", loaded.project.Name)
			}
		}
		if impact.deploy {
			selected[loaded.project.Name] = loadedStack{project: loaded.project, build: impact.build}
		}
	}
	return selected, nil
}

// deployStacks builds and deploys selected stacks in the supplied order.
func (r *Reconciler) deployStacks(ctx context.Context, stacks map[string]loadedStack, order []string) {
	if len(order) > 0 {
		slog.Info("Deploying stacks", "count", len(order), "order", strings.Join(order, ","))
	}
	for _, name := range order {
		stack := stacks[name]
		if stack.build {
			if err := r.dClient.Build(ctx, stack.project); err != nil {
				slog.Warn("Failed to build stack image, skipping deploy", "stack_name", name, "error", err)
				continue
			}
		}
		if err := r.Deploy(ctx, stack.project); err != nil {
			slog.Warn("Failed to deploy the stack", "stack_name", name, "error", err)
			continue
		}
		slog.Info("Successfully deployed the stack", "stack_name", name)
	}
}

// deploymentOrder places configured startup-order stacks before all remaining selections.
func deploymentOrder(stacks map[string]loadedStack, startupOrder []string) []string {
	order := make([]string, 0, len(stacks))
	added := make(map[string]struct{}, len(stacks))
	for _, name := range startupOrder {
		if _, exists := stacks[name]; exists {
			order = append(order, name)
			added[name] = struct{}{}
		}
	}
	for name := range stacks {
		if _, exists := added[name]; !exists {
			order = append(order, name)
		}
	}
	return order
}

// absolutePaths converts repository-relative Git paths into a deduplicated absolute set.
func absolutePaths(root string, paths []string) map[string]struct{} {
	absolute := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		absolute[filepath.Clean(filepath.Join(root, path))] = struct{}{}
	}
	return absolute
}

// warnMissingStartupOrder reports configured startup entries without source directories.
func warnMissingStartupOrder(stackRoot string, startupOrder []string) {
	for _, stackName := range startupOrder {
		path := filepath.Clean(filepath.Join(stackRoot, stackName))
		if !pathWithinRoot(stackRoot, path) {
			slog.Warn("Stack directory in startup_order is outside stack root", "startup_order_item", stackName)
			continue
		}
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			slog.Warn("Stack directory in startup_order not found", "startup_order_item", stackName, "expected_path", path)
		}
	}
}

// Sync changes from upstream Git repository and deploy updated/new stacks.
package reconcile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/veerendra2/composeflux/pkg/dockercompose"
	"github.com/veerendra2/composeflux/pkg/gitrepo"
)

type loadedStack struct {
	project *types.Project
	build   bool
}

type pendingGitSync struct {
	changes     []gitrepo.FileChange
	force       bool
	retryStacks map[string]bool
}

type syncState struct {
	repoPath          string
	configFile        string
	currentStacks     StackStateMap
	changedPaths      map[string]gitrepo.ChangeAction
	force             bool
	sharedSecrets     map[string]*string
	sharedSecretFiles []string
	sharedSecretDir   string
	retryStacks       map[string]bool
}

// GitSync pulls changes from the Git repository and deploys stacks which are changed or new.
// If force is true, all non-suspended stacks are deployed regardless of git diff.
func (r *Reconciler) GitSync(ctx context.Context, force bool) error {
	r.reconcileMu.Lock()
	defer r.reconcileMu.Unlock()

	if r.pendingGitSync == nil {
		r.pendingGitSync = &pendingGitSync{force: force}
	} else {
		r.pendingGitSync.force = r.pendingGitSync.force || force
		slog.Debug("Retrying pending Git reconciliation", "changed_files", len(r.pendingGitSync.changes), "force", r.pendingGitSync.force)
	}
	pending := r.pendingGitSync
	changedFiles, err := r.gClient.Pull(ctx)
	if err != nil {
		return err
	}
	pending.changes = mergeFileChanges(pending.changes, changedFiles)

	repoPath, stackRoot, err := r.sourceRoots()
	if err != nil {
		return err
	}
	changedPaths := absoluteChanges(repoPath, pending.changes)

	globalEnv, startupOrder, configFile, err := r.loadStackConfig(stackRoot)
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

	toDeploy, loadFailures, err := r.selectStacks(ctx, composeCfgs, syncState{
		repoPath:          repoPath,
		configFile:        configFile,
		currentStacks:     currentStacks,
		changedPaths:      changedPaths,
		force:             pending.force,
		sharedSecrets:     sharedSecrets,
		sharedSecretFiles: sharedSecretFiles,
		sharedSecretDir:   sharedSecretDir,
		retryStacks:       pending.retryStacks,
	})
	if err != nil {
		slog.Warn("Stack selection aborted, no deployments attempted", "load_failed", loadFailures, "error", err)
		return err
	}
	failedStacks, err := r.deployStacks(ctx, toDeploy, deploymentOrder(toDeploy, startupOrder))
	level := slog.LevelInfo
	if loadFailures > 0 || len(failedStacks) > 0 {
		level = slog.LevelWarn
	}
	slog.Log(ctx, level, "Stack deployment summary",
		"deployed", len(toDeploy)-len(failedStacks),
		"deploy_failed", len(failedStacks),
		"load_failed", loadFailures,
		"skipped", len(composeCfgs)-len(toDeploy)-loadFailures,
	)
	pending.changes = nil
	pending.force = false
	pending.retryStacks = failedStacks
	if err != nil {
		return err
	}

	if err := r.PruneStacks(ctx, composeCfgs); err != nil {
		return fmt.Errorf("failed to prune stacks: %w", err)
	}
	clear(r.healthFailCounts)
	r.pendingGitSync = nil
	return nil
}

// hasPendingGitSync reports whether a pulled change set still requires successful reconciliation.
func (r *Reconciler) hasPendingGitSync() bool {
	r.reconcileMu.Lock()
	defer r.reconcileMu.Unlock()
	return r.pendingGitSync != nil
}

// selectStacks loads each source stack and selects those requiring deployment or rebuilding.
func (r *Reconciler) selectStacks(
	ctx context.Context,
	composeCfgs []dockercompose.ComposeConfig,
	state syncState,
) (map[string]loadedStack, int, error) {
	selected := make(map[string]loadedStack)
	loadFailures := 0
	for _, composeCfg := range composeCfgs {
		loaded, err := r.loadProjectWithSecrets(ctx, state.repoPath, composeCfg, state.sharedSecrets)
		if err != nil {
			loadFailures++
			if errors.Is(err, errLocalSecrets) {
				return nil, loadFailures, err
			}
			slog.Warn("Skipping, failed to load project with secrets", "path", composeCfg.WorkingDir, "error", err)
			continue
		}

		if hasProjectLabel(loaded.project, LabelSuspend) {
			slog.Debug("Skipping suspended stack", "stack_name", loaded.project.Name)
			continue
		}

		secretDirs := append([]string(nil), loaded.localSecretDirs...)
		if state.sharedSecretDir != "" {
			secretDirs = append(secretDirs, state.sharedSecretDir)
		}
		extraFiles := append(loaded.localSecretFiles, state.sharedSecretFiles...)
		extraFiles = append(extraFiles, loaded.sources.dependencyFiles...)
		extraFiles = append(extraFiles, loaded.sources.envFiles...)
		extraFiles = append(extraFiles, state.configFile)
		dependencies := buildStackDependencies(state.repoPath, loaded.project, extraFiles, secretDirs, loaded.sources.optionalFiles)
		if r.lClient != nil {
			dependencies.isLocalSecret = r.lClient.IsSecretFile
		}

		stackInfo, exists := state.currentStacks[loaded.project.Name]

		retryBuild, retry := state.retryStacks[loaded.project.Name]
		impact := changeImpact{}
		switch {
		case retry:
			slog.Debug("Retrying failed stack", "stack_name", loaded.project.Name, "rebuild", retryBuild)
			impact = changeImpact{deploy: true, build: retryBuild || state.force}
		case !exists:
			slog.Info("New stack detected", "stack_name", loaded.project.Name)
			impact = changeImpact{deploy: true, build: true}
		case !stackInfo.Healthy:
			slog.Info("Unhealthy stack detected", "stack_name", loaded.project.Name)
			impact = changeImpact{deploy: true, build: true}
		case state.force:
			slog.Debug("Stack selected by force sync", "stack_name", loaded.project.Name)
			impact = changeImpact{deploy: true, build: true}
		}
		if len(state.changedPaths) > 0 && (retry || !impact.deploy) {
			changedImpact := dependencies.impact(state.changedPaths)
			if changedImpact.deploy {
				for _, match := range changedImpact.matches {
					path := match.path
					if relativePath, err := filepath.Rel(state.repoPath, path); err == nil {
						path = filepath.ToSlash(relativePath)
					}
					slog.Debug("Git change affects stack",
						"stack_name", loaded.project.Name,
						"action", string(match.action),
						"path", path,
						"reason", match.reason,
						"rebuild", match.rebuild,
					)
				}
				slog.Info("Changed stack detected", "stack_name", loaded.project.Name)
				impact.deploy = true
				impact.build = impact.build || changedImpact.build
			}
		}
		if impact.deploy {
			selected[loaded.project.Name] = loadedStack{project: loaded.project, build: impact.build}
		}
	}
	return selected, loadFailures, nil
}

// deployStacks builds and deploys selected stacks, returning failed stacks for targeted retries.
func (r *Reconciler) deployStacks(ctx context.Context, stacks map[string]loadedStack, order []string) (map[string]bool, error) {
	if len(order) > 0 {
		slog.Info("Deploying stacks", "count", len(order), "order", strings.Join(order, ","))
	}
	failedStacks := make(map[string]bool)
	var deployErrors []error
	for _, name := range order {
		stack := stacks[name]
		if stack.build {
			if err := r.dClient.Build(ctx, stack.project); err != nil {
				slog.Warn("Failed to build stack image, skipping deploy", "stack_name", name, "error", err)
				failedStacks[name] = true
				deployErrors = append(deployErrors, fmt.Errorf("failed to build stack %s: %w", name, err))
				continue
			}
		}
		if err := r.Deploy(ctx, stack.project); err != nil {
			slog.Warn("Failed to deploy the stack", "stack_name", name, "error", err)
			failedStacks[name] = false
			deployErrors = append(deployErrors, fmt.Errorf("failed to deploy stack %s: %w", name, err))
			continue
		}
		slog.Info("Successfully deployed the stack", "stack_name", name)
	}
	return failedStacks, errors.Join(deployErrors...)
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

// absoluteChanges converts repository-relative Git changes into an absolute path map.
func absoluteChanges(root string, changes []gitrepo.FileChange) map[string]gitrepo.ChangeAction {
	absolute := make(map[string]gitrepo.ChangeAction, len(changes))
	for _, change := range changes {
		absolute[filepath.Clean(filepath.Join(root, change.Path))] = change.Action
	}
	return absolute
}

// mergeFileChanges coalesces pending and newly pulled changes by path, keeping the latest action.
func mergeFileChanges(pending, incoming []gitrepo.FileChange) []gitrepo.FileChange {
	actions := make(map[string]gitrepo.ChangeAction, len(pending)+len(incoming))
	for _, change := range pending {
		actions[change.Path] = change.Action
	}
	for _, change := range incoming {
		actions[change.Path] = change.Action
	}

	paths := make([]string, 0, len(actions))
	for path := range actions {
		paths = append(paths, path)
	}
	slices.Sort(paths)

	changes := make([]gitrepo.FileChange, 0, len(paths))
	for _, path := range paths {
		changes = append(changes, gitrepo.FileChange{Path: path, Action: actions[path]})
	}
	return changes
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

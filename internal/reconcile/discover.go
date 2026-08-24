package reconcile

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/veerendra2/composeflux/pkg/dockercompose"
)

const (
	StateRunning = "running"
	StateExited  = "exited"
	Unhealthy    = "unhealthy"
)

var (
	defaultFileNames         = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}
	defaultOverrideFileNames = []string{"compose.override.yml", "compose.override.yaml", "docker-compose.override.yml", "docker-compose.override.yaml"}
)

type StackStateMap map[string]StackInfo

type StackInfo struct {
	Healthy bool
	Suspend bool
}

// buildComposeConfig builds `dockercompose.ComposeConfig` for given directory if compose files exists
func (r *Reconciler) buildComposeConfig(dirPath string, envs []string) (dockercompose.ComposeConfig, error) {
	// Find compose files
	composeFilePaths := findExistingFiles(dirPath, defaultFileNames)
	if len(composeFilePaths) == 0 {
		return dockercompose.ComposeConfig{}, fmt.Errorf("no compose files found in directory %s", dirPath)
	}

	// Add override files to compose files
	composeFilePaths = append(composeFilePaths, findExistingFiles(dirPath, defaultOverrideFileNames)...)

	return dockercompose.ComposeConfig{
		ComposeFiles: composeFilePaths,
		WorkingDir:   dirPath,
		Env:          envs,
	}, nil
}

// discoverComposeStack finds the directories containing docker compose files
func (r *Reconciler) discoverComposeStack(envs []string) ([]dockercompose.ComposeConfig, error) {
	// Read all entries in the stacks directory
	stackFullPath := filepath.Join(r.gClient.Path(), r.stackPath)
	entries, err := os.ReadDir(stackFullPath)
	if err != nil {
		return nil, err
	}

	var stacks []dockercompose.ComposeConfig

	for _, entry := range entries {
		// Skip files, only process directories
		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(stackFullPath, entry.Name())

		// Build compose configuration from the directory
		composeCfg, err := r.buildComposeConfig(dirPath, envs)
		if err != nil {
			slog.Warn("Ignoring directory without valid compose files", "stack_dir_name", entry.Name(), "error", err)
			continue
		}

		stacks = append(stacks, composeCfg)
	}

	return stacks, nil
}

// getStackStates returns a StackStateMap keyed by stack name containing each stack's health and suspend info
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

		// Ignore the stack if it's not managed by composeflux.
		if !isManagedStack(containers) {
			continue
		}

		stackHealthy := true
		stackSuspend := false
		for _, container := range containers {
			if !isContainerHealthy(container) {
				slog.Debug("Container is not healthy", "stack_name", stack.Name, "container", container.Name,
					"exit_code", container.ExitCode, "status", container.State, "container_health", container.Health,
				)
				stackHealthy = false
			}
			if container.Labels[LabelSuspend] == ValueTrue {
				stackSuspend = true
			}
		}

		stackStateMap[stack.Name] = StackInfo{
			Healthy: stackHealthy,
			Suspend: stackSuspend,
		}
	}
	return stackStateMap, nil
}

// findExistingFiles finds files in given directory and returns slice of matched files
func findExistingFiles(dirPath string, fileNames []string) []string {
	var found []string
	for _, fileName := range fileNames {
		fullPath := filepath.Join(dirPath, fileName)
		if _, err := os.Stat(fullPath); err == nil {
			found = append(found, fullPath)
		}
	}
	return found
}

// isManagedStack checks if the stack is managed by composeflux via container labels.
func isManagedStack(containers []api.ContainerSummary) bool {
	return len(containers) > 0 && containers[0].Labels != nil && containers[0].Labels[LabelManaged] == ValueTrue
}

func isContainerHealthy(container api.ContainerSummary) bool {
	if container.ExitCode == 0 && container.State == StateRunning {
		return container.Health != Unhealthy
	} else if container.Labels[LabelInit] == ValueTrue && container.ExitCode == 0 && container.State == StateExited {
		return true
	}

	return false
}

// loadSharedSecrets gathers shared secrets from:
// 1. External secrets manager (Bitwarden / Infisical), if configured
//    DEPRECATED: External secrets manager integration will be removed in a future release.
// 2. Root *.age files in the stacks directory
// Root *.age secrets override external secrets manager values on collision.
// Returns the merged secret map and the list of root *.age file paths for change tracking.
func (r *Reconciler) loadSharedSecrets() (map[string]*string, []string, error) {
	sharedSecrets := make(map[string]*string)

	// 1. Fetch from external secrets manager if configured (deprecated)
	if r.sClient != nil {
		secrets, err := r.sClient.FetchAll()
		if err != nil {
			slog.Warn("Failed to fetch external secrets, continuing with root age secrets", "error", err)
		} else {
			for _, s := range secrets {
				val := s.Value
				sharedSecrets[s.Key] = &val
			}
		}
	}

	// 2. Discover and decrypt root *.age files
	stacksRootDir := filepath.Join(r.gClient.Path(), r.stackPath)
	mergedSecrets, rootAgeFiles, err := r.decryptAgeEnvs(stacksRootDir, sharedSecrets)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decrypt root shared age secrets in %s: %w", stacksRootDir, err)
	}

	return mergedSecrets, rootAgeFiles, nil
}

// decryptAgeEnvs scans dir for *.age files, decrypts each, and merges results
// into base (which may be nil). Returns a new map and the discovered *.age file paths.
// Stack-specific keys override base keys.
func (r *Reconciler) decryptAgeEnvs(dir string, base map[string]*string) (map[string]*string, []string, error) {
	result := make(map[string]*string, len(base))
	maps.Copy(result, base)

	if r.ageClient == nil {
		return result, nil, nil
	}

	files, err := r.ageClient.FindAgeFiles(dir)
	if err != nil {
		return result, nil, err
	}

	for _, ageFile := range files {
		envs, err := r.ageClient.DecryptEnvFile(ageFile)
		if err != nil {
			return nil, nil, err
		}
		maps.Copy(result, envs)
	}

	if len(files) > 0 {
		slog.Debug("Loaded age secret files for directory", "dir", dir, "files_count", len(files), "total_keys", len(result))
	}

	return result, files, nil
}

// loadProjectWithSecrets loads a compose project with decrypted age and shared secrets
// populated into the compose environment for ${VAR} interpolation.
// It also returns all discovered *.age files for the project.
func (r *Reconciler) loadProjectWithSecrets(ctx context.Context, composeCfg dockercompose.ComposeConfig, sharedAgeEnvs map[string]*string) (*types.Project, []string, error) {
	stackAgeEnvs := make(map[string]*string)
	maps.Copy(stackAgeEnvs, sharedAgeEnvs)

	var allStackAgeFiles []string
	seenDirs := make(map[string]struct{})

	// Scan stack working directory first
	stackAgeEnvs, stackAgeFiles, err := r.decryptAgeEnvs(composeCfg.WorkingDir, stackAgeEnvs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to process age files in %s: %w", composeCfg.WorkingDir, err)
	}
	seenDirs[composeCfg.WorkingDir] = struct{}{}
	allStackAgeFiles = append(allStackAgeFiles, stackAgeFiles...)

	buildEnvList := func() []string {
		var envList []string
		envList = append(envList, composeCfg.Env...)
		for k, v := range stackAgeEnvs {
			if v != nil {
				envList = append(envList, fmt.Sprintf("%s=%s", k, *v))
			}
		}
		return envList
	}

	initialCfg := composeCfg
	initialCfg.Env = buildEnvList()

	project, err := r.dClient.LoadProject(ctx, initialCfg)
	if err != nil {
		return nil, nil, err
	}

	// Scan included compose file directories for additional age secret files
	var hasIncludedAgeFiles bool
	for _, composeFile := range project.ComposeFiles {
		if !filepath.IsAbs(composeFile) {
			composeFile = filepath.Join(project.WorkingDir, composeFile)
		}
		dir := filepath.Dir(filepath.Clean(composeFile))
		if _, seen := seenDirs[dir]; seen {
			continue
		}
		seenDirs[dir] = struct{}{}
		var ageFiles []string
		stackAgeEnvs, ageFiles, err = r.decryptAgeEnvs(dir, stackAgeEnvs)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to process age files in %s: %w", dir, err)
		}
		if len(ageFiles) > 0 {
			hasIncludedAgeFiles = true
			allStackAgeFiles = append(allStackAgeFiles, ageFiles...)
		}
	}

	// If included directories provided new secrets, reload the project so included compose files can interpolate them
	if hasIncludedAgeFiles {
		finalCfg := composeCfg
		finalCfg.Env = buildEnvList()
		project, err = r.dClient.LoadProject(ctx, finalCfg)
		if err != nil {
			return nil, nil, err
		}
	}

	return project, allStackAgeFiles, nil
}

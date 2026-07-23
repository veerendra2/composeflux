package reconcile

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

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
	Hash    string
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

		// Ignore the stack if it's not managed by composeflux.
		// isManagedStack guarantees len(containers) > 0, so containers[0] below is safe.
		if !isManagedStack(containers) {
			continue
		}

		containerHash := ""
		if hash, ok := containers[0].Labels[LabelStackHash]; ok {
			containerHash = hash
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
			Hash:    containerHash,
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

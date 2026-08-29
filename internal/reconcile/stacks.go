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
	Healthy bool
	Suspend bool
}

// buildComposeConfig discovers the primary and override Compose files in a stack directory.
func buildComposeConfig(dir string, env []string) (dockercompose.ComposeConfig, error) {
	composeFiles := findExistingFiles(dir, defaultFileNames)
	if len(composeFiles) == 0 {
		return dockercompose.ComposeConfig{}, fmt.Errorf("no compose files found in directory %s", dir)
	}
	composeFiles = append(composeFiles, findExistingFiles(dir, defaultOverrideFileNames)...)
	return dockercompose.ComposeConfig{ComposeFiles: composeFiles, WorkingDir: dir, Env: env}, nil
}

// discoverComposeStacks builds configurations for valid stack directories beneath stackRoot.
func discoverComposeStacks(stackRoot string, env []string) ([]dockercompose.ComposeConfig, error) {
	entries, err := os.ReadDir(stackRoot)
	if err != nil {
		return nil, err
	}

	var stacks []dockercompose.ComposeConfig
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		stackDir := filepath.Join(stackRoot, entry.Name())
		composeCfg, err := buildComposeConfig(stackDir, env)
		if err != nil {
			slog.Warn("Ignoring directory without valid compose files", "stack_dir_name", entry.Name(), "error", err)
			continue
		}
		stacks = append(stacks, composeCfg)
	}
	return stacks, nil
}

// getStackStates reports health and suspension state for managed Docker Compose stacks.
func (r *Reconciler) getStackStates(ctx context.Context) (StackStateMap, error) {
	states := make(StackStateMap)
	stacks, err := r.dClient.List(ctx)
	if err != nil {
		return states, err
	}
	for _, stack := range stacks {
		containers, err := r.dClient.Ps(ctx, stack.Name)
		if err != nil {
			slog.Error("Failed to list containers for stack", "stack_name", stack.Name, "error", err)
			continue
		}
		if !isManagedStack(containers) {
			continue
		}

		info := StackInfo{Healthy: true}
		for _, container := range containers {
			if !isContainerHealthy(container) {
				slog.Debug("Container is not healthy", "stack_name", stack.Name, "container", container.Name, "exit_code", container.ExitCode, "status", container.State, "container_health", container.Health)
				info.Healthy = false
			}
			if container.Labels[LabelSuspend] == ValueTrue {
				info.Suspend = true
			}
		}
		states[stack.Name] = info
	}
	return states, nil
}

// findExistingFiles returns candidate files that exist directly under a directory.
func findExistingFiles(dir string, names []string) []string {
	var files []string
	for _, name := range names {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			files = append(files, path)
		}
	}
	return files
}

// isManagedStack reports whether the stack carries the ComposeFlux management label.
func isManagedStack(containers []api.ContainerSummary) bool {
	return len(containers) > 0 && containers[0].Labels != nil && containers[0].Labels[LabelManaged] == ValueTrue
}

// isContainerHealthy accepts running healthy containers and successful init containers.
func isContainerHealthy(container api.ContainerSummary) bool {
	if container.ExitCode == 0 && container.State == StateRunning {
		return container.Health != Unhealthy
	}
	return container.Labels[LabelInit] == ValueTrue && container.ExitCode == 0 && container.State == StateExited
}

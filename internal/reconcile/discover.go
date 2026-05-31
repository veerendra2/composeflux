package reconcile

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/veerendra2/composeflux/pkg/dockercompose"
)

var (
	defaultFileNames         = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}
	defaultOverrideFileNames = []string{"compose.override.yml", "compose.override.yaml", "docker-compose.override.yml", "docker-compose.override.yaml"}
)

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

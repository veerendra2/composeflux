package reconcile

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/veerendra2/composeflux/pkg/dockercompose"
)

var (
	defaultFileNames         = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}
	defaultOverrideFileNames = []string{"compose.override.yml", "compose.override.yaml", "docker-compose.override.yml", "docker-compose.override.yaml"}
)

type StackStateMap map[string]StackInfo

type StackInfo struct {
	Hash string
}

// projectChecksum computes sha256 of docker compose yaml content
func projectChecksum(project *types.Project) (string, error) {
	content, err := project.MarshalYAML()
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(content)
	return fmt.Sprintf("sha256:%x", hash), nil
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

// buildComposeConfig builds `dockercompose.ComposeConfig` for given directory if compose files exists
func (r *Reconciler) buildComposeConfig(dirPath string) (dockercompose.ComposeConfig, error) {
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
		Env:          r.CacheGet(),
	}, nil
}

// DiscoverComposeStack find the directory contains docker compose files
func (r *Reconciler) discoverComposeStack() ([]dockercompose.ComposeConfig, error) {
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
		composeCfg, err := r.buildComposeConfig(dirPath)
		if err != nil {
			slog.Warn("Ignoring directory without valid compose files", "stack_dir_name", entry.Name(), "error", err)
			continue
		}

		stacks = append(stacks, composeCfg)
	}

	return stacks, nil
}

// loadCache loads secrets and env vars from the stack config into the cache.
// Returns the parsed StackConfig (may be nil if config file is absent).
func (r *Reconciler) loadCache() (*StackConfig, error) {
	configPath := filepath.Join(r.gClient.Path(), r.stackPath, r.configFile)
	var cfg *StackConfig

	if _, err := os.Stat(configPath); err == nil {
		cfg, err = Load(configPath)
		if err != nil {
			return nil, err
		}
	}

	if err := r.CacheLoadSecrets(); err != nil {
		return nil, err
	}

	if cfg != nil && len(cfg.Envs) > 0 {
		slog.Info("Adding env vars to cache", "count", len(cfg.Envs))
		r.CacheSet(cfg.Envs)
	}

	return cfg, nil
}

// getStackStates return StackStateMap which contains the stack hash
func (r *Reconciler) getStackStates(ctx context.Context) (StackStateMap, error) {
	stackStateMap := make(StackStateMap)
	stacks, err := r.dClient.List(ctx)
	if err != nil {
		return stackStateMap, err
	}

	for _, stack := range stacks {
		containers, err := r.dClient.Ps(ctx, stack.Name)
		if err != nil {
			slog.Error("Failed to get stack", "error", err)
			continue
		}

		// Ignore the stack if any container doesn't have managed label
		if len(containers) == 0 || containers[0].Labels[LabelManagedBy] == "" {
			continue
		}

		containerHash := ""
		if hash, ok := containers[0].Labels[LabelStackHash]; ok {
			containerHash = hash
		}

		stackStateMap[stack.Name] = StackInfo{
			Hash: containerHash,
		}
	}
	return stackStateMap, nil
}

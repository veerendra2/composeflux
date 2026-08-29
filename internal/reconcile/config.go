package reconcile

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v4"
)

type StackConfig struct {
	StartupOrder []string          `yaml:"startup_order"`
	Envs         map[string]string `yaml:"envs"`
}

// Load reads and parses a stack configuration file.
func Load(path string) (*StackConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg StackConfig
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// sourceRoots returns canonical repository and stack roots after containment validation.
func (r *Reconciler) sourceRoots() (string, string, error) {
	repoPath, err := resolvePathWithinRoot(r.gClient.Path(), r.gClient.Path())
	if err != nil {
		return "", "", fmt.Errorf("invalid repository path: %w", err)
	}
	stackPath, err := resolvePathWithinRoot(repoPath, filepath.Join(repoPath, r.stackPath))
	if err != nil {
		return "", "", fmt.Errorf("invalid stack path: %w", err)
	}
	return repoPath, stackPath, nil
}

// stackConfigPath resolves the configured path beneath the stack root.
func (r *Reconciler) stackConfigPath(stackRoot string) (string, error) {
	path, err := filepath.Abs(filepath.Clean(filepath.Join(stackRoot, r.configFile)))
	if err != nil {
		return "", err
	}
	if !pathWithinRoot(stackRoot, path) {
		return "", fmt.Errorf("config path %s is outside stack root %s", path, stackRoot)
	}
	parent, err := resolvePathWithinRoot(stackRoot, filepath.Dir(path))
	if err != nil {
		return "", fmt.Errorf("invalid config directory: %w", err)
	}
	path = filepath.Join(parent, filepath.Base(path))
	if _, err = os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return path, nil
	} else if err != nil {
		return "", err
	}
	return resolvePathWithinRoot(stackRoot, path)
}

// loadStackConfig loads stack.yml for startup_order and global environment variables statelessly.
func (r *Reconciler) loadStackConfig(stackRoot string) ([]string, []string, error) {
	var envs []string
	var startupOrder []string

	configPath, err := r.stackConfigPath(stackRoot)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := Load(configPath)
	if err != nil {
		slog.Warn("Failed to load stack config", "path", configPath, "error", err)
	} else {
		for key, value := range cfg.Envs {
			envs = append(envs, fmt.Sprintf("%s=%s", key, value))
		}
		startupOrder = cfg.StartupOrder
	}

	return envs, startupOrder, nil
}

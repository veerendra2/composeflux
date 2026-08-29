package reconcile

import (
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

// loadStackConfig loads stack.yml for startup_order and global environment variables statelessly.
func (r *Reconciler) loadStackConfig() ([]string, []string, error) {
	var envs []string
	var startupOrder []string

	configPath := filepath.Join(r.gClient.Path(), r.stackPath, r.configFile)
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

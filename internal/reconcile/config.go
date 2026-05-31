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

// loadEnvAndConfig loads secrets and environment variables from stack.yml statelessly.
func (r *Reconciler) loadEnvAndConfig() ([]string, []string, error) {
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

	if r.sClient != nil {
		secrets, err := r.sClient.FetchAll()
		if err != nil {
			return envs, startupOrder, err
		}

		for _, secret := range secrets {
			envs = append(envs, fmt.Sprintf("%s=%s", secret.Key, secret.Value))
		}
	}

	return envs, startupOrder, nil
}

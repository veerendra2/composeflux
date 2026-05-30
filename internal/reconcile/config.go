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
func (r *Reconciler) loadEnvAndConfig() ([]string, *StackConfig, error) {
	configPath := filepath.Join(r.gClient.Path(), r.stackPath, r.configFile)
	cfg, err := Load(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("Failed to load stack config", "path", configPath, "error", err)
		}
	}

	var envs []string
	if r.sClient != nil {
		secrets, err := r.sClient.FetchAll()
		if err != nil {
			return nil, nil, err
		}
		slog.Debug("Fetched secrets from secrets manager", "count", len(secrets))
		envs = make([]string, 0, len(secrets))
		for _, secret := range secrets {
			envs = append(envs, fmt.Sprintf("%s=%s", secret.Key, secret.Value))
		}
	}

	if cfg != nil && len(cfg.Envs) > 0 {
		slog.Debug("Adding env vars from stack config", "count", len(cfg.Envs))
		if envs == nil {
			envs = make([]string, 0, len(cfg.Envs))
		}
		for key, value := range cfg.Envs {
			envs = append(envs, fmt.Sprintf("%s=%s", key, value))
		}
	}

	return envs, cfg, nil
}

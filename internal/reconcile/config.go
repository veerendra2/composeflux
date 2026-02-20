package reconcile

import (
	"os"

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

package secrets

import (
	"context"
	"fmt"
)

type Config struct {
	BitwardenConfig `prefix:"bitwarden-" envprefix:"BITWARDEN_" embed:""`
	InfisicalConfig `prefix:"infisical-" envprefix:"INFISICAL_" embed:""`
}

type Secret struct {
	Key   string
	Value string
}

type SecretsCollection struct {
	Secrets []Secret
}

type Client interface {
	Get(id string) (string, error)
	FetchAll() (*SecretsCollection, error)
	Close()
}

// New creates a secrets client based on the provider type
func New(ctx context.Context, provider string, cfg Config) (Client, error) {
	switch provider {
	case "bitwarden":
		return NewBitwardenClient(cfg.BitwardenConfig)
	case "infisical":
		return NewInfisicalClient(ctx, cfg.InfisicalConfig)
	default:
		return nil, fmt.Errorf("unsupported secrets provider: %s", provider)
	}
}

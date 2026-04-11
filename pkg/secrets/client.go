package secrets

import (
	"context"
	"fmt"
)

type Config struct {
	Provider string `name:"secrets-provider" enum:",bitwarden,infisical" env:"SECRETS_PROVIDER" default:"" help:"Secrets manager provider to use (bitwarden or infisical)"`

	Bitwarden BitwardenConfig `embed:"" prefix:"bitwarden-" envprefix:"BITWARDEN_" group:"Bitwarden Options:"`
	Infisical InfisicalConfig `embed:"" prefix:"infisical-" envprefix:"INFISICAL_" group:"Infisical Options:"`
}

type Secret struct {
	Key   string
	Value string
}

type Client interface {
	Get(id string) (string, error)
	FetchAll() ([]Secret, error)
	Close()
}

// New creates a secrets client based on the provider type.
// Returns nil, nil when no provider is configured.
func New(ctx context.Context, cfg Config) (Client, error) {
	switch cfg.Provider {
	case "":
		return nil, nil
	case "bitwarden":
		return NewBitwardenClient(cfg.Bitwarden)
	case "infisical":
		return NewInfisicalClient(ctx, cfg.Infisical)
	default:
		return nil, fmt.Errorf("unsupported secrets provider: %s", cfg.Provider)
	}
}

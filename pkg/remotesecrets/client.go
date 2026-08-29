package remotesecrets

import (
	"context"
	"fmt"
)

type Config struct {
	Provider string `name:"remote-secrets-provider" enum:",bitwarden,infisical" env:"REMOTE_SECRETS_PROVIDER" default:"" help:"Remote secrets provider to use (bitwarden or infisical)"`

	Bitwarden BitwardenConfig `embed:"" prefix:"bitwarden-" envprefix:"BITWARDEN_" group:"Bitwarden Options:"`
	Infisical InfisicalConfig `embed:"" prefix:"infisical-" envprefix:"INFISICAL_" group:"Infisical Options:"`
}

type Client interface {
	// FetchAll retrieves all secrets from the remote provider.
	FetchAll() (map[string]*string, error)
	// Get retrieves one secret by key or ID.
	Get(id string) (string, error)
	Close()
}

// New creates a remote secrets client based on the provider type.
// It returns nil, nil when no provider is configured.
func New(ctx context.Context, cfg Config) (Client, error) {
	switch cfg.Provider {
	case "":
		return nil, nil
	case "bitwarden":
		return NewBitwardenClient(cfg.Bitwarden)
	case "infisical":
		return NewInfisicalClient(ctx, cfg.Infisical)
	default:
		return nil, fmt.Errorf("unsupported remote secrets provider: %s", cfg.Provider)
	}
}

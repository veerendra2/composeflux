package remotesecrets

import (
	"context"
	"fmt"
)

type Config struct {
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

// Configured reports whether any remote secrets provider configuration is present.
func (c Config) Configured() bool {
	return c.Bitwarden.configured() || c.Infisical.configured()
}

// Provider returns and validates the configured remote secrets provider.
func (c Config) Provider() (string, error) {
	bitwardenConfigured := c.Bitwarden.configured()
	infisicalConfigured := c.Infisical.configured()

	if bitwardenConfigured && infisicalConfigured {
		return "", fmt.Errorf("multiple remote secrets providers configured: bitwarden and infisical")
	}
	if bitwardenConfigured {
		if err := c.Bitwarden.validate(); err != nil {
			return "", err
		}
		return "bitwarden", nil
	}
	if infisicalConfigured {
		if err := c.Infisical.validate(); err != nil {
			return "", err
		}
		return "infisical", nil
	}
	return "", nil
}

// New creates a remote secrets client based on the configured provider.
// It returns nil, nil when no provider is configured.
func New(ctx context.Context, cfg Config) (Client, error) {
	provider, err := cfg.Provider()
	if err != nil {
		return nil, err
	}

	switch provider {
	case "":
		return nil, nil
	case "bitwarden":
		return NewBitwardenClient(cfg.Bitwarden)
	case "infisical":
		return NewInfisicalClient(ctx, cfg.Infisical)
	default:
		return nil, fmt.Errorf("unsupported remote secrets provider: %s", provider)
	}
}

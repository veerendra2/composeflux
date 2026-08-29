package remotesecrets

import (
	"fmt"

	"github.com/bitwarden/sdk-go/v2"
)

const (
	defaultBitwardenAPIURL      = "https://vault.bitwarden.com/api"
	defaultBitwardenIdentityURL = "https://vault.bitwarden.com/identity"
)

type BitwardenConfig struct {
	ApiURL      string `name:"api-url" help:"API URL" env:"API_URL" default:"https://vault.bitwarden.com/api"`
	IdentityURL string `name:"identity-url" help:"Identity URL" env:"IDENTITY_URL" default:"https://vault.bitwarden.com/identity"`
	AccessToken string `name:"access-token" help:"Access token" env:"ACCESS_TOKEN" and:"bitwarden" xor:"remote-provider"`
	OrgID       string `name:"organization-id" help:"Organization ID" env:"ORGANIZATION_ID" and:"bitwarden"`
	ProjectID   string `name:"project-id" help:"Project ID" env:"PROJECT_ID" and:"bitwarden"`
}

// configured reports whether any Bitwarden-specific configuration was supplied.
func (c BitwardenConfig) configured() bool {
	return c.AccessToken != "" || c.OrgID != "" || c.ProjectID != "" ||
		(c.ApiURL != "" && c.ApiURL != defaultBitwardenAPIURL) ||
		(c.IdentityURL != "" && c.IdentityURL != defaultBitwardenIdentityURL)
}

// validate checks that all required Bitwarden credentials are configured.
func (c BitwardenConfig) validate() error {
	if c.AccessToken == "" || c.OrgID == "" || c.ProjectID == "" {
		return fmt.Errorf("bitwarden provider requires: --bitwarden-access-token, " +
			"--bitwarden-organization-id, --bitwarden-project-id")
	}
	return nil
}

type bitwardenClient struct {
	organizationID string
	projectID      string

	bwClient sdk.BitwardenClientInterface
}

// FetchAll retrieves all secrets.
func (c *bitwardenClient) FetchAll() (map[string]*string, error) {
	resp, err := c.bwClient.Secrets().Sync(c.organizationID, nil)
	if err != nil {
		return nil, err
	}

	secrets := make(map[string]*string)
	for _, secret := range resp.Secrets {
		if secret.ProjectID != nil && *secret.ProjectID == c.projectID {
			value := secret.Value
			secrets[secret.Key] = &value
		}
	}

	return secrets, nil
}

// Get retrieves a secret value by secret ID.
func (c *bitwardenClient) Get(id string) (string, error) {
	secret, err := c.bwClient.Secrets().Get(id)
	if err != nil {
		return "", err
	}
	return secret.Value, nil
}

// Close cleans up resources.
func (c *bitwardenClient) Close() {
	c.bwClient.Close()
}

// NewBitwardenClient authenticates and returns a Bitwarden secrets client.
func NewBitwardenClient(cfg BitwardenConfig) (Client, error) {
	apiEndpoint := cfg.ApiURL
	if apiEndpoint == "" {
		apiEndpoint = defaultBitwardenAPIURL
	}
	identityEndpoint := cfg.IdentityURL
	if identityEndpoint == "" {
		identityEndpoint = defaultBitwardenIdentityURL
	}

	bwClient, err := sdk.NewBitwardenClient(&apiEndpoint, &identityEndpoint)
	if err != nil {
		return nil, err
	}

	if err := bwClient.AccessTokenLogin(cfg.AccessToken, nil); err != nil {
		bwClient.Close()
		return nil, err
	}

	return &bitwardenClient{
		organizationID: cfg.OrgID,
		projectID:      cfg.ProjectID,
		bwClient:       bwClient,
	}, nil
}

package secrets

import (
	"github.com/bitwarden/sdk-go/v2"
)

type BitwardenConfig struct {
	ApiURL      string `name:"api-url" help:"API URL" env:"API_URL" default:"https://vault.bitwarden.com/api"`
	IdentityURL string `name:"identity-url" help:"Identity URL" env:"IDENTITY_URL" default:"https://vault.bitwarden.com/identity"`
	AccessToken string `name:"access-token" help:"Access token" env:"ACCESS_TOKEN"`
	OrgID     string `name:"organization-id" help:"Organization ID" env:"ORGANIZATION_ID"`
	ProjectID string `name:"project-id" help:"Project ID" env:"PROJECT_ID"`
}

type bitwardenClient struct {
	organizationID string
	projectID      string

	bwClient sdk.BitwardenClientInterface
}

// FetchAll retrieves all secrets.
func (c *bitwardenClient) FetchAll() ([]Secret, error) {
	resp, err := c.bwClient.Secrets().Sync(c.organizationID, nil)
	if err != nil {
		return nil, err
	}

	// Convert to our Secret format
	var secrets []Secret
	for _, bwSecret := range resp.Secrets {
		// Filter secrets by project ID
		// Note: Sync() already returns only secrets the access token has permission to access,
		// but we filter by project ID to ensure we only sync secrets from the specified project
		if bwSecret.ProjectID != nil && *bwSecret.ProjectID == c.projectID {
			secrets = append(secrets, Secret{
				Key:   bwSecret.Key,
				Value: bwSecret.Value,
			})
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

// Close cleans up resources
func (c *bitwardenClient) Close() {
	c.bwClient.Close()
}

func NewBitwardenClient(cfg BitwardenConfig) (Client, error) {
	apiEndpoint := cfg.ApiURL
	identityEndpoint := cfg.IdentityURL

	bwClient, err := sdk.NewBitwardenClient(&apiEndpoint, &identityEndpoint)
	if err != nil {
		return nil, err
	}

	err = bwClient.AccessTokenLogin(cfg.AccessToken, nil)
	if err != nil {
		return nil, err
	}

	return &bitwardenClient{
		organizationID: cfg.OrgID,
		projectID:      cfg.ProjectID,
		bwClient:       bwClient,
	}, nil
}

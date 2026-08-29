package remotesecrets

import "github.com/bitwarden/sdk-go/v2"

type BitwardenConfig struct {
	ApiURL      string `name:"api-url" help:"API URL" env:"API_URL" default:"https://vault.bitwarden.com/api"`
	IdentityURL string `name:"identity-url" help:"Identity URL" env:"IDENTITY_URL" default:"https://vault.bitwarden.com/identity"`
	AccessToken string `name:"access-token" help:"Access token" env:"ACCESS_TOKEN"`
	OrgID       string `name:"organization-id" help:"Organization ID" env:"ORGANIZATION_ID"`
	ProjectID   string `name:"project-id" help:"Project ID" env:"PROJECT_ID"`
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
	identityEndpoint := cfg.IdentityURL

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

package secrets

import (
	"context"

	infisical "github.com/infisical/go-sdk"
)

type InfisicalConfig struct {
	SiteUrl      string `name:"site-url" help:"Site URL" env:"SITE_URL" default:"https://app.infisical.com"`
	ClientID     string `name:"client-id" help:"Client ID (Universal Auth)" env:"CLIENT_ID"`
	ClientSecret string `name:"client-secret" help:"Client Secret (Universal Auth)" env:"CLIENT_SECRET"`
	Environment  string `name:"environment" help:"Environment slug" env:"ENVIRONMENT"`
	ProjectId    string `name:"project-id" help:"Project ID" env:"PROJECT_ID"`
	SecretPath   string `name:"secret-path" help:"Project ID" env:"SECRET_PATH" default:"/"`
}

type infisicalClient struct {
	projectId   string
	environment string
	secretPath  string

	infClient infisical.InfisicalClientInterface
}

// Get retrieves a secret value by secret key.
func (c *infisicalClient) Get(key string) (string, error) {
	secret, err := c.infClient.Secrets().Retrieve(infisical.RetrieveSecretOptions{
		SecretKey:   key,
		Environment: c.environment,
		ProjectID:   c.projectId,
		SecretPath:  c.secretPath,
	})
	if err != nil {
		return "", err
	}

	return secret.SecretValue, nil
}

// FetchAll retrieves all secrets.
func (c *infisicalClient) FetchAll() (*SecretsCollection, error) {
	secrets, err := c.infClient.Secrets().List(infisical.ListSecretsOptions{
		Environment: c.environment,
		ProjectID:   c.projectId,
		SecretPath:  c.secretPath,
	})
	if err != nil {
		return nil, err
	}

	var secretsCollection SecretsCollection
	for _, secret := range secrets {
		secretsCollection.Secrets = append(secretsCollection.Secrets, Secret{
			Key:   secret.SecretKey,
			Value: secret.SecretValue,
		})
	}

	return &secretsCollection, nil
}

func (c *infisicalClient) Close() {
}

func NewInfisicalClient(ctx context.Context, cfg InfisicalConfig) (Client, error) {
	client := infisical.NewInfisicalClient(ctx, infisical.Config{
		SiteUrl:          cfg.SiteUrl,
		AutoTokenRefresh: true,
	})
	_, err := client.Auth().UniversalAuthLogin(cfg.ClientID, cfg.ClientSecret)
	if err != nil {
		return nil, err
	}

	return &infisicalClient{
		projectId:   cfg.ProjectId,
		environment: cfg.Environment,
		secretPath:  cfg.SecretPath,
		infClient:   client,
	}, nil
}

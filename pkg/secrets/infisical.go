package secrets

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	infisical "github.com/infisical/go-sdk"
)

type InfisicalConfig struct {
	SiteUrl      string `name:"site-url" help:"Site URL" env:"SITE_URL" default:"https://app.infisical.com"`
	ClientID     string `name:"client-id" help:"Client ID (Universal Auth)" env:"CLIENT_ID"`
	ClientSecret string `name:"client-secret" help:"Client Secret (Universal Auth)" env:"CLIENT_SECRET"`
	Environment  string `name:"environment" help:"Environment slug" env:"ENVIRONMENT"`
	ProjectID    string `name:"project-id" help:"Project ID" env:"PROJECT_ID"`
	SecretPath   string `name:"secret-path" help:"Secret path (comma-separated for multiple paths)" env:"SECRET_PATH" default:"/"`
}

type infisicalClient struct {
	projectId   string
	environment string
	secretPaths []string

	infClient infisical.InfisicalClientInterface
}

// Get retrieves a secret value by secret key.
func (c *infisicalClient) Get(key string) (string, error) {
	for _, path := range c.secretPaths {
		secret, err := c.infClient.Secrets().Retrieve(infisical.RetrieveSecretOptions{
			SecretKey:   key,
			Environment: c.environment,
			ProjectID:   c.projectId,
			SecretPath:  path,
		})
		if err == nil {
			return secret.SecretValue, nil
		}
	}

	return "", fmt.Errorf("secret %q not found in any configured path", key)
}

// FetchAll retrieves all secrets.
func (c *infisicalClient) FetchAll() ([]Secret, error) {
	var result []Secret
	var hasSuccess bool

	for _, path := range c.secretPaths {
		listResult, err := c.infClient.Secrets().ListSecrets(infisical.ListSecretsOptions{
			Environment: c.environment,
			ProjectID:   c.projectId,
			SecretPath:  path,
		})
		if err != nil {
			slog.Warn("Failed to fetch secrets from path", "path", path, "error", err)
			continue
		}

		hasSuccess = true
		slog.Debug("Fetched secrets from path", "path", path, "count", len(listResult.Secrets))
		for _, secret := range listResult.Secrets {
			result = append(result, Secret{
				Key:   secret.SecretKey,
				Value: secret.SecretValue,
			})
		}
	}

	if !hasSuccess {
		return nil, fmt.Errorf("failed to fetch secrets from all configured paths")
	}

	return result, nil
}

func (c *infisicalClient) Close() {
}

func NewInfisicalClient(ctx context.Context, cfg InfisicalConfig) (Client, error) {
	autoTokenRefresh := true
	client := infisical.NewInfisicalClient(ctx, infisical.Config{
		SiteUrl:          cfg.SiteUrl,
		AutoTokenRefresh: &autoTokenRefresh,
	})
	_, err := client.Auth().UniversalAuthLogin(cfg.ClientID, cfg.ClientSecret)
	if err != nil {
		return nil, err
	}

	// Parse comma-separated paths once
	rawPaths := strings.Split(cfg.SecretPath, ",")
	var secretPaths []string
	for _, p := range rawPaths {
		p = strings.TrimSpace(p)
		if p != "" {
			secretPaths = append(secretPaths, p)
		}
	}

	if len(secretPaths) == 0 {
		return nil, fmt.Errorf("no secret paths provided in INFISICAL_SECRET_PATH")
	}

	return &infisicalClient{
		projectId:   cfg.ProjectID,
		environment: cfg.Environment,
		secretPaths: secretPaths,
		infClient:   client,
	}, nil
}

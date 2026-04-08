package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/veerendra2/composeflux/internal/reconcile"
	"github.com/veerendra2/composeflux/pkg/dockercompose"
	"github.com/veerendra2/composeflux/pkg/secrets"
	"github.com/veerendra2/composeflux/pkg/source"
	"github.com/veerendra2/gopackages/version"
)

type CommonConfig struct {
	Secrets       secrets.Config       `embed:""`
	Reconciler    reconcile.Config     `embed:"" group:"Reconciler Options:"`
	Source        source.Config        `embed:"" group:"Git Source Options:"`
	DockerCompose dockercompose.Config `embed:"" group:"Docker Compose Options:"`
}

// Validate checks provider-specific configuration
func (c *CommonConfig) Validate() error {
	switch c.Secrets.Provider {
	case "":
		if c.Source.DeployKeySecretRef != "" {
			return fmt.Errorf("--deploy-key-secret-ref requires a secrets provider (--secrets-provider)")
		}
	case "bitwarden":
		if c.Secrets.Bitwarden.AccessToken == "" || c.Secrets.Bitwarden.OrgID == "" || c.Secrets.Bitwarden.ProjectID == "" {
			return fmt.Errorf("bitwarden provider requires: --bitwarden-access-token, " +
				"--bitwarden-organization-id, --bitwarden-project-id")
		}
	case "infisical":
		if c.Secrets.Infisical.ClientID == "" || c.Secrets.Infisical.ClientSecret == "" ||
			c.Secrets.Infisical.Environment == "" || c.Secrets.Infisical.ProjectID == "" {
			return fmt.Errorf("infisical provider requires: --infisical-client-id, " +
				"--infisical-client-secret, --infisical-environment, --infisical-project-id")
		}
	}
	return nil
}

// InitClients initializes all required clients (secrets, git, docker, reconciler)
func (c *CommonConfig) InitClients(ctx context.Context) (*reconcile.Reconciler, func(), error) {
	sClient, err := secrets.New(ctx, c.Secrets)
	if err != nil {
		slog.Error("Failed to create secrets manager client", "provider", c.Secrets.Provider, "error", err)
		return nil, nil, err
	}

	cleanup := func() {
		if sClient != nil {
			sClient.Close()
		}
	}

	// Fetch SSH deploy key from secrets manager if specified
	if c.Source.DeployKeySecretRef != "" {
		if sClient == nil {
			return nil, cleanup, fmt.Errorf("--deploy-key-secret-ref requires a secrets provider (--secrets-provider)")
		}
		slog.Debug("Fetching SSH deploy key from secrets manager", "deploy_key_ref", c.Source.DeployKeySecretRef)

		keyContent, err := sClient.Get(c.Source.DeployKeySecretRef)
		if err != nil {
			slog.Error("Failed to fetch SSH deploy key", "deploy_key_ref", c.Source.DeployKeySecretRef, "error", err)
			return nil, cleanup, err
		}

		if keyContent == "" {
			slog.Error("SSH deploy key content is empty", "deploy_key_ref", c.Source.DeployKeySecretRef)
			return nil, cleanup, fmt.Errorf("SSH deploy key content is empty: %s", c.Source.DeployKeySecretRef)
		}

		sshDir := filepath.Dir(c.Source.SSHKeyPath)
		if err := os.MkdirAll(sshDir, 0700); err != nil {
			slog.Error("Unable to create ssh directory", "path", sshDir, "error", err)
			return nil, cleanup, err
		}

		_ = os.Remove(c.Source.SSHKeyPath)

		if err := os.WriteFile(c.Source.SSHKeyPath, []byte(keyContent), 0600); err != nil {
			slog.Error("Unable to write ssh deploy key content to file", "path", c.Source.SSHKeyPath, "error", err)
			return nil, cleanup, err
		}

		slog.Info("SSH deploy key fetched and written successfully", "deploy_key_ref", c.Source.DeployKeySecretRef, "path", c.Source.SSHKeyPath)
	}

	// Create git client
	gClient, err := source.New(c.Source)
	if err != nil {
		slog.Error("Failed to create git client", "error", err)
		return nil, cleanup, err
	}

	// Create docker compose client
	dClient, err := dockercompose.New(c.DockerCompose)
	if err != nil {
		slog.Error("Failed to create docker compose client", "error", err)
		return nil, cleanup, err
	}

	versionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Fetch docker compose version information
	dockerVersion, err := dClient.Version(versionCtx)
	if err != nil {
		slog.Error("Failed to get docker compose version", "error", err)
		return nil, cleanup, err
	}
	slog.Info("Docker version", dockerVersion...)

	// Create reconciler
	rClient, err := reconcile.New(c.Reconciler, sClient, gClient, dClient)
	if err != nil {
		slog.Error("Failed to create reconciler client", "error", err)
		return nil, cleanup, err
	}

	slog.Info("Reconciler configured", "stack_path", c.Reconciler.StackPath, "config_file", c.Reconciler.ConfigFile,
		"secrets_manager", c.Secrets.Provider, "git_poll_interval", c.Reconciler.GitInterval,
		"image_update_cron", c.Reconciler.ImageUpdateSchedule)

	return rClient, cleanup, nil
}

// Setup performs shared startup: logs version info, sets up signal handling,
// and initializes all clients. Returns the reconciler, a context bound to
// OS signals, and a cleanup function that cancels the context and closes clients.
func (c *CommonConfig) Setup() (*reconcile.Reconciler, context.Context, func(), error) {
	slog.Info("Version information", version.Info()...)
	slog.Info("Build context", version.BuildContext()...)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	rClient, clientCleanup, err := c.InitClients(ctx)
	if err != nil {
		stop()
		if clientCleanup != nil {
			clientCleanup()
		}
		return nil, nil, nil, err
	}

	cleanup := func() {
		stop()
		clientCleanup()
	}

	return rClient, ctx, cleanup, nil
}

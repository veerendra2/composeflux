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
	"github.com/veerendra2/composeflux/pkg/localsecrets"
	"github.com/veerendra2/composeflux/pkg/remotesecrets"
	"github.com/veerendra2/composeflux/pkg/source"
	"github.com/veerendra2/gopackages/version"
)

type CommonConfig struct {
	RemoteSecrets remotesecrets.Config `embed:""`
	LocalSecrets  localsecrets.Config  `embed:""`
	Reconciler    reconcile.Config     `embed:"" group:"Reconciler Options:"`
	Source        source.Config        `embed:"" group:"Git Source Options:"`
	DockerCompose dockercompose.Config `embed:"" group:"Docker Compose Options:"`
}

// Validate checks provider-specific configuration
func (c *CommonConfig) Validate() error {
	switch c.RemoteSecrets.Provider {
	case "":
		if c.Source.DeployKeySecretRef != "" {
			return fmt.Errorf("--deploy-key-secret-ref requires a remote secrets provider (--remote-secrets-provider)")
		}
	case "bitwarden":
		if c.RemoteSecrets.Bitwarden.AccessToken == "" || c.RemoteSecrets.Bitwarden.OrgID == "" || c.RemoteSecrets.Bitwarden.ProjectID == "" {
			return fmt.Errorf("bitwarden provider requires: --bitwarden-access-token, " +
				"--bitwarden-organization-id, --bitwarden-project-id")
		}
	case "infisical":
		if c.RemoteSecrets.Infisical.ClientID == "" || c.RemoteSecrets.Infisical.ClientSecret == "" ||
			c.RemoteSecrets.Infisical.Environment == "" || c.RemoteSecrets.Infisical.ProjectID == "" {
			return fmt.Errorf("infisical provider requires: --infisical-client-id, " +
				"--infisical-client-secret, --infisical-environment, --infisical-project-id")
		}
	}
	return nil
}

// InitClients initializes all required clients (secrets, git, docker, reconciler)
func (c *CommonConfig) InitClients(ctx context.Context) (*reconcile.Reconciler, func(), error) {
	rClient, err := remotesecrets.New(ctx, c.RemoteSecrets)
	if err != nil {
		slog.Error("Failed to create remote secrets client", "provider", c.RemoteSecrets.Provider, "error", err)
		return nil, nil, err
	}

	cleanup := func() {
		if rClient != nil {
			rClient.Close()
		}
	}

	if c.Source.DeployKeySecretRef != "" {
		if err := c.writeDeployKey(rClient); err != nil {
			return nil, cleanup, err
		}
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

	lClient := localsecrets.New(c.LocalSecrets)

	// Create reconciler
	reconciler, err := reconcile.New(c.Reconciler, lClient, rClient, gClient, dClient)
	if err != nil {
		slog.Error("Failed to create reconciler client", "error", err)
		return nil, cleanup, err
	}

	slog.Info("Reconciler configured", "stack_path", c.Reconciler.StackPath, "config_file", c.Reconciler.ConfigFile,
		"remote_secrets_provider", c.RemoteSecrets.Provider, "git_poll_interval", c.Reconciler.GitInterval,
		"health_reconcile_interval", c.Reconciler.HealthInterval, "prune_interval", c.Reconciler.PruneInterval,
		"image_update_cron", c.Reconciler.ImageUpdateSchedule)

	return reconciler, cleanup, nil
}

// writeDeployKey fetches the configured SSH key and writes it with restricted permissions.
func (c *CommonConfig) writeDeployKey(client remotesecrets.Client) error {
	if client == nil {
		return fmt.Errorf("--deploy-key-secret-ref requires a remote secrets provider (--remote-secrets-provider)")
	}

	ref := c.Source.DeployKeySecretRef
	slog.Debug("Fetching SSH deploy key from secrets manager", "deploy_key_ref", ref)
	content, err := client.Get(ref)
	if err != nil {
		slog.Error("Failed to fetch SSH deploy key", "deploy_key_ref", ref, "error", err)
		return err
	}
	if content == "" {
		slog.Error("SSH deploy key content is empty", "deploy_key_ref", ref)
		return fmt.Errorf("SSH deploy key content is empty: %s", ref)
	}

	sshDir := filepath.Dir(c.Source.SSHKeyPath)
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		slog.Error("Unable to create ssh directory", "path", sshDir, "error", err)
		return err
	}
	_ = os.Remove(c.Source.SSHKeyPath)
	if err := os.WriteFile(c.Source.SSHKeyPath, []byte(content), 0600); err != nil {
		slog.Error("Unable to write ssh deploy key content to file", "path", c.Source.SSHKeyPath, "error", err)
		return err
	}

	slog.Info("SSH deploy key fetched and written successfully", "deploy_key_ref", ref, "path", c.Source.SSHKeyPath)
	return nil
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

package dockercompose

import (
	"context"
	"io"
	"log/slog"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/filters"
	dockerClient "github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

type Config struct {
	RemoveOrphans bool `name:"remove-orphans" help:"Remove orphan containers" env:"REMOVE_ORPHANS" default:"true" group:"Docker Compose Options:"`
}
type Client interface {
	LoadProject(ctx context.Context, composeCfg ComposeConfig) (*types.Project, error)

	Down(ctx context.Context, projectName string) error
	List(ctx context.Context) ([]api.Stack, error)
	Prune(ctx context.Context)
	Ps(ctx context.Context, projectName string) ([]api.ContainerSummary, error)
	Pull(ctx context.Context, project *types.Project) error
	Restart(ctx context.Context, projectName string) error
	Up(ctx context.Context, project *types.Project) error
	Version(ctx context.Context) ([]any, error)
}

type client struct {
	compose       api.Compose
	docker        dockerClient.APIClient
	removeOrphans bool
}

type ComposeConfig struct {
	ComposeFiles []string
	WorkingDir   string
	Env          []string
}

func (c *client) LoadProject(ctx context.Context, composeCfg ComposeConfig) (*types.Project, error) {
	return c.compose.LoadProject(ctx, api.ProjectLoadOptions{
		ConfigPaths: composeCfg.ComposeFiles,
		WorkingDir:  composeCfg.WorkingDir,
		ProjectOptionsFns: []cli.ProjectOptionsFn{
			cli.WithEnv(composeCfg.Env),
			cli.WithInterpolation(true),
			cli.WithNormalization(true),
			cli.WithResolvedPaths(true),
		},
	})
}

func (c *client) Down(ctx context.Context, projectName string) error {
	return c.compose.Down(ctx, projectName, api.DownOptions{
		RemoveOrphans: c.removeOrphans,
	})
}

func (c *client) List(ctx context.Context) ([]api.Stack, error) {
	return c.compose.List(ctx, api.ListOptions{
		All: true,
	})
}

func (c *client) Prune(ctx context.Context) {
	f := filters.NewArgs()

	if _, err := c.docker.ContainersPrune(ctx, f); err != nil {
		slog.Warn("Failed to prune containers", "error", err)
	}

	if _, err := c.docker.ImagesPrune(ctx, f); err != nil {
		slog.Warn("Failed to prune images", "error", err)
	}

	if _, err := c.docker.VolumesPrune(ctx, filters.NewArgs(filters.Arg("all", "1"))); err != nil {
		slog.Warn("Failed to prune volumes", "error", err)
	}

	if _, err := c.docker.NetworksPrune(ctx, f); err != nil {
		slog.Warn("Failed to prune networks", "error", err)
	}

	if _, err := c.docker.BuildCachePrune(ctx, build.CachePruneOptions{All: true}); err != nil {
		slog.Warn("Failed to prune build cache", "error", err)
	}
}

func (c *client) Ps(ctx context.Context, projectName string) ([]api.ContainerSummary, error) {
	return c.compose.Ps(ctx, projectName, api.PsOptions{
		All: true,
	})
}

func (c *client) Pull(ctx context.Context, project *types.Project) error {
	return c.compose.Pull(ctx, project, api.PullOptions{
		Quiet: true,
	})
}

func (c *client) Restart(ctx context.Context, projectName string) error {
	return c.compose.Restart(ctx, projectName, api.RestartOptions{})
}

func (c *client) Up(ctx context.Context, project *types.Project) error {
	return c.compose.Up(ctx, project, api.UpOptions{
		Create: api.CreateOptions{
			RemoveOrphans:        c.removeOrphans,
			QuietPull:            true,
			Recreate:             api.RecreateDiverged,
			RecreateDependencies: api.RecreateDiverged,
			Inherit:              true,
		},
		Start: api.StartOptions{
			Project: project,
		},
	})
}

func (c *client) Version(ctx context.Context) ([]any, error) {
	serverVersion, err := c.docker.ServerVersion(ctx)
	if err != nil {
		return []any{}, err
	}
	clientVersion := c.docker.ClientVersion()

	return []any{
		"server_engine", serverVersion.Version,
		"server_api", serverVersion.APIVersion,
		"client_api", clientVersion,
	}, nil
}

func New(cfg Config) (Client, error) {
	// Redirect Docker SDK's logrus output to slog
	logrus.SetOutput(io.Discard)
	logrus.SetLevel(logrus.WarnLevel)
	logrus.AddHook(&slogHook{})

	dockerCLI, err := command.NewDockerCli(
		command.WithOutputStream(&slogWriter{level: slog.LevelInfo, maxSize: 1024 * 1024}), // 1MB limit
		command.WithErrorStream(&slogWriter{level: slog.LevelWarn, maxSize: 1024 * 1024}),  // 1MB limit
	)
	if err != nil {
		return nil, err
	}

	if err = dockerCLI.Initialize(&flags.ClientOptions{}); err != nil {
		return nil, err
	}

	service, err := compose.NewComposeService(dockerCLI)
	if err != nil {
		return nil, err
	}

	return &client{
		compose:       service,
		docker:        dockerCLI.Client(),
		removeOrphans: cfg.RemoveOrphans,
	}, nil
}

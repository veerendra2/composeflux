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
	"github.com/sirupsen/logrus"
)

type Config struct {
	RemoveOrphans bool `name:"remove-orphans" help:"Remove orphan containers" env:"REMOVE_ORPHANS" default:"true" group:"Docker Compose Options:"`
}

type Client interface {
	LoadProject(ctx context.Context, composeCfg ComposeConfig) (*types.Project, error)

	Down(ctx context.Context, projectName string) error
	List(ctx context.Context) ([]api.Stack, error)
	Ps(ctx context.Context, projectName string) ([]api.ContainerSummary, error)
	Pull(ctx context.Context, project *types.Project) error
	Restart(ctx context.Context, projectName string) error
	Up(ctx context.Context, project *types.Project) error
	Version(ctx context.Context) ([]any, error)
}

type client struct {
	compose       api.Compose
	dockerCli     *command.DockerCli
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
	serverVersion, err := c.dockerCli.Client().ServerVersion(ctx)
	if err != nil {
		return []any{}, err
	}
	clientVersion := c.dockerCli.Client().ClientVersion()

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
		dockerCli:     dockerCLI,
		removeOrphans: cfg.RemoveOrphans,
	}, nil
}

package dockercompose

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/distribution/reference"
	"github.com/docker/cli/cli/command"
	dockerconfigtypes "github.com/docker/cli/cli/config/types"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	dockerregistry "github.com/docker/docker/registry"
	mobyClient "github.com/moby/moby/client"
	"github.com/sirupsen/logrus"
)

type Config struct {
	RemoveOrphans bool `name:"remove-orphans" help:"Remove orphan containers" env:"REMOVE_ORPHANS" default:"true" group:"Docker Compose Options:"`
}
type Client interface {
	LoadProject(ctx context.Context, composeCfg ComposeConfig) (*types.Project, error)

	Down(ctx context.Context, projectName string) error
	HasImageUpdates(ctx context.Context, project *types.Project) (bool, error)
	List(ctx context.Context) ([]api.Stack, error)
	Ps(ctx context.Context, projectName string) ([]api.ContainerSummary, error)
	Pull(ctx context.Context, project *types.Project) error
	Restart(ctx context.Context, projectName string) error
	Up(ctx context.Context, project *types.Project) error

	Prune(ctx context.Context)
	Version(ctx context.Context) ([]any, error)
}

type client struct {
	compose       api.Compose
	docker        mobyClient.APIClient
	dockerCLI     *command.DockerCli
	removeOrphans bool
	logHook       *slogHook
}

type ComposeConfig struct {
	ComposeFiles []string
	WorkingDir   string
	Env          []string
}

func (c *client) LoadProject(ctx context.Context, composeCfg ComposeConfig) (*types.Project, error) {
	c.logHook.setStackName(filepath.Base(composeCfg.WorkingDir))
	defer c.logHook.setStackName("")
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

// GetDependencyPaths extracts all file paths that this compose project depends on
// (compose files, included files, env files, bind mount host sources, config/secret files, build context/dockerfiles).
func GetDependencyPaths(project *types.Project) []string {
	if project == nil {
		return nil
	}

	pathSet := make(map[string]struct{})

	addPath := func(p string) {
		if p == "" {
			return
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(project.WorkingDir, p)
		}
		cleaned := filepath.Clean(p)
		pathSet[cleaned] = struct{}{}
	}

	for _, f := range project.ComposeFiles {
		addPath(f)
	}

	for _, cfg := range project.Configs {
		addPath(cfg.File)
	}
	for _, sec := range project.Secrets {
		addPath(sec.File)
	}

	for _, svc := range project.Services {
		for _, envFile := range svc.EnvFiles {
			addPath(envFile.Path)
		}
		for _, vol := range svc.Volumes {
			if vol.Type == types.VolumeTypeBind {
				addPath(vol.Source)
			}
		}
		if svc.Build != nil {
			addPath(svc.Build.Context)
			if svc.Build.Dockerfile != "" {
				if filepath.IsAbs(svc.Build.Dockerfile) {
					addPath(svc.Build.Dockerfile)
				} else {
					addPath(filepath.Join(svc.Build.Context, svc.Build.Dockerfile))
				}
			}
		}
	}

	paths := make([]string, 0, len(pathSet))
	for p := range pathSet {
		paths = append(paths, p)
	}
	return paths
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

// https://pkg.go.dev/github.com/docker/compose/v5/pkg/api#ContainerSummary
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
	for _, svc := range project.Services {
		if svc.Build != nil {
			if err := c.compose.Build(ctx, project, api.BuildOptions{Quiet: true}); err != nil {
				return err
			}
			break
		}
	}

	return c.compose.Up(ctx, project, api.UpOptions{
		Create: api.CreateOptions{
			RemoveOrphans:        c.removeOrphans,
			QuietPull:            true,
			Recreate:             api.RecreateForce,
			RecreateDependencies: api.RecreateForce,
			Inherit:              true,
		},
		Start: api.StartOptions{
			Project: project,
		},
	})
}

// HasImageUpdates checks if any service image in the project has a newer version in the registry.
func (c *client) HasImageUpdates(ctx context.Context, project *types.Project) (bool, error) {
	for _, svc := range project.Services {
		if svc.Build != nil || svc.Image == "" {
			continue
		}

		// Skip digest-pinned images (e.g. image@sha256:abc…) — they are immutable
		named, parseErr := reference.ParseNormalizedNamed(svc.Image)
		if parseErr == nil {
			if _, isDigested := named.(reference.Digested); isDigested {
				continue
			}
		}

		localInfo, err := c.docker.ImageInspect(ctx, svc.Image)
		if err != nil {
			type notFound interface{ NotFound() }
			if _, ok := err.(notFound); !ok {
				slog.Warn("Failed to inspect image, skipping", "stack", project.Name, "service", svc.Name, "image", svc.Image, "error", err)
				continue
			}
			// Image not present locally — treat as needs update; compose up will pull it
			slog.Debug("Image not found locally, treating as update needed",
				"stack", project.Name, "service", svc.Name, "image", svc.Image)
			return true, nil
		}

		if len(localInfo.RepoDigests) == 0 {
			// No repo digests means the image was built or loaded locally — skip
			continue
		}

		// Build auth token from docker config for private registry support
		encodedAuth := ""
		if parseErr == nil {
			if repoInfo, repoErr := dockerregistry.ParseRepositoryInfo(named); repoErr == nil {
				cliAuth, _ := c.dockerCLI.ConfigFile().GetAuthConfig(repoInfo.Index.Name)
				if buf, err := json.Marshal(dockerconfigtypes.AuthConfig(cliAuth)); err == nil {
					encodedAuth = base64.URLEncoding.EncodeToString(buf)
				}
			}
		}

		remoteDist, err := c.docker.DistributionInspect(ctx, svc.Image, mobyClient.DistributionInspectOptions{
			EncodedRegistryAuth: encodedAuth,
		})
		if err != nil {
			slog.Warn("Failed to fetch remote manifest, skipping service", "image", svc.Image, "error", err)
			continue
		}

		remoteDigest := remoteDist.Descriptor.Digest.String()
		hasMatch := false
		for _, localDigest := range localInfo.RepoDigests {
			// localDigest format: "name@sha256:abc…" — compare only the digest part
			parts := strings.SplitN(localDigest, "@", 2)
			if len(parts) == 2 && parts[1] == remoteDigest {
				hasMatch = true
				break
			}
		}

		if !hasMatch {
			slog.Info("Image update available", "stack", project.Name, "service", svc.Name, "image", svc.Image)
			slog.Debug("Image digest mismatch", "image", svc.Image,
				"local_digests", localInfo.RepoDigests, "remote_digest", remoteDigest)
			return true, nil
		}
	}
	return false, nil
}

func New(cfg Config) (Client, error) {
	// Redirect Docker SDK's logrus output to slog
	logrus.SetOutput(io.Discard)
	logrus.SetLevel(logrus.WarnLevel)
	hook := &slogHook{}
	logrus.AddHook(hook)

	dockerCLI, err := command.NewDockerCli(
		command.WithOutputStream(&slogWriter{level: slog.LevelDebug, maxSize: 1024 * 1024, logHook: hook}), // 1MB limit
		command.WithErrorStream(&slogWriter{level: slog.LevelWarn, maxSize: 1024 * 1024, logHook: hook}),   // 1MB limit
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
		dockerCLI:     dockerCLI,
		removeOrphans: cfg.RemoveOrphans,
		logHook:       hook,
	}, nil
}

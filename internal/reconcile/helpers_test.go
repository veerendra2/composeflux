package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	"github.com/veerendra2/composeflux/pkg/dockercompose"
)

// fixture is a temporary Git checkout with mocked integration clients.
type fixture struct {
	repo      string
	stackRoot string
	docker    *MockDockerClient
	git       *MockGitClient
	local     *MockLocalSecretsClient
	remote    *MockRemoteSecretsClient
}

func newFixture() *fixture {
	repo, err := filepath.EvalSymlinks(GinkgoT().TempDir())
	Expect(err).NotTo(HaveOccurred())
	ctrl := gomock.NewController(GinkgoT())
	f := &fixture{
		repo:      repo,
		stackRoot: filepath.Join(repo, "stacks"),
		docker:    NewMockDockerClient(ctrl),
		git:       NewMockGitClient(ctrl),
		local:     NewMockLocalSecretsClient(ctrl),
		remote:    NewMockRemoteSecretsClient(ctrl),
	}
	Expect(os.MkdirAll(f.stackRoot, 0o755)).To(Succeed())
	f.git.EXPECT().Path().Return(repo).AnyTimes()
	f.docker.EXPECT().LoadProject(gomock.Any(), gomock.Any()).DoAndReturn(loadProject).AnyTimes()
	return f
}

// reconciler builds a reconciler without secret providers unless they are passed explicitly.
func (f *fixture) reconciler(options ...func(*Reconciler)) *Reconciler {
	r, err := New(Config{StackPath: "stacks", ConfigFile: "stack.yml", GitInterval: time.Minute}, nil, nil, f.git, f.docker)
	Expect(err).NotTo(HaveOccurred())
	for _, option := range options {
		option(r)
	}
	return r
}

func (f *fixture) withLocalSecrets(r *Reconciler)  { r.lClient = f.local }
func (f *fixture) withRemoteSecrets(r *Reconciler) { r.rClient = f.remote }

// write creates a repository file, creating parent directories as needed.
func (f *fixture) write(path, content string) string {
	path = filepath.Join(f.repo, path)
	Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
	Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
	return path
}

// stack writes stacks/<name>/compose.yml.
func (f *fixture) stack(name, compose string) string {
	return f.write(filepath.Join("stacks", name, "compose.yml"), compose)
}

// running reports Docker stacks and their containers for every List and Ps call.
func (f *fixture) running(stacks map[string][]api.ContainerSummary) {
	var list []api.Stack
	for name := range stacks {
		list = append(list, api.Stack{Name: name})
	}
	f.docker.EXPECT().List(gomock.Any()).Return(list, nil).AnyTimes()
	f.docker.EXPECT().Ps(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, name string) ([]api.ContainerSummary, error) {
			return stacks[name], nil
		}).AnyTimes()
}

// loadProject mirrors dockercompose.Client.LoadProject without a Docker daemon.
func loadProject(ctx context.Context, cfg dockercompose.ComposeConfig) (*types.Project, error) {
	options, err := cli.NewProjectOptions(cfg.ComposeFiles,
		cli.WithWorkingDirectory(cfg.WorkingDir),
		cli.WithEnv(cfg.Env),
		cli.WithInterpolation(true),
		cli.WithNormalization(true),
		cli.WithResolvedPaths(true),
	)
	if err != nil {
		return nil, err
	}
	return options.LoadProject(ctx)
}

func managed(state container.ContainerState, health container.HealthStatus) api.ContainerSummary {
	return api.ContainerSummary{State: state, Health: health, Labels: map[string]string{LabelManaged: ValueTrue}}
}

func healthy() []api.ContainerSummary { return []api.ContainerSummary{managed(StateRunning, "")} }
func unhealthy() []api.ContainerSummary {
	return []api.ContainerSummary{managed(StateRunning, Unhealthy)}
}

func ptr(value string) *string { return &value }

const webService = "services:\n  web:\n    image: nginx\n"
const suspendedService = "services:\n  web:\n    image: nginx\n    labels:\n      composeflux.health.suspend: \"true\"\n"

package reconcile

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("stack discovery", func() {
	var f *fixture

	BeforeEach(func() {
		f = newFixture()
	})

	Describe("buildComposeConfig", func() {
		It("adds override files after the primary Compose file", func() {
			compose := f.write("stacks/app/compose.yaml", webService)
			override := f.write("stacks/app/compose.override.yml", webService)

			cfg, err := buildComposeConfig(filepath.Dir(compose), []string{"A=1"})
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.ComposeFiles).To(Equal([]string{compose, override}))
			Expect(cfg.WorkingDir).To(Equal(filepath.Dir(compose)))
			Expect(cfg.Env).To(Equal([]string{"A=1"}))
		})

		It("fails without a primary Compose file", func() {
			f.write("stacks/app/compose.override.yml", webService)
			_, err := buildComposeConfig(filepath.Join(f.stackRoot, "app"), nil)
			Expect(err).To(MatchError(ContainSubstring("no compose files found")))
		})
	})

	Describe("discoverComposeStacks", func() {
		It("returns directories with Compose files and ignores everything else", func() {
			f.stack("app", webService)
			f.stack("db", webService)
			f.write("stacks/docs/README.md", "")
			f.write("stacks/stack.yml", "")

			stacks, err := discoverComposeStacks(f.stackRoot, []string{"A=1"})
			Expect(err).NotTo(HaveOccurred())
			Expect(stacks).To(HaveLen(2))
			Expect(stacks[0].WorkingDir).To(Equal(filepath.Join(f.stackRoot, "app")))
			Expect(stacks[1].WorkingDir).To(Equal(filepath.Join(f.stackRoot, "db")))
			Expect(stacks[1].Env).To(Equal([]string{"A=1"}))
		})

		It("fails when the stack root cannot be read", func() {
			_, err := discoverComposeStacks(filepath.Join(f.stackRoot, "missing"), nil)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("getStackStates", func() {
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
		})

		It("reports health for managed stacks only", func() {
			f.docker.EXPECT().List(ctx).Return([]api.Stack{{Name: "ok"}, {Name: "sick"}, {Name: "foreign"}, {Name: "empty"}, {Name: "broken"}}, nil)
			f.docker.EXPECT().Ps(ctx, "ok").Return(healthy(), nil)
			f.docker.EXPECT().Ps(ctx, "sick").Return(append(healthy(), unhealthy()...), nil)
			f.docker.EXPECT().Ps(ctx, "foreign").Return([]api.ContainerSummary{{State: StateRunning}}, nil)
			f.docker.EXPECT().Ps(ctx, "empty").Return(nil, nil)
			f.docker.EXPECT().Ps(ctx, "broken").Return(nil, errors.New("daemon error"))

			states, err := f.reconciler().getStackStates(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(states).To(Equal(StackStateMap{"ok": {Healthy: true}, "sick": {Healthy: false}}))
		})

		It("returns list errors", func() {
			f.docker.EXPECT().List(gomock.Any()).Return(nil, errors.New("daemon down"))
			_, err := f.reconciler().getStackStates(ctx)
			Expect(err).To(MatchError("daemon down"))
		})
	})
})

var _ = DescribeTable("isContainerHealthy",
	func(state, health string, exitCode int, init bool, expected bool) {
		c := api.ContainerSummary{
			State:    container.ContainerState(state),
			Health:   container.HealthStatus(health),
			ExitCode: exitCode,
			Labels:   map[string]string{},
		}
		if init {
			c.Labels[LabelInit] = ValueTrue
		}
		Expect(isContainerHealthy(c)).To(Equal(expected))
	},
	Entry("running without health check", StateRunning, "", 0, false, true),
	Entry("running and healthy", StateRunning, "healthy", 0, false, true),
	Entry("running and starting", StateRunning, "starting", 0, false, true),
	Entry("running and unhealthy", StateRunning, Unhealthy, 0, false, false),
	Entry("exited service", StateExited, "", 0, false, false),
	Entry("successful init container", StateExited, "", 0, true, true),
	Entry("failed init container", StateExited, "", 1, true, false),
	Entry("restarting init container", "restarting", "", 0, true, false),
)

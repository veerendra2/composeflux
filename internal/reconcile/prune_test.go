package reconcile

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/docker/compose/v5/pkg/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/veerendra2/composeflux/pkg/dockercompose"
)

var _ = Describe("PruneResources", func() {
	var (
		ctx context.Context
		f   *fixture
		r   *Reconciler
	)

	BeforeEach(func() {
		ctx = context.Background()
		f = newFixture()
		r = f.reconciler()
		f.stack("app", webService)
	})

	It("prunes when every source stack is running and healthy", func() {
		f.running(map[string][]api.ContainerSummary{"app": healthy()})
		f.docker.EXPECT().Prune(ctx)
		Expect(r.PruneResources(ctx)).To(Succeed())
	})

	DescribeTable("skips pruning",
		func(compose string, running map[string][]api.ContainerSummary) {
			f.stack("app", compose)
			f.running(running)
			Expect(r.PruneResources(ctx)).To(Succeed())
		},
		Entry("when a source stack is not running", webService, map[string][]api.ContainerSummary{}),
		Entry("when a stack is unhealthy", webService, map[string][]api.ContainerSummary{"app": unhealthy()}),
		Entry("when a stack is suspended", suspendedService, map[string][]api.ContainerSummary{"app": healthy()}),
	)

	DescribeTable("returns errors that stop the pass",
		func(setup func(*fixture, *Reconciler), expected string) {
			setup(f, r)
			Expect(r.PruneResources(ctx)).To(MatchError(ContainSubstring(expected)))
		},
		Entry("malformed stack.yml", func(f *fixture, _ *Reconciler) {
			f.write("stacks/stack.yml", "envs: [broken\n")
		}, "failed to load stack config"),
		Entry("stack load failure", func(f *fixture, _ *Reconciler) {
			f.stack("app", "name: other\n"+webService)
			f.running(map[string][]api.ContainerSummary{"app": healthy()})
		}, "failed to load stack app for pruning"),
	)
})

var _ = Describe("PruneStacks", func() {
	var (
		ctx context.Context
		f   *fixture
	)

	BeforeEach(func() {
		ctx = context.Background()
		f = newFixture()
	})

	It("removes managed stacks missing from Git and continues after failures", func() {
		f.docker.EXPECT().List(ctx).Return([]api.Stack{{Name: "app"}, {Name: "foreign"}, {Name: "old"}, {Name: "busy"}, {Name: "broken"}}, nil)
		f.docker.EXPECT().Ps(ctx, "foreign").Return([]api.ContainerSummary{{}}, nil)
		f.docker.EXPECT().Ps(ctx, "old").Return(healthy(), nil)
		f.docker.EXPECT().Ps(ctx, "busy").Return(healthy(), nil)
		f.docker.EXPECT().Ps(ctx, "broken").Return(nil, errors.New("daemon error"))
		f.docker.EXPECT().Down(ctx, "old").Return(nil)
		f.docker.EXPECT().Down(ctx, "busy").Return(errors.New("busy"))

		err := f.reconciler().PruneStacks(ctx, []dockercompose.ComposeConfig{{WorkingDir: filepath.Join(f.stackRoot, "app")}})
		Expect(err).To(MatchError(ContainSubstring("failed to prune stack busy: busy")))
		Expect(err).To(MatchError(ContainSubstring("failed to list containers for stack broken")))
	})

	It("returns list errors", func() {
		f.docker.EXPECT().List(ctx).Return(nil, errors.New("daemon down"))
		Expect(f.reconciler().PruneStacks(ctx, nil)).To(MatchError("daemon down"))
	})
})

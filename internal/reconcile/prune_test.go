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
	})

	It("prunes unused Docker resources", func() {
		f.docker.EXPECT().Prune(ctx).Return(nil)
		Expect(r.PruneResources(ctx)).To(Succeed())
	})

	It("returns prune errors", func() {
		f.docker.EXPECT().Prune(ctx).Return(errors.New("daemon error"))
		Expect(r.PruneResources(ctx)).To(MatchError("daemon error"))
	})
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

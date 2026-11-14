package reconcile

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("UpdateImages", func() {
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

	It("does nothing after cancellation", func() {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		Expect(r.UpdateImages(cancelled)).To(MatchError(context.Canceled))
	})

	It("pulls and redeploys only stacks with image updates", func() {
		for _, name := range []string{"current", "stale", "checkfail", "pullfail", "deployfail"} {
			f.stack(name, webService)
		}
		f.stack("excluded", "services:\n  web:\n    image: nginx\n    labels:\n      composeflux.image-update.exclude: \"true\"\n")
		f.stack("suspended", suspendedService)
		f.stack("broken", "name: other\n"+webService)
		r.healthFailCounts["stale"] = 2

		f.docker.EXPECT().HasImageUpdates(ctx, named("current")).Return(false, nil)
		f.docker.EXPECT().HasImageUpdates(ctx, named("checkfail")).Return(false, errors.New("registry down"))
		for _, name := range []string{"stale", "pullfail", "deployfail"} {
			f.docker.EXPECT().HasImageUpdates(ctx, named(name)).Return(true, nil)
		}
		f.docker.EXPECT().Pull(ctx, named("pullfail")).Return(errors.New("rate limited"))
		f.docker.EXPECT().Pull(ctx, named("stale")).Return(nil)
		f.docker.EXPECT().Pull(ctx, named("deployfail")).Return(nil)
		f.docker.EXPECT().Up(ctx, named("stale")).Return(nil)
		f.docker.EXPECT().Up(ctx, named("deployfail")).Return(errors.New("port in use"))

		Expect(r.UpdateImages(ctx)).To(Succeed())
		Expect(r.healthFailCounts).To(Equal(map[string]int{"stale": 0}))
	})

	It("stops the pass on a malformed stack.yml", func() {
		f.write("stacks/stack.yml", "envs: [broken\n")
		Expect(r.UpdateImages(ctx)).To(MatchError(ContainSubstring("failed to load stack config")))
	})
})

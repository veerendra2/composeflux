package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/docker/compose/v5/pkg/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ReconcileHealth", func() {
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

	It("does nothing when every managed stack is healthy", func() {
		f.running(map[string][]api.ContainerSummary{"app": healthy()})
		Expect(r.ReconcileHealth(ctx)).To(Succeed())
	})

	It("redeploys unhealthy stacks and resets their failure count", func() {
		f.stack("app", webService)
		f.running(map[string][]api.ContainerSummary{"app": unhealthy()})
		f.docker.EXPECT().Up(ctx, named("app")).Return(nil)
		r.healthFailCounts["app"] = 2

		Expect(r.ReconcileHealth(ctx)).To(Succeed())
		Expect(r.healthFailCounts).To(HaveKeyWithValue("app", 0))
	})

	It("stops redeploying after the maximum number of failed attempts", func() {
		f.stack("app", webService)
		f.running(map[string][]api.ContainerSummary{"app": unhealthy()})
		f.docker.EXPECT().Up(ctx, named("app")).Return(errors.New("crash loop")).Times(maxHealthReconcileAttempts)

		for range maxHealthReconcileAttempts + 1 {
			Expect(r.ReconcileHealth(ctx)).To(Succeed())
		}
		Expect(r.healthFailCounts).To(HaveKeyWithValue("app", maxHealthReconcileAttempts))
	})

	It("skips suspended stacks without counting a failure", func() {
		f.stack("app", suspendedService)
		f.running(map[string][]api.ContainerSummary{"app": unhealthy()})

		Expect(r.ReconcileHealth(ctx)).To(Succeed())
		Expect(r.healthFailCounts).NotTo(HaveKey("app"))
	})

	It("counts failures for stacks that cannot be loaded from Git", func() {
		f.stack("renamed", "name: other\n"+webService)
		f.write("stacks/nocompose/README.md", "")
		f.write("stacks/file", "")
		Expect(os.Symlink(f.repo, filepath.Join(f.stackRoot, "escape"))).To(Succeed())
		f.running(map[string][]api.ContainerSummary{
			"missing": unhealthy(), "..": unhealthy(), "file": unhealthy(),
			"escape": unhealthy(), "nocompose": unhealthy(), "renamed": unhealthy(),
		})

		Expect(r.ReconcileHealth(ctx)).To(Succeed())
		Expect(r.healthFailCounts).To(Equal(map[string]int{
			"missing": 1, "..": 1, "file": 1, "escape": 1, "nocompose": 1, "renamed": 1,
		}))
	})

	It("stops the pass on a malformed stack.yml", func() {
		f.running(map[string][]api.ContainerSummary{"app": unhealthy()})
		f.write("stacks/stack.yml", "envs: [broken\n")
		Expect(r.ReconcileHealth(ctx)).To(MatchError(ContainSubstring("failed to load stack config")))
	})
})

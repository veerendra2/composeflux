package reconcile

import (
	"context"
	"errors"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	"github.com/veerendra2/composeflux/pkg/gitrepo"
)

// named matches a Compose project by name.
func named(name string) gomock.Matcher {
	return gomock.Cond(func(project *types.Project) bool { return project.Name == name })
}

func change(path string, action gitrepo.ChangeAction) []gitrepo.FileChange {
	return []gitrepo.FileChange{{Path: path, Action: action}}
}

var _ = Describe("GitSync", func() {
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

	pullNothing := func() { f.git.EXPECT().Pull(gomock.Any()).Return(nil, nil) }

	It("deploys new stacks in startup order and prunes removed managed stacks", func() {
		f.write("stacks/stack.yml", "startup_order: [db, app, missing, ../escape]\n")
		f.stack("app", webService)
		f.stack("db", webService)
		f.running(map[string][]api.ContainerSummary{"old": healthy(), "foreign": {{State: StateRunning}}})
		pullNothing()
		gomock.InOrder(
			f.docker.EXPECT().Build(ctx, named("db")).Return(nil),
			f.docker.EXPECT().Up(ctx, named("db")).Return(nil),
			f.docker.EXPECT().Build(ctx, named("app")).Return(nil),
			f.docker.EXPECT().Up(ctx, named("app")).DoAndReturn(func(_ context.Context, project *types.Project) error {
				Expect(project.Services["web"].Labels).To(HaveKeyWithValue(LabelManaged, ValueTrue))
				return nil
			}),
			f.docker.EXPECT().Down(ctx, "old").Return(nil),
		)
		r.healthFailCounts["app"] = 2

		Expect(r.GitSync(ctx, false)).To(Succeed())
		Expect(r.hasPendingGitSync()).To(BeFalse())
		Expect(r.healthFailCounts).To(BeEmpty())
	})

	Context("with running healthy stacks", func() {
		BeforeEach(func() {
			f.stack("app", webService)
			f.write("stacks/built/Dockerfile", "FROM scratch\n")
			f.stack("built", "services:\n  web:\n    build: .\n")
			f.running(map[string][]api.ContainerSummary{"app": healthy(), "built": healthy()})
		})

		It("leaves unchanged stacks alone", func() {
			pullNothing()
			Expect(r.GitSync(ctx, false)).To(Succeed())
		})

		It("ignores unrelated changes", func() {
			f.git.EXPECT().Pull(ctx).Return(change("README.md", gitrepo.ChangeActionUpdate), nil)
			Expect(r.GitSync(ctx, false)).To(Succeed())
		})

		It("redeploys without rebuilding when an image-only stack changes", func() {
			f.git.EXPECT().Pull(ctx).Return(change("stacks/app/compose.yml", gitrepo.ChangeActionUpdate), nil)
			f.docker.EXPECT().Up(ctx, named("app")).Return(nil)
			Expect(r.GitSync(ctx, false)).To(Succeed())
		})

		It("rebuilds when a build input changes", func() {
			f.git.EXPECT().Pull(ctx).Return(change("stacks/built/Dockerfile", gitrepo.ChangeActionUpdate), nil)
			f.docker.EXPECT().Build(ctx, named("built")).Return(nil)
			f.docker.EXPECT().Up(ctx, named("built")).Return(nil)
			Expect(r.GitSync(ctx, false)).To(Succeed())
		})

		It("redeploys every stack when forced", func() {
			pullNothing()
			f.docker.EXPECT().Build(ctx, gomock.Any()).Return(nil).Times(2)
			f.docker.EXPECT().Up(ctx, gomock.Any()).Return(nil).Times(2)
			Expect(r.GitSync(ctx, true)).To(Succeed())
		})

		It("skips suspended stacks even when forced", func() {
			f.stack("app", suspendedService)
			pullNothing()
			f.docker.EXPECT().Build(ctx, named("built")).Return(nil)
			f.docker.EXPECT().Up(ctx, named("built")).Return(nil)
			Expect(r.GitSync(ctx, true)).To(Succeed())
		})

		It("skips stacks that fail to load and keeps them from being pruned", func() {
			f.stack("app", "name: renamed\n"+webService)
			pullNothing()
			Expect(r.GitSync(ctx, false)).To(Succeed())
		})

		It("retries only failed stacks on the next sync", func() {
			f.git.EXPECT().Pull(ctx).Return(append(
				change("stacks/app/compose.yml", gitrepo.ChangeActionUpdate),
				change("stacks/built/Dockerfile", gitrepo.ChangeActionUpdate)...), nil)
			f.docker.EXPECT().Build(ctx, named("built")).Return(errors.New("build failed"))
			f.docker.EXPECT().Up(ctx, named("app")).Return(errors.New("port in use"))

			err := r.GitSync(ctx, false)
			Expect(err).To(MatchError(ContainSubstring("failed to build stack built: build failed")))
			Expect(err).To(MatchError(ContainSubstring("failed to deploy stack app: port in use")))
			Expect(r.hasPendingGitSync()).To(BeTrue())

			pullNothing()
			f.docker.EXPECT().Build(ctx, named("built")).Return(nil)
			f.docker.EXPECT().Up(ctx, named("built")).Return(nil)
			f.docker.EXPECT().Up(ctx, named("app")).Return(nil)
			Expect(r.GitSync(ctx, false)).To(Succeed())
			Expect(r.hasPendingGitSync()).To(BeFalse())
		})

		It("keeps pulled changes and force when a sync fails before deployment", func() {
			f.git.EXPECT().Pull(ctx).Return(nil, errors.New("network down"))
			Expect(r.GitSync(ctx, true)).To(MatchError("network down"))
			Expect(r.hasPendingGitSync()).To(BeTrue())

			f.git.EXPECT().Pull(ctx).Return(change("stacks/app/compose.yml", gitrepo.ChangeActionUpdate), nil)
			f.write("stacks/stack.yml", "envs: [broken\n")
			Expect(r.GitSync(ctx, false)).To(MatchError(ContainSubstring("failed to load stack config")))

			f.write("stacks/stack.yml", "")
			pullNothing()
			f.docker.EXPECT().Build(ctx, gomock.Any()).Return(nil).Times(2)
			f.docker.EXPECT().Up(ctx, gomock.Any()).Return(nil).Times(2)
			Expect(r.GitSync(ctx, false)).To(Succeed())
		})
	})

	It("redeploys unhealthy stacks", func() {
		f.stack("app", webService)
		f.running(map[string][]api.ContainerSummary{"app": unhealthy()})
		pullNothing()
		f.docker.EXPECT().Build(ctx, named("app")).Return(nil)
		f.docker.EXPECT().Up(ctx, named("app")).Return(nil)
		Expect(r.GitSync(ctx, false)).To(Succeed())
	})

	It("redeploys stacks whose local secrets changed", func() {
		r = f.reconciler(f.withLocalSecrets)
		f.stack("app", webService)
		f.running(map[string][]api.ContainerSummary{"app": healthy()})
		f.git.EXPECT().Pull(ctx).Return(change("stacks/app/secrets.age", gitrepo.ChangeActionCreate), nil)
		f.local.EXPECT().Decrypt(gomock.Any()).Return(nil, nil, nil).Times(2)
		f.local.EXPECT().IsSecretFile(gomock.Any()).DoAndReturn(func(path string) bool { return path != "" }).AnyTimes()
		f.docker.EXPECT().Up(ctx, named("app")).Return(nil)
		Expect(r.GitSync(ctx, false)).To(Succeed())
	})

	It("aborts before deployment when stack secrets cannot be decrypted", func() {
		r = f.reconciler(f.withLocalSecrets)
		f.stack("app", webService)
		f.running(nil)
		pullNothing()
		f.local.EXPECT().Decrypt(f.stackRoot).Return(nil, nil, nil)
		f.local.EXPECT().Decrypt(gomock.Any()).Return(nil, nil, errors.New("bad key"))
		Expect(r.GitSync(ctx, false)).To(MatchError(errLocalSecrets))
	})

	It("returns prune failures", func() {
		pullNothing()
		f.docker.EXPECT().List(gomock.Any()).Return([]api.Stack{{Name: "old"}}, nil).Times(2)
		f.docker.EXPECT().Ps(gomock.Any(), "old").Return(healthy(), nil).Times(2)
		f.docker.EXPECT().Down(gomock.Any(), "old").Return(errors.New("busy"))
		Expect(r.GitSync(ctx, false)).To(MatchError(ContainSubstring("failed to prune stacks")))
	})
})

var _ = DescribeTable("deploymentOrder",
	func(stacks []string, startupOrder []string, expectedFirst []string) {
		selected := make(map[string]loadedStack)
		for _, name := range stacks {
			selected[name] = loadedStack{}
		}
		order := deploymentOrder(selected, startupOrder)
		Expect(order).To(ConsistOf(stacks))
		Expect(order[:len(expectedFirst)]).To(Equal(expectedFirst))
	},
	Entry("no startup order", []string{"a"}, nil, []string{}),
	Entry("startup order first", []string{"a", "b", "c"}, []string{"c", "a"}, []string{"c", "a"}),
	Entry("unselected startup entries skipped", []string{"a", "b"}, []string{"x", "b"}, []string{"b"}),
	Entry("nothing selected", []string{}, []string{"a"}, []string{}),
)

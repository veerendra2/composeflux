package dockercompose

import (
	"context"
	"errors"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	mobyClient "github.com/moby/moby/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("client", func() {
	var (
		ctx     context.Context
		compose *MockCompose
		docker  *MockAPIClient
		c       *client
		project *types.Project
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl := gomock.NewController(GinkgoT())
		compose = NewMockCompose(ctrl)
		docker = NewMockAPIClient(ctrl)
		c = &client{compose: compose, docker: docker, removeOrphans: true, logHook: &slogHook{}}
		project = &types.Project{Name: "app", Services: types.Services{
			"web": {Name: "web", Image: "nginx"},
		}}
	})

	Describe("LoadProject", func() {
		It("loads with the configured files and tags Docker logs with the stack name during the call", func() {
			cfg := ComposeConfig{ComposeFiles: []string{"/stacks/app/compose.yml"}, WorkingDir: "/stacks/app", Env: []string{"A=1"}}
			compose.EXPECT().LoadProject(ctx, gomock.Any()).DoAndReturn(
				func(_ context.Context, options api.ProjectLoadOptions) (*types.Project, error) {
					Expect(options.ConfigPaths).To(Equal(cfg.ComposeFiles))
					Expect(options.WorkingDir).To(Equal(cfg.WorkingDir))
					Expect(options.ProjectOptionsFns).To(HaveLen(4))
					Expect(c.logHook.getStackName()).To(Equal("app"))
					return project, nil
				})

			loaded, err := c.LoadProject(ctx, cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded).To(BeIdenticalTo(project))
			Expect(c.logHook.getStackName()).To(BeEmpty())
		})

		It("returns load errors and clears the stack name", func() {
			compose.EXPECT().LoadProject(ctx, gomock.Any()).Return(nil, errors.New("invalid compose"))

			_, err := c.LoadProject(ctx, ComposeConfig{WorkingDir: "/stacks/app"})
			Expect(err).To(MatchError("invalid compose"))
			Expect(c.logHook.getStackName()).To(BeEmpty())
		})
	})

	Describe("Build", func() {
		It("skips projects without build configuration", func() {
			Expect(c.Build(ctx, project)).To(Succeed())
		})

		It("builds projects with at least one build service", func() {
			project.Services["api"] = types.ServiceConfig{Name: "api", Build: &types.BuildConfig{Context: "."}}
			compose.EXPECT().Build(ctx, project, api.BuildOptions{Quiet: true}).Return(errors.New("build failed"))

			Expect(c.Build(ctx, project)).To(MatchError("build failed"))
		})
	})

	It("Down removes orphans according to configuration", func() {
		compose.EXPECT().Down(ctx, "app", api.DownOptions{RemoveOrphans: true}).Return(nil)
		Expect(c.Down(ctx, "app")).To(Succeed())
	})

	It("List includes stopped stacks", func() {
		stacks := []api.Stack{{Name: "app"}}
		compose.EXPECT().List(ctx, api.ListOptions{All: true}).Return(stacks, nil)
		Expect(c.List(ctx)).To(Equal(stacks))
	})

	It("Ps includes stopped containers", func() {
		containers := []api.ContainerSummary{{Name: "app-web-1"}}
		compose.EXPECT().Ps(ctx, "app", api.PsOptions{All: true}).Return(containers, nil)
		Expect(c.Ps(ctx, "app")).To(Equal(containers))
	})

	It("Up recovers interrupted recreates before recreating diverged resources", func() {
		gomock.InOrder(
			docker.EXPECT().ContainerList(ctx, gomock.Any()).Return(mobyClient.ContainerListResult{}, nil),
			compose.EXPECT().Up(ctx, project, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ *types.Project, options api.UpOptions) error {
					Expect(options.Create.RemoveOrphans).To(BeTrue())
					Expect(options.Create.Recreate).To(Equal(api.RecreateDiverged))
					Expect(options.Create.RecreateDependencies).To(Equal(api.RecreateDiverged))
					Expect(options.Create.Inherit).To(BeTrue())
					Expect(options.Start.Project).To(BeIdenticalTo(project))
					return nil
				}),
		)
		Expect(c.Up(ctx, project)).To(Succeed())
	})

	Describe("Prune", func() {
		It("prunes volumes, dangling images, and all build cache", func() {
			docker.EXPECT().VolumePrune(ctx, mobyClient.VolumePruneOptions{}).Return(mobyClient.VolumePruneResult{}, nil)
			docker.EXPECT().ImagePrune(ctx, gomock.Any()).DoAndReturn(
				func(_ context.Context, options mobyClient.ImagePruneOptions) (mobyClient.ImagePruneResult, error) {
					Expect(options.Filters).To(HaveKeyWithValue("dangling", HaveKey("true")))
					return mobyClient.ImagePruneResult{}, nil
				})
			docker.EXPECT().BuildCachePrune(ctx, mobyClient.BuildCachePruneOptions{All: true}).Return(mobyClient.BuildCachePruneResult{}, nil)

			c.Prune(ctx)
		})

		It("continues after individual prune failures", func() {
			docker.EXPECT().VolumePrune(ctx, gomock.Any()).Return(mobyClient.VolumePruneResult{}, errors.New("volume"))
			docker.EXPECT().ImagePrune(ctx, gomock.Any()).Return(mobyClient.ImagePruneResult{}, errors.New("image"))
			docker.EXPECT().BuildCachePrune(ctx, gomock.Any()).Return(mobyClient.BuildCachePruneResult{}, errors.New("cache"))

			c.Prune(ctx)
		})
	})

	Describe("recoverReplaced", func() {
		const replacedID = "0123456789ab"

		summary := func(id, name, service, number string) container.Summary {
			return container.Summary{ID: id, Names: []string{"/" + name}, Labels: map[string]string{
				api.ServiceLabel:         service,
				api.ContainerNumberLabel: number,
			}}
		}
		listing := func(items ...container.Summary) {
			docker.EXPECT().ContainerList(ctx, gomock.Any()).DoAndReturn(
				func(_ context.Context, options mobyClient.ContainerListOptions) (mobyClient.ContainerListResult, error) {
					Expect(options.All).To(BeTrue())
					Expect(options.Filters).To(HaveKeyWithValue("label", HaveKey(api.ProjectLabel+"=app")))
					return mobyClient.ContainerListResult{Items: items}, nil
				})
		}

		It("does nothing when listing fails", func() {
			docker.EXPECT().ContainerList(ctx, gomock.Any()).Return(mobyClient.ContainerListResult{}, errors.New("daemon down"))
			c.recoverReplaced(ctx, project)
		})

		It("renames a leftover replacement when the canonical name is free", func() {
			listing(summary("new", replacedID+"_app-web-1", "web", "1"))
			docker.EXPECT().ContainerRename(ctx, "new", mobyClient.ContainerRenameOptions{NewName: "app-web-1"}).
				Return(mobyClient.ContainerRenameResult{}, nil)

			c.recoverReplaced(ctx, project)
		})

		It("removes a leftover replacement when the canonical container still exists", func() {
			listing(
				summary("old", "app-web-1", "web", "1"),
				summary("new", replacedID+"_app-web-1", "web", "1"),
			)
			docker.EXPECT().ContainerRemove(ctx, "new", mobyClient.ContainerRemoveOptions{Force: true}).
				Return(mobyClient.ContainerRemoveResult{}, nil)

			c.recoverReplaced(ctx, project)
		})

		It("removes a second leftover after the first one was renamed", func() {
			listing(
				summary("first", replacedID+"_app-web-1", "web", "1"),
				summary("second", "ba9876543210_app-web-1", "web", "1"),
			)
			docker.EXPECT().ContainerRename(ctx, "first", gomock.Any()).Return(mobyClient.ContainerRenameResult{}, nil)
			docker.EXPECT().ContainerRemove(ctx, "second", gomock.Any()).Return(mobyClient.ContainerRemoveResult{}, nil)

			c.recoverReplaced(ctx, project)
		})

		It("uses a custom container_name as the canonical name", func() {
			project.Services["web"] = types.ServiceConfig{Name: "web", ContainerName: "frontend"}
			listing(summary("new", replacedID+"_frontend", "web", "1"))
			docker.EXPECT().ContainerRename(ctx, "new", mobyClient.ContainerRenameOptions{NewName: "frontend"}).
				Return(mobyClient.ContainerRenameResult{}, nil)

			c.recoverReplaced(ctx, project)
		})

		It("continues after rename and remove failures", func() {
			listing(
				summary("canonical", "app-db-1", "db", "1"),
				summary("rename", replacedID+"_app-web-1", "web", "1"),
				summary("remove", replacedID+"_app-db-1", "db", "1"),
			)
			project.Services["db"] = types.ServiceConfig{Name: "db"}
			docker.EXPECT().ContainerRename(ctx, "rename", gomock.Any()).Return(mobyClient.ContainerRenameResult{}, errors.New("conflict"))
			docker.EXPECT().ContainerRemove(ctx, "remove", gomock.Any()).Return(mobyClient.ContainerRemoveResult{}, errors.New("in use"))

			c.recoverReplaced(ctx, project)
		})

		It("ignores containers that are not interrupted-recreate leftovers", func() {
			listing(
				summary("canonical", "app-web-1", "web", "1"),
				summary("unknown-service", replacedID+"_app-cache-1", "cache", "1"),
				summary("no-number", replacedID+"_app-web-1", "web", ""),
				summary("bad-prefix", "NOTAHEXPREFIX_app-web-1", "web", "1"),
				summary("other-name", "something-else", "web", "1"),
				container.Summary{ID: "unnamed", Labels: map[string]string{api.ServiceLabel: "web", api.ContainerNumberLabel: "1"}},
			)

			c.recoverReplaced(ctx, project)
		})
	})
})

var _ = DescribeTable("containerName",
	func(names []string, expected string) {
		Expect(containerName(names)).To(Equal(expected))
	},
	Entry("canonical name", []string{"/app-web-1"}, "app-web-1"),
	Entry("skips link aliases", []string{"/other/alias", "/app-web-1"}, "app-web-1"),
	Entry("only link aliases", []string{"/other/alias"}, ""),
	Entry("no names", nil, ""),
)

var _ = Describe("New", func() {
	It("initializes the Compose service without contacting the daemon", func() {
		GinkgoT().Setenv("DOCKER_HOST", "unix:///nonexistent/docker.sock")
		dc, err := New(Config{RemoveOrphans: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(dc.(*client).removeOrphans).To(BeTrue())
	})
})

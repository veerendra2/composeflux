package dockercompose

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/config"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/registry"
	mobyClient "github.com/moby/moby/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"go.uber.org/mock/gomock"
)

type notFoundError struct{}

func (notFoundError) Error() string { return "no such image" }
func (notFoundError) NotFound()     {}

const (
	localDigest  = digest.Digest("sha256:1111111111111111111111111111111111111111111111111111111111111111")
	remoteDigest = digest.Digest("sha256:2222222222222222222222222222222222222222222222222222222222222222")
)

var _ = Describe("HasImageUpdates", func() {
	var (
		ctx    context.Context
		docker *MockAPIClient
		c      *client
	)

	projectWith := func(services ...types.ServiceConfig) *types.Project {
		project := &types.Project{Name: "app", Services: types.Services{}}
		for _, service := range services {
			project.Services[service.Name] = service
		}
		return project
	}
	localImage := func(repoDigests ...string) mobyClient.ImageInspectResult {
		return mobyClient.ImageInspectResult{InspectResponse: image.InspectResponse{RepoDigests: repoDigests}}
	}
	remoteImage := func(d digest.Digest) mobyClient.DistributionInspectResult {
		return mobyClient.DistributionInspectResult{DistributionInspect: registry.DistributionInspect{
			Descriptor: ocispec.Descriptor{Digest: d},
		}}
	}

	BeforeEach(func() {
		ctx = context.Background()
		docker = NewMockAPIClient(gomock.NewController(GinkgoT()))

		// Isolate registry credentials from the developer's Docker configuration.
		configDir := GinkgoT().TempDir()
		auth := base64.StdEncoding.EncodeToString([]byte("user:pass"))
		Expect(os.WriteFile(filepath.Join(configDir, "config.json"),
			[]byte(`{"auths":{"registry.example.com":{"auth":"`+auth+`"}}}`), 0o600)).To(Succeed())
		previousDir := config.Dir()
		config.SetDir(configDir)
		DeferCleanup(config.SetDir, previousDir)

		dockerCLI, err := command.NewDockerCli()
		Expect(err).NotTo(HaveOccurred())
		c = &client{docker: docker, dockerCLI: dockerCLI}
	})

	It("skips build services, services without images, and digest-pinned images", func() {
		project := projectWith(
			types.ServiceConfig{Name: "built", Image: "app:latest", Build: &types.BuildConfig{Context: "."}},
			types.ServiceConfig{Name: "noimage"},
			types.ServiceConfig{Name: "pinned", Image: "nginx@" + string(remoteDigest)},
		)
		Expect(c.HasImageUpdates(ctx, project)).To(BeFalse())
	})

	It("reports an update when the image is missing locally", func() {
		docker.EXPECT().ImageInspect(ctx, "nginx:1").Return(mobyClient.ImageInspectResult{}, notFoundError{})
		Expect(c.HasImageUpdates(ctx, projectWith(types.ServiceConfig{Name: "web", Image: "nginx:1"}))).To(BeTrue())
	})

	It("skips images that cannot be inspected", func() {
		docker.EXPECT().ImageInspect(ctx, "nginx:1").Return(mobyClient.ImageInspectResult{}, errors.New("daemon error"))
		Expect(c.HasImageUpdates(ctx, projectWith(types.ServiceConfig{Name: "web", Image: "nginx:1"}))).To(BeFalse())
	})

	It("skips locally built or loaded images without repository digests", func() {
		docker.EXPECT().ImageInspect(ctx, "local:1").Return(localImage(), nil)
		Expect(c.HasImageUpdates(ctx, projectWith(types.ServiceConfig{Name: "web", Image: "local:1"}))).To(BeFalse())
	})

	It("skips services whose remote manifest cannot be fetched", func() {
		docker.EXPECT().ImageInspect(ctx, "nginx:1").Return(localImage("nginx@"+string(localDigest)), nil)
		docker.EXPECT().DistributionInspect(ctx, "nginx:1", gomock.Any()).Return(mobyClient.DistributionInspectResult{}, errors.New("rate limited"))
		Expect(c.HasImageUpdates(ctx, projectWith(types.ServiceConfig{Name: "web", Image: "nginx:1"}))).To(BeFalse())
	})

	It("reports no update when a local repository digest matches the remote", func() {
		docker.EXPECT().ImageInspect(ctx, "nginx:1").Return(localImage("mirror/nginx@"+string(remoteDigest), "nginx@"+string(localDigest)), nil)
		docker.EXPECT().DistributionInspect(ctx, "nginx:1", gomock.Any()).Return(remoteImage(remoteDigest), nil)
		Expect(c.HasImageUpdates(ctx, projectWith(types.ServiceConfig{Name: "web", Image: "nginx:1"}))).To(BeFalse())
	})

	It("reports an update with registry credentials when the remote digest differs", func() {
		const ref = "registry.example.com/team/app:1"
		docker.EXPECT().ImageInspect(ctx, ref).Return(localImage("registry.example.com/team/app@"+string(localDigest)), nil)
		docker.EXPECT().DistributionInspect(ctx, ref, gomock.Any()).DoAndReturn(
			func(_ context.Context, _ string, options mobyClient.DistributionInspectOptions) (mobyClient.DistributionInspectResult, error) {
				decoded, err := base64.URLEncoding.DecodeString(options.EncodedRegistryAuth)
				Expect(err).NotTo(HaveOccurred())
				var auth map[string]any
				Expect(json.Unmarshal(decoded, &auth)).To(Succeed())
				Expect(auth).To(HaveKeyWithValue("username", "user"))
				Expect(auth).To(HaveKeyWithValue("password", "pass"))
				return remoteImage(remoteDigest), nil
			})

		Expect(c.HasImageUpdates(ctx, projectWith(types.ServiceConfig{Name: "web", Image: ref}))).To(BeTrue())
	})

	It("does not send credentials for unparseable image references", func() {
		const ref = "INVALID_Reference"
		docker.EXPECT().ImageInspect(ctx, ref).Return(localImage("x@"+string(localDigest)), nil)
		docker.EXPECT().DistributionInspect(ctx, ref, mobyClient.DistributionInspectOptions{}).Return(remoteImage(localDigest), nil)

		Expect(c.HasImageUpdates(ctx, projectWith(types.ServiceConfig{Name: "web", Image: ref}))).To(BeFalse())
	})
})

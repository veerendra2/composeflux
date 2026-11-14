package reconcile

import (
	"context"
	"errors"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	"github.com/veerendra2/composeflux/pkg/dockercompose"
)

var _ = Describe("secret loading", func() {
	var (
		ctx context.Context
		f   *fixture
	)

	BeforeEach(func() {
		ctx = context.Background()
		f = newFixture()
	})

	Describe("loadSharedSecrets", func() {
		It("returns an empty map without secret providers", func() {
			secrets, files, err := f.reconciler().loadSharedSecrets(f.stackRoot)
			Expect(err).NotTo(HaveOccurred())
			Expect(secrets).To(BeEmpty())
			Expect(files).To(BeNil())
		})

		It("overlays root local secrets on remote secrets", func() {
			f.remote.EXPECT().FetchAll().Return(map[string]*string{"A": ptr("remote"), "B": ptr("remote")}, nil)
			f.local.EXPECT().Decrypt(f.stackRoot).Return(map[string]*string{"B": ptr("local")}, []string{"/secrets.age"}, nil)

			secrets, files, err := f.reconciler(f.withRemoteSecrets, f.withLocalSecrets).loadSharedSecrets(f.stackRoot)
			Expect(err).NotTo(HaveOccurred())
			Expect(secrets).To(Equal(map[string]*string{"A": ptr("remote"), "B": ptr("local")}))
			Expect(files).To(Equal([]string{"/secrets.age"}))
		})

		It("returns remote fetch errors", func() {
			f.remote.EXPECT().FetchAll().Return(nil, errors.New("unauthorized"))
			_, _, err := f.reconciler(f.withRemoteSecrets).loadSharedSecrets(f.stackRoot)
			Expect(err).To(MatchError(ContainSubstring("failed to fetch remote secrets: unauthorized")))
		})

		It("returns root decryption errors", func() {
			f.local.EXPECT().Decrypt(f.stackRoot).Return(nil, nil, errors.New("bad key"))
			_, _, err := f.reconciler(f.withLocalSecrets).loadSharedSecrets(f.stackRoot)
			Expect(err).To(MatchError(ContainSubstring("failed to decrypt root local secrets")))
		})
	})

	Describe("loadProjectWithSecrets", func() {
		config := func(stack string, env ...string) dockercompose.ComposeConfig {
			cfg, err := buildComposeConfig(filepath.Join(f.stackRoot, stack), env)
			Expect(err).NotTo(HaveOccurred())
			return cfg
		}

		It("interpolates shared and stack secrets and records Compose sources", func() {
			compose := f.stack("app", "services:\n  web:\n    image: ${IMAGE}:${TAG}\n    environment:\n      DOMAIN: ${DOMAIN}\n")
			f.local.EXPECT().Decrypt(filepath.Dir(compose)).Return(map[string]*string{"TAG": ptr("2")}, []string{"/app/secrets.age"}, nil)

			loaded, err := f.reconciler(f.withLocalSecrets).loadProjectWithSecrets(ctx, f.repo, config("app", "DOMAIN=example.com"),
				map[string]*string{"IMAGE": ptr("nginx"), "TAG": ptr("1"), "UNSET": nil})
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded.project.Services["web"].Image).To(Equal("nginx:2"))
			Expect(loaded.project.Services["web"].Environment).To(HaveKeyWithValue("DOMAIN", ptr("example.com")))
			Expect(loaded.project.ComposeFiles).To(Equal([]string{compose}))
			Expect(loaded.localSecretFiles).To(Equal([]string{"/app/secrets.age"}))
			Expect(loaded.localSecretDirs).To(Equal([]string{filepath.Dir(compose)}))
		})

		It("decrypts each included directory once and applies its secrets", func() {
			f.stack("app", "include:\n  - ../shared/a.yml\n  - ../shared/b.yml\n")
			f.write("stacks/shared/a.yml", "services:\n  a:\n    image: ${SHARED_IMAGE}\n")
			f.write("stacks/shared/b.yml", "services:\n  b:\n    image: ${SHARED_IMAGE}\n")
			sharedDir := filepath.Join(f.stackRoot, "shared")
			f.local.EXPECT().Decrypt(filepath.Join(f.stackRoot, "app")).Return(nil, nil, nil)
			f.local.EXPECT().Decrypt(sharedDir).Return(map[string]*string{"SHARED_IMAGE": ptr("redis")}, []string{"/shared/s.age"}, nil).Times(1)

			loaded, err := f.reconciler(f.withLocalSecrets).loadProjectWithSecrets(ctx, f.repo, config("app"), nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded.project.Services["a"].Image).To(Equal("redis"))
			Expect(loaded.project.Services["b"].Image).To(Equal("redis"))
			Expect(loaded.localSecretDirs).To(ContainElement(sharedDir))
			Expect(loaded.localSecretFiles).To(Equal([]string{"/shared/s.age"}))
		})

		It("marks stack decryption failures as local secret errors", func() {
			f.stack("app", webService)
			f.local.EXPECT().Decrypt(gomock.Any()).Return(nil, nil, errors.New("bad key"))

			_, err := f.reconciler(f.withLocalSecrets).loadProjectWithSecrets(ctx, f.repo, config("app"), nil)
			Expect(err).To(MatchError(errLocalSecrets))
		})

		It("marks included-directory decryption failures as local secret errors", func() {
			f.stack("app", "include:\n  - ../shared/a.yml\n")
			f.write("stacks/shared/a.yml", webService)
			f.local.EXPECT().Decrypt(filepath.Join(f.stackRoot, "app")).Return(nil, nil, nil)
			f.local.EXPECT().Decrypt(filepath.Join(f.stackRoot, "shared")).Return(nil, nil, errors.New("bad key"))

			_, err := f.reconciler(f.withLocalSecrets).loadProjectWithSecrets(ctx, f.repo, config("app"), nil)
			Expect(err).To(MatchError(errLocalSecrets))
		})

		DescribeTable("returns load errors",
			func(compose, dotenv, expected string) {
				f.stack("app", compose)
				if dotenv != "" {
					f.write("stacks/app/.env", dotenv)
				}
				_, err := f.reconciler().loadProjectWithSecrets(ctx, f.repo, config("app"), nil)
				Expect(err).To(MatchError(ContainSubstring(expected)))
			},
			Entry("project name differs from the directory", "name: other\n"+webService, "", `compose project name "other" must match stack directory "app"`),
			Entry("invalid Compose project", "services:\n  web:\n    image: ${IMAGE?required}\n", "", "required"),
			Entry("missing include", "include:\n  - missing.yml\n", "", "invalid included compose file path"),
			Entry("malformed .env", webService, "BAD LINE\n", "failed to prepare Compose environment"),
		)

		It("rejects stack directories outside the repository", func() {
			_, err := f.reconciler().loadProjectWithSecrets(ctx, f.repo, dockercompose.ComposeConfig{WorkingDir: "/"}, nil)
			Expect(err).To(MatchError(ContainSubstring("invalid stack directory")))
		})
	})
})

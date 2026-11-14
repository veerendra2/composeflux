package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"golang.org/x/crypto/ssh"

	"github.com/veerendra2/composeflux/internal/reconcile"
	"github.com/veerendra2/composeflux/pkg/gitrepo"
	"github.com/veerendra2/composeflux/pkg/remotesecrets"
)

var validReconciler = reconcile.Config{StackPath: "stacks", ConfigFile: "stack.yml", GitInterval: time.Minute}

var _ = DescribeTable("CommonConfig.Validate",
	func(cfg CommonConfig, expectedError string) {
		run, sync := &RunCmd{CommonConfig: cfg}, &SyncCmd{CommonConfig: cfg}
		for _, err := range []error{cfg.Validate(), run.AfterApply(), sync.AfterApply()} {
			if expectedError == "" {
				Expect(err).NotTo(HaveOccurred())
			} else {
				Expect(err).To(MatchError(ContainSubstring(expectedError)))
			}
		}
	},
	Entry("valid configuration", CommonConfig{Reconciler: validReconciler}, ""),
	Entry("deploy key with remote secrets", CommonConfig{
		Reconciler:    validReconciler,
		Source:        gitrepo.Config{DeployKeySecretRef: "deploy-key"},
		RemoteSecrets: remotesecrets.Config{Infisical: remotesecrets.InfisicalConfig{ClientID: "id"}},
	}, ""),
	Entry("deploy key without remote secrets", CommonConfig{
		Reconciler: validReconciler,
		Source:     gitrepo.Config{DeployKeySecretRef: "deploy-key"},
	}, "--deploy-key-secret-ref requires Bitwarden or Infisical credentials"),
	Entry("invalid reconciler configuration", CommonConfig{}, "--git-interval must be greater than zero"),
)

var _ = Describe("writeDeployKey", func() {
	var (
		secrets *MockRemoteSecretsClient
		cfg     *CommonConfig
	)

	BeforeEach(func() {
		secrets = NewMockRemoteSecretsClient(gomock.NewController(GinkgoT()))
		cfg = &CommonConfig{Source: gitrepo.Config{
			DeployKeySecretRef: "deploy-key",
			SSHKeyPath:         filepath.Join(GinkgoT().TempDir(), ".ssh", "id_ed25519"),
		}}
	})

	It("requires a remote secrets client", func() {
		Expect(cfg.writeDeployKey(nil)).To(MatchError(ContainSubstring("requires Bitwarden or Infisical credentials")))
	})

	It("returns secret lookup errors", func() {
		secrets.EXPECT().Get("deploy-key").Return("", errors.New("not found"))
		Expect(cfg.writeDeployKey(secrets)).To(MatchError("not found"))
	})

	It("rejects empty keys", func() {
		secrets.EXPECT().Get("deploy-key").Return("", nil)
		Expect(cfg.writeDeployKey(secrets)).To(MatchError("SSH deploy key content is empty: deploy-key"))
	})

	It("replaces an existing key with owner-only permissions", func() {
		Expect(os.MkdirAll(filepath.Dir(cfg.Source.SSHKeyPath), 0o700)).To(Succeed())
		Expect(os.WriteFile(cfg.Source.SSHKeyPath, []byte("old"), 0o400)).To(Succeed())
		secrets.EXPECT().Get("deploy-key").Return("new key", nil)

		Expect(cfg.writeDeployKey(secrets)).To(Succeed())
		Expect(os.ReadFile(cfg.Source.SSHKeyPath)).To(Equal([]byte("new key")))
		info, err := os.Stat(cfg.Source.SSHKeyPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
	})
})

var _ = Describe("client initialization", func() {
	var (
		ctx context.Context
		cfg *CommonConfig
	)

	// localRepository creates a Git repository with one commit on main.
	localRepository := func() string {
		dir := GinkgoT().TempDir()
		repo, err := git.PlainInitWithOptions(dir, &git.PlainInitOptions{
			InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(os.MkdirAll(filepath.Join(dir, "stacks"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "stacks", "stack.yml"), nil, 0o644)).To(Succeed())
		w, err := repo.Worktree()
		Expect(err).NotTo(HaveOccurred())
		_, err = w.Add("stacks/stack.yml")
		Expect(err).NotTo(HaveOccurred())
		_, err = w.Commit("init", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()}})
		Expect(err).NotTo(HaveOccurred())
		return dir
	}

	writeSSHKey := func() string {
		_, key, err := ed25519.GenerateKey(rand.Reader)
		Expect(err).NotTo(HaveOccurred())
		block, err := ssh.MarshalPrivateKey(key, "")
		Expect(err).NotTo(HaveOccurred())
		path := filepath.Join(GinkgoT().TempDir(), "id_ed25519")
		Expect(os.WriteFile(path, pem.EncodeToMemory(block), 0o600)).To(Succeed())
		return path
	}

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(context.Background())
		DeferCleanup(cancel)
		cfg = &CommonConfig{
			Reconciler: validReconciler,
			Source: gitrepo.Config{
				RepoURL:    filepath.Join(GinkgoT().TempDir(), "missing"),
				SSHKeyPath: filepath.Join(GinkgoT().TempDir(), "missing-key"),
				ClonePath:  filepath.Join(GinkgoT().TempDir(), "clone"),
				Branch:     "main",
			},
		}
	})

	DescribeTable("InitClients returns setup errors",
		func(mutate func(*CommonConfig), expectedError string, hasCleanup bool) {
			mutate(cfg)
			reconciler, cleanup, err := cfg.InitClients(ctx)
			Expect(err).To(MatchError(ContainSubstring(expectedError)))
			Expect(reconciler).To(BeNil())
			if hasCleanup {
				Expect(cleanup).NotTo(BeNil())
				cleanup()
			} else {
				Expect(cleanup).To(BeNil())
			}
		},
		Entry("multiple remote secret providers", func(c *CommonConfig) {
			c.RemoteSecrets = remotesecrets.Config{
				Bitwarden: remotesecrets.BitwardenConfig{AccessToken: "t", OrgID: "o", ProjectID: "p"},
				Infisical: remotesecrets.InfisicalConfig{ClientID: "id", ClientSecret: "s", Environment: "e", ProjectID: "p"},
			}
		}, "multiple remote secrets providers", false),
		Entry("invalid Git source", func(*CommonConfig) {}, "", true),
	)

	It("returns remote secrets client errors", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
		}))
		DeferCleanup(server.Close)
		cfg.RemoteSecrets.Infisical = remotesecrets.InfisicalConfig{
			ClientID: "id", ClientSecret: "secret", Environment: "prod", ProjectID: "project",
			SiteUrl: server.URL, SecretPath: "/",
		}

		reconciler, cleanup, err := cfg.InitClients(ctx)
		Expect(err).To(HaveOccurred())
		Expect(reconciler).To(BeNil())
		Expect(cleanup).To(BeNil())
	})

	It("returns Docker errors after cloning the repository", func() {
		GinkgoT().Setenv("DOCKER_HOST", "unix://"+filepath.Join(GinkgoT().TempDir(), "docker.sock"))
		cfg.Source.RepoURL = localRepository()
		cfg.Source.SSHKeyPath = writeSSHKey()

		reconciler, cleanup, err := cfg.InitClients(ctx)
		Expect(err).To(HaveOccurred())
		Expect(reconciler).To(BeNil())
		cleanup()
		Expect(filepath.Join(cfg.Source.ClonePath, "stacks", "stack.yml")).To(BeAnExistingFile())
	})

	It("commands return setup errors before running", func() {
		cfg.Source.DeployKeySecretRef = "deploy-key"
		_, _, cleanup, err := cfg.Setup()
		Expect(err).To(HaveOccurred())
		Expect(cleanup).To(BeNil())

		Expect((&RunCmd{CommonConfig: *cfg}).Run()).To(MatchError(ContainSubstring("requires Bitwarden or Infisical credentials")))
		Expect((&SyncCmd{CommonConfig: *cfg}).Run()).To(MatchError(ContainSubstring("requires Bitwarden or Infisical credentials")))
	})
})

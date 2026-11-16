package remotesecrets

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"

	infisical "github.com/infisical/go-sdk"
	"github.com/infisical/go-sdk/packages/models"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("infisicalClient", func() {
	var (
		secrets *MockInfisicalSecrets
		client  *infisicalClient
	)

	listOptions := func(path string) infisical.ListSecretsOptions {
		return infisical.ListSecretsOptions{Environment: "prod", ProjectID: "project", SecretPath: path}
	}
	retrieveOptions := func(key, path string) infisical.RetrieveSecretOptions {
		return infisical.RetrieveSecretOptions{SecretKey: key, Environment: "prod", ProjectID: "project", SecretPath: path}
	}

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		inf := NewMockInfisicalClient(ctrl)
		secrets = NewMockInfisicalSecrets(ctrl)
		inf.EXPECT().Secrets().Return(secrets).AnyTimes()
		client = &infisicalClient{
			projectID:   "project",
			environment: "prod",
			secretPaths: []string{"/shared", "/app"},
			infClient:   inf,
		}
	})

	Describe("FetchAll", func() {
		It("merges all paths with later paths taking precedence", func() {
			secrets.EXPECT().ListSecrets(listOptions("/shared")).Return(infisical.ListSecretsResult{Secrets: []models.Secret{
				{SecretKey: "A", SecretValue: "shared-a"},
				{SecretKey: "B", SecretValue: "shared-b"},
			}}, nil)
			secrets.EXPECT().ListSecrets(listOptions("/app")).Return(infisical.ListSecretsResult{Secrets: []models.Secret{
				{SecretKey: "B", SecretValue: "app-b"},
			}}, nil)

			result, err := client.FetchAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(2))
			Expect(*result["A"]).To(Equal("shared-a"))
			Expect(*result["B"]).To(Equal("app-b"))
		})

		It("keeps successful paths when another path fails", func() {
			secrets.EXPECT().ListSecrets(listOptions("/shared")).Return(infisical.ListSecretsResult{}, errors.New("forbidden"))
			secrets.EXPECT().ListSecrets(listOptions("/app")).Return(infisical.ListSecretsResult{Secrets: []models.Secret{
				{SecretKey: "A", SecretValue: "app-a"},
			}}, nil)

			result, err := client.FetchAll()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveKey("A"))
		})

		It("fails when every path fails", func() {
			secrets.EXPECT().ListSecrets(gomock.Any()).Return(infisical.ListSecretsResult{}, errors.New("down")).Times(2)

			_, err := client.FetchAll()
			Expect(err).To(MatchError(ContainSubstring("all configured paths")))
		})
	})

	Describe("Get", func() {
		It("returns the first path that contains the key", func() {
			secrets.EXPECT().Retrieve(retrieveOptions("SSH_KEY", "/shared")).Return(models.Secret{}, errors.New("not found"))
			secrets.EXPECT().Retrieve(retrieveOptions("SSH_KEY", "/app")).Return(models.Secret{SecretValue: "key"}, nil)

			Expect(client.Get("SSH_KEY")).To(Equal("key"))
		})

		It("fails when no path contains the key", func() {
			secrets.EXPECT().Retrieve(gomock.Any()).Return(models.Secret{}, errors.New("not found")).Times(2)

			_, err := client.Get("SSH_KEY")
			Expect(err).To(MatchError(ContainSubstring(`secret "SSH_KEY" not found`)))
		})
	})
})

var _ = Describe("NewInfisicalClient", func() {
	var server *httptest.Server

	BeforeEach(func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/v1/auth/universal-auth/login") {
				http.NotFound(w, r)
				return
			}
			var body struct {
				ClientSecret string `json:"clientSecret"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.ClientSecret != "valid" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"invalid credentials"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"accessToken":"token","expiresIn":3600,"accessTokenMaxTTL":7200,"tokenType":"Bearer"}`))
		}))
		DeferCleanup(server.Close)
	})

	newClient := func(cfg InfisicalConfig) (Client, error) {
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel) // stops the SDK token-refresh goroutine
		cfg.SiteUrl = server.URL
		cfg.ClientID = "id"
		cfg.Environment = "prod"
		cfg.ProjectID = "project"
		return NewInfisicalClient(ctx, cfg)
	}

	It("authenticates and parses comma-separated secret paths", func() {
		client, err := newClient(InfisicalConfig{ClientSecret: "valid", SecretPath: " /a , ,/b "})
		Expect(err).NotTo(HaveOccurred())
		inf := client.(*infisicalClient)
		Expect(inf.secretPaths).To(Equal([]string{"/a", "/b"}))
		Expect(inf.projectID).To(Equal("project"))
		Expect(inf.environment).To(Equal("prod"))
	})

	It("defaults an empty secret path to the root", func() {
		client, err := newClient(InfisicalConfig{ClientSecret: "valid"})
		Expect(err).NotTo(HaveOccurred())
		Expect(client.(*infisicalClient).secretPaths).To(Equal([]string{"/"}))
	})

	It("rejects a secret path list without entries", func() {
		_, err := newClient(InfisicalConfig{ClientSecret: "valid", SecretPath: " , "})
		Expect(err).To(MatchError(ContainSubstring("no secret paths provided")))
	})

	It("returns authentication errors", func() {
		_, err := newClient(InfisicalConfig{ClientSecret: "wrong"})
		Expect(err).To(HaveOccurred())
	})

	It("is selected by New for an Infisical configuration", func() {
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)
		client, err := New(ctx, Config{Infisical: InfisicalConfig{
			SiteUrl: server.URL, ClientID: "id", ClientSecret: "valid", Environment: "prod", ProjectID: "project",
		}})
		Expect(err).NotTo(HaveOccurred())
		Expect(client).To(BeAssignableToTypeOf(&infisicalClient{}))
	})
})

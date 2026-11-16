package remotesecrets

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Config", func() {
	validBitwarden := BitwardenConfig{AccessToken: "token", OrgID: "org", ProjectID: "project"}
	validInfisical := InfisicalConfig{ClientID: "id", ClientSecret: "secret", Environment: "prod", ProjectID: "project"}

	DescribeTable("Configured",
		func(cfg Config, expected bool) {
			Expect(cfg.Configured()).To(Equal(expected))
		},
		Entry("empty", Config{}, false),
		Entry("bitwarden access token", Config{Bitwarden: BitwardenConfig{AccessToken: "t"}}, true),
		Entry("infisical client ID", Config{Infisical: InfisicalConfig{ClientID: "id"}}, true),
	)

	DescribeTable("Provider",
		func(cfg Config, expected, errSubstring string) {
			provider, err := cfg.Provider()
			if errSubstring != "" {
				Expect(err).To(MatchError(ContainSubstring(errSubstring)))
				Expect(provider).To(BeEmpty())
				return
			}
			Expect(err).NotTo(HaveOccurred())
			Expect(provider).To(Equal(expected))
		},
		Entry("none configured", Config{}, "", ""),
		Entry("valid bitwarden", Config{Bitwarden: validBitwarden}, "bitwarden", ""),
		Entry("valid infisical", Config{Infisical: validInfisical}, "infisical", ""),
		Entry("both providers", Config{Bitwarden: validBitwarden, Infisical: validInfisical}, "", "multiple remote secrets providers"),
		Entry("incomplete bitwarden", Config{Bitwarden: BitwardenConfig{AccessToken: "t"}}, "", "bitwarden provider requires"),
		Entry("incomplete infisical", Config{Infisical: InfisicalConfig{ClientID: "id"}}, "", "infisical provider requires"),
	)
})

var _ = Describe("New", func() {
	It("returns a nil client when no provider is configured", func() {
		client, err := New(context.Background(), Config{})
		Expect(err).NotTo(HaveOccurred())
		Expect(client).To(BeNil())
	})

	It("returns the provider validation error", func() {
		_, err := New(context.Background(), Config{Infisical: InfisicalConfig{ProjectID: "p"}})
		Expect(err).To(MatchError(ContainSubstring("infisical provider requires")))
	})
})

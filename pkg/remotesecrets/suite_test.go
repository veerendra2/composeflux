package remotesecrets

//go:generate go tool mockgen -destination=mock_infisical_test.go -package=remotesecrets -mock_names=SecretsInterface=MockInfisicalSecrets,InfisicalClientInterface=MockInfisicalClient github.com/infisical/go-sdk InfisicalClientInterface,SecretsInterface
//go:generate go tool mockgen -destination=mock_bitwarden_test.go -package=remotesecrets -mock_names=SecretsInterface=MockBitwardenSecrets,BitwardenClientInterface=MockBitwardenClient github.com/bitwarden/sdk-go/v2 BitwardenClientInterface,SecretsInterface

import (
	"io"
	"log/slog"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRemoteSecrets(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	RegisterFailHandler(Fail)
	RunSpecs(t, "RemoteSecrets Suite")
}

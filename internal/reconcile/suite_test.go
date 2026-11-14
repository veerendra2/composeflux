package reconcile

//go:generate go tool mockgen -destination=mock_dockercompose_test.go -package=reconcile -mock_names=Client=MockDockerClient github.com/veerendra2/composeflux/pkg/dockercompose Client
//go:generate go tool mockgen -destination=mock_gitrepo_test.go -package=reconcile -mock_names=Client=MockGitClient github.com/veerendra2/composeflux/pkg/gitrepo Client
//go:generate go tool mockgen -destination=mock_localsecrets_test.go -package=reconcile -mock_names=Client=MockLocalSecretsClient github.com/veerendra2/composeflux/pkg/localsecrets Client
//go:generate go tool mockgen -destination=mock_remotesecrets_test.go -package=reconcile -mock_names=Client=MockRemoteSecretsClient github.com/veerendra2/composeflux/pkg/remotesecrets Client

import (
	"io"
	"log/slog"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestReconcile(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	RegisterFailHandler(Fail)
	RunSpecs(t, "Reconcile Suite")
}

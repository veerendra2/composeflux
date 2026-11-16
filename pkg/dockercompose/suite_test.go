package dockercompose

//go:generate go tool mockgen -destination=mock_compose_test.go -package=dockercompose github.com/docker/compose/v5/pkg/api Compose
// Alias the moby package (it collides with the client type) and name unnamed variadics that mockgen cannot render.
//go:generate sh -c "go tool mockgen -package=dockercompose github.com/moby/moby/client APIClient | sed -e 's/client \"github.com/mobyClient \"github.com/' -e 's/\\([^A-Za-z_]\\)client\\./\\1mobyClient./g' -e 's/, _ \\.\\.\\./, opts .../' -e 's/range _ {/range opts {/' -e 's/}, _\\.\\.\\.)/}, opts...)/' > mock_moby_test.go"

import (
	"io"
	"log/slog"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDockerCompose(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	RegisterFailHandler(Fail)
	RunSpecs(t, "DockerCompose Suite")
}

package main

//go:generate go tool mockgen -destination=mock_remotesecrets_test.go -package=main -mock_names=Client=MockRemoteSecretsClient github.com/veerendra2/composeflux/pkg/remotesecrets Client

import (
	"io"
	"log/slog"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestComposeFlux(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	RegisterFailHandler(Fail)
	RunSpecs(t, "ComposeFlux Suite")
}

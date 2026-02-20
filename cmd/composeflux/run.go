package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/veerendra2/gopackages/version"
)

type RunCmd struct {
	CommonConfig `embed:""`
}

func (r *RunCmd) AfterApply() error {
	return r.Validate()
}

func (r *RunCmd) Run() error {
	slog.Info("Version information", version.Info()...)
	slog.Info("Build context", version.BuildContext()...)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rClient, cleanup, err := r.InitClients(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	rClient.Run(ctx)
	return nil
}

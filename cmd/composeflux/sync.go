package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/veerendra2/gopackages/version"
)

type SyncCmd struct {
	CommonConfig `embed:""`
}

func (s *SyncCmd) AfterApply() error {
	return s.Validate()
}

func (s *SyncCmd) Run() error {
	slog.Info("Version information", version.Info()...)
	slog.Info("Build context", version.BuildContext()...)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rClient, cleanup, err := s.InitClients(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	syncCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	slog.Info("Starting one-shot sync")
	if err := rClient.Sync(syncCtx); err != nil {
		slog.Error("Sync failed", "error", err)
		return err
	}

	slog.Info("Sync completed successfully")
	return nil
}

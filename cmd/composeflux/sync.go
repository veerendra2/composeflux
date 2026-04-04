package main

import (
	"context"
	"log/slog"
	"time"
)

type SyncCmd struct {
	CommonConfig `embed:""`
}

func (s *SyncCmd) AfterApply() error {
	return s.Validate()
}

func (s *SyncCmd) Run() error {
	rClient, ctx, cleanup, err := s.Setup()
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

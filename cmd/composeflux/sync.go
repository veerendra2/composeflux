package main

import (
	"context"
	"log/slog"
	"time"
)

type SyncCmd struct {
	CommonConfig `embed:""`
}

// AfterApply validates the shared configuration after CLI values are applied.
func (s *SyncCmd) AfterApply() error {
	return s.Validate()
}

// Run performs one forced reconciliation and then exits.
func (s *SyncCmd) Run() error {
	rClient, ctx, cleanup, err := s.Setup()
	if err != nil {
		return err
	}
	defer cleanup()

	syncCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	slog.Info("Starting one-shot sync")
	if err := rClient.GitSync(syncCtx, true); err != nil {
		slog.Error("Sync failed", "error", err)
		return err
	}

	slog.Info("One-shot sync finished")
	return nil
}

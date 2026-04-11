// Runs reconciliation loop to schedule tasks periodically
package reconcile

import (
	"context"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
)

func (r *Reconciler) Run(ctx context.Context) {
	// Sync from Git during bootstrap
	if err := r.Sync(ctx); err != nil {
		slog.Error("Failed initial sync", "error", err)
		return
	}

	gitTicker := time.NewTicker(r.gitInterval)
	defer gitTicker.Stop()

	// Set up image update cron job if schedule is configured
	if r.imageUpdateSchedule != "" {
		c := cron.New()
		if _, err := c.AddFunc(r.imageUpdateSchedule, func() {
			imageCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			defer cancel()
			slog.Debug("Running image update check", "cron", r.imageUpdateSchedule)
			if err := r.SyncImages(imageCtx); err != nil {
				slog.Error("Failed to sync image updates", "error", err)
			}
		}); err != nil {
			slog.Error("Invalid image update cron schedule, image updates disabled", "cron", r.imageUpdateSchedule, "error", err)
		} else {
			c.Start()
			defer c.Stop()
			slog.Debug("Image update checks scheduled", "cron", r.imageUpdateSchedule)
		}
	}

	slog.Info("Starting reconciliation")

	for {
		select {
		case <-ctx.Done():
			slog.Info("Shutdown signal received, reconciliation stopped")
			return

		case <-gitTicker.C:
			func() {
				checkCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
				defer cancel()

				ok, remoteSHA, localSHA, err := r.gClient.HasUpdates(checkCtx)
				slog.Debug("Fetch git updates", "remote_sha", remoteSHA, "local_sha",
					localSHA, "updates", ok)

				if err != nil {
					slog.Error("Failed to check git updates", "error", err)
					return
				}
				if ok {
					if err := r.Sync(checkCtx); err != nil {
						slog.Error("Failed to sync from git", "error", err)
					}
				}
			}()
		}
	}
}

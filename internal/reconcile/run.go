// Runs reconciliation loop to schedule tasks periodically
package reconcile

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
)

// Run schedules Git, health, image-update, and prune reconciliation until cancellation.
func (r *Reconciler) Run(ctx context.Context) error {
	// Sync from Git during bootstrap
	if err := r.GitSync(ctx, false); err != nil {
		return fmt.Errorf("failed initial sync: %w", err)
	}

	gitTicker := time.NewTicker(r.gitInterval)
	defer gitTicker.Stop()

	// nil channel blocks forever — safe to use in select when HEALTH_RECONCILE_INTERVAL is 0
	var healthTickerC <-chan time.Time
	if r.healthInterval != 0 {
		healthTicker := time.NewTicker(r.healthInterval)
		defer healthTicker.Stop()
		healthTickerC = healthTicker.C
	}

	// nil channel blocks forever — safe to use in select when PRUNE_INTERVAL is unset
	var pruneTickerC <-chan time.Time
	if r.pruneInterval != 0 {
		pruneTicker := time.NewTicker(r.pruneInterval)
		defer pruneTicker.Stop()
		pruneTickerC = pruneTicker.C
	}

	// Set up image update cron job if schedule is configured
	if r.imageUpdateSchedule != "" {
		cronLogger := cron.VerbosePrintfLogger(
			slog.NewLogLogger(slog.Default().With("source", "image-update-cron").Handler(), slog.LevelDebug),
		)
		c := cron.New(cron.WithChain(cron.SkipIfStillRunning(cronLogger)))
		if _, err := c.AddFunc(r.imageUpdateSchedule, func() {
			imageCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			defer cancel()
			slog.Debug("Running image update check", "cron", r.imageUpdateSchedule)
			if err := r.UpdateImages(imageCtx); err != nil {
				slog.Error("Failed to sync image updates", "error", err)
			}
		}); err != nil {
			slog.Error("Invalid image update cron schedule, image updates disabled", "cron", r.imageUpdateSchedule, "error", err)
		} else {
			c.Start()
			defer func() {
				<-c.Stop().Done()
			}()
			slog.Debug("Image update checks scheduled", "cron", r.imageUpdateSchedule)
		}
	}

	slog.Info("Starting reconciliation")

	for {
		select {
		case <-ctx.Done():
			slog.Info("Shutdown signal received, stopping reconciliation")
			return nil

		case <-gitTicker.C:
			func() {
				checkCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
				defer cancel()

				if r.hasPendingGitSync() {
					if err := r.GitSync(checkCtx, false); err != nil {
						slog.Error("Failed to retry pending Git sync", "error", err)
					}
					return
				}

				ok, remoteSHA, localSHA, err := r.gClient.HasUpdates(checkCtx)
				slog.Debug("Fetch git updates", "remote_sha", remoteSHA, "local_sha",
					localSHA, "updates", ok)

				if err != nil {
					slog.Error("Failed to check git updates", "error", err)
					return
				}
				if ok {
					if err := r.GitSync(checkCtx, false); err != nil {
						slog.Error("Failed to sync from git", "error", err)
					}
				}
			}()

		case <-healthTickerC:
			func() {
				hCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
				defer cancel()

				if err := r.ReconcileHealth(hCtx); err != nil {
					slog.Error("Health reconciliation failed", "error", err)
				}
			}()

		case <-pruneTickerC:
			func() {
				pCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
				defer cancel()

				if err := r.PruneResources(pCtx); err != nil {
					slog.Error("Docker resource prune failed", "error", err)
				}
			}()
		}
	}
}

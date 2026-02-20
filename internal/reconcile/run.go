// Runs reconciliation loop to schedule tasks periodically
package reconcile

import (
	"context"
	"log/slog"
	"time"
)

type Timers struct {
	GitInterval time.Duration `name:"git-interval" help:"Git repository polling interval" env:"GIT_INTERVAL" default:"5m" group:"Reconciler Options:"`
}

func (r *Reconciler) Run(ctx context.Context) {
	// Sync from Git during bootstrap
	if err := r.Sync(ctx); err != nil {
		slog.Error("Failed initial sync", "error", err)
		return
	}

	gitTicker := time.NewTicker(r.gitInterval)
	defer gitTicker.Stop()

	slog.Info("Starting reconciliation loop", "git_poll_interval", r.gitInterval)

	for {
		select {
		case <-ctx.Done():
			slog.Info("Shutdown signal received, reconciliation loop stopped")
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

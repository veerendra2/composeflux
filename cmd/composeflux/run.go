package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type RunCmd struct {
	CommonConfig `embed:""`
	MetricsAddr  string `name:"metrics-addr" help:"Prometheus metrics listen address. Empty to disable." env:"METRICS_ADDR" default:":9090" group:"Metrics Options:"`
}

func (r *RunCmd) AfterApply() error {
	return r.Validate()
}

func (r *RunCmd) Run() error {
	rClient, ctx, cleanup, err := r.Setup()
	if err != nil {
		return err
	}
	defer cleanup()

	if r.MetricsAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		srv := &http.Server{Addr: r.MetricsAddr, Handler: mux}

		go func() {
			slog.Info("Starting metrics server", "addr", r.MetricsAddr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("Metrics server failed", "error", err)
			}
		}()

		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := srv.Shutdown(shutdownCtx); err != nil {
				slog.Error("Metrics server shutdown failed", "error", err)
			}
			slog.Info("Metrics server stopped")
		}()
	}

	rClient.Run(ctx)
	return nil
}

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/migueljfsc/wtc/internal/config"
	"github.com/migueljfsc/wtc/internal/server"
	"github.com/migueljfsc/wtc/internal/store"
)

func newServeCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the wtc daemon (ingest HTTP + query API)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// A config file is optional unless --config was given explicitly.
			explicit := cmd.Flags().Changed("config")
			return runServe(configPath, !explicit)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "wtc.yaml", "path to wtc.yaml")
	return cmd
}

func runServe(configPath string, configOptional bool) error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.Load(configPath, configOptional)
	if err != nil {
		return err
	}
	if len(cfg.Auth.APITokens) == 0 {
		log.Warn("no api_tokens configured — /api/* and /ingest/generic will reject all requests")
	}

	if dir := filepath.Dir(cfg.Server.DB); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create db directory: %w", err)
		}
	}

	st, err := store.Open(cfg.Server.DB)
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           server.New(st, cfg.Auth.APITokens, log).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("wtc serve listening", "addr", cfg.Server.Listen, "db", cfg.Server.DB, "version", version)
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		_ = st.Close()
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Stop accepting requests first so most events drain cleanly; if
	// Shutdown times out with handlers still in flight, Store.Close is safe
	// anyway — stragglers get ErrStoreClosed instead of racing the writer.
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown", "error", err)
	}
	if err := st.Close(); err != nil {
		return fmt.Errorf("close store: %w", err)
	}
	return nil
}

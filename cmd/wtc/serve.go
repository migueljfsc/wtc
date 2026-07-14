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
	"github.com/migueljfsc/wtc/internal/ingest/github"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/server"
	"github.com/migueljfsc/wtc/internal/store"
)

func newServeCmd() *cobra.Command {
	var configPath, captureDir string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the wtc daemon (ingest HTTP + query API)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// A config file is optional unless --config was given explicitly.
			explicit := cmd.Flags().Changed("config")
			return runServe(configPath, !explicit, captureDir)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "wtc.yaml", "path to wtc.yaml")
	cmd.Flags().StringVar(&captureDir, "capture-dir", "", "dump raw ingest bodies here for fixture capture (dev only; overrides server.capture_dir)")
	return cmd
}

func runServe(configPath string, configOptional bool, captureDir string) error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.Load(configPath, configOptional)
	if err != nil {
		return err
	}
	if captureDir != "" {
		cfg.Server.CaptureDir = captureDir
	}
	if cfg.Server.CaptureDir != "" {
		log.Warn("capture mode ON — raw ingest bodies are written to disk", "dir", cfg.Server.CaptureDir)
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

	// Rules engine — compiled once; config errors surface at startup.
	engine, err := normalize.NewEngine(cfg.Rules)
	if err != nil {
		_ = st.Close()
		return fmt.Errorf("rules: %w", err)
	}
	if len(cfg.Rules) == 0 {
		log.Warn("no rules configured — events will land with env=\"\" (see wtc doctor)")
	}
	tags, err := normalize.NewTagResolver(cfg.TagPatterns)
	if err != nil {
		_ = st.Close()
		return fmt.Errorf("tag_patterns: %w", err)
	}

	httpSrv := &http.Server{
		Addr: cfg.Server.Listen,
		Handler: server.New(st, server.Options{
			Tokens:              cfg.Auth.APITokens,
			GitHubWebhookSecret: cfg.Sources.GitHub.WebhookSecret,
			FluxHMACKey:         cfg.Sources.Flux.HMACKey,
			FluxSuppression:     cfg.Sources.Flux.SuppressionWindow.Std(),
			Engine:              engine,
			Tags:                tags,
			CaptureDir:          cfg.Server.CaptureDir,
		}, log).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// GitHub API poller — primary GitHub ingest for private deployments.
	if gh := cfg.Sources.GitHub; gh.APIToken != "" && len(gh.Repos) > 0 && gh.PollInterval.Std() > 0 {
		poller := github.NewPoller(
			github.NewClient(gh.APIToken, ""),
			st, engine, gh.Repos, gh.PollInterval.Std(), cfg.Server.CaptureDir, log,
		)
		go poller.Run(ctx)
	} else {
		log.Info("github poller disabled",
			"has_token", gh.APIToken != "", "repos", len(gh.Repos), "interval", gh.PollInterval.Std())
	}

	// Optional scheduled Slack digest (nil when unconfigured).
	if ds := server.NewDigestScheduler(st, cfg.Digest.SlackWebhook,
		cfg.Digest.Interval.Std(), cfg.Digest.Window.Std(), log); ds != nil {
		go ds.Run(ctx)
	}

	// Optional retention prune job (nil unless retention.keep is set).
	if rs := server.NewRetentionScheduler(st,
		cfg.Retention.Keep.Std(), cfg.Retention.EphemeralKeep.Std(),
		cfg.Retention.Interval.Std(), cfg.Retention.EphemeralEnvPattern, log); rs != nil {
		go rs.Run(ctx)
	}

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

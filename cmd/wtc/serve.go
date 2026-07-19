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
	"github.com/migueljfsc/wtc/internal/ingest/gitlab"
	"github.com/migueljfsc/wtc/internal/ingest/mapping"
	"github.com/migueljfsc/wtc/internal/metrics"
	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/notify"
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

	var st *store.Store
	switch cfg.Storage.Backend {
	case "postgres":
		// P15: external database — the serve pod is stateless.
		st, err = store.OpenPostgres(cfg.Storage.DSN)
		log.Info("storage backend", "backend", "postgres")
	default: // "sqlite" (validated by config.Load)
		if dir := filepath.Dir(cfg.Server.DB); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return fmt.Errorf("create db directory: %w", err)
			}
		}
		st, err = store.Open(cfg.Server.DB)
	}
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
	// /api/v1/config shows the EFFECTIVE tag patterns, so surface the defaults
	// when none are configured (the resolver falls back to them internally).
	effectiveTagPatterns := cfg.TagPatterns
	if len(effectiveTagPatterns) == 0 {
		effectiveTagPatterns = normalize.DefaultTagPatterns
	}

	// Mapping webhooks (P14) — compile config-declared sources; template/config
	// errors surface at startup. Each name is registered as a first-class source
	// so it appears under its real name in log/facets/doctor.
	mappers, err := mapping.Compile(cfg.Sources.Webhooks)
	if err != nil {
		_ = st.Close()
		return fmt.Errorf("mapping webhooks: %w", err)
	}
	for name := range mappers {
		model.RegisterSource(model.Source(name))
	}
	if len(mappers) > 0 {
		log.Info("mapping webhooks enabled", "count", len(mappers))
	}

	// Hot-reloadable holders (P10) — shared by the server AND the poller so a
	// live rule edit re-routes every ingest path. server.New applies any DB
	// overrides on top, swapping these before ingest starts.
	engineHolder := normalize.NewEngineHolder(engine)
	tagHolder := normalize.NewTagResolverHolder(tags)

	// Ingest scope filters — errors already surfaced by config.Load's
	// validation, so a compile miss here is impossible; ignore it.
	fluxScope, _ := cfg.Sources.Flux.Scope.Compile()
	argocdScope, _ := cfg.Sources.ArgoCD.Scope.Compile()

	httpSrv := &http.Server{
		Addr: cfg.Server.Listen,
		Handler: server.New(st, server.Options{
			Tokens:              cfg.Auth.APITokens,
			GitHubWebhookSecret: cfg.Sources.GitHub.WebhookSecret,
			FluxHMACKey:         cfg.Sources.Flux.HMACKey,
			FluxSuppression:     cfg.Sources.Flux.SuppressionWindow.Std(),
			ArgoCDWebhookToken:  cfg.Sources.ArgoCD.WebhookSecret,
			ArgoCDSuppression:   cfg.Sources.ArgoCD.SuppressionWindow.Std(),
			FluxScope:           fluxScope,
			ArgoCDScope:         argocdScope,
			GitLabWebhookToken:  cfg.Sources.GitLab.WebhookSecret,
			Engine:              engineHolder,
			Tags:                tagHolder,
			CaptureDir:          cfg.Server.CaptureDir,
			CORSAllowedOrigins:  cfg.Server.CORS.AllowedOrigins,
			Rules:               cfg.Rules,
			TagPatterns:         effectiveTagPatterns,
			Mappers:             mappers,
			ConfigView:          config.NewView(cfg),
			Version:             version,
		}, log).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Notification dispatcher (P21) — nil when no notifications configured.
	// Wired before the listener and pollers start so no ingest can slip past
	// the hook; Enqueue is non-blocking (bounded queue, dropped counter).
	subs, err := notify.Compile(cfg.Notifications) // validated at config.Load; cannot fail here
	if err != nil {
		_ = st.Close()
		return fmt.Errorf("notifications: %w", err)
	}
	if d := notify.NewDispatcher(subs, log); d != nil {
		st.SetNotifyFunc(d.Enqueue)
		go d.Run(ctx)
	}

	// GitHub API poller — primary GitHub ingest for private deployments.
	// repos may be empty — the poller then auto-discovers every repo the token
	// can access.
	if gh := cfg.Sources.GitHub; gh.APIToken != "" && gh.PollInterval.Std() > 0 {
		poller := github.NewPoller(
			github.NewClient(gh.APIToken, ""),
			st, engineHolder, gh.Repos, gh.PollInterval.Std(), gh.Backfill.Std(), cfg.Server.CaptureDir, log,
		)
		go poller.Run(ctx)
	} else {
		log.Info("github poller disabled",
			"has_token", gh.APIToken != "", "repos", len(gh.Repos), "interval", gh.PollInterval.Std())
	}

	// GitLab API poller — primary GitLab ingest for private deployments.
	// Projects must be configured explicitly (no accessible-project discovery
	// analog that fits the poller model).
	if gl := cfg.Sources.GitLab; gl.APIToken != "" && gl.PollInterval.Std() > 0 && len(gl.Projects) > 0 {
		poller := gitlab.NewPoller(
			gitlab.NewClient(gl.APIToken, gl.BaseURL),
			st, engineHolder, gl.Projects, gl.PollInterval.Std(), gl.Backfill.Std(), cfg.Server.CaptureDir, log,
		)
		go poller.Run(ctx)
	} else {
		log.Info("gitlab poller disabled",
			"has_token", gl.APIToken != "", "projects", len(gl.Projects), "interval", gl.PollInterval.Std())
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

	errCh := make(chan error, 2)
	go func() {
		log.Info("wtc serve listening", "addr", cfg.Server.Listen, "db", cfg.Server.DB, "version", version)
		errCh <- httpSrv.ListenAndServe()
	}()

	// Optional separate UNAUTHENTICATED metrics listener (P16) — for
	// in-cluster scrapes where an api_token would be over-privileged. A
	// configured listener that cannot bind is fatal, same as the main one:
	// silently running without metrics defeats the point of configuring them.
	var metricsSrv *http.Server
	if cfg.Metrics.Listen != "" {
		mmux := http.NewServeMux()
		mmux.Handle("GET /metrics", metrics.Handler())
		metricsSrv = &http.Server{
			Addr:              cfg.Metrics.Listen,
			Handler:           mmux,
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			log.Info("metrics listener (unauthenticated — keep in-cluster)", "addr", cfg.Metrics.Listen)
			errCh <- metricsSrv.ListenAndServe()
		}()
	}

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
	if metricsSrv != nil {
		if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
			log.Error("metrics shutdown", "error", err)
		}
	}
	if err := st.Close(); err != nil {
		return fmt.Errorf("close store: %w", err)
	}
	return nil
}

package config

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/ingest/mapping"
	"github.com/migueljfsc/wtc/internal/notify"
)

// sentinel marks a secret value; if any View JSON ever contains it, a secret
// leaked. Distinct per field via suffixes so a failure names the culprit.
const sentinel = "LEAKED-SECRET-3f9a"

// fullyPopulatedConfig fills EVERY config field, with sentinels in every
// secret position. Extend this (and secretFields below) whenever a config
// field is added — the view is an allowlist, so a forgotten field fails
// closed, but this test is what proves the masking of the fields we do expose.
func fullyPopulatedConfig() *Config {
	return &Config{
		Server: Server{
			Listen:     ":8484",
			DB:         "/data/wtc.db",
			CaptureDir: "/captures",
			CORS:       CORS{AllowedOrigins: []string{"https://portal.example.com"}},
		},
		Storage: Storage{
			Backend: "postgres",
			DSN:     "postgres://wtcuser:" + sentinel + "-dsn@db.internal:5433/wtcledger?sslmode=require",
		},
		Auth: Auth{APITokens: []string{sentinel + "-token1", sentinel + "-token2"}},
		Sources: Sources{
			GitHub: GitHub{
				WebhookSecret: sentinel + "-gh-webhook",
				APIToken:      sentinel + "-gh-pat",
				PollInterval:  Duration(90 * time.Second),
				Repos:         []string{"org/api", "org/web"},
				InfraPath:     "infrastructure/",
				Backfill:      Duration(7 * 24 * time.Hour),
			},
			GitLab: GitLab{
				BaseURL:       "https://gitlab.example.com",
				WebhookSecret: sentinel + "-gl-webhook",
				APIToken:      sentinel + "-gl-token",
				PollInterval:  Duration(2 * time.Minute),
				Projects:      []string{"group/svc"},
				InfraPath:     "deploy/",
				Backfill:      Duration(24 * time.Hour),
			},
			Flux: Flux{
				HMACKey:           sentinel + "-flux-hmac",
				SuppressionWindow: Duration(10 * time.Minute),
			},
			ArgoCD: ArgoCD{
				WebhookSecret:     sentinel + "-argo-token",
				SuppressionWindow: Duration(10 * time.Minute),
			},
			Webhooks: []mapping.Webhook{
				{
					Name:     "harbor",
					Auth:     mapping.Auth{Token: sentinel + "-wh-token", Header: "X-Harbor-Token"},
					DedupKey: "harbor:{{ .payload.id }}",
					Mapping:  mapping.FieldTemplates{Kind: "deploy", Title: "{{ .payload.name }}"},
					Facts:    map[string]string{"project": "{{ .payload.project }}"},
				},
				{
					Name: "tfc",
					Auth: mapping.Auth{HMAC: &mapping.HMACAuth{
						Secret: sentinel + "-wh-hmac",
						Header: "X-TFE-Notification-Signature",
						Algo:   "sha512",
						Prefix: "",
					}},
					DedupKey: "tfc:{{ .payload.run_id }}",
					Mapping:  mapping.FieldTemplates{Kind: "infra", Title: "{{ .payload.message }}"},
				},
				{
					Name:     "grafana-alerts",
					Preset:   "grafana",
					Auth:     mapping.Auth{Token: sentinel + "-grafana-token"},
					DedupKey: "",
				},
			},
		},
		Digest: Digest{
			Interval:     Duration(24 * time.Hour),
			Window:       Duration(24 * time.Hour),
			SlackWebhook: "https://hooks.slack.com/services/" + sentinel + "-slack",
		},
		Retention: Retention{
			Keep:                Duration(180 * 24 * time.Hour),
			EphemeralEnvPattern: "pr-*",
			EphemeralKeep:       Duration(30 * 24 * time.Hour),
			Interval:            Duration(24 * time.Hour),
		},
		Metrics: Metrics{Listen: ":9091"},
		Notifications: []notify.Subscription{
			{
				Name:  "prod-deploys",
				Match: notify.Match{Env: "prod", Kind: "deploy"},
				Sink:  notify.Sink{Type: "slack", URL: "https://hooks.slack.com/services/" + sentinel + "-notify-slack"},
			},
			{
				Match: notify.Match{Status: "failed"},
				Sink: notify.Sink{
					Type:  "grafana-annotation",
					URL:   "https://grafana.example.com/" + sentinel + "-notify-url",
					Token: sentinel + "-notify-grafana-token",
					Tags:  []string{"deploys"},
				},
			},
		},
	}
}

// TestViewNeverLeaksSecrets is the P17 guard: marshal the view of a config
// whose every secret carries the sentinel and assert the sentinel never
// appears — masks do.
func TestViewNeverLeaksSecrets(t *testing.T) {
	raw, err := json.Marshal(NewView(fullyPopulatedConfig()))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)

	if strings.Contains(body, sentinel) {
		t.Fatalf("sentinel leaked into view JSON:\n%s", body)
	}
	// The DSN's non-secret parts are allowed; its credentials are not.
	if strings.Contains(body, "wtcuser") {
		t.Errorf("DSN username leaked into view JSON")
	}
	if !strings.Contains(body, `"`+Mask+`"`) {
		t.Errorf("no mask present — secrets should render as %q", Mask)
	}
}

func TestViewShape(t *testing.T) {
	v := NewView(fullyPopulatedConfig())

	// Secrets → constant mask; unset ones stay empty (see zero-config test).
	for name, got := range map[string]string{
		"github.webhook_secret": v.Sources.GitHub.WebhookSecret,
		"github.api_token":      v.Sources.GitHub.APIToken,
		"gitlab.api_token":      v.Sources.GitLab.APIToken,
		"flux.hmac_key":         v.Sources.Flux.HMACKey,
		"argocd.webhook_secret": v.Sources.ArgoCD.WebhookSecret,
		"digest.slack_webhook":  v.Digest.SlackWebhook,
		"storage.dsn":           v.Storage.DSN,
	} {
		if got != Mask {
			t.Errorf("%s = %q, want mask", name, got)
		}
	}
	if len(v.Auth.APITokens) != 2 || v.Auth.APITokens[0] != Mask {
		t.Errorf("api_tokens = %v, want two masks", v.Auth.APITokens)
	}

	// DSN location parts survive with credentials stripped.
	if v.Storage.Host != "db.internal" || v.Storage.Port != 5433 || v.Storage.Database != "wtcledger" {
		t.Errorf("storage location = %s:%d/%s, want db.internal:5433/wtcledger",
			v.Storage.Host, v.Storage.Port, v.Storage.Database)
	}

	// Poller gating mirrors serve.go.
	if !v.Sources.GitHub.PollerEnabled || !v.Sources.GitLab.PollerEnabled {
		t.Errorf("pollers should be enabled: gh=%v gl=%v",
			v.Sources.GitHub.PollerEnabled, v.Sources.GitLab.PollerEnabled)
	}
	if v.Sources.GitHub.PollInterval != "1m30s" {
		t.Errorf("github poll_interval = %q, want 1m30s", v.Sources.GitHub.PollInterval)
	}
	if v.Sources.GitHub.Backfill != "7d" {
		t.Errorf("github backfill = %q, want 7d", v.Sources.GitHub.Backfill)
	}

	// Jobs and flags.
	if !v.Digest.Enabled || !v.Retention.Enabled || !v.Server.CaptureEnabled {
		t.Errorf("digest/retention/capture flags: %v %v %v",
			v.Digest.Enabled, v.Retention.Enabled, v.Server.CaptureEnabled)
	}
	// Whole-day durations render in the config's own d-suffix form.
	if v.Retention.Keep != "180d" || v.Retention.EphemeralKeep != "30d" {
		t.Errorf("retention durations = %q/%q, want 180d/30d",
			v.Retention.Keep, v.Retention.EphemeralKeep)
	}
	if v.Metrics.Listen != ":9091" {
		t.Errorf("metrics.listen = %q", v.Metrics.Listen)
	}

	// Mapping webhooks: auth shape without secrets, templates in full.
	if len(v.Sources.Webhooks) != 3 {
		t.Fatalf("webhooks = %d, want 3", len(v.Sources.Webhooks))
	}
	harbor := v.Sources.Webhooks[0]
	if harbor.Auth.Mode != "token" || harbor.Auth.Header != "X-Harbor-Token" || harbor.Auth.Secret != Mask {
		t.Errorf("harbor auth = %+v", harbor.Auth)
	}
	if harbor.Mapping["title"] != "{{ .payload.name }}" {
		t.Errorf("harbor mapping shown = %v, want full templates", harbor.Mapping)
	}
	tfc := v.Sources.Webhooks[1]
	if tfc.Auth.Mode != "hmac" || tfc.Auth.Algo != "sha512" || tfc.Auth.Secret != Mask {
		t.Errorf("tfc auth = %+v", tfc.Auth)
	}
	// Preset-based hook: view shows the EFFECTIVE (resolved) templates.
	grafana := v.Sources.Webhooks[2]
	if grafana.Preset != "grafana" || grafana.DedupKey == "" || len(grafana.Mapping) == 0 {
		t.Errorf("grafana preset not resolved in view: dedup=%q mapping=%v",
			grafana.DedupKey, grafana.Mapping)
	}

	// Notifications (P21): match shown in full, sink URL + token always masked,
	// unnamed entries get the metric-label default name.
	if len(v.Notifications) != 2 {
		t.Fatalf("notifications = %d, want 2", len(v.Notifications))
	}
	slack := v.Notifications[0]
	if slack.Name != "prod-deploys" || slack.Match.Env != "prod" || slack.Sink.Type != "slack" {
		t.Errorf("slack notification view = %+v", slack)
	}
	if slack.Sink.URL != Mask {
		t.Errorf("slack sink url = %q, want mask", slack.Sink.URL)
	}
	graf := v.Notifications[1]
	if graf.Name != "notifications[1]" {
		t.Errorf("unnamed notification = %q, want notifications[1]", graf.Name)
	}
	if graf.Sink.URL != Mask || graf.Sink.Token != Mask {
		t.Errorf("grafana sink = %+v, want masked url+token", graf.Sink)
	}
	if len(graf.Sink.Tags) != 1 || graf.Sink.Tags[0] != "deploys" {
		t.Errorf("grafana sink tags = %v", graf.Sink.Tags)
	}
}

// TestViewZeroConfig: a default config exposes empty secrets (""), disabled
// pollers/jobs, and no DSN location — nothing invented.
func TestViewZeroConfig(t *testing.T) {
	cfg := Default()
	v := NewView(&cfg)

	if v.Sources.GitHub.APIToken != "" || v.Sources.Flux.HMACKey != "" || v.Storage.DSN != "" {
		t.Errorf("unset secrets must render empty, got gh=%q flux=%q dsn=%q",
			v.Sources.GitHub.APIToken, v.Sources.Flux.HMACKey, v.Storage.DSN)
	}
	if v.Sources.GitHub.PollerEnabled || v.Sources.GitLab.PollerEnabled {
		t.Error("pollers must be disabled without tokens")
	}
	if v.Digest.Enabled || v.Retention.Enabled || v.Server.CaptureEnabled {
		t.Error("jobs/capture must be disabled by default")
	}
	if v.Storage.Host != "" || v.Storage.Port != 0 {
		t.Errorf("no DSN → no location, got %s:%d", v.Storage.Host, v.Storage.Port)
	}
	if len(v.Auth.APITokens) != 0 {
		t.Errorf("api_tokens = %v, want empty", v.Auth.APITokens)
	}
}

// TestViewRetentionDefaults: an enabled retention with unset optionals shows
// the EFFECTIVE scheduler defaults (interval 24h→"1d", pattern "pr-*",
// ephemeral keep = keep) — the view reports what the job actually runs with.
func TestViewRetentionDefaults(t *testing.T) {
	cfg := Default()
	cfg.Retention.Keep = Duration(90 * 24 * time.Hour)
	v := NewView(&cfg)

	if !v.Retention.Enabled {
		t.Fatal("retention should be enabled")
	}
	if v.Retention.Interval != "1d" || v.Retention.EphemeralEnvPattern != "pr-*" || v.Retention.EphemeralKeep != "90d" {
		t.Errorf("effective defaults = interval %q pattern %q ephemeral_keep %q, want 1d / pr-* / 90d",
			v.Retention.Interval, v.Retention.EphemeralEnvPattern, v.Retention.EphemeralKeep)
	}
}

// TestViewBadDSN: an unparseable DSN exposes nothing beyond the mask.
func TestViewBadDSN(t *testing.T) {
	cfg := Default()
	cfg.Storage.Backend = "postgres"
	cfg.Storage.DSN = "%%%not-a-dsn with " + sentinel
	v := NewView(&cfg)

	if v.Storage.DSN != Mask {
		t.Errorf("dsn = %q, want mask", v.Storage.DSN)
	}
	if v.Storage.Host != "" || v.Storage.Port != 0 || v.Storage.Database != "" {
		t.Errorf("bad DSN must expose no location, got %s:%d/%s",
			v.Storage.Host, v.Storage.Port, v.Storage.Database)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), sentinel) {
		t.Error("bad DSN leaked into view JSON")
	}
}

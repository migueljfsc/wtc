package config

import (
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/migueljfsc/wtc/internal/ingest/mapping"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/notify"
)

// Mask is the constant placeholder for every configured secret in a View.
// Fixed-length regardless of the real value — a secret's length is
// information too. An unconfigured secret renders as "".
const Mask = "********"

// View is the redacted, operator-facing snapshot of the effective config
// (post ${VAR} expansion and WTC_* overrides), served by /api/v1/config and
// rendered by the portal Configuration tab and `wtc config`.
//
// SECURITY INVARIANT: this is an ALLOWLIST — every field is copied
// individually by NewView, secrets pass through mask(), and config.Config is
// never marshalled. A config field that is not explicitly copied here is NOT
// exposed (fail safe). The sentinel test in view_test.go proves no secret
// value survives into the JSON; extend it when adding any secret field.
type View struct {
	Server        ServerView         `json:"server"`
	Storage       StorageView        `json:"storage"`
	Auth          AuthView           `json:"auth"`
	Sources       SourcesView        `json:"sources"`
	Digest        DigestView         `json:"digest"`
	Retention     RetentionView      `json:"retention"`
	Metrics       MetricsView        `json:"metrics"`
	Notifications []NotificationView `json:"notifications"`
}

// ServerView is the serve daemon surface. CaptureEnabled is a data-exposure
// flag (raw ingest bodies written to disk) — the portal badges it as a
// warning; the directory path itself stays private.
type ServerView struct {
	Listen         string   `json:"listen"`
	CORSOrigins    []string `json:"cors_origins"`
	CaptureEnabled bool     `json:"capture_enabled"`
}

// StorageView shows the backend plus, for postgres, the DSN's location parts
// (host/port/database) with credentials stripped via the real pgx parser. A
// DSN that fails to parse exposes nothing beyond the mask.
type StorageView struct {
	Backend  string `json:"backend"`
	DSN      string `json:"dsn"` // Mask when set — the DSN embeds credentials
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Database string `json:"database,omitempty"`
}

// AuthView lists one Mask per configured api_token: the count is visible,
// the values never are.
type AuthView struct {
	APITokens []string `json:"api_tokens"`
}

// SourcesView groups the per-source ingest configuration.
type SourcesView struct {
	GitHub   GitHubView    `json:"github"`
	GitLab   GitLabView    `json:"gitlab"`
	Flux     FluxView      `json:"flux"`
	ArgoCD   ArgoCDView    `json:"argocd"`
	Webhooks []WebhookView `json:"webhooks"`
}

// GitHubView mirrors config.GitHub. PollerEnabled restates the serve-time
// gating (token set AND interval > 0) so clients don't re-derive it; empty
// Repos with an enabled poller means auto-discovery.
type GitHubView struct {
	WebhookSecret string   `json:"webhook_secret"`
	APIToken      string   `json:"api_token"`
	PollInterval  string   `json:"poll_interval"`
	PollerEnabled bool     `json:"poller_enabled"`
	Repos         []string `json:"repos"`
	InfraPath     string   `json:"infra_path"`
	Backfill      string   `json:"backfill"`
}

// GitLabView mirrors config.GitLab; unlike github the poller also requires an
// explicit project list (no auto-discovery).
type GitLabView struct {
	BaseURL       string   `json:"base_url"`
	WebhookSecret string   `json:"webhook_secret"`
	APIToken      string   `json:"api_token"`
	PollInterval  string   `json:"poll_interval"`
	PollerEnabled bool     `json:"poller_enabled"`
	Projects      []string `json:"projects"`
	InfraPath     string   `json:"infra_path"`
	Backfill      string   `json:"backfill"`
}

// FluxView mirrors config.Flux.
type FluxView struct {
	HMACKey           string    `json:"hmac_key"`
	SuppressionWindow string    `json:"suppression_window"`
	Scope             ScopeView `json:"scope"`
}

// ArgoCDView mirrors config.ArgoCD.
type ArgoCDView struct {
	WebhookSecret     string    `json:"webhook_secret"`
	SuppressionWindow string    `json:"suppression_window"`
	Scope             ScopeView `json:"scope"`
}

// ScopeView mirrors normalize.ScopeFilter — the ingest allow/deny lists. Not a
// secret (raw-fact globs, operator-authored config-as-code, same exposure
// class as rules), so shown in full. Lists are always arrays, never null.
type ScopeView struct {
	Allow []normalize.ScopeMatch `json:"allow"`
	Deny  []normalize.ScopeMatch `json:"deny"`
}

// WebhookView is one mapping webhook. Templates are shown in full — they are
// operator-authored config-as-code, the same exposure class as rules. Only the
// auth secret is masked.
type WebhookView struct {
	Name     string            `json:"name"`
	Preset   string            `json:"preset,omitempty"`
	Auth     WebhookAuthView   `json:"auth"`
	DedupKey string            `json:"dedup_key"`
	Mapping  map[string]string `json:"mapping"`
	Facts    map[string]string `json:"facts,omitempty"`
}

// WebhookAuthView shows the auth shape (mode/header/algo/prefix) with the
// secret masked.
type WebhookAuthView struct {
	Mode   string `json:"mode"` // "token" | "hmac"
	Header string `json:"header,omitempty"`
	Algo   string `json:"algo,omitempty"`
	Prefix string `json:"prefix,omitempty"`
	Secret string `json:"secret"`
}

// DigestView mirrors config.Digest; the Slack webhook URL is a
// capability-bearing secret and is masked.
type DigestView struct {
	Enabled      bool   `json:"enabled"`
	Interval     string `json:"interval"`
	Window       string `json:"window"`
	SlackWebhook string `json:"slack_webhook"`
}

// RetentionView mirrors config.Retention (opt-in: disabled unless keep set).
type RetentionView struct {
	Enabled             bool   `json:"enabled"`
	Keep                string `json:"keep"`
	EphemeralEnvPattern string `json:"ephemeral_env_pattern,omitempty"`
	EphemeralKeep       string `json:"ephemeral_keep,omitempty"`
	Interval            string `json:"interval,omitempty"`
}

// MetricsView shows whether the separate unauthenticated metrics listener
// is open; the address is topology, not a secret.
type MetricsView struct {
	Listen string `json:"listen"`
}

// NotificationView is one notification subscription. The match is operator-authored
// config-as-code (shown in full, same exposure class as rules); the sink URL
// and token are ALWAYS masked — a Slack incoming-webhook URL is
// capability-bearing and webhook URLs may embed credentials, so the view
// never distinguishes.
type NotificationView struct {
	Name  string               `json:"name"`
	Match notify.Match         `json:"match"`
	Sink  NotificationSinkView `json:"sink"`
}

// NotificationSinkView shows the sink shape with URL and token masked.
type NotificationSinkView struct {
	Type  string   `json:"type"`
	URL   string   `json:"url"`
	Token string   `json:"token"`
	Tags  []string `json:"tags,omitempty"`
}

// scopeView copies the ingest allow/deny lists, normalizing nil to empty so
// the JSON is always an array. ScopeMatch is a value type, so copy is a deep
// copy — the view never aliases the loaded config.
func scopeView(f normalize.ScopeFilter) ScopeView {
	cp := func(in []normalize.ScopeMatch) []normalize.ScopeMatch {
		out := make([]normalize.ScopeMatch, len(in))
		copy(out, in)
		return out
	}
	return ScopeView{Allow: cp(f.Allow), Deny: cp(f.Deny)}
}

// mask replaces a configured secret with the constant Mask; an unset secret
// stays "".
func mask(secret string) string {
	if secret == "" {
		return ""
	}
	return Mask
}

// copyList clones a string list, normalizing nil to an empty slice so the
// JSON is always an array — clients index these without null checks.
func copyList(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// durString renders a config duration for display: whole days as "180d"
// (matching the config's own d-suffix syntax), everything else in Go's
// duration form ("1m30s"; zero = "0s").
func durString(d Duration) string {
	std := d.Std()
	if std >= 24*time.Hour && std%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", std/(24*time.Hour))
	}
	return std.String()
}

// NewView builds the redacted snapshot from the loaded config. Field by
// field on purpose — see the View security invariant.
func NewView(cfg *Config) View {
	v := View{
		Server: ServerView{
			Listen:         cfg.Server.Listen,
			CORSOrigins:    copyList(cfg.Server.CORS.AllowedOrigins),
			CaptureEnabled: cfg.Server.CaptureDir != "",
		},
		Storage: StorageView{
			Backend: cfg.Storage.Backend,
			DSN:     mask(cfg.Storage.DSN),
		},
		Auth: AuthView{APITokens: make([]string, len(cfg.Auth.APITokens))},
		Sources: SourcesView{
			GitHub: GitHubView{
				WebhookSecret: mask(cfg.Sources.GitHub.WebhookSecret),
				APIToken:      mask(cfg.Sources.GitHub.APIToken),
				PollInterval:  durString(cfg.Sources.GitHub.PollInterval),
				// Same gating as serve.go: poller runs with a token and a
				// positive interval; empty repos = auto-discover.
				PollerEnabled: cfg.Sources.GitHub.APIToken != "" && cfg.Sources.GitHub.PollInterval.Std() > 0,
				Repos:         copyList(cfg.Sources.GitHub.Repos),
				InfraPath:     cfg.Sources.GitHub.InfraPath,
				Backfill:      durString(cfg.Sources.GitHub.Backfill),
			},
			GitLab: GitLabView{
				BaseURL:       cfg.Sources.GitLab.BaseURL,
				WebhookSecret: mask(cfg.Sources.GitLab.WebhookSecret),
				APIToken:      mask(cfg.Sources.GitLab.APIToken),
				PollInterval:  durString(cfg.Sources.GitLab.PollInterval),
				// Same gating as serve.go: gitlab additionally needs explicit
				// projects (no discovery analog).
				PollerEnabled: cfg.Sources.GitLab.APIToken != "" && cfg.Sources.GitLab.PollInterval.Std() > 0 && len(cfg.Sources.GitLab.Projects) > 0,
				Projects:      copyList(cfg.Sources.GitLab.Projects),
				InfraPath:     cfg.Sources.GitLab.InfraPath,
				Backfill:      durString(cfg.Sources.GitLab.Backfill),
			},
			Flux: FluxView{
				HMACKey:           mask(cfg.Sources.Flux.HMACKey),
				SuppressionWindow: durString(cfg.Sources.Flux.SuppressionWindow),
				Scope:             scopeView(cfg.Sources.Flux.Scope),
			},
			ArgoCD: ArgoCDView{
				WebhookSecret:     mask(cfg.Sources.ArgoCD.WebhookSecret),
				SuppressionWindow: durString(cfg.Sources.ArgoCD.SuppressionWindow),
				Scope:             scopeView(cfg.Sources.ArgoCD.Scope),
			},
			Webhooks: make([]WebhookView, 0, len(cfg.Sources.Webhooks)),
		},
		Digest:        digestView(cfg.Digest),
		Retention:     retentionView(cfg.Retention),
		Metrics:       MetricsView{Listen: cfg.Metrics.Listen},
		Notifications: make([]NotificationView, 0, len(cfg.Notifications)),
	}

	for i, n := range cfg.Notifications {
		name := n.Name
		if name == "" {
			// Mirror notify.Compile's defaulting so the view names match the
			// wtc_notify_* metric labels.
			name = fmt.Sprintf("notifications[%d]", i)
		}
		v.Notifications = append(v.Notifications, NotificationView{
			Name:  name,
			Match: n.Match,
			Sink: NotificationSinkView{
				Type:  n.Sink.Type,
				URL:   mask(n.Sink.URL),
				Token: mask(n.Sink.Token),
				Tags:  copyList(n.Sink.Tags),
			},
		})
	}

	for i := range v.Auth.APITokens {
		v.Auth.APITokens[i] = Mask
	}

	// Postgres DSN location parts, credentials stripped by the real parser —
	// never string surgery. Parse failure exposes nothing beyond the mask.
	if cfg.Storage.Backend == "postgres" && cfg.Storage.DSN != "" {
		if cc, err := pgx.ParseConfig(cfg.Storage.DSN); err == nil {
			v.Storage.Host = cc.Host
			v.Storage.Port = int(cc.Port)
			v.Storage.Database = cc.Database
		}
	}

	// Presets applied first so the view shows the EFFECTIVE templates a
	// delivery runs through, not the operator's shorthand.
	for _, w := range mapping.Resolved(cfg.Sources.Webhooks) {
		v.Sources.Webhooks = append(v.Sources.Webhooks, webhookView(w))
	}
	return v
}

// digestView mirrors the digest scheduler's own defaulting (window falls back
// to interval) so the view shows what the job actually runs with.
func digestView(d Digest) DigestView {
	enabled := d.Interval.Std() > 0 && d.SlackWebhook != ""
	window := d.Window
	if enabled && window.Std() <= 0 {
		window = d.Interval
	}
	return DigestView{
		Enabled:      enabled,
		Interval:     durString(d.Interval),
		Window:       durString(window),
		SlackWebhook: mask(d.SlackWebhook),
	}
}

// retentionView mirrors the retention scheduler's own defaulting (interval
// 24h, ephemeral pattern "pr-*", ephemeral keep = keep) so the view shows the
// EFFECTIVE job parameters, not the raw zeros.
func retentionView(r Retention) RetentionView {
	v := RetentionView{
		Enabled:             r.Keep.Std() > 0,
		Keep:                durString(r.Keep),
		EphemeralEnvPattern: r.EphemeralEnvPattern,
		EphemeralKeep:       durString(r.EphemeralKeep),
		Interval:            durString(r.Interval),
	}
	if !v.Enabled {
		return v
	}
	if r.Interval.Std() <= 0 {
		v.Interval = durString(Duration(24 * time.Hour))
	}
	if r.EphemeralEnvPattern == "" {
		v.EphemeralEnvPattern = "pr-*"
	}
	if r.EphemeralKeep.Std() <= 0 {
		v.EphemeralKeep = v.Keep
	}
	return v
}

// webhookView redacts one mapping webhook: auth shape kept, secret masked,
// templates in full.
func webhookView(w mapping.Webhook) WebhookView {
	out := WebhookView{
		Name:     w.Name,
		Preset:   w.Preset,
		DedupKey: w.DedupKey,
		Mapping:  fieldTemplates(w.Mapping),
		Facts:    w.Facts,
	}
	if w.Auth.HMAC != nil && w.Auth.HMAC.Secret != "" {
		out.Auth = WebhookAuthView{
			Mode:   "hmac",
			Header: w.Auth.HMAC.Header,
			Algo:   w.Auth.HMAC.Algo,
			Prefix: w.Auth.HMAC.Prefix,
			Secret: Mask,
		}
	} else {
		out.Auth = WebhookAuthView{
			Mode:   "token",
			Header: w.Auth.Header,
			Secret: mask(w.Auth.Token),
		}
	}
	return out
}

// fieldTemplates flattens the set members of a FieldTemplates into a
// field→template map, so the view shows exactly what the operator wrote.
func fieldTemplates(ft mapping.FieldTemplates) map[string]string {
	out := map[string]string{}
	for k, tpl := range map[string]string{
		"kind": ft.Kind, "status": ft.Status, "env": ft.Env,
		"cluster": ft.Cluster, "namespace": ft.Namespace, "service": ft.Service,
		"actor": ft.Actor, "ref": ft.Ref, "artifact": ft.Artifact,
		"title": ft.Title, "url": ft.URL, "ts": ft.TS,
		"duration_ms": ft.DurationMS,
	} {
		if tpl != "" {
			out[k] = tpl
		}
	}
	return out
}

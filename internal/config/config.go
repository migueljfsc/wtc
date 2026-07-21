// Package config loads the single wtc.yaml configuration file, expands
// ${VAR} references from the environment, and applies WTC_* env overrides.
// Hand-rolled on purpose: no viper/koanf, one YAML file plus WTC_* overrides.
package config

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/migueljfsc/wtc/internal/catalog"
	"github.com/migueljfsc/wtc/internal/ingest/mapping"
	"github.com/migueljfsc/wtc/internal/normalize"
	"github.com/migueljfsc/wtc/internal/notify"
)

// Duration wraps time.Duration with YAML support for "60s"/"10m" strings.
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("duration must be a string like \"60s\": %w", err)
	}
	parsed, err := parseDuration(s)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// parseDuration extends time.ParseDuration (ns…h) with standalone day ("d")
// and week ("w") suffixes, so retention windows read as "180d"/"2w" instead of
// "4320h". Composite forms like "1d12h" are not supported — d/w must stand
// alone; everything else delegates unchanged.
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if n, ok := strings.CutSuffix(s, "d"); ok {
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return time.Duration(f * 24 * float64(time.Hour)), nil
		}
	}
	if n, ok := strings.CutSuffix(s, "w"); ok {
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return time.Duration(f * 7 * 24 * float64(time.Hour)), nil
		}
	}
	return time.ParseDuration(s)
}

// Std returns the underlying time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// Server holds the serve daemon settings. Fields for later phases (base_url,
// retention) are added with the code that reads them — dead config surface is
// a silent-no-op trap for operators.
type Server struct {
	Listen     string `yaml:"listen"`
	DB         string `yaml:"db"`
	CaptureDir string `yaml:"capture_dir"` // non-empty => dump raw ingest bodies (dev only)
	CORS       CORS   `yaml:"cors"`
}

// CORS configures cross-origin access for the separately-served portal SPA.
// Off by default (no origins => no CORS headers). A single "*" allows any
// origin. Only the portal needs this; same-origin CLI/embedded-web don't.
type CORS struct {
	AllowedOrigins []string `yaml:"allowed_origins"`
}

// Auth holds static bearer tokens accepted on /api/* and /ingest/generic.
type Auth struct {
	APITokens []string `yaml:"api_tokens"`
}

// Storage selects the storage backend. Default sqlite keeps using
// server.db as the file path — the single-binary story. backend=postgres
// requires DSN and makes the serve pod stateless; DSN carries credentials, so
// inject it via ${VAR}/secretRef, never a plain value in a committed file.
type Storage struct {
	Backend string `yaml:"backend"` // "sqlite" (default) | "postgres"
	DSN     string `yaml:"dsn"`     // postgres connection string; required when backend=postgres
}

// GitHub configures the GitHub ingest paths (SPEC §2). The poller is the
// primary path for private deployments; webhooks need a public endpoint.
type GitHub struct {
	WebhookSecret string   `yaml:"webhook_secret"` // enables /ingest/github HMAC verification
	APIToken      string   `yaml:"api_token"`      // enables the poller + PR-diff enrichment
	PollInterval  Duration `yaml:"poll_interval"`  // 0 disables the poller (webhook-only mode)
	Repos         []string `yaml:"repos"`          // poller scope, owner/name; empty = every repo the token can access
	InfraPath     string   `yaml:"infra_path"`     // per-repo manifests dir (microservices layout)
	Backfill      Duration `yaml:"backfill"`       // first-poll history window (default 24h); GitHub retains runs ~90d
}

// GitLab configures the GitLab ingest paths (SPEC §2) — the SCM/CI-axis
// neutrality peer of GitHub. The poller is the primary path for private
// deployments; the webhook needs a public endpoint. BaseURL targets
// self-managed instances (empty = gitlab.com).
type GitLab struct {
	BaseURL       string   `yaml:"base_url"`       // instance root; empty = https://gitlab.com
	WebhookSecret string   `yaml:"webhook_secret"` // enables /ingest/gitlab X-Gitlab-Token verification
	APIToken      string   `yaml:"api_token"`      // enables the poller + MR-diff enrichment (PRIVATE-TOKEN)
	PollInterval  Duration `yaml:"poll_interval"`  // 0 disables the poller (webhook-only mode)
	Projects      []string `yaml:"projects"`       // poller scope, group/service paths (no auto-discovery)
	InfraPath     string   `yaml:"infra_path"`     // per-project manifests dir (microservices layout)
	Backfill      Duration `yaml:"backfill"`       // first-poll history window (default 24h)
}

// Flux configures the notification-controller ingest path (SPEC §2).
type Flux struct {
	HMACKey           string   `yaml:"hmac_key"`           // generic-hmac provider shared key
	SuppressionWindow Duration `yaml:"suppression_window"` // drop repeats of (object,revision,reason) inside this window
	// Scope is the ingest allow/deny list (raw facts: namespace, object_name,
	// object_kind, cluster). Empty = ingest every reconcile. Deny wins over
	// allow; validated/compiled at config load.
	Scope normalize.ScopeFilter `yaml:"scope"`
}

// ArgoCD configures the notifications-controller webhook ingest path
// (SPEC §2). Argo's notification templates can't HMAC-sign the body
// like Flux's generic-hmac provider, so auth is a static shared secret sent
// as the X-WTC-Token header (see docs/setup/argocd-notifications.yaml).
type ArgoCD struct {
	WebhookSecret     string   `yaml:"webhook_secret"`     // static shared secret, X-WTC-Token header
	SuppressionWindow Duration `yaml:"suppression_window"` // drop repeats of (app,revision,phase|health) inside this window
	// Scope is the ingest allow/deny list (raw facts: namespace, object_name
	// [=app], object_kind, project). Empty = ingest every notification. Deny
	// wins over allow; validated/compiled at config load.
	Scope normalize.ScopeFilter `yaml:"scope"`
}

// Sources groups per-source ingest configuration.
type Sources struct {
	GitHub   GitHub            `yaml:"github"`
	GitLab   GitLab            `yaml:"gitlab"`
	Flux     Flux              `yaml:"flux"`
	ArgoCD   ArgoCD            `yaml:"argocd"`
	Webhooks []mapping.Webhook `yaml:"webhooks"` // config-declared mapping webhooks
}

// Metrics configures the Prometheus exposition surface. /metrics on the
// main listener is always on and bearer-authed with api_tokens. Listen
// optionally opens a SECOND, UNAUTHENTICATED listener serving only /metrics —
// for in-cluster scrapes where handing Prometheus an api_token (which also
// grants /api/* including config writes) would be over-privileged. Never
// expose that listener publicly; it is meant to stay behind a NetworkPolicy.
type Metrics struct {
	Listen string `yaml:"listen"` // e.g. ":9091"; empty disables the extra listener
}

// Digest configures the optional serve-side scheduled Slack digest. Enabled
// only when both interval and slack_webhook are set.
type Digest struct {
	Interval     Duration `yaml:"interval"`      // e.g. 24h; 0 disables
	Window       Duration `yaml:"window"`        // how far back each digest looks (default = interval)
	SlackWebhook string   `yaml:"slack_webhook"` // incoming-webhook URL (secret; use ${VAR})
}

// Retention configures the prune job (SPEC §8). Opt-in: the whole job is
// disabled unless Keep is set, so a fresh operator never gets silent
// auto-deletion. Rows whose env matches EphemeralEnvPattern (a SQLite GLOB,
// e.g. "pr-*") use the shorter EphemeralKeep so ephemeral-env churn doesn't
// accumulate. Interval defaults to 24h and the pattern to "pr-*" when unset.
type Retention struct {
	Keep                Duration `yaml:"keep"`                  // 0 disables the whole job
	EphemeralEnvPattern string   `yaml:"ephemeral_env_pattern"` // SQLite GLOB; default "pr-*"
	EphemeralKeep       Duration `yaml:"ephemeral_keep"`        // 0 => same as Keep
	Interval            Duration `yaml:"interval"`              // run cadence; default 24h when Keep set
}

// Catalog configures the service→owner lookup. Sources are scanned in a fixed
// priority order (backstage > datadog > services > codeowners); the first
// non-empty owner per service wins. Files are read by `wtc serve` at startup,
// not by CLI clients. Empty => owner is never inferred.
type Catalog struct {
	Sources []catalog.Source `yaml:"sources"`
}

// Config is the full wtc.yaml.
type Config struct {
	Server      Server           `yaml:"server"`
	Storage     Storage          `yaml:"storage"` // backend selection; default sqlite
	Auth        Auth             `yaml:"auth"`
	Sources     Sources          `yaml:"sources"`
	Rules       []normalize.Rule `yaml:"rules"`        // ordered env/service inference rules (SPEC §3)
	TagPatterns []string         `yaml:"tag_patterns"` // tag→sha extraction; empty = defaults (SPEC §2)
	Catalog     Catalog          `yaml:"catalog"`      // optional service→owner lookup
	Digest      Digest           `yaml:"digest"`       // optional scheduled Slack digest
	Retention   Retention        `yaml:"retention"`    // optional prune job (SPEC §8)
	Metrics     Metrics          `yaml:"metrics"`      // Prometheus exposition
	// Notifications are the outbound subscriptions: glob match over stored
	// events → sink (slack | webhook | grafana-annotation). Schema lives in
	// internal/notify, mirroring the mapping.Webhook pattern.
	Notifications []notify.Subscription `yaml:"notifications"`
}

// Default returns the config used when no file or overrides are present.
func Default() Config {
	return Config{
		Server: Server{
			Listen: ":8484",
			DB:     "./wtc.db",
		},
		Storage: Storage{
			Backend: "sqlite",
		},
		Sources: Sources{
			GitHub: GitHub{
				PollInterval: Duration(60 * time.Second),
				InfraPath:    "infrastructure/",
				Backfill:     Duration(24 * time.Hour),
			},
			GitLab: GitLab{
				PollInterval: Duration(60 * time.Second),
				InfraPath:    "infrastructure/",
				Backfill:     Duration(24 * time.Hour),
			},
			Flux: Flux{
				SuppressionWindow: Duration(10 * time.Minute),
			},
			ArgoCD: ArgoCD{
				SuppressionWindow: Duration(10 * time.Minute),
			},
		},
	}
}

// varPattern matches ${VAR} only — a bare $ (e.g. in a regex) is left alone.
var varPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// commentIndex returns the byte offset of the YAML comment on a line, or
// len(line) if there is none. A '#' starts a comment only when it is at the
// line start or preceded by whitespace, and not inside quotes — enough to
// tell a real ${VAR} reference from one sitting in a comment.
func commentIndex(line []byte) int {
	var inSingle, inDouble bool
	for i := 0; i < len(line); i++ {
		switch c := line[i]; c {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
				return i
			}
		}
	}
	return len(line)
}

// expandVars substitutes ${VAR} from the environment. Referencing an UNSET
// variable is an error: silently expanding to "" turns `db: ${WTC_DB_PATH}`
// into an ephemeral temp database and the ledger vanishes on restart. A
// variable explicitly set to the empty string is allowed (e.g. optional
// tokens, which Load filters out).
//
// Runs line-by-line and ignores the comment portion of each line, so a
// commented-out example like `#   api_token: ${WTC_GH_API_TOKEN}` is not
// treated as a live reference (that would make disabling a source by
// commenting it out fail with a confusing "unset variable" error).
func expandVars(raw []byte) ([]byte, error) {
	var missing []string
	replace := func(code []byte) []byte {
		return varPattern.ReplaceAllFunc(code, func(m []byte) []byte {
			name := string(m[2 : len(m)-1]) // strip "${" and "}"
			val, ok := os.LookupEnv(name)
			if !ok {
				missing = append(missing, name)
			}
			return []byte(val)
		})
	}

	lines := bytes.Split(raw, []byte("\n"))
	for i, line := range lines {
		c := commentIndex(line)
		lines[i] = append(replace(line[:c]), line[c:]...) // comment part kept verbatim
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("config references unset environment variable(s): %s", strings.Join(missing, ", "))
	}
	return bytes.Join(lines, []byte("\n")), nil
}

// Load reads path, expands ${VAR}, unmarshals over defaults, then applies
// WTC_* env overrides. When optional is true a missing file is not an error
// (defaults + env are used).
func Load(path string, optional bool) (*Config, error) {
	cfg := Default()

	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		expanded, err := expandVars(raw)
		if err != nil {
			return nil, fmt.Errorf("config %s: %w", path, err)
		}
		if err := yaml.Unmarshal(expanded, &cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	case os.IsNotExist(err) && optional:
		// fall through to defaults + env
	default:
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	applyEnvOverrides(&cfg)

	// Drop tokens that are empty strings so an intentionally-empty ${VAR}
	// cannot become an accept-anything credential.
	cfg.Auth.APITokens = slices.DeleteFunc(cfg.Auth.APITokens, func(t string) bool { return t == "" })

	if cfg.Server.Listen == "" {
		return nil, fmt.Errorf("config %s: server.listen must not be empty", path)
	}
	if cfg.Server.DB == "" {
		return nil, fmt.Errorf("config %s: server.db must not be empty", path)
	}
	// Storage backend: fail fast on a typo'd backend or a postgres
	// selection without a DSN — a silently-wrong storage config must never
	// reach Open.
	switch cfg.Storage.Backend {
	case "", "sqlite":
		cfg.Storage.Backend = "sqlite"
	case "postgres":
		if cfg.Storage.DSN == "" {
			return nil, fmt.Errorf("config %s: storage.backend=postgres requires storage.dsn", path)
		}
	default:
		return nil, fmt.Errorf("config %s: storage.backend must be sqlite or postgres, got %q", path, cfg.Storage.Backend)
	}

	// Backfill windows must be positive — a negative window would poll the
	// future; zero falls back to the 24h default at poller construction.
	if cfg.Sources.GitHub.Backfill.Std() < 0 || cfg.Sources.GitLab.Backfill.Std() < 0 {
		return nil, fmt.Errorf("config %s: sources.*.backfill must not be negative", path)
	}

	// Poller scope globs: compile up front — a bad pattern must fail
	// startup, never become a silently-empty scope. GitLab patterns
	// additionally need a static namespace prefix: that prefix is the bounded
	// discovery call, and unscoped listing is not supported there. GitHub
	// accepts any glob — its discovery is already bounded by token
	// affiliation, so a pattern only ever filters.
	for _, r := range cfg.Sources.GitHub.Repos {
		if !normalize.IsGlob(r) {
			continue
		}
		if _, err := normalize.CompileGlob(r); err != nil {
			return nil, fmt.Errorf("config %s: sources.github.repos %q: %w", path, r, err)
		}
	}
	for _, pr := range cfg.Sources.GitLab.Projects {
		if !normalize.IsGlob(pr) {
			continue
		}
		if _, err := normalize.CompileGlob(pr); err != nil {
			return nil, fmt.Errorf("config %s: sources.gitlab.projects %q: %w", path, pr, err)
		}
		if _, ok := normalize.ScopeNamespace(pr); !ok {
			return nil, fmt.Errorf("config %s: sources.gitlab.projects glob %q needs a static namespace prefix (e.g. my-group/*) — unscoped discovery is not supported on gitlab", path, pr)
		}
	}

	// Notifications: compile up front — a bad sink shape or glob must
	// fail startup, never a delivery.
	if _, err := notify.Compile(cfg.Notifications); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}

	// Flux/ArgoCD ingest scope (allow/deny): compile the globs up front so a
	// bad pattern or an empty (match-everything) entry fails startup instead of
	// silently dropping — or admitting — every event.
	if _, err := cfg.Sources.Flux.Scope.Compile(); err != nil {
		return nil, fmt.Errorf("config %s: sources.flux.scope: %w", path, err)
	}
	if _, err := cfg.Sources.ArgoCD.Scope.Compile(); err != nil {
		return nil, fmt.Errorf("config %s: sources.argocd.scope: %w", path, err)
	}

	// Catalog sources: validate shape up front (type known, path set, codeowners
	// carries a repo). The files themselves are read by serve, not here — a CLI
	// client need not have the catalog on disk.
	for i, cs := range cfg.Catalog.Sources {
		if !slices.Contains(catalog.ValidTypes, cs.Type) {
			return nil, fmt.Errorf("config %s: catalog.sources[%d].type %q invalid (want one of %s)",
				path, i, cs.Type, strings.Join(catalog.ValidTypes, ", "))
		}
		if cs.Path == "" {
			return nil, fmt.Errorf("config %s: catalog.sources[%d] needs a path", path, i)
		}
		if cs.Type == "codeowners" && cs.Repo == "" {
			return nil, fmt.Errorf("config %s: catalog.sources[%d] (codeowners) needs `repo`", path, i)
		}
	}

	return &cfg, nil
}

// applyEnvOverrides maps WTC_* variables onto config fields. Explicit table,
// extended as new sections land.
func applyEnvOverrides(cfg *Config) {
	set := func(target *string, key string) {
		if v, ok := os.LookupEnv(key); ok {
			*target = v
		}
	}
	set(&cfg.Server.Listen, "WTC_SERVER_LISTEN")
	set(&cfg.Server.DB, "WTC_SERVER_DB")
	set(&cfg.Server.CaptureDir, "WTC_SERVER_CAPTURE_DIR")
	set(&cfg.Storage.Backend, "WTC_STORAGE_BACKEND")
	set(&cfg.Storage.DSN, "WTC_STORAGE_DSN")
	// Comma-separated origins, e.g. "https://portal.example.com,http://localhost:5173".
	if v, ok := os.LookupEnv("WTC_SERVER_CORS_ALLOWED_ORIGINS"); ok {
		cfg.Server.CORS.AllowedOrigins = splitAndTrim(v)
	}
	set(&cfg.Sources.GitHub.APIToken, "WTC_GH_API_TOKEN")
	set(&cfg.Sources.GitHub.WebhookSecret, "WTC_GH_WEBHOOK_SECRET")
	set(&cfg.Sources.GitLab.APIToken, "WTC_GITLAB_API_TOKEN")
	set(&cfg.Sources.GitLab.WebhookSecret, "WTC_GITLAB_WEBHOOK_SECRET")
	set(&cfg.Sources.GitLab.BaseURL, "WTC_GITLAB_BASE_URL")
	set(&cfg.Sources.Flux.HMACKey, "WTC_FLUX_HMAC_KEY")
	set(&cfg.Sources.ArgoCD.WebhookSecret, "WTC_ARGOCD_WEBHOOK_SECRET")
	set(&cfg.Digest.SlackWebhook, "WTC_SLACK_WEBHOOK")
	set(&cfg.Metrics.Listen, "WTC_METRICS_LISTEN")

	if v, ok := os.LookupEnv("WTC_API_TOKEN"); ok && v != "" {
		if !slices.Contains(cfg.Auth.APITokens, v) {
			cfg.Auth.APITokens = append(cfg.Auth.APITokens, v)
		}
	}
}

// splitAndTrim splits a comma-separated list, trimming whitespace and dropping
// empty entries — used for env-supplied string lists.
func splitAndTrim(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

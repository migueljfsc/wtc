// Package config loads the single wtc.yaml configuration file, expands
// ${VAR} references from the environment, and applies WTC_* env overrides.
// Hand-rolled on purpose (no viper/koanf) — see CLAUDE.md hard decisions.
package config

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/migueljfsc/wtc/internal/normalize"
)

// Duration wraps time.Duration with YAML support for "60s"/"10m" strings.
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("duration must be a string like \"60s\": %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
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
}

// Auth holds static bearer tokens accepted on /api/* and /ingest/generic.
type Auth struct {
	APITokens []string `yaml:"api_tokens"`
}

// GitHub configures the GitHub ingest paths (SPEC §2). The poller is the
// primary path for private deployments; webhooks need a public endpoint.
type GitHub struct {
	WebhookSecret string   `yaml:"webhook_secret"` // enables /ingest/github HMAC verification
	APIToken      string   `yaml:"api_token"`      // enables the poller + PR-diff enrichment
	PollInterval  Duration `yaml:"poll_interval"`  // 0 disables the poller (webhook-only mode)
	Repos         []string `yaml:"repos"`          // poller scope, owner/name
	InfraPath     string   `yaml:"infra_path"`     // per-repo manifests dir (microservices layout)
}

// Flux configures the notification-controller ingest path (SPEC §2).
type Flux struct {
	HMACKey           string   `yaml:"hmac_key"`           // generic-hmac provider shared key
	SuppressionWindow Duration `yaml:"suppression_window"` // drop repeats of (object,revision,reason) inside this window
}

// Sources groups per-source ingest configuration.
type Sources struct {
	GitHub GitHub `yaml:"github"`
	Flux   Flux   `yaml:"flux"`
}

// Digest configures the optional serve-side scheduled Slack digest. Enabled
// only when both interval and slack_webhook are set.
type Digest struct {
	Interval     Duration `yaml:"interval"`      // e.g. 24h; 0 disables
	Window       Duration `yaml:"window"`        // how far back each digest looks (default = interval)
	SlackWebhook string   `yaml:"slack_webhook"` // incoming-webhook URL (secret; use ${VAR})
}

// Config is the full wtc.yaml.
type Config struct {
	Server      Server           `yaml:"server"`
	Auth        Auth             `yaml:"auth"`
	Sources     Sources          `yaml:"sources"`
	Rules       []normalize.Rule `yaml:"rules"`        // ordered env/service inference rules (SPEC §3)
	TagPatterns []string         `yaml:"tag_patterns"` // tag→sha extraction; empty = defaults (SPEC §2)
	Digest      Digest           `yaml:"digest"`       // optional scheduled Slack digest
}

// Default returns the config used when no file or overrides are present.
func Default() Config {
	return Config{
		Server: Server{
			Listen: ":8484",
			DB:     "./wtc.db",
		},
		Sources: Sources{
			GitHub: GitHub{
				PollInterval: Duration(60 * time.Second),
				InfraPath:    "infrastructure/",
			},
			Flux: Flux{
				SuppressionWindow: Duration(10 * time.Minute),
			},
		},
	}
}

// varPattern matches ${VAR} only — a bare $ (e.g. in a regex) is left alone.
var varPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandVars substitutes ${VAR} from the environment. Referencing an UNSET
// variable is an error: silently expanding to "" turns `db: ${WTC_DB_PATH}`
// into an ephemeral temp database and the ledger vanishes on restart. A
// variable explicitly set to the empty string is allowed (e.g. optional
// tokens, which Load filters out).
func expandVars(raw []byte) ([]byte, error) {
	var missing []string
	expanded := varPattern.ReplaceAllFunc(raw, func(m []byte) []byte {
		name := string(m[2 : len(m)-1]) // strip "${" and "}"
		val, ok := os.LookupEnv(name)
		if !ok {
			missing = append(missing, name)
		}
		return []byte(val)
	})
	if len(missing) > 0 {
		return nil, fmt.Errorf("config references unset environment variable(s): %s", strings.Join(missing, ", "))
	}
	return expanded, nil
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
	set(&cfg.Sources.GitHub.APIToken, "WTC_GH_API_TOKEN")
	set(&cfg.Sources.GitHub.WebhookSecret, "WTC_GH_WEBHOOK_SECRET")
	set(&cfg.Sources.Flux.HMACKey, "WTC_FLUX_HMAC_KEY")
	set(&cfg.Digest.SlackWebhook, "WTC_SLACK_WEBHOOK")

	if v, ok := os.LookupEnv("WTC_API_TOKEN"); ok && v != "" {
		if !slices.Contains(cfg.Auth.APITokens, v) {
			cfg.Auth.APITokens = append(cfg.Auth.APITokens, v)
		}
	}
}

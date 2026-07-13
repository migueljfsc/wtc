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

	"gopkg.in/yaml.v3"
)

// Server holds the serve daemon settings. Fields for later phases (base_url,
// capture_dir, sources, rules, retention) are added with the code that reads
// them — dead config surface is a silent-no-op trap for operators.
type Server struct {
	Listen string `yaml:"listen"`
	DB     string `yaml:"db"`
}

// Auth holds static bearer tokens accepted on /api/* and /ingest/generic.
type Auth struct {
	APITokens []string `yaml:"api_tokens"`
}

// Config is the full wtc.yaml.
type Config struct {
	Server Server `yaml:"server"`
	Auth   Auth   `yaml:"auth"`
}

// Default returns the config used when no file or overrides are present.
func Default() Config {
	return Config{
		Server: Server{
			Listen: ":8484",
			DB:     "./wtc.db",
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

	if v, ok := os.LookupEnv("WTC_API_TOKEN"); ok && v != "" {
		if !slices.Contains(cfg.Auth.APITokens, v) {
			cfg.Auth.APITokens = append(cfg.Auth.APITokens, v)
		}
	}
}

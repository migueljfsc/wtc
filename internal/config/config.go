// Package config loads the single wtc.yaml configuration file, expands
// ${VAR} references from the environment, and applies WTC_* env overrides.
// Hand-rolled on purpose (no viper/koanf) — see CLAUDE.md hard decisions.
package config

import (
	"fmt"
	"os"
	"regexp"
	"slices"

	"gopkg.in/yaml.v3"
)

// Server holds the serve daemon settings.
type Server struct {
	Listen     string `yaml:"listen"`
	DB         string `yaml:"db"`
	BaseURL    string `yaml:"base_url"`
	CaptureDir string `yaml:"capture_dir"`
}

// Auth holds static bearer tokens accepted on /api/* and /ingest/generic.
type Auth struct {
	APITokens []string `yaml:"api_tokens"`
}

// Config is the full wtc.yaml. Sections for sources/rules/tag_patterns/
// retention are added in the phases that implement them.
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

func expandVars(raw []byte) []byte {
	return varPattern.ReplaceAllFunc(raw, func(m []byte) []byte {
		name := varPattern.FindSubmatch(m)[1]
		return []byte(os.Getenv(string(name)))
	})
}

// Load reads path, expands ${VAR}, unmarshals over defaults, then applies
// WTC_* env overrides. When optional is true a missing file is not an error
// (defaults + env are used).
func Load(path string, optional bool) (*Config, error) {
	cfg := Default()

	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(expandVars(raw), &cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	case os.IsNotExist(err) && optional:
		// fall through to defaults + env
	default:
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	applyEnvOverrides(&cfg)

	// Drop tokens that expanded to empty strings so an unset ${VAR} cannot
	// silently become an accept-anything credential.
	cfg.Auth.APITokens = slices.DeleteFunc(cfg.Auth.APITokens, func(t string) bool { return t == "" })

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
	set(&cfg.Server.BaseURL, "WTC_SERVER_BASE_URL")
	set(&cfg.Server.CaptureDir, "WTC_SERVER_CAPTURE_DIR")

	if v, ok := os.LookupEnv("WTC_API_TOKEN"); ok && v != "" {
		if !slices.Contains(cfg.Auth.APITokens, v) {
			cfg.Auth.APITokens = append(cfg.Auth.APITokens, v)
		}
	}
}

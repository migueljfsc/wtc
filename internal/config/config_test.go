package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wtc.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"), true)
	if err != nil {
		t.Fatalf("Load optional missing file: %v", err)
	}
	if cfg.Server.Listen != ":8484" {
		t.Errorf("Listen = %q, want :8484", cfg.Server.Listen)
	}
	if cfg.Server.DB != "./wtc.db" {
		t.Errorf("DB = %q, want ./wtc.db", cfg.Server.DB)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml"), false); err == nil {
		t.Fatal("Load non-optional missing file: want error, got nil")
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"60s", 60 * time.Second, false},
		{"10m", 10 * time.Minute, false},
		{"24h", 24 * time.Hour, false},
		{"180d", 180 * 24 * time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},
		{"2w", 14 * 24 * time.Hour, false},
		{"0.5d", 12 * time.Hour, false},
		{"1d12h", 0, true}, // composite d-forms unsupported by design
		{"nope", 0, true},
	}
	for _, tt := range tests {
		got, err := parseDuration(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseDuration(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("parseDuration(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestRetentionConfig(t *testing.T) {
	path := writeFile(t, `
server:
  listen: ":8484"
  db: ./wtc.db
retention:
  keep: 180d
  ephemeral_env_pattern: "pr-*"
  ephemeral_keep: 30d
  interval: 24h
`)
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Retention.Keep.Std() != 180*24*time.Hour {
		t.Errorf("Keep = %s, want 180d", cfg.Retention.Keep.Std())
	}
	if cfg.Retention.EphemeralKeep.Std() != 30*24*time.Hour {
		t.Errorf("EphemeralKeep = %s, want 30d", cfg.Retention.EphemeralKeep.Std())
	}
	if cfg.Retention.EphemeralEnvPattern != "pr-*" {
		t.Errorf("EphemeralEnvPattern = %q, want pr-*", cfg.Retention.EphemeralEnvPattern)
	}
	if cfg.Retention.Interval.Std() != 24*time.Hour {
		t.Errorf("Interval = %s, want 24h", cfg.Retention.Interval.Std())
	}
}

func TestWebhooksConfig(t *testing.T) {
	t.Setenv("TEST_GRAFANA_TOKEN", "graf-sekrit")
	path := writeFile(t, `
server:
  listen: ":8484"
  db: ./wtc.db
sources:
  webhooks:
    - name: grafana
      preset: grafana
      auth:
        token: ${TEST_GRAFANA_TOKEN}
        header: Authorization
    - name: harbor
      auth:
        hmac: { secret: xyz, header: X-Sig, algo: sha256 }
      dedup_key: 'harbor:{{ .id }}'
      mapping: { kind: build, title: 'Harbor {{ .id }}' }
      facts: { service: '{{ .repo }}' }
`)
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Sources.Webhooks) != 2 {
		t.Fatalf("webhooks = %d, want 2", len(cfg.Sources.Webhooks))
	}
	g := cfg.Sources.Webhooks[0]
	if g.Name != "grafana" || g.Preset != "grafana" || g.Auth.Token != "graf-sekrit" || g.Auth.Header != "Authorization" {
		t.Errorf("grafana webhook = %+v", g)
	}
	h := cfg.Sources.Webhooks[1]
	if h.Auth.HMAC == nil || h.Auth.HMAC.Header != "X-Sig" || h.DedupKey != "harbor:{{ .id }}" {
		t.Errorf("harbor webhook = %+v (hmac %+v)", h, h.Auth.HMAC)
	}
	if h.Facts["service"] != "{{ .repo }}" {
		t.Errorf("harbor facts = %v", h.Facts)
	}
}

func TestLoadFileAndVarExpansion(t *testing.T) {
	t.Setenv("TEST_WTC_TOKEN", "sekrit")
	t.Setenv("TEST_WTC_EMPTY_TOKEN", "") // set-but-empty is allowed and filtered
	path := writeFile(t, `
server:
  listen: ":9999"
  db: /data/wtc.db
auth:
  api_tokens:
    - ${TEST_WTC_TOKEN}
    - ${TEST_WTC_EMPTY_TOKEN}
`)
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != ":9999" {
		t.Errorf("Listen = %q, want :9999", cfg.Server.Listen)
	}
	if len(cfg.Auth.APITokens) != 1 || cfg.Auth.APITokens[0] != "sekrit" {
		t.Errorf("APITokens = %v, want [sekrit] (empty-expansion dropped)", cfg.Auth.APITokens)
	}
}

func TestVarInCommentIsIgnored(t *testing.T) {
	// A ${VAR} in a commented-out line must NOT be treated as a live
	// reference — this is exactly `make run` with the sources block disabled.
	path := writeFile(t, `
server:
  db: /data/wtc.db
auth:
  api_tokens: [dev-token]
# sources:
#   github:
#     api_token: ${WTC_GH_API_TOKEN}   # unset, but commented — must not error
  db2: value # trailing ${ALSO_UNSET} in a comment
`)
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatalf("commented ${VAR} must not error: %v", err)
	}
	if cfg.Server.DB != "/data/wtc.db" {
		t.Errorf("DB = %q", cfg.Server.DB)
	}
}

func TestUnsetVarIsError(t *testing.T) {
	path := writeFile(t, `
server:
  db: ${TEST_WTC_DEFINITELY_UNSET_DB}
`)
	_, err := Load(path, false)
	if err == nil {
		t.Fatal("Load with unset ${VAR}: want error (silent empty expansion loses the ledger), got nil")
	}
	if !strings.Contains(err.Error(), "TEST_WTC_DEFINITELY_UNSET_DB") {
		t.Errorf("error %q must name the unset variable", err)
	}
}

func TestEmptyCriticalFieldsRejected(t *testing.T) {
	for _, body := range []string{
		"server:\n  db: \"\"\n",
		"server:\n  listen: \"\"\n",
	} {
		path := writeFile(t, body)
		if _, err := Load(path, false); err == nil {
			t.Errorf("Load(%q): want error for empty critical field, got nil", body)
		}
	}

	// A var set to "" expands to YAML null, which yaml.v3 skips — the
	// default survives. Safe (never an empty path), just worth pinning.
	t.Setenv("TEST_WTC_EMPTY", "")
	path := writeFile(t, "server:\n  db: ${TEST_WTC_EMPTY}\n")
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatalf("Load with empty-expanded db: %v", err)
	}
	if cfg.Server.DB != "./wtc.db" {
		t.Errorf("DB = %q, want default ./wtc.db when expansion yields null", cfg.Server.DB)
	}
}

func TestBareDollarUntouched(t *testing.T) {
	path := writeFile(t, `
server:
  db: "/data/$notavar-and-${}-stay.db"
`)
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.DB != "/data/$notavar-and-${}-stay.db" {
		t.Errorf("DB = %q, bare $ must be preserved", cfg.Server.DB)
	}
}

func TestGitHubSourceConfig(t *testing.T) {
	t.Setenv("TEST_WTC_GH_TOKEN", "ghtok")
	path := writeFile(t, `
sources:
  github:
    api_token: ${TEST_WTC_GH_TOKEN}
    poll_interval: 90s
    repos: [org/app-api, org/app-web]
`)
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	gh := cfg.Sources.GitHub
	if gh.APIToken != "ghtok" {
		t.Errorf("APIToken = %q", gh.APIToken)
	}
	if gh.PollInterval.Std() != 90*time.Second {
		t.Errorf("PollInterval = %v, want 90s", gh.PollInterval.Std())
	}
	if len(gh.Repos) != 2 || gh.Repos[0] != "org/app-api" {
		t.Errorf("Repos = %v", gh.Repos)
	}
	// Defaults survive partial config.
	if gh.InfraPath != "infrastructure/" {
		t.Errorf("InfraPath = %q, want default infrastructure/", gh.InfraPath)
	}

	// Bad duration is a load error, not a silent zero.
	bad := writeFile(t, "sources:\n  github:\n    poll_interval: fast\n")
	if _, err := Load(bad, false); err == nil {
		t.Error("Load with poll_interval 'fast': want error")
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("WTC_SERVER_LISTEN", ":7777")
	t.Setenv("WTC_API_TOKEN", "env-token")
	path := writeFile(t, `
server:
  listen: ":9999"
auth:
  api_tokens: [file-token]
`)
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != ":7777" {
		t.Errorf("Listen = %q, env override must win", cfg.Server.Listen)
	}
	want := []string{"file-token", "env-token"}
	if len(cfg.Auth.APITokens) != 2 || cfg.Auth.APITokens[0] != want[0] || cfg.Auth.APITokens[1] != want[1] {
		t.Errorf("APITokens = %v, want %v", cfg.Auth.APITokens, want)
	}
}

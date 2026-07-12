package config

import (
	"os"
	"path/filepath"
	"testing"
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

func TestLoadFileAndVarExpansion(t *testing.T) {
	t.Setenv("TEST_WTC_TOKEN", "sekrit")
	path := writeFile(t, `
server:
  listen: ":9999"
  db: /data/wtc.db
auth:
  api_tokens:
    - ${TEST_WTC_TOKEN}
    - ${TEST_WTC_UNSET_TOKEN}
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

func TestBareDollarUntouched(t *testing.T) {
	path := writeFile(t, `
server:
  base_url: "http://x/$notavar-and-${}-stay"
`)
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.BaseURL != "http://x/$notavar-and-${}-stay" {
		t.Errorf("BaseURL = %q, bare $ must be preserved", cfg.Server.BaseURL)
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

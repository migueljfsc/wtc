package normalize

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantGone []string // substrings that must not survive
		wantKept []string // substrings that must survive
	}{
		{
			name:     "aws access key",
			in:       "deploy used AKIAIOSFODNN7EXAMPLE for s3",
			wantGone: []string{"AKIAIOSFODNN7EXAMPLE"},
			wantKept: []string{"deploy used", "for s3"},
		},
		{
			name:     "github classic pat",
			in:       "push with ghp_abcdefghijklmnopqrstuvwxyz0123456789",
			wantGone: []string{"ghp_abcdefghijklmnopqrstuvwxyz0123456789"},
			wantKept: []string{"push with"},
		},
		{
			name:     "github fine-grained pat",
			in:       "creds github_pat_11ABCDEFG0123456789_abcdef rotated",
			wantGone: []string{"github_pat_11ABCDEFG0123456789_abcdef"},
			wantKept: []string{"rotated"},
		},
		{
			name:     "bearer token",
			in:       "header was Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig",
			wantGone: []string{"eyJhbGciOiJIUzI1NiJ9"},
			wantKept: []string{"header was"},
		},
		{
			name:     "password kv",
			in:       "hotfix: set password=hunter2 on db",
			wantGone: []string{"hunter2"},
			wantKept: []string{"hotfix", "password", "on db"},
		},
		{
			name:     "token in json payload",
			in:       `{"artifacts":["app:v1"],"token": "s3cr3t-value"}`,
			wantGone: []string{"s3cr3t-value"},
			wantKept: []string{"app:v1"},
		},
		{
			name:     "clean text untouched",
			in:       "deploy api sha-abc1234 to prod",
			wantKept: []string{"deploy api sha-abc1234 to prod"},
		},
		{
			name: "empty",
			in:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.in)
			for _, s := range tt.wantGone {
				if strings.Contains(got, s) {
					t.Errorf("Redact(%q) = %q — still contains %q", tt.in, got, s)
				}
			}
			for _, s := range tt.wantKept {
				if !strings.Contains(got, s) {
					t.Errorf("Redact(%q) = %q — lost %q", tt.in, got, s)
				}
			}
			if tt.in == "" && got != "" {
				t.Errorf("Redact(\"\") = %q, want empty", got)
			}
		})
	}
}

package wrap

import (
	"bytes"
	"context"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/migueljfsc/wtc/internal/client"
	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/server"
	"github.com/migueljfsc/wtc/internal/store"
)

func TestSniff(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want Sniffed
	}{
		{
			name: "helm upgrade with namespace and image tag",
			args: strings.Fields("helm upgrade pr-123 ./charts/app -n pr-123 --set image.tag=sha-abc1234,replicas=2 --wait"),
			want: Sniffed{Source: "helm", Kind: "deploy", Service: "pr-123", Namespace: "pr-123", Artifact: "./charts/app", ImageTag: "sha-abc1234"},
		},
		{
			name: "helm install with flag-separated values before positionals",
			args: strings.Fields("helm install -n edge --values v.yaml myapp repo/chart"),
			want: Sniffed{Source: "helm", Kind: "deploy", Service: "myapp", Namespace: "edge", Artifact: "repo/chart"},
		},
		{
			name: "helm non-deploy subcommand stays generic",
			args: strings.Fields("helm list -A"),
			want: Sniffed{Source: "generic", Kind: "manual"},
		},
		{
			name: "terraform apply",
			args: strings.Fields("terraform apply -auto-approve -json"),
			want: Sniffed{Source: "terraform", Kind: "infra_change", Terraform: true},
		},
		{
			name: "tofu destroy",
			args: strings.Fields("tofu destroy -auto-approve"),
			want: Sniffed{Source: "terraform", Kind: "infra_change", Terraform: true},
		},
		{
			name: "terraform plan is not a change",
			args: strings.Fields("terraform plan"),
			want: Sniffed{Source: "generic", Kind: "manual"},
		},
		{
			name: "arbitrary command",
			args: strings.Fields("kubectl rollout restart deploy/app"),
			want: Sniffed{Source: "generic", Kind: "manual"},
		},
		{
			name: "full path helm binary",
			args: strings.Fields("/usr/local/bin/helm upgrade rel ./c"),
			want: Sniffed{Source: "helm", Kind: "deploy", Service: "rel", Artifact: "./c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Sniff(tt.args); got != tt.want {
				t.Errorf("Sniff(%v)\n got %+v\nwant %+v", tt.args, got, tt.want)
			}
		})
	}
}

func newTestBackend(t *testing.T) (*client.Client, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "wtc.db"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.New(st, server.Options{Tokens: []string{"t"}}, slog.New(slog.DiscardHandler)).Handler())
	t.Cleanup(func() { srv.Close(); _ = st.Close() })
	return client.New(srv.URL, "t"), st
}

func TestRunLifecycle(t *testing.T) {
	c, st := newTestBackend(t)
	var out, errBuf bytes.Buffer

	code := Run(context.Background(), c, Options{Env: "pr-123"},
		[]string{"sh", "-c", "echo doing work"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "doing work") {
		t.Errorf("stdout not inherited: %q", out.String())
	}

	events, _, err := st.ListEvents(context.Background(), store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d rows, want 1 (started upserted to terminal)", len(events))
	}
	ev := events[0]
	if ev.Status != model.StatusSucceeded || ev.Env != "pr-123" {
		t.Errorf("status/env = %s/%s", ev.Status, ev.Env)
	}
	if ev.DurationMS == nil {
		t.Error("duration missing")
	}
	if !strings.Contains(ev.Payload, `"exit_code":0`) {
		t.Errorf("payload = %q, want exit_code", ev.Payload)
	}
}

func TestRunFailurePropagatesExitCode(t *testing.T) {
	c, st := newTestBackend(t)
	var out, errBuf bytes.Buffer

	code := Run(context.Background(), c, Options{},
		[]string{"sh", "-c", "exit 3"}, &out, &errBuf)
	if code != 3 {
		t.Fatalf("exit code = %d, want 3 (transparent passthrough)", code)
	}
	events, _, _ := st.ListEvents(context.Background(), store.Filter{})
	if len(events) != 1 || events[0].Status != model.StatusFailed {
		t.Fatalf("events = %+v, want one failed", events)
	}
	if !strings.Contains(events[0].Payload, `"exit_code":3`) {
		t.Errorf("payload = %q", events[0].Payload)
	}
}

func TestRunServerDownStillRuns(t *testing.T) {
	c := client.New("http://127.0.0.1:1", "t") // nothing listens here
	var out, errBuf bytes.Buffer

	code := Run(context.Background(), c, Options{},
		[]string{"sh", "-c", "echo survived"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit code = %d — a dead server must never block the command", code)
	}
	if !strings.Contains(out.String(), "survived") {
		t.Errorf("command did not run: %q", out.String())
	}
	if !strings.Contains(errBuf.String(), "unreachable") {
		t.Errorf("expected an unreachable warning, got %q", errBuf.String())
	}
}

func TestRunTerraformJSONSummary(t *testing.T) {
	c, st := newTestBackend(t)
	var out, errBuf bytes.Buffer

	// Fake terraform emitting a -json stream with a change_summary line.
	script := `echo '{"type":"planned_change","hook":{}}'
echo '{"type":"change_summary","changes":{"add":3,"change":1,"remove":2}}'`
	code := Run(context.Background(), c, Options{Env: "prod"},
		[]string{"sh", "-c", "exec 2>/dev/null; " + script}, &out, &errBuf)
	_ = code

	// The sniffer keyed on argv[0] "sh" → generic; run again simulating the
	// real shape by invoking through a terraform-named shim.
	dir := t.TempDir()
	shim := filepath.Join(dir, "terraform")
	if err := writeShim(shim, script); err != nil {
		t.Fatal(err)
	}
	code = Run(context.Background(), c, Options{Env: "prod"},
		[]string{shim, "apply", "-auto-approve", "-json"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit = %d, stderr %s", code, errBuf.String())
	}

	events, _, err := st.ListEvents(context.Background(), store.Filter{Kind: "infra_change"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("infra_change rows = %d, want 1", len(events))
	}
	ev := events[0]
	if !strings.Contains(ev.Title, "(+3 ~1 -2)") {
		t.Errorf("title = %q, want change counts", ev.Title)
	}
	if strings.Contains(ev.Payload, "planned_change") {
		t.Errorf("payload must never contain plan bodies: %q", ev.Payload)
	}
	if ev.Source != model.SourceTerraform {
		t.Errorf("source = %s", ev.Source)
	}
}

func writeShim(path, body string) error {
	return os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755) //nolint:gosec // test shim must be executable
}

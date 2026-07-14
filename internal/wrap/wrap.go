// Package wrap implements `wtc wrap -- <command>`: record a started event,
// run the command with inherited stdio, then upsert the terminal status onto
// the same dedup key. wtc must never block operations — a dead server is a
// warning, not a failure (SPEC §5).
package wrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/migueljfsc/wtc/internal/client"
	"github.com/migueljfsc/wtc/internal/ingest/generic"
	"github.com/migueljfsc/wtc/internal/model"
)

// Options carry operator overrides; sniffed values fill the gaps.
type Options struct {
	Env     string
	Service string
	Title   string
}

// Sniffed is what the arg sniffers infer from the wrapped command line.
type Sniffed struct {
	Source    string // helm | terraform | generic
	Kind      string // deploy | infra_change | manual
	Service   string
	Namespace string
	Artifact  string
	ImageTag  string // helm --set image.tag=... when present
	Terraform bool   // enables -json change-summary scanning
}

// Sniff inspects a command line per SPEC §5. Unknown commands wrap fine —
// they just land as source=generic kind=manual.
func Sniff(args []string) Sniffed {
	s := Sniffed{Source: "generic", Kind: "manual"}
	if len(args) == 0 {
		return s
	}
	switch base(args[0]) {
	case "helm":
		sub, rest := firstNonFlag(args[1:])
		if sub != "upgrade" && sub != "install" {
			return s
		}
		s.Source = "helm"
		s.Kind = "deploy"
		release, rest2 := firstNonFlag(rest)
		chart, _ := firstNonFlag(rest2)
		s.Service = release
		s.Artifact = chart
		s.Namespace = flagValue(args, "-n", "--namespace")
		for _, set := range flagValues(args, "--set") {
			for _, kv := range strings.Split(set, ",") {
				if v, ok := strings.CutPrefix(kv, "image.tag="); ok {
					s.ImageTag = v
				}
			}
		}
	case "terraform", "tofu":
		sub, _ := firstNonFlag(args[1:])
		if sub != "apply" && sub != "destroy" {
			return s
		}
		s.Source = "terraform"
		s.Kind = "infra_change"
		s.Terraform = true
	}
	return s
}

func base(cmd string) string {
	if i := strings.LastIndex(cmd, "/"); i >= 0 {
		return cmd[i+1:]
	}
	return cmd
}

// firstNonFlag returns the first argument that isn't a flag or a flag value,
// plus the remainder after it. Heuristic: a token starting with '-' consumes
// the next token when it doesn't contain '=' (best effort; helm flags with
// separate values are the common case).
func firstNonFlag(args []string) (string, []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if !strings.Contains(a, "=") && i+1 < len(args) && isFlagWithValue(a) {
				i++ // skip the flag's value
			}
			continue
		}
		return a, args[i+1:]
	}
	return "", nil
}

// isFlagWithValue lists the common helm/terraform flags that take a separate
// value; boolean flags (--wait, --atomic, -auto-approve) must not swallow the
// next token.
func isFlagWithValue(flag string) bool {
	switch strings.TrimLeft(flag, "-") {
	case "n", "namespace", "f", "values", "set", "set-string", "set-file",
		"version", "kube-context", "kubeconfig", "timeout", "var", "var-file":
		return true
	}
	return false
}

func flagValue(args []string, names ...string) string {
	for i, a := range args {
		for _, n := range names {
			if a == n && i+1 < len(args) {
				return args[i+1]
			}
			if v, ok := strings.CutPrefix(a, n+"="); ok {
				return v
			}
		}
	}
	return ""
}

func flagValues(args []string, name string) []string {
	var out []string
	for i, a := range args {
		if a == name && i+1 < len(args) {
			out = append(out, args[i+1])
		} else if v, ok := strings.CutPrefix(a, name+"="); ok {
			out = append(out, v)
		}
	}
	return out
}

// tfSummary counts resources from terraform's -json change_summary line.
type tfSummary struct {
	Add    int
	Change int
	Remove int
	seen   bool
}

// Run executes the wrapped command and reports its lifecycle. Returns the
// command's exit code — wrap is transparent to scripts.
func Run(ctx context.Context, c *client.Client, opts Options, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "wtc wrap: no command given (usage: wtc wrap [flags] -- <command...>)")
		return 2
	}
	sniffed := Sniff(args)

	service := opts.Service
	if service == "" {
		service = sniffed.Service
	}
	title := opts.Title
	if title == "" {
		title = strings.Join(args, " ")
		if len(title) > 120 {
			title = title[:117] + "..."
		}
	}

	dedupKey := "local:" + model.NewID()
	req := generic.Request{
		Source:    sniffed.Source,
		Kind:      sniffed.Kind,
		Status:    string(model.StatusStarted),
		Env:       opts.Env,
		Service:   service,
		Namespace: sniffed.Namespace,
		Artifact:  sniffed.Artifact,
		Title:     title,
		DedupKey:  dedupKey,
	}

	// Best effort: a dead wtc server must never block the operation.
	reachable := true
	if _, err := c.IngestGeneric(ctx, req); err != nil {
		reachable = false
		_, _ = fmt.Fprintf(stderr, "wtc wrap: server unreachable, running anyway: %v\n", err)
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...) //nolint:gosec // wrapping arbitrary operator commands is the feature
	cmd.Stdin = os.Stdin
	cmd.Stderr = stderr
	var summary tfSummary
	finishScan := func() {}
	if sniffed.Terraform && hasJSONFlag(args) {
		// Tee stdout: the operator still sees the stream; we count the
		// change_summary. Only counts are kept, never plan bodies.
		pr, pw := io.Pipe()
		cmd.Stdout = io.MultiWriter(stdout, pw)
		done := make(chan struct{})
		go func() {
			defer close(done)
			scanTFStream(pr, &summary)
		}()
		finishScan = func() { _ = pw.Close(); <-done }
	} else {
		cmd.Stdout = stdout
	}

	start := time.Now()
	err := cmd.Run()
	durationMS := time.Since(start).Milliseconds()
	finishScan() // join the scanner before reading summary (no race)

	exitCode := 0
	status := model.StatusSucceeded
	if err != nil {
		status = model.StatusFailed
		exitCode = 1
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}
	}

	if summary.seen {
		req.Title = fmt.Sprintf("%s (+%d ~%d -%d)", title, summary.Add, summary.Change, summary.Remove)
	}
	req.Status = string(status)
	req.DurationMS = &durationMS
	req.Details = map[string]any{"exit_code": exitCode}
	if sniffed.ImageTag != "" {
		req.Details["image_tag"] = sniffed.ImageTag
	}
	if summary.seen {
		req.Details["tf_add"], req.Details["tf_change"], req.Details["tf_remove"] =
			summary.Add, summary.Change, summary.Remove
	}
	if sniffed.ImageTag != "" && sniffed.Artifact != "" {
		req.Artifacts = []string{sniffed.Artifact + ":" + sniffed.ImageTag}
	}

	if reachable {
		if _, err := c.IngestGeneric(ctx, req); err != nil {
			_, _ = fmt.Fprintf(stderr, "wtc wrap: failed to report completion: %v\n", err)
		}
	}
	return exitCode
}

func hasJSONFlag(args []string) bool {
	for _, a := range args {
		if a == "-json" || a == "--json" {
			return true
		}
	}
	return false
}

// scanTFStream picks the change_summary line out of terraform's -json output.
func scanTFStream(r io.Reader, out *tfSummary) {
	dec := json.NewDecoder(r)
	for {
		var line struct {
			Type    string `json:"type"`
			Changes struct {
				Add    int `json:"add"`
				Change int `json:"change"`
				Remove int `json:"remove"`
			} `json:"changes"`
		}
		if err := dec.Decode(&line); err != nil {
			return
		}
		if line.Type == "change_summary" {
			out.Add, out.Change, out.Remove = line.Changes.Add, line.Changes.Change, line.Changes.Remove
			out.seen = true
		}
	}
}

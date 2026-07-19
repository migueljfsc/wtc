package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/migueljfsc/wtc/internal/config"
	"github.com/migueljfsc/wtc/internal/notify"
)

// newConfigCmd shows the server's effective configuration: which ingest
// paths are wired and with what parameters. Thin client of /api/config —
// secrets arrive as constant "********" masks, never values.
func newConfigCmd(flags *clientFlags) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show the server's effective configuration (secrets masked)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := flags.resolve().Config(cmd.Context())
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(r)
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "server    listen %s", r.Server.Listen)
			if len(r.Server.CORSOrigins) > 0 {
				_, _ = fmt.Fprintf(out, " · cors %s", strings.Join(r.Server.CORSOrigins, ", "))
			}
			if r.Server.CaptureEnabled {
				_, _ = fmt.Fprint(out, " · CAPTURE ON (raw ingest bodies written to disk)")
			}
			_, _ = fmt.Fprintln(out)

			_, _ = fmt.Fprintf(out, "storage   %s", r.Storage.Backend)
			if r.Storage.Host != "" {
				_, _ = fmt.Fprintf(out, " · %s:%d/%s", r.Storage.Host, r.Storage.Port, r.Storage.Database)
			}
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintf(out, "auth      %d api token(s)\n", len(r.Auth.APITokens))
			if r.Metrics.Listen != "" {
				_, _ = fmt.Fprintf(out, "metrics   unauthenticated listener %s (keep in-cluster)\n", r.Metrics.Listen)
			}
			_, _ = fmt.Fprintln(out)

			w := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "SOURCE\tINGEST\tDETAILS")
			gh := r.Sources.GitHub
			_, _ = fmt.Fprintf(w, "github\t%s\t%s\n", githubModes(gh.PollerEnabled, gh.WebhookSecret != ""), githubDetails(gh))
			gl := r.Sources.GitLab
			_, _ = fmt.Fprintf(w, "gitlab\t%s\t%s\n", githubModes(gl.PollerEnabled, gl.WebhookSecret != ""), gitlabDetails(gl))
			_, _ = fmt.Fprintf(w, "flux\t%s\t%s\n", onOff(r.Sources.Flux.HMACKey != "", "webhook"), "suppression "+r.Sources.Flux.SuppressionWindow+scopeDetails(r.Sources.Flux.Scope))
			_, _ = fmt.Fprintf(w, "argocd\t%s\t%s\n", onOff(r.Sources.ArgoCD.WebhookSecret != "", "webhook"), "suppression "+r.Sources.ArgoCD.SuppressionWindow+scopeDetails(r.Sources.ArgoCD.Scope))
			for _, wh := range r.Sources.Webhooks {
				details := "auth " + wh.Auth.Mode
				if wh.Preset != "" {
					details += " · preset " + wh.Preset
				}
				details += fmt.Sprintf(" · %d field template(s)", len(wh.Mapping))
				_, _ = fmt.Fprintf(w, "%s\twebhook\t%s\n", wh.Name, details)
			}
			_ = w.Flush()

			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintf(out, "normalization  %d rule(s)%s · %d tag pattern(s)%s\n",
				len(r.Rules), overridden(r.RulesOverridden),
				len(r.TagPatterns), overridden(r.TagPatternsOverridden))
			if r.Digest.Enabled {
				_, _ = fmt.Fprintf(out, "digest         every %s (window %s) → slack %s\n",
					r.Digest.Interval, r.Digest.Window, r.Digest.SlackWebhook)
			} else {
				_, _ = fmt.Fprintln(out, "digest         off")
			}
			if r.Retention.Enabled {
				_, _ = fmt.Fprintf(out, "retention      keep %s · ephemeral %q keep %s · every %s\n",
					r.Retention.Keep, r.Retention.EphemeralEnvPattern, r.Retention.EphemeralKeep, r.Retention.Interval)
			} else {
				_, _ = fmt.Fprintln(out, "retention      off (nothing is pruned)")
			}
			if len(r.Notifications) == 0 {
				_, _ = fmt.Fprintln(out, "notifications  off")
			} else {
				_, _ = fmt.Fprintf(out, "notifications  %d subscription(s)\n", len(r.Notifications))
				for _, n := range r.Notifications {
					_, _ = fmt.Fprintf(out, "  %s: %s → %s\n", n.Name, matchSummary(n.Match), n.Sink.Type)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}

// onOff renders an ingest-mode cell: the mode name when active, "off" when not.
func onOff(on bool, mode string) string {
	if on {
		return mode
	}
	return "off"
}

// githubModes summarizes which of the two peer ingest modes are active.
func githubModes(poller, webhook bool) string {
	switch {
	case poller && webhook:
		return "poller+webhook"
	case poller:
		return "poller"
	case webhook:
		return "webhook"
	}
	return "off"
}

// scopeDetails appends an ingest-scope summary when a source has an allow/deny
// list configured; empty scope (ingest all) adds nothing.
func scopeDetails(s config.ScopeView) string {
	if len(s.Allow) == 0 && len(s.Deny) == 0 {
		return ""
	}
	return fmt.Sprintf(" · scope %d allow/%d deny", len(s.Allow), len(s.Deny))
}

func githubDetails(gh config.GitHubView) string {
	if !gh.PollerEnabled && gh.WebhookSecret == "" {
		return "-"
	}
	d := "interval " + gh.PollInterval
	if gh.PollerEnabled {
		d += " · backfill " + gh.Backfill
	}
	if gh.PollerEnabled && len(gh.Repos) == 0 {
		d += " · repos auto-discover"
	} else if len(gh.Repos) > 0 {
		d += fmt.Sprintf(" · %d repo(s)", len(gh.Repos))
	}
	return d
}

func gitlabDetails(gl config.GitLabView) string {
	if !gl.PollerEnabled && gl.WebhookSecret == "" {
		return "-"
	}
	d := "interval " + gl.PollInterval
	if gl.PollerEnabled {
		d += " · backfill " + gl.Backfill
	}
	d += fmt.Sprintf(" · %d project(s)", len(gl.Projects))
	if gl.BaseURL != "" {
		d += " · " + gl.BaseURL
	}
	return d
}

// matchSummary renders a notification match as "field=glob" pairs; an empty
// match subscribes to everything.
func matchSummary(m notify.Match) string {
	var parts []string
	for _, p := range []struct{ k, v string }{
		{"env", m.Env}, {"service", m.Service}, {"repo", m.Repo},
		{"kind", m.Kind}, {"status", m.Status},
	} {
		if p.v != "" {
			parts = append(parts, p.k+"="+p.v)
		}
	}
	if len(parts) == 0 {
		return "(all events)"
	}
	return strings.Join(parts, " ")
}

// overridden marks a normalization part that comes from a DB override.
func overridden(fromDB bool) string {
	if fromDB {
		return " (DB override)"
	}
	return ""
}

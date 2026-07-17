package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// initTemplate is the scaffold written by `wtc init` — a minimal working
// config the operator fills in. Kept aligned with SPEC §2.
const initTemplate = `# wtc configuration — see docs/SPEC.md §2
server:
  listen: ":8484"
  db: ./wtc.db

auth:
  api_tokens:
    - ${WTC_API_TOKEN}          # export WTC_API_TOKEN before starting

sources:
  github:
    api_token: ${WTC_GH_API_TOKEN}   # PAT with read on Actions/Contents/PRs
    poll_interval: 60s
    repos: []                        # owner/name entries to poll
    # webhook_secret: ${WTC_GH_WEBHOOK_SECRET}  # only with a public endpoint

# Ordered env/service inference rules — SPEC §3. Unmatched events land with
# env="" and show up in wtc doctor; wtc never guesses.
rules:
  - match: { source: github, event: workflow_run }
    set:   { service: "{{ trimPrefix .Repo \"YOUR-ORG/\" }}" }
`

const initChecklist = `
wrote %s

next steps:
  1. export WTC_API_TOKEN=<random secret>       # CLI/API auth
  2. export WTC_GH_API_TOKEN=<github PAT>       # poller auth (read-only:
     Actions, Contents, Pull requests, Metadata)
  3. edit %s: add repos, adjust rules to your org
  4. wtc serve --config %s
  5. wtc log --since 24h                        # events within one poll interval
  6. wtc doctor                                 # source health + unmapped events

docs: docs/setup/github-poller.md · docs/setup/github-webhook.md
`

func newInitCmd() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a wtc.yaml and print the wiring checklist",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists — refusing to overwrite", path)
			}
			if err := os.WriteFile(path, []byte(initTemplate), 0o600); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), initChecklist, path, path, path)
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "config", "wtc.yaml", "where to write the scaffold")
	return cmd
}

func newDoctorCmd(flags *clientFlags) *cobra.Command {
	var (
		asJSON     bool
		maxSilence time.Duration
	)
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Report ingest source health, unmapped events, and poller state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := flags.resolve().Doctor(cmd.Context())
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(r)
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "events: %d total · db %s", r.TotalEvents, humanBytes(r.DBSizeBytes))
			if r.OldestEvent != nil {
				_, _ = fmt.Fprintf(out, " · oldest %s ago", time.Since(*r.OldestEvent).Round(time.Hour))
			}
			_, _ = fmt.Fprint(out, "\n\n")

			w := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "SOURCE\tLAST EVENT\t24H COUNT")
			var silent []string
			for _, s := range r.Sources {
				age := time.Since(s.LastTS).Round(time.Minute)
				_, _ = fmt.Fprintf(w, "%s\t%s ago\t%d\n", s.Source, age, s.Count24h)
				if maxSilence > 0 && age > maxSilence {
					silent = append(silent, s.Source)
				}
			}
			_ = w.Flush()

			if r.Unmapped24h > 0 {
				_, _ = fmt.Fprintf(out, "\nunmapped events (env=\"\", 24h): %d — fix rules or wait for flux/enrichment\n", r.Unmapped24h)
				for _, t := range r.UnmappedSamples {
					_, _ = fmt.Fprintf(out, "  · %s\n", t)
				}
			}
			if r.ClockSkew24h > 0 {
				_, _ = fmt.Fprintf(out, "\nclock-skew flagged (|ts−ingested|>10m, 24h): %d\n", r.ClockSkew24h)
			}
			if len(r.Poll) > 0 {
				_, _ = fmt.Fprintln(out, "\ngithub poller watermarks:")
				pw := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
				for _, p := range r.Poll {
					_, _ = fmt.Fprintf(pw, "  %s\t%s\t%s\n", p.Repo, p.Resource,
						p.Watermark.Local().Format("2006-01-02 15:04"))
				}
				_ = pw.Flush()
			}
			if len(r.WebhookChurn) > 0 {
				_, _ = fmt.Fprintln(out, "\nwebhook dedup_key churn (rows that should have collapsed — check the dedup_key template):")
				for _, c := range r.WebhookChurn {
					_, _ = fmt.Fprintf(out, "  %s: %d rows in %ds — %q\n", c.Source, c.Rows, c.WindowS, c.Title)
				}
			}
			if len(r.WebhookMappingErrors) > 0 {
				_, _ = fmt.Fprintln(out, "\nwebhook mapping errors (deliveries rejected — template never guessed):")
				for _, e := range r.WebhookMappingErrors {
					_, _ = fmt.Fprintf(out, "  %s: %d error(s), last %s — %s\n",
						e.Source, e.Count, e.LastAt.Local().Format("2006-01-02 15:04"), e.LastError)
				}
			}

			if len(silent) > 0 {
				return fmt.Errorf("source(s) silent longer than %s: %v", maxSilence, silent)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	cmd.Flags().DurationVar(&maxSilence, "max-silence", 0, "exit non-zero if any source's last event is older than this (0 = disabled)")
	return cmd
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

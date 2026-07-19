package main

import (
	"cmp"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/migueljfsc/wtc/internal/model"
)

// "Harden the record" commands: export, backup, explain.

func newExportCmd(flags *clientFlags) *cobra.Command {
	var env, service, repo, kind, status, source, since, until, format, out string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export the change ledger (CSV / NDJSON / JSON) for audit or analysis",
		Long: `Export events as CSV (flat columns, stable order), NDJSON (one full event
per line — includes payload and facts) or a JSON array. Streams from the
server; large ranges never buffer. Ordering is newest first, like wtc log.`,
		Example: `  wtc export --env prod --since 2026-04-01T00:00:00Z --until 2026-07-01T00:00:00Z > q2-prod.csv
  wtc export --format ndjson --since 30d --out changes.ndjson
  wtc export --service api --kind deploy --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := url.Values{"format": {format}}
			set := func(k, v string) {
				if v != "" {
					params.Set(k, v)
				}
			}
			set("env", env)
			set("service", service)
			set("repo", repo)
			set("kind", kind)
			set("status", status)
			set("source", source)
			for flag, v := range map[string]string{"since": since, "until": until} {
				if v == "" {
					continue
				}
				ts, err := parseTimeRef(v, time.Now())
				if err != nil {
					return fmt.Errorf("--%s: %w", flag, err)
				}
				params.Set(flag, model.FormatTS(ts))
			}

			w := cmd.OutOrStdout()
			if out != "" && out != "-" {
				fh, err := os.Create(out)
				if err != nil {
					return err
				}
				defer func() { _ = fh.Close() }()
				w = fh
			}
			n, err := flags.resolve().Download(cmd.Context(), "/api/export?"+params.Encode(), w)
			if err != nil {
				return err
			}
			if out != "" && out != "-" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "exported %d bytes to %s\n", n, out)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&env, "env", "", "filter: env(s), comma-separated")
	cmd.Flags().StringVar(&service, "service", "", "filter: service(s), comma-separated")
	cmd.Flags().StringVar(&repo, "repo", "", "filter: repo(s), comma-separated")
	cmd.Flags().StringVar(&kind, "kind", "", "filter: kind(s), comma-separated")
	cmd.Flags().StringVar(&status, "status", "", "filter: status(es), comma-separated")
	cmd.Flags().StringVar(&source, "source", "", "filter: source(s), comma-separated")
	cmd.Flags().StringVar(&since, "since", "", "window start: 2h, 7d, 1w, or RFC3339 (default: all)")
	cmd.Flags().StringVar(&until, "until", "", "window end: 2h, 7d, 1w, or RFC3339 (default: now)")
	cmd.Flags().StringVar(&format, "format", "csv", "csv, ndjson or json")
	cmd.Flags().StringVar(&out, "out", "", "write to this file instead of stdout")
	return cmd
}

func newBackupCmd(flags *clientFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup <path>",
		Short: "Save a consistent snapshot of the ledger (sqlite backend)",
		Long: `Download a point-in-time snapshot of the server's sqlite database, taken
with VACUUM INTO while it keeps serving (WAL-safe, comes out compacted).
The write is atomic: a temp file next to <path>, renamed on success.

Postgres deployments: back up with pg_dump instead — see docs/setup/backup.md.`,
		Example: `  wtc backup ./wtc-$(date +%F).db`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest := args[0]
			tmp, err := os.CreateTemp(filepath.Dir(dest), ".wtc-backup-*")
			if err != nil {
				return err
			}
			defer func() { _ = os.Remove(tmp.Name()) }() // no-op after the rename

			n, err := flags.resolve().Download(cmd.Context(), "/api/backup", tmp)
			if cerr := tmp.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
			if err := os.Rename(tmp.Name(), dest); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "backup written: %s (%d bytes)\n", dest, n)
			return nil
		},
	}
	return cmd
}

func newExplainCmd(flags *clientFlags) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "explain <event-id>",
		Short: "Show which rule set each inferred field of an event",
		Long: `Replay the rules engine over an event's recorded ingest-time facts and
report, per field, which rule set it (index + match spec), that the source's
normalizer had already filled it, or that nothing matched. Runs the CURRENT
rules — after a rules edit the trace may differ from the stored row, and the
output says so. Events ingested before the facts migration (or via
generic/record/wrap, which set fields directly) report "facts not recorded".`,
		Example: `  wtc explain 01KXGA0ZP6QQ9M129XYAR1KTSY   # id from wtc log --json`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := flags.resolve().Explain(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if asJSON {
				return jsonOut(cmd, r)
			}
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "%s  [%s]  %s\n", r.Title, r.Source, r.EventID)
			if r.Recorded {
				w := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
				_, _ = fmt.Fprintln(w, "  FIELD\tVALUE\tORIGIN")
				for _, t := range r.Traces {
					origin := t.Origin
					if t.Origin == "rule" && t.RuleIndex != nil {
						origin = fmt.Sprintf("rule %d: %s", *t.RuleIndex, t.RuleMatch)
					}
					_, _ = fmt.Fprintf(w, "  %s\t%s\t%s\n", t.Field, cmp.Or(t.Value, "-"), origin)
				}
				if err := w.Flush(); err != nil {
					return err
				}
				if f := r.Facts; f != nil {
					var facts []string
					for _, kv := range [][2]string{
						{"source", f.Source}, {"repo", f.Repo}, {"branch", f.Branch},
						{"workflow", f.Workflow}, {"cluster", f.Cluster},
						{"namespace", f.Namespace}, {"object", f.ObjectName},
					} {
						if kv[1] != "" {
							facts = append(facts, kv[0]+"="+kv[1])
						}
					}
					if len(facts) > 0 {
						_, _ = fmt.Fprintf(out, "  facts: %s\n", strings.Join(facts, " "))
					}
				}
			}
			for _, n := range r.Notes {
				_, _ = fmt.Fprintf(out, "  note: %s\n", n)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

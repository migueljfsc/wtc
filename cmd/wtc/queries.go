package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/notify"
	"github.com/migueljfsc/wtc/internal/query"
)

func jsonOut(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func newWhereCmd(flags *clientFlags) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "where <sha|tag>",
		Short: "Trace a change: build → intent (merge/push) → applied per env",
		Args:  cobra.ExactArgs(1),
		Example: `  wtc where 4f2a91c
  wtc where ghcr.io/org/app:sha-4f2a91c --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := flags.resolve().Where(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if asJSON {
				return jsonOut(cmd, r)
			}
			out := cmd.OutOrStdout()

			_, _ = fmt.Fprintf(out, "%s\n", r.Sha)
			for _, b := range r.Builds {
				_, _ = fmt.Fprintf(out, "  BUILD    %s  %s  %s  %s\n",
					b.TS.Local().Format("2006-01-02 15:04"), b.Status, cmp.Or(b.Service, "-"), b.Title)
			}
			if len(r.Builds) == 0 {
				_, _ = fmt.Fprintln(out, "  BUILD    (none recorded)")
			}
			for _, e := range r.Envs {
				_, _ = fmt.Fprintf(out, "  ENV %s\n", cmp.Or(e.Env, "(unmapped)"))
				if e.Intent != nil {
					_, _ = fmt.Fprintf(out, "    intent   %s  %s  %s\n",
						e.Intent.TS.Local().Format("2006-01-02 15:04"), e.Intent.Kind, e.Intent.Title)
				}
				if e.Applied != nil {
					lag := ""
					if e.LagMS != nil {
						lag = fmt.Sprintf("  (lag %s)", (time.Duration(*e.LagMS) * time.Millisecond).Round(time.Second))
					}
					_, _ = fmt.Fprintf(out, "    applied  %s  %s  %s%s\n",
						e.Applied.TS.Local().Format("2006-01-02 15:04"), e.Applied.Status, e.Applied.Title, lag)
				}
				for _, u := range e.Unknown {
					_, _ = fmt.Fprintf(out, "    unknown  %s\n", u)
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

func newDoraCmd(flags *clientFlags) *cobra.Command {
	var asJSON bool
	var since, until, window string
	cmd := &cobra.Command{
		Use:   "dora",
		Short: "Deploy-quality metrics: change-failure rate and MTTR, by env and team",
		Long: `Change-failure rate (deploys that failed or were followed by an alert or
rollback in the same env within --window) and MTTR (mean alert firing→resolved),
overall and grouped by env and owning team. Deploy frequency lives in the
dashboard; lead time is not yet computed.`,
		Example: "  wtc dora --since 30d\n  wtc dora --since 90d --window 2h --json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			now := time.Now()
			sinceTS, err := parseTimeRef(since, now)
			if err != nil {
				return fmt.Errorf("--since: %w", err)
			}
			params := url.Values{"since": {model.FormatTS(sinceTS)}}
			if until != "" {
				u, err := parseTimeRef(until, now)
				if err != nil {
					return fmt.Errorf("--until: %w", err)
				}
				params.Set("until", model.FormatTS(u))
			}
			if window != "" {
				params.Set("window", window)
			}

			r, err := flags.resolve().DORA(cmd.Context(), params)
			if err != nil {
				return err
			}
			if asJSON {
				return jsonOut(cmd, r)
			}

			out := cmd.OutOrStdout()
			win := (time.Duration(r.WindowSeconds) * time.Second).String()
			_, _ = fmt.Fprintf(out, "%s → %s   (failure window %s)\n\n",
				r.Since.Local().Format("2006-01-02"), r.Until.Local().Format("2006-01-02"), win)
			_, _ = fmt.Fprintf(out, "overall: %d deploys · %s change-failure rate · %s MTTR (%d incidents)\n",
				r.Overall.Deploys, doraPct(r.Overall.ChangeFailureRate), doraMTTR(r.Overall.MTTRSeconds), r.Overall.Incidents)

			table := func(header string, groups []query.DORAGroup) {
				if len(groups) == 0 {
					return
				}
				_, _ = fmt.Fprintf(out, "\n%s\n", header)
				w := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
				_, _ = fmt.Fprintln(w, "\tDEPLOYS\tCFR\tMTTR\tINCIDENTS")
				for _, g := range groups {
					_, _ = fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%d\n",
						g.Key, g.Deploys, doraPct(g.ChangeFailureRate), doraMTTR(g.MTTRSeconds), g.Incidents)
				}
				_ = w.Flush()
			}
			table("BY ENV", r.ByEnv)
			table("BY TEAM", r.ByOwner)
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "30d", "window start: 2h, 30d, or RFC3339")
	cmd.Flags().StringVar(&until, "until", "", "window end: duration ago or RFC3339 (default now)")
	cmd.Flags().StringVar(&window, "window", "", "deploy→failure attribution span, e.g. 60m (default 1h)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func doraPct(f float64) string { return fmt.Sprintf("%.1f%%", f*100) }

func doraMTTR(secs *float64) string {
	if secs == nil {
		return "-"
	}
	return (time.Duration(*secs) * time.Second).Round(time.Second).String()
}

func newDiffCmd(flags *clientFlags) *cobra.Command {
	var asJSON bool
	var atStr string
	cmd := &cobra.Command{
		Use:   "diff <envA> <envB>",
		Short: "Compare what is running in two environments",
		Args:  cobra.ExactArgs(2),
		Example: "  wtc diff staging prod\n" +
			"  wtc diff staging prod --at 24h        # what differed a day ago\n" +
			"  wtc diff staging prod --at 2026-07-01T00:00:00Z",
		RunE: func(cmd *cobra.Command, args []string) error {
			var at time.Time
			if atStr != "" {
				var err error
				if at, err = parseTimeRef(atStr, time.Now()); err != nil {
					return fmt.Errorf("--at: %w", err)
				}
			}
			r, err := flags.resolve().Diff(cmd.Context(), args[0], args[1], at)
			if err != nil {
				return err
			}
			if asJSON {
				return jsonOut(cmd, r)
			}
			out := cmd.OutOrStdout()
			if !at.IsZero() {
				_, _ = fmt.Fprintf(out, "as of %s\n", at.Local().Format(time.RFC3339))
			}
			w := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
			_, _ = fmt.Fprintf(w, "SERVICE\t%s\t%s\tSTATE\tDRIFT\tLAST ACTOR\n", r.EnvA, r.EnvB)
			revisionOnly := false
			for _, row := range r.Rows {
				state := "in sync"
				switch {
				case row.OnlyIn != "":
					state = "only in " + row.OnlyIn
				case !row.InSync:
					state = "DRIFT"
				}
				drift := "-"
				if row.DriftSeconds != nil {
					drift = (time.Duration(*row.DriftSeconds) * time.Second).Round(time.Minute).String()
				}
				mark := ""
				if row.RevisionOnly {
					mark = "*"
					revisionOnly = true
				}
				_, _ = fmt.Fprintf(w, "%s%s\t%s\t%s\t%s\t%s\t%s\n",
					row.Service, mark, cmp.Or(row.A, "-"), cmp.Or(row.B, "-"), state, drift, cmp.Or(row.LastActor, "-"))
			}
			if err := w.Flush(); err != nil {
				return err
			}
			if revisionOnly {
				_, _ = fmt.Fprintln(out, "* revision-only comparison — deploy events carry no artifact data")
			}
			if len(r.Rows) == 0 {
				_, _ = fmt.Fprintf(out, "no services with successful deploys in %s or %s\n", r.EnvA, r.EnvB)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	cmd.Flags().StringVar(&atStr, "at", "", "point-in-time: reconstruct state as of 2h, 7d, or RFC3339")
	return cmd
}

var ulidLike = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

func newAroundCmd(flags *clientFlags) *cobra.Command {
	var window time.Duration
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "around <ts|event-id>",
		Short: "What changed in the window before an instant (or an alert event)",
		Args:  cobra.ExactArgs(1),
		Example: `  wtc around 2026-07-14T13:41:34Z --window 30m
  wtc around 01KXGA0ZP6QQ9M129XYAR1KTSY     # an alert's event id`,
		RunE: func(cmd *cobra.Command, args []string) error {
			params := url.Values{"window": {window.String()}}
			if ulidLike.MatchString(args[0]) {
				params.Set("id", args[0])
			} else {
				ts, err := parseTimeRef(args[0], time.Now())
				if err != nil {
					return fmt.Errorf("anchor: %w", err)
				}
				params.Set("ts", model.FormatTS(ts))
			}
			resp, err := flags.resolve().Around(cmd.Context(), params)
			if err != nil {
				return err
			}
			if asJSON {
				return jsonOut(cmd, resp)
			}
			if len(resp.Events) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no changes in the window")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "TIME\tENV\tKIND\tSTATUS\tSERVICE\tTITLE")
			for _, ev := range resp.Events {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					ev.TS.Local().Format("15:04:05"),
					cmp.Or(ev.Env, "-"), ev.Kind, ev.Status, cmp.Or(ev.Service, "-"), ev.Title)
			}
			return w.Flush()
		},
	}
	cmd.Flags().DurationVar(&window, "window", 30*time.Minute, "how far back to look")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func newBlastCmd(flags *clientFlags) *cobra.Command {
	var window time.Duration
	var env, service string
	var limit int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "blast <ts|event-id>",
		Short: "Rank the changes most likely to have caused an alert",
		Long: `Rank the changes most likely to have caused an alert ("blast radius").

Anchor on an alert's event id (or a bare instant) to score the changes in the
preceding window as suspects. Anchor on a deploy (any non-alert change) and
the direction flips: it lists the alerts that fired after it — "did my change
cause noise?".

Scoring is a fixed, documented heuristic — deterministic, never ML:
  recency        0–30  linear within the window (closer to the anchor = higher)
  same env       +30   the hard signal (disabled when the anchor has no env)
  same service   +20   booster only — alerts often lack a clean service
  kind           +15   deploy / rollback / config_change
                 +12   infra_change   +10 manual   +5 merge/push   +2 build
  failed state   +10   a failed/degraded change right before the alert`,
		Args: cobra.ExactArgs(1),
		Example: `  wtc blast 01KXGA0ZP6QQ9M129XYAR1KTSY          # an alert's event id
  wtc blast 2026-07-18T12:00:00Z --env prod   # a bare instant needs --env
  wtc blast <deploy-event-id> --window 1h     # alerts after my deploy`,
		RunE: func(cmd *cobra.Command, args []string) error {
			params := url.Values{"window": {window.String()}}
			if ulidLike.MatchString(args[0]) {
				params.Set("id", args[0])
			} else {
				ts, err := parseTimeRef(args[0], time.Now())
				if err != nil {
					return fmt.Errorf("anchor: %w", err)
				}
				params.Set("ts", model.FormatTS(ts))
			}
			if env != "" {
				params.Set("env", env)
			}
			if service != "" {
				params.Set("service", service)
			}
			if limit > 0 {
				params.Set("limit", fmt.Sprint(limit))
			}
			r, err := flags.resolve().Blast(cmd.Context(), params)
			if err != nil {
				return err
			}
			if asJSON {
				return jsonOut(cmd, r)
			}
			out := cmd.OutOrStdout()

			what := "Likely causes"
			if r.Direction == "effects" {
				what = "Alerts after"
			}
			anchor := r.AnchorTS.Local().Format("2006-01-02 15:04:05")
			if r.Anchor != nil {
				anchor = fmt.Sprintf("%s (%s)", r.Anchor.Title, anchor)
			}
			_, _ = fmt.Fprintf(out, "%s %s — window %s\n", what, anchor, time.Duration(r.WindowMS)*time.Millisecond)

			if len(r.Suspects) > 0 {
				w := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
				_, _ = fmt.Fprintln(w, "SCORE\tTIME\tENV\tKIND\tSTATUS\tSERVICE\tTITLE\tWHY")
				for _, sp := range r.Suspects {
					e := sp.Event
					_, _ = fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
						sp.Score, e.TS.Local().Format("15:04:05"),
						cmp.Or(e.Env, "-"), e.Kind, e.Status, cmp.Or(e.Service, "-"),
						e.Title, strings.Join(sp.Reasons, " · "))
				}
				if err := w.Flush(); err != nil {
					return err
				}
			}
			for _, n := range r.Notes {
				_, _ = fmt.Fprintf(out, "  note: %s\n", n)
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&window, "window", 2*time.Hour, "how far to look (back for causes, forward for effects)")
	cmd.Flags().StringVar(&env, "env", "", "env scoring context (overrides the anchor event's)")
	cmd.Flags().StringVar(&service, "service", "", "service scoring context (overrides the anchor event's)")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum suspects (default 20, capped at 100)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func newHandoffCmd(flags *clientFlags) *cobra.Command {
	var since, slackWebhook string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "handoff",
		Short: "Digest of everything that changed in a window (markdown)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ts, err := parseTimeRef(since, time.Now())
			if err != nil {
				return fmt.Errorf("--since: %w", err)
			}
			r, err := flags.resolve().Handoff(cmd.Context(), model.FormatTS(ts))
			if err != nil {
				return err
			}
			switch {
			case slackWebhook != "":
				if err := notify.Slack(cmd.Context(), slackWebhook, r.SlackText(time.Now())); err != nil {
					return err
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "digest posted to slack")
			case asJSON:
				return jsonOut(cmd, r)
			default:
				_, _ = fmt.Fprint(cmd.OutOrStdout(), (&r).Markdown(time.Now()))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "7d", "window: 2h, 7d, 1w, or RFC3339")
	cmd.Flags().StringVar(&slackWebhook, "slack-webhook", "", "post the digest to this Slack incoming-webhook URL instead of printing")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

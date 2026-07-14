package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/migueljfsc/wtc/internal/model"
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

func newDiffCmd(flags *clientFlags) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "diff <envA> <envB>",
		Short:   "Compare what is running in two environments",
		Args:    cobra.ExactArgs(2),
		Example: "  wtc diff staging prod",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := flags.resolve().Diff(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			if asJSON {
				return jsonOut(cmd, r)
			}
			out := cmd.OutOrStdout()
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

func newHandoffCmd(flags *clientFlags) *cobra.Command {
	var since string
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
			if asJSON {
				return jsonOut(cmd, r)
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), (&r).Markdown(time.Now()))
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "7d", "window: 2h, 7d, 1w, or RFC3339")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

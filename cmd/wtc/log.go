package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/migueljfsc/wtc/internal/model"
)

func newLogCmd(flags *clientFlags) *cobra.Command {
	var (
		env, service, kind, status string
		since, until               string
		limit                      int
		asJSON                     bool
	)

	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show the change timeline, newest first",
		Example: `  wtc log --env prod --since 2h
  wtc log --service api --since 7d --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := url.Values{}
			set := func(k, v string) {
				if v != "" {
					params.Set(k, v)
				}
			}
			set("env", env)
			set("service", service)
			set("kind", kind)
			set("status", status)
			params.Set("limit", strconv.Itoa(limit))

			sinceTS, err := parseTimeRef(since, time.Now())
			if err != nil {
				return fmt.Errorf("--since: %w", err)
			}
			params.Set("since", model.FormatTS(sinceTS))

			if until != "" {
				untilTS, err := parseTimeRef(until, time.Now())
				if err != nil {
					return fmt.Errorf("--until: %w", err)
				}
				params.Set("until", model.FormatTS(untilTS))
			}

			resp, err := flags.resolve().Events(cmd.Context(), params)
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(resp.Events)
			}

			if len(resp.Events) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no events")
				return nil
			}
			// Write errors surface at Flush; per-row errors are redundant.
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "TIME\tENV\tKIND\tSTATUS\tSERVICE\tTITLE\tACTOR")
			for _, ev := range resp.Events {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					ev.TS.Local().Format("2006-01-02 15:04"),
					orDash(ev.Env), ev.Kind, ev.Status,
					orDash(ev.Service), ev.Title, orDash(ev.Actor),
				)
			}
			if err := w.Flush(); err != nil {
				return err
			}
			if resp.NextCursor != "" {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "… more events; raise --limit or narrow the window")
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&env, "env", "", "filter by environment")
	f.StringVar(&service, "service", "", "filter by service")
	f.StringVar(&kind, "kind", "", "filter by kind")
	f.StringVar(&status, "status", "", "filter by status")
	f.StringVar(&since, "since", "24h", "how far back: 2h, 7d, 1w, or RFC3339")
	f.StringVar(&until, "until", "", "upper bound: duration ago or RFC3339")
	f.IntVar(&limit, "limit", 100, "max events")
	f.BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

var relPattern = regexp.MustCompile(`^(\d+)([smhdw])$`)

// parseTimeRef accepts a relative age ("2h", "30m", "7d", "1w") or an
// absolute RFC3339 timestamp and returns the corresponding instant.
func parseTimeRef(s string, now time.Time) (time.Time, error) {
	if m := relPattern.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return time.Time{}, err
		}
		var unit time.Duration
		switch m[2] {
		case "s":
			unit = time.Second
		case "m":
			unit = time.Minute
		case "h":
			unit = time.Hour
		case "d":
			unit = 24 * time.Hour
		case "w":
			unit = 7 * 24 * time.Hour
		}
		return now.Add(-time.Duration(n) * unit), nil
	}
	return model.ParseTS(s)
}

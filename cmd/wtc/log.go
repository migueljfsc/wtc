package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
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
		env, service, repo, owner, kind, status string
		since, until, query                     string
		limit                                   int
		asJSON                                  bool
	)

	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show the change timeline, newest first",
		Example: `  wtc log --env prod --since 2h
  wtc log --service api --since 7d --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			now := time.Now()

			sinceTS, err := parseTimeRef(since, now)
			if err != nil {
				return fmt.Errorf("--since: %w", err)
			}

			var untilTS time.Time
			if until != "" {
				untilTS, err = parseTimeRef(until, now)
				if err != nil {
					return fmt.Errorf("--until: %w", err)
				}
				if untilTS.Before(sinceTS) {
					return fmt.Errorf("empty window: --until (%s) is older than --since (%s, default 24h) — set --since explicitly",
						untilTS.Local().Format(time.RFC3339), sinceTS.Local().Format(time.RFC3339))
				}
			}

			params := url.Values{}
			set := func(k, v string) {
				if v != "" {
					params.Set(k, v)
				}
			}
			set("env", env)
			set("service", service)
			set("repo", repo)
			set("owner", owner)
			set("kind", kind)
			set("status", status)
			set("q", query)
			params.Set("limit", strconv.Itoa(limit))
			params.Set("since", model.FormatTS(sinceTS))
			if !untilTS.IsZero() {
				params.Set("until", model.FormatTS(untilTS))
			}

			resp, err := flags.resolve().Events(cmd.Context(), params)
			if err != nil {
				return err
			}

			if asJSON {
				// Full response object: scripted consumers need next_cursor
				// to detect truncation and paginate.
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
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
					cmp.Or(ev.Env, "-"), ev.Kind, ev.Status,
					cmp.Or(ev.Service, "-"), ev.Title, cmp.Or(ev.Actor, "-"),
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
	f.StringVar(&repo, "repo", "", "filter by source repo (owner/name)")
	f.StringVar(&owner, "owner", "", "filter by owning team")
	f.StringVar(&kind, "kind", "", "filter by kind")
	f.StringVar(&status, "status", "", "filter by status")
	f.StringVarP(&query, "query", "q", "", "full-text search over title/service/actor/artifact")
	f.StringVar(&since, "since", "24h", "how far back: 2h, 7d, 1w, or RFC3339")
	f.StringVar(&until, "until", "", "upper bound: duration ago or RFC3339")
	f.IntVar(&limit, "limit", 100, "max events")
	f.BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
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
		// Guard int64 overflow: a huge value would silently wrap into a
		// nonsense instant instead of erroring.
		if int64(n) > math.MaxInt64/int64(unit) {
			return time.Time{}, fmt.Errorf("duration %q is too large", s)
		}
		return now.Add(-time.Duration(n) * unit), nil
	}
	return model.ParseTS(s)
}

package main

import (
	"fmt"
	"hash/fnv"
	"time"

	"github.com/spf13/cobra"

	"github.com/migueljfsc/wtc/internal/ingest/generic"
	"github.com/migueljfsc/wtc/internal/model"
)

// newDemoCmd seeds a synthetic week of change events so log/where/diff and the
// UI can be tried without wiring any real source. Events post through the API
// (the CLI never opens the DB) via /ingest/generic, so the daemon must be up.
func newDemoCmd(flags *clientFlags) *cobra.Command {
	var days int

	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Seed a synthetic week of change events (posts to /ingest/generic)",
		Long: `Seed a self-contained synthetic history — builds, deploys promoted
dev → staging → prod with realistic lag, a few build failures, ephemeral
pr-* envs, a manual change and an alert — so you can explore log, where,
diff, around and the UI without wiring GitHub or Flux.

Each run posts a fresh now-anchored window (unique dedup keys), so calling
it repeatedly stacks more history rather than updating in place.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if days < 1 {
				return fmt.Errorf("--days must be >= 1")
			}
			c := flags.resolve()
			reqs, exampleSHA := buildDemoEvents(time.Now().UTC(), days, model.NewID())

			var deduped int
			for _, r := range reqs {
				resp, err := c.IngestGeneric(cmd.Context(), r)
				if err != nil {
					return fmt.Errorf("seed %q: %w", r.DedupKey, err)
				}
				if resp.Deduped {
					deduped++
				}
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "seeded %d events over %d days", len(reqs), days)
			if deduped > 0 {
				_, _ = fmt.Fprintf(out, " (%d updated existing)", deduped)
			}
			_, _ = fmt.Fprintf(out, "\n\nTry:\n")
			_, _ = fmt.Fprintf(out, "  wtc log --since %dh\n", days*24)
			_, _ = fmt.Fprintf(out, "  wtc diff staging prod\n")
			_, _ = fmt.Fprintf(out, "  wtc where %s\n", exampleSHA[:7])
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 7, "span of synthetic history in days")
	return cmd
}

// demoServices are the fictional microservices the seed data revolves around.
var demoServices = []string{"api", "web", "worker"}

// demoActors rotate as the humans opening staging/prod promotion PRs.
var demoActors = []string{"alice", "bob", "carol"}

// buildDemoEvents returns a deterministic (given now/days/run) set of generic
// ingest requests spanning the last `days`, plus one example sha that reached
// prod (handy for a `wtc where` hint). run namespaces the dedup keys so repeat
// invocations accumulate instead of colliding.
//
// Sources are limited to those /ingest/generic accepts (generic, helm, manual);
// github/flux/alertmanager are reserved for their own ingest paths.
func buildDemoEvents(now time.Time, days int, run string) ([]generic.Request, string) {
	var reqs []generic.Request
	var exampleSHA string

	// emit appends a deploy only if it is already in the past — the newest
	// release's later stages (prod) fall in the future and are naturally
	// skipped, giving `wtc diff staging prod` real drift to show.
	emit := func(svc, env, sha, short, artifact string, ts time.Time, actor string) bool {
		if ts.After(now) {
			return false
		}
		reqs = append(reqs, demoDeploy(run, svc, env, sha, short, artifact, ts, actor))
		return true
	}

	for si, svc := range demoServices {
		for r := 0; r < days; r++ {
			ageDays := days - 1 - r // r == days-1 → newest release
			// Build ~6h before its release slot so the newest release's build
			// and early deploys are already in the past; stagger services so a
			// day's releases don't share a timestamp.
			bt := now.
				Add(-time.Duration(ageDays) * 24 * time.Hour).
				Add(-6 * time.Hour).
				Add(-time.Duration(si) * 97 * time.Minute)
			sha := demoSHA(run, svc, r)
			short := sha[:7]
			artifact := fmt.Sprintf("ghcr.io/acme/%s:sha-%s", svc, short)

			// Every 7th build fails — but never the newest release, so the
			// tip of each service always has a clean pipeline to inspect.
			failed := (si+r)%7 == 6 && ageDays != 0

			reqs = append(reqs, demoReq(run, "build", svc, bt, generic.Request{
				Source:   string(model.SourceGeneric),
				Kind:     string(model.KindBuild),
				Status:   statusIf(failed),
				Service:  svc,
				Actor:    "ci-bot",
				Ref:      sha,
				Artifact: artifact,
				Title:    fmt.Sprintf("build %s (sha-%s)", svc, short),
				URL:      fmt.Sprintf("https://ci.acme.dev/%s/runs/%d", svc, r+1),
			}))
			if failed {
				continue // failed build never promotes
			}

			// dev auto-reconciles minutes after the build; staging a few hours
			// later (human merge); prod lags ~a day.
			emit(svc, "dev", sha, short, artifact, bt.Add(8*time.Minute), "flux-bot")
			emit(svc, "staging", sha, short, artifact, bt.Add(4*time.Hour), demoActors[r%len(demoActors)])
			if emit(svc, "prod", sha, short, artifact, bt.Add(20*time.Hour), demoActors[(r+1)%len(demoActors)]) && svc == "api" {
				exampleSHA = sha // most-recent api release that reached prod
			}
		}
	}

	// Two ephemeral PR environments in the last couple of days.
	reqs = append(reqs,
		demoDeploy(run, "api", "pr-501", demoSHA(run, "api-pr", 501), demoSHA(run, "api-pr", 501)[:7],
			"ghcr.io/acme/api:pr-501", now.Add(-30*time.Hour), "alice"),
		demoDeploy(run, "web", "pr-502", demoSHA(run, "web-pr", 502), demoSHA(run, "web-pr", 502)[:7],
			"ghcr.io/acme/web:pr-502", now.Add(-6*time.Hour), "bob"),
	)

	// A hand-recorded change and a config bump — not everything flows through CI.
	reqs = append(reqs,
		demoReq(run, "manual", "prod", now.Add(-46*time.Hour), generic.Request{
			Source: string(model.SourceManual),
			Kind:   string(model.KindManual),
			Status: string(model.StatusSucceeded),
			Env:    "prod",
			Actor:  "carol",
			Title:  "rotated production database credentials",
		}),
		demoReq(run, "config", "prod", now.Add(-70*time.Hour), generic.Request{
			Source:  string(model.SourceGeneric),
			Kind:    string(model.KindConfigChange),
			Status:  string(model.StatusSucceeded),
			Env:     "prod",
			Service: "api",
			Actor:   "alice",
			Title:   "raise api memory limit 512Mi → 768Mi",
		}),
	)

	// An alert firing ~30m after an api prod deploy, for `wtc around` to correlate.
	if exampleSHA != "" {
		reqs = append(reqs, demoReq(run, "alert", "prod", now.Add(-24*time.Hour+30*time.Minute), generic.Request{
			Source:  string(model.SourceGeneric),
			Kind:    string(model.KindAlert),
			Status:  string(model.StatusStarted),
			Env:     "prod",
			Service: "api",
			Title:   "HighErrorRate firing on api (prod)",
			URL:     "https://alerts.acme.dev/HighErrorRate",
		}))
	} else {
		// days==1: no release reached prod. Point the hint at the newest build.
		exampleSHA = demoSHA(run, "api", days-1)
	}

	return reqs, exampleSHA
}

// demoDeploy builds a succeeded deploy request for a service into an env.
func demoDeploy(run, svc, env, sha, short, artifact string, ts time.Time, actor string) generic.Request {
	return demoReq(run, "deploy-"+env, svc, ts, generic.Request{
		Source:   string(model.SourceHelm),
		Kind:     string(model.KindDeploy),
		Status:   string(model.StatusSucceeded),
		Env:      env,
		Cluster:  demoCluster(env),
		Service:  svc,
		Actor:    actor,
		Ref:      sha,
		Artifact: artifact,
		Title:    fmt.Sprintf("deploy %s to %s (sha-%s)", svc, env, short),
	})
}

// demoReq stamps the timestamp and a stable, run-namespaced dedup key onto a
// partially-filled request. tag is a short discriminator (e.g. "deploy-prod").
func demoReq(run, tag, svc string, ts time.Time, r generic.Request) generic.Request {
	r.TS = model.FormatTS(ts)
	r.DedupKey = fmt.Sprintf("demo:%s:%s:%s:%d", run, tag, svc, ts.UnixNano())
	return r
}

// demoCluster maps env → cluster (cluster-per-env; ephemeral pr-* land on dev).
func demoCluster(env string) string {
	switch env {
	case "dev", "staging", "prod":
		return env
	default:
		return "dev"
	}
}

func statusIf(failed bool) string {
	if failed {
		return string(model.StatusFailed)
	}
	return string(model.StatusSucceeded)
}

// demoSHA derives a deterministic 40-hex pseudo-sha from the run nonce and a
// per-release seed, so a release's build and deploys share one ref (the join
// `wtc where` walks) without pulling in a crypto dependency.
func demoSHA(run, svc string, n int) string {
	var b []byte
	for i := 0; len(b) < 20; i++ {
		h := fnv.New64a()
		_, _ = fmt.Fprintf(h, "%s|%s|%d|%d", run, svc, n, i)
		b = h.Sum(b)
	}
	return fmt.Sprintf("%x", b[:20])
}

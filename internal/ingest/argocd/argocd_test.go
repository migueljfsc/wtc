package argocd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
)

const fixtureDir = "../../../testdata/argocd"

func loadNotification(t *testing.T, name string) *Notification {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	n, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	return n
}

var testNow = time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)

const fixtureSha = "8088f4c0d970abb09e250248cc97e35623447cb5"

func TestGoldenSyncSucceeded(t *testing.T) {
	ev, facts, suppress := Normalize(loadNotification(t, "sync_succeeded.json"), testNow)
	if err := ev.Validate(); err != nil {
		t.Fatalf("invalid: %v", err)
	}
	if ev.Source != model.SourceArgoCD || ev.Kind != model.KindDeploy || ev.Status != model.StatusSucceeded {
		t.Errorf("source/kind/status = %s/%s/%s", ev.Source, ev.Kind, ev.Status)
	}
	if ev.Ref != fixtureSha {
		t.Errorf("ref = %q — must be the bare sync revision for the where join", ev.Ref)
	}
	if ev.DedupKey != "argocd:wtc-guestbook-labeled:"+fixtureSha+":2026-07-16T10:21:16Z" {
		t.Errorf("dedup = %q — one row per sync OPERATION (app:revision:startedAt), no phase", ev.DedupKey)
	}
	if suppress != ev.DedupKey+":Succeeded" {
		t.Errorf("suppress key = %q — must append the phase discriminator", suppress)
	}
	if ev.Title != "Application wtc-guestbook-labeled: sync Succeeded" {
		t.Errorf("title = %q", ev.Title)
	}
	if ev.Namespace != "default" || ev.Actor != "argocd" {
		t.Errorf("ns/actor = %q/%q", ev.Namespace, ev.Actor)
	}
	if ev.Cluster != "" {
		t.Errorf("cluster = %q — destServer is a URL, never an env/cluster", ev.Cluster)
	}
	if want := time.Date(2026, 7, 16, 10, 21, 16, 0, time.UTC); !ev.TS.Equal(want) {
		t.Errorf("ts = %v, want finishedAt %v", ev.TS, want)
	}
	if ev.DurationMS == nil {
		t.Error("terminal sync with both timestamps must carry duration_ms")
	}
	if !strings.Contains(ev.Payload, `"healthStatus":"Healthy"`) {
		t.Errorf("payload must carry health: %q", ev.Payload)
	}
	if facts.Source != "argocd" || facts.ObjectKind != "Application" ||
		facts.ObjectName != "wtc-guestbook-labeled" || facts.EnvLabel != "staging" ||
		facts.Namespace != "default" || facts.Project != "default" ||
		facts.SourcePath != "guestbook" {
		t.Errorf("facts = %+v", facts)
	}
}

func TestGoldenSyncRunning(t *testing.T) {
	ev, _, suppress := Normalize(loadNotification(t, "sync_running.json"), testNow)
	if ev.Status != model.StatusStarted {
		t.Errorf("status = %s, want started (phase Running)", ev.Status)
	}
	// Captured on a LATER operation (startedAt 10:22:05) than sync_succeeded
	// (10:21:16): distinct operations get distinct rows — a resync/retry of
	// the same revision is a new logical change for the ledger.
	if ev.DedupKey != "argocd:wtc-guestbook-labeled:"+fixtureSha+":2026-07-16T10:22:05Z" {
		t.Errorf("dedup = %q", ev.DedupKey)
	}
	if suppress != ev.DedupKey+":Running" {
		t.Errorf("suppress key = %q", suppress)
	}
	if want := time.Date(2026, 7, 16, 10, 22, 5, 0, time.UTC); !ev.TS.Equal(want) {
		t.Errorf("ts = %v, want startedAt %v (finishedAt is null mid-sync)", ev.TS, want)
	}
	if ev.DurationMS != nil {
		t.Error("running sync must not carry duration_ms")
	}
}

func TestGoldenSyncFailed(t *testing.T) {
	ev, _, suppress := Normalize(loadNotification(t, "sync_failed.json"), testNow)
	if ev.Status != model.StatusFailed {
		t.Errorf("status = %s, want failed — Argo reports phase Error for a sync that never applied", ev.Status)
	}
	if ev.Ref != "" {
		t.Errorf("ref = %q — unresolved revision %q must not enter the where join", ev.Ref, "HEAD")
	}
	// The per-operation key handles unresolved revisions for free: distinct
	// failed operations reporting "HEAD" don't collapse onto one stale row.
	if ev.DedupKey != "argocd:wtc-guestbook-ns:HEAD:2026-07-16T10:18:06Z" {
		t.Errorf("dedup = %q", ev.DedupKey)
	}
	if !strings.HasSuffix(suppress, ":Error") {
		t.Errorf("suppress key = %q must end in the phase", suppress)
	}
}

func TestGoldenHealthDegraded(t *testing.T) {
	ev, facts, suppress := Normalize(loadNotification(t, "health_degraded.json"), testNow)
	if ev.Status != model.StatusDegraded {
		t.Errorf("status = %s, want degraded — healthStatus wins over the (stale) operationPhase", ev.Status)
	}
	// The degraded body carries the previous completed sync's startedAt
	// (10:18:53), so it keys onto THAT operation's row and upserts it in
	// place — no separate alert row.
	if ev.DedupKey != "argocd:demo-api-staging:"+fixtureSha+":2026-07-16T10:18:53Z" {
		t.Errorf("dedup = %q", ev.DedupKey)
	}
	if suppress != ev.DedupKey+":Degraded" {
		t.Errorf("suppress key = %q", suppress)
	}
	if !ev.TS.Equal(testNow) {
		t.Errorf("ts = %v, want receipt time — the body carries the PREVIOUS sync's stale timestamps", ev.TS)
	}
	if ev.DurationMS != nil {
		t.Error("degraded must not carry the stale operation duration")
	}
	if ev.Title != "Application demo-api-staging: health Degraded" {
		t.Errorf("title = %q", ev.Title)
	}
	if facts.Reason != "Degraded" {
		t.Errorf("facts.Reason = %q", facts.Reason)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte(`{"project":"default"}`)); err == nil {
		t.Error("notification without app must be rejected")
	}
	if _, err := Parse([]byte(`{"app":"x","syncStatus":"Synced"}`)); err == nil {
		t.Error("notification with neither operationPhase nor healthStatus must be rejected")
	}
	if _, err := Parse([]byte(`not json`)); err == nil {
		t.Error("non-JSON must be rejected")
	}
}

// defaultArgoRules mirrors the SPEC §2 example block — the shipped env
// inference for argocd: env label > destination namespace > app-name suffix,
// never cluster=env (one Argo instance manages many clusters and destServer
// is a URL). Keep in sync with SPEC §2 / docs/setup/argocd.md.
func defaultArgoRules(t *testing.T) *normalize.Engine {
	t.Helper()
	eng, err := normalize.NewEngine([]normalize.Rule{
		{Match: normalize.RuleMatch{Source: "argocd"},
			Set: normalize.RuleSet{Env: "{{ .EnvLabel }}"}}, // no label → empty render → field stays unset
		{Match: normalize.RuleMatch{Source: "argocd", Namespace: "dev"},
			Set: normalize.RuleSet{Env: "dev"}},
		{Match: normalize.RuleMatch{Source: "argocd", Namespace: "staging"},
			Set: normalize.RuleSet{Env: "staging"}},
		{Match: normalize.RuleMatch{Source: "argocd", Namespace: "prod"},
			Set: normalize.RuleSet{Env: "prod"}},
		{Match: normalize.RuleMatch{Source: "argocd", ObjectName: "*-dev"},
			Set: normalize.RuleSet{Env: "dev", Service: `{{ trimSuffix .ObjectName "-dev" }}`}},
		{Match: normalize.RuleMatch{Source: "argocd", ObjectName: "*-staging"},
			Set: normalize.RuleSet{Env: "staging", Service: `{{ trimSuffix .ObjectName "-staging" }}`}},
		{Match: normalize.RuleMatch{Source: "argocd", ObjectName: "*-prod"},
			Set: normalize.RuleSet{Env: "prod", Service: `{{ trimSuffix .ObjectName "-prod" }}`}},
		{Match: normalize.RuleMatch{Source: "argocd"},
			Set: normalize.RuleSet{Service: "{{ .ObjectName }}"}},
	})
	if err != nil {
		t.Fatalf("compile default argocd rules: %v", err)
	}
	return eng
}

// TestEnvInferenceMatrix drives the shipped default rules with the captured
// fixtures: one tier per fixture, plus the precedence collision and the
// unmatched → env="" contract (never guess).
func TestEnvInferenceMatrix(t *testing.T) {
	eng := defaultArgoRules(t)

	apply := func(t *testing.T, n *Notification) *model.Event {
		t.Helper()
		ev, facts, _ := Normalize(n, testNow)
		if err := eng.Apply(ev, facts); err != nil {
			t.Fatalf("apply: %v", err)
		}
		return ev
	}

	t.Run("label tier", func(t *testing.T) {
		ev := apply(t, loadNotification(t, "sync_succeeded.json"))
		if ev.Env != "staging" {
			t.Errorf("env = %q, want staging from the env label", ev.Env)
		}
		if ev.Service != "wtc-guestbook-labeled" {
			t.Errorf("service = %q, want app-name fallback", ev.Service)
		}
	})

	t.Run("namespace tier", func(t *testing.T) {
		ev := apply(t, loadNotification(t, "sync_succeeded_env_from_namespace.json"))
		if ev.Env != "staging" {
			t.Errorf("env = %q, want staging from destNamespace", ev.Env)
		}
	})

	t.Run("namespace beats name suffix", func(t *testing.T) {
		// demo-api-staging deployed into the prod namespace: the ordered
		// tiers make destNamespace win — the fixture captures exactly this
		// collision. Service still comes from the name-suffix rule.
		ev := apply(t, loadNotification(t, "sync_succeeded_env_from_name_suffix.json"))
		if ev.Env != "prod" {
			t.Errorf("env = %q, want prod (namespace tier outranks name suffix)", ev.Env)
		}
		if ev.Service != "demo-api" {
			t.Errorf("service = %q, want demo-api (suffix stripped)", ev.Service)
		}
	})

	t.Run("name-suffix tier", func(t *testing.T) {
		// Same app in a non-env namespace: only the name signals staging.
		n := loadNotification(t, "sync_succeeded_env_from_name_suffix.json")
		n.DestNamespace = "default"
		ev := apply(t, n)
		if ev.Env != "staging" {
			t.Errorf("env = %q, want staging from the -staging name suffix", ev.Env)
		}
		if ev.Service != "demo-api" {
			t.Errorf("service = %q", ev.Service)
		}
	})

	t.Run("unmatched stays unmapped", func(t *testing.T) {
		n := loadNotification(t, "sync_succeeded_env_from_namespace.json")
		n.DestNamespace = "default"
		ev := apply(t, n)
		if ev.Env != "" {
			t.Errorf("env = %q, want \"\" — unmatched events are doctor's job, never guessed", ev.Env)
		}
		if ev.Service != "wtc-guestbook-ns" {
			t.Errorf("service = %q, want app-name fallback", ev.Service)
		}
	})
}

// TestOneOperationOneRow: within a single sync operation (same startedAt),
// the Running→Succeeded transition shares the row key (upserts in place)
// while the suppression keys differ so the transition is not shed.
// The captured running/succeeded fixtures are from different operations, so
// the running leg is derived from the succeeded fixture (fixture-mutation
// precedent: flux TestProgressingDropped).
func TestOneOperationOneRow(t *testing.T) {
	succeededN := loadNotification(t, "sync_succeeded.json")
	runningN := loadNotification(t, "sync_succeeded.json")
	runningN.OperationPhase = "Running"
	runningN.FinishedAt = time.Time{} // null mid-sync

	running, _, sRunning := Normalize(runningN, testNow)
	succeeded, _, sSucceeded := Normalize(succeededN, testNow)
	if running.Status != model.StatusStarted {
		t.Fatalf("status = %s", running.Status)
	}
	if running.DedupKey != succeeded.DedupKey {
		t.Errorf("row keys differ (%q vs %q) — one operation must upsert one row", running.DedupKey, succeeded.DedupKey)
	}
	if sRunning == sSucceeded {
		t.Error("suppression keys must differ across phases or transitions get shed")
	}
}

// TestRetrySameRevisionNewRow: a Failed sync then a Succeeded retry of the
// SAME revision are distinct operations (different startedAt) and must get
// distinct rows — equal terminal ranks never overwrite, so a shared key
// would freeze the ledger at failed forever (observed live in stage 3).
func TestRetrySameRevisionNewRow(t *testing.T) {
	failedN := loadNotification(t, "sync_succeeded.json")
	failedN.OperationPhase = "Failed"
	failedN.StartedAt = failedN.StartedAt.Add(-2 * time.Minute)
	failedN.FinishedAt = failedN.FinishedAt.Add(-2 * time.Minute)

	failed, _, _ := Normalize(failedN, testNow)
	succeeded, _, _ := Normalize(loadNotification(t, "sync_succeeded.json"), testNow)
	if failed.Status != model.StatusFailed || succeeded.Status != model.StatusSucceeded {
		t.Fatalf("statuses = %s/%s", failed.Status, succeeded.Status)
	}
	if failed.DedupKey == succeeded.DedupKey {
		t.Errorf("retry shares row key %q — the failed attempt would be frozen instead of both attempts showing", failed.DedupKey)
	}
}

// TestZeroStartedAtOmitsSegment: pre-first-sync health events have a null
// operationState (no phase, no startedAt). The key omits the operation
// segment — folding in receipt time would break at-least-once idempotency.
func TestZeroStartedAtOmitsSegment(t *testing.T) {
	n, err := Parse([]byte(`{"app":"web","revision":"` + fixtureSha + `","healthStatus":"Degraded","destNamespace":"prod"}`))
	if err != nil {
		t.Fatal(err)
	}
	ev, _, suppress := Normalize(n, testNow)
	if ev.Status != model.StatusDegraded {
		t.Fatalf("status = %s", ev.Status)
	}
	if ev.DedupKey != "argocd:web:"+fixtureSha {
		t.Errorf("dedup = %q, want the startedAt segment omitted", ev.DedupKey)
	}
	if suppress != ev.DedupKey+":Degraded" {
		t.Errorf("suppress key = %q", suppress)
	}
	if !ev.TS.Equal(testNow) {
		t.Errorf("ts = %v, want receipt time", ev.TS)
	}
}

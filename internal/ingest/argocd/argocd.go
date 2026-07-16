// Package argocd normalizes Argo CD notifications-controller webhooks. There
// is no fixed webhook schema like Flux's eventv1 — the body is templated by
// the operator, and docs/setup/argocd-notifications.yaml ships the canonical
// template this parser targets. Field semantics are verified against captured
// fixtures under testdata/argocd/ (Argo CD v3.4.5), never documentation
// memory (trap #9 applies to Argo exactly as it did to Flux).
package argocd

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
)

// Notification is the canonical body emitted by the shipped template.
// Nullable fields (revisions, finishedAt, triggeredBy, envLabel,
// operationPhase pre-first-sync) decode as their zero values.
type Notification struct {
	App            string    `json:"app"`
	Project        string    `json:"project"`
	Revision       string    `json:"revision"`  // sync git sha; may be unresolved (e.g. "HEAD") when the sync errored before resolution
	Revisions      []string  `json:"revisions"` // multi-source apps (2.6+); null for single-source
	SyncStatus     string    `json:"syncStatus"`
	HealthStatus   string    `json:"healthStatus"`
	OperationPhase string    `json:"operationPhase"` // Running | Succeeded | Failed | Error; null before the first sync
	DestServer     string    `json:"destServer"`     // destination *server URL* — NOT an env (never cluster=env for argocd)
	DestNamespace  string    `json:"destNamespace"`
	RepoURL        string    `json:"repoURL"`
	SourcePath     string    `json:"sourcePath"`
	TargetRevision string    `json:"targetRevision"`
	StartedAt      time.Time `json:"startedAt"`
	FinishedAt     time.Time `json:"finishedAt"`
	TriggeredBy    string    `json:"triggeredBy"` // empty for API/controller-initiated syncs (observed: always null on kubectl-driven syncs)
	EnvLabel       string    `json:"envLabel"`    // the Application's `env` label; top tier of env inference
}

// Parse decodes a raw delivery body.
func Parse(raw []byte) (*Notification, error) {
	var n Notification
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, fmt.Errorf("decode argocd notification: %w", err)
	}
	if n.App == "" {
		return nil, fmt.Errorf("argocd notification missing app")
	}
	if n.OperationPhase == "" && n.HealthStatus == "" {
		return nil, fmt.Errorf("argocd notification for %q carries neither operationPhase nor healthStatus", n.App)
	}
	return &n, nil
}

// shaLike matches a resolved git revision — same shape the where join keys on.
var shaLike = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// Normalize maps a notification onto the Event schema + facts, and derives
// the suppression key (Argo can re-notify on resyncs — same spam trap as
// Flux's #1 — though its own notified-annotation hash gates some repeats;
// see docs/setup/argocd.md).
//
// The row dedup key is argocd:<app>:<revision>:<startedAt> — one row per
// sync OPERATION, which is trap #5's "logical change" for Argo: a retry of
// the same revision is a new change and the ledger must show both attempts.
// Keying on (app, revision) alone froze rows live: a Failed sync followed by
// a Succeeded retry of the same revision could never update the row (equal
// terminal ranks never overwrite — flux avoids this only because reason is
// part of its row key). Within one operation startedAt is constant, so
// Running→Succeeded/Error still upserts a single row. The suppression key
// appends the phase-or-health discriminator so identical re-notifications
// are shed while genuine transitions pass through. A zero startedAt
// (pre-first-sync health events, where operationState is null) omits the
// segment — folding in receipt time instead would break at-least-once
// idempotency across redeliveries.
//
// healthStatus=Degraded wins over operationPhase: on-health-degraded fires
// carrying the LAST completed sync's phase and startedAt (observed: phase
// stays Succeeded), so it keys onto that operation's row and — per the
// operator decision — upserts it to status=degraded rather than creating an
// alert row.
func Normalize(n *Notification, now time.Time) (*model.Event, normalize.Facts, string) {
	degraded := n.HealthStatus == "Degraded"

	status := model.StatusUnknown
	switch {
	case degraded:
		status = model.StatusDegraded
	case n.OperationPhase == "Running":
		status = model.StatusStarted
	case n.OperationPhase == "Succeeded":
		status = model.StatusSucceeded
	case n.OperationPhase == "Failed" || n.OperationPhase == "Error":
		// "Error" = the sync never started applying (e.g. unresolvable
		// path); "Failed" = it applied and a resource/hook failed. Both are
		// a failed deploy for the ledger. Observed live: a nonexistent
		// source path reports Error, not Failed.
		status = model.StatusFailed
	}

	// Source event time: the operation's completion, else its start. Degraded
	// notifications carry the *previous* sync's stale timestamps (health
	// regresses independently of any operation — observed in the fixture), so
	// the degradation is stamped with receipt time instead.
	ts := n.FinishedAt
	if ts.IsZero() {
		ts = n.StartedAt
	}
	if degraded || ts.IsZero() {
		ts = now
	}

	var durationMS *int64
	if !degraded && status != model.StatusStarted && !n.StartedAt.IsZero() && !n.FinishedAt.IsZero() {
		d := n.FinishedAt.Sub(n.StartedAt).Milliseconds()
		durationMS = &d
	}

	// The where join (APPLIED stage) keys on Ref carrying a bare sha, exactly
	// like a Flux reconcile revision. Unresolved revisions must not pollute it.
	var ref string
	if shaLike.MatchString(n.Revision) {
		ref = n.Revision
	}

	opKey := n.Revision
	if !n.StartedAt.IsZero() {
		opKey += ":" + n.StartedAt.UTC().Format(time.RFC3339)
	}
	dedupKey := fmt.Sprintf("argocd:%s:%s", n.App, opKey)

	discriminator := n.OperationPhase
	if degraded {
		discriminator = "Degraded"
	}
	suppressKey := dedupKey + ":" + discriminator

	actor := n.TriggeredBy
	if actor == "" {
		actor = "argocd"
	}

	title := fmt.Sprintf("Application %s: sync %s", n.App, n.OperationPhase)
	if degraded {
		title = fmt.Sprintf("Application %s: health Degraded", n.App)
	}

	ev := &model.Event{
		ID:         model.NewID(),
		TS:         ts.UTC(),
		IngestedAt: now.UTC(),
		Source:     model.SourceArgoCD,
		Kind:       model.KindDeploy,
		Status:     status,
		Namespace:  n.DestNamespace,
		Actor:      actor,
		Ref:        ref,
		Title:      title,
		DurationMS: durationMS,
		DedupKey:   dedupKey,
		Payload:    mustPayload(n),
	}
	facts := normalize.Facts{
		Source:     "argocd",
		Repo:       n.RepoURL,
		ObjectKind: "Application",
		ObjectName: n.App,
		Namespace:  n.DestNamespace,
		Reason:     discriminator,
		Project:    n.Project,
		DestServer: n.DestServer,
		SourcePath: n.SourcePath,
		EnvLabel:   n.EnvLabel,
	}
	return ev, facts, suppressKey
}

// mustPayload keeps the fields the drawer/doctor may want that don't map onto
// Event columns. The raw body is not stored verbatim: the canonical shape is
// ours, so re-marshaling the subset is lossless where it matters and keeps
// the redaction surface small.
func mustPayload(n *Notification) string {
	b, err := json.Marshal(map[string]any{
		"project":        n.Project,
		"revision":       n.Revision,
		"revisions":      n.Revisions,
		"syncStatus":     n.SyncStatus,
		"healthStatus":   n.HealthStatus,
		"operationPhase": n.OperationPhase,
		"destServer":     n.DestServer,
		"repoURL":        n.RepoURL,
		"sourcePath":     n.SourcePath,
		"targetRevision": n.TargetRevision,
	})
	if err != nil {
		return "" // map of plain values cannot fail; belt and braces
	}
	return string(b)
}

// Strings that make suppressed-vs-stored observable in handler responses
// (mirrors flux; kept per-package so ingest packages stay independent).
const (
	ResultStored     = "stored"
	ResultSuppressed = "suppressed"
)

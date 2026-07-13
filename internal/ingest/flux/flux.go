// Package flux normalizes notification-controller events. Payload structs are
// verified against captured fixtures under testdata/flux/ (trap #9: never
// trust documentation memory for this shape).
package flux

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/normalize"
)

// Event is the notification-controller payload (eventv1 shape, as captured).
type Event struct {
	InvolvedObject struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"involvedObject"`
	Severity  string            `json:"severity"` // info | error
	Timestamp time.Time         `json:"timestamp"`
	Message   string            `json:"message"`
	Reason    string            `json:"reason"`
	Metadata  map[string]string `json:"metadata"` // cluster (Alert eventMetadata), revision, …
}

// Parse decodes a raw delivery body.
func Parse(raw []byte) (*Event, error) {
	var ev Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, fmt.Errorf("decode flux event: %w", err)
	}
	if ev.InvolvedObject.Kind == "" || ev.InvolvedObject.Name == "" {
		return nil, fmt.Errorf("flux event missing involvedObject kind/name")
	}
	return &ev, nil
}

// gitRevision matches Flux revision strings like "master@sha1:46b93c87…" and
// captures the bare sha, which is what merge/push events carry in ref — the
// join `wtc where` depends on.
var gitRevision = regexp.MustCompile(`@sha1:([0-9a-f]{7,40})$`)

// Normalize maps a notification event onto the Event schema + facts.
// kind=deploy: a reconcile applies change to a runtime env. severity info →
// succeeded, error → failed (reconciles report outcomes, never "started").
func Normalize(fe *Event, now time.Time) (*model.Event, normalize.Facts) {
	status := model.StatusUnknown
	switch fe.Severity {
	case "info":
		status = model.StatusSucceeded
	case "error":
		status = model.StatusFailed
	}

	cluster := fe.Metadata["cluster"] // Alert eventMetadata; "" when unset
	revision := fe.Metadata["revision"]

	var ref, artifact string
	if m := gitRevision.FindStringSubmatch(revision); m != nil {
		ref = m[1] // git-sourced revision → joinable sha
	} else if revision != "" {
		// e.g. Helm chart version "6.14.0" — an artifact version, not a sha.
		artifact = fe.InvolvedObject.Name + "@" + revision
	}

	obj := fe.InvolvedObject
	ts := fe.Timestamp
	if ts.IsZero() {
		ts = now
	}

	ev := &model.Event{
		ID:         model.NewID(),
		TS:         ts.UTC(),
		IngestedAt: now.UTC(),
		Source:     model.SourceFlux,
		Kind:       model.KindDeploy,
		Status:     status,
		Cluster:    cluster,
		Namespace:  obj.Namespace,
		Actor:      "flux",
		Ref:        ref,
		Artifact:   artifact,
		Title:      fmt.Sprintf("%s %s/%s: %s", obj.Kind, obj.Namespace, obj.Name, fe.Reason),
		DedupKey: fmt.Sprintf("flux:%s:%s/%s/%s:%s:%s",
			cluster, obj.Kind, obj.Namespace, obj.Name, revision, fe.Reason),
		Payload: mustPayload(fe.Message, revision),
	}
	facts := normalize.Facts{
		Source:     "flux",
		Cluster:    cluster,
		ObjectKind: obj.Kind,
		ObjectName: obj.Name,
		Namespace:  obj.Namespace,
		Reason:     fe.Reason,
	}
	return ev, facts
}

func mustPayload(message, revision string) string {
	b, err := json.Marshal(map[string]string{"message": message, "revision": revision})
	if err != nil {
		return "" // map[string]string cannot fail; belt and braces
	}
	return string(b)
}

// Suppressor drops repeats of the same dedup key inside a window (trap #1:
// notification-controller re-emits on every reconcile; without this the
// write path is hammered with no-ops). The store's strict-rank upsert already
// guarantees one row — this is load shedding, not correctness.
type Suppressor struct {
	mu     sync.Mutex
	window time.Duration
	seen   map[string]time.Time
}

// NewSuppressor builds a suppressor; window <= 0 disables suppression.
func NewSuppressor(window time.Duration) *Suppressor {
	return &Suppressor{window: window, seen: map[string]time.Time{}}
}

// Suppress reports whether key was already seen within the window, recording
// it otherwise. Evicts stale entries opportunistically to bound memory.
func (s *Suppressor) Suppress(key string, now time.Time) bool {
	if s.window <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if last, ok := s.seen[key]; ok && now.Sub(last) < s.window {
		return true
	}
	// Opportunistic eviction keeps the map bounded across long uptimes.
	if len(s.seen) > 4096 {
		for k, v := range s.seen {
			if now.Sub(v) >= s.window {
				delete(s.seen, k)
			}
		}
	}
	s.seen[key] = now
	return false
}

// Strings that make suppressed-vs-stored observable in handler responses.
const (
	ResultStored     = "stored"
	ResultSuppressed = "suppressed"
)

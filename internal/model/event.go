// Package model defines the normalized Event — the single schema every
// ingest source is mapped onto — plus its enums and validation.
package model

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// Source identifies the system a change event originated from.
type Source string

const (
	SourceGitHub       Source = "github"
	SourceFlux         Source = "flux"
	SourceHelm         Source = "helm"
	SourceTerraform    Source = "terraform"
	SourceManual       Source = "manual"
	SourceGeneric      Source = "generic"
	SourceAlertmanager Source = "alertmanager"
)

var validSources = map[Source]bool{
	SourceGitHub: true, SourceFlux: true, SourceHelm: true, SourceTerraform: true,
	SourceManual: true, SourceGeneric: true, SourceAlertmanager: true,
}

// ValidSource reports whether s is a known source.
func ValidSource(s Source) bool { return validSources[s] }

// Kind classifies what a change event represents. See SPEC §1 for semantics.
type Kind string

const (
	KindBuild        Kind = "build"
	KindMerge        Kind = "merge"
	KindPush         Kind = "push"
	KindDeploy       Kind = "deploy"
	KindConfigChange Kind = "config_change"
	KindInfraChange  Kind = "infra_change"
	KindRollback     Kind = "rollback"
	KindAlert        Kind = "alert"
	KindManual       Kind = "manual"
)

var validKinds = map[Kind]bool{
	KindBuild: true, KindMerge: true, KindPush: true, KindDeploy: true,
	KindConfigChange: true, KindInfraChange: true, KindRollback: true,
	KindAlert: true, KindManual: true,
}

// ValidKind reports whether k is a known kind.
func ValidKind(k Kind) bool { return validKinds[k] }

// Status is the lifecycle state of a logical change.
type Status string

const (
	StatusStarted   Status = "started"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusUnknown   Status = "unknown"
)

var validStatuses = map[Status]bool{
	StatusStarted: true, StatusSucceeded: true, StatusFailed: true, StatusUnknown: true,
}

// ValidStatus reports whether s is a known status.
func ValidStatus(s Status) bool { return validStatuses[s] }

// StatusRank orders statuses for the upsert rule: an incoming event may only
// overwrite a stored one when its rank is >= the stored rank, so a
// late-arriving "started" never regresses a completed row.
func StatusRank(s Status) int {
	switch s {
	case StatusSucceeded, StatusFailed:
		return 2
	case StatusStarted:
		return 1
	default:
		return 0
	}
}

// Event is one logical change. One row in the events table; status
// transitions update the row in place (keyed by DedupKey).
type Event struct {
	ID         string    `json:"id"`
	TS         time.Time `json:"ts"`          // source event time, UTC
	IngestedAt time.Time `json:"ingested_at"` // when wtc stored it, UTC
	Source     Source    `json:"source"`
	Kind       Kind      `json:"kind"`
	Status     Status    `json:"status"`
	Env        string    `json:"env"` // "" = unmapped, surfaced by doctor
	Cluster    string    `json:"cluster"`
	Namespace  string    `json:"namespace"`
	Service    string    `json:"service"`
	Actor      string    `json:"actor"`
	Ref        string    `json:"ref"`      // git sha / revision
	Artifact   string    `json:"artifact"` // primary artifact, e.g. registry/app:tag
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	DurationMS *int64    `json:"duration_ms,omitempty"`
	DedupKey   string    `json:"dedup_key"`
	Payload    string    `json:"payload,omitempty"` // redacted raw JSON
}

// Validate checks the invariants every event must satisfy before storage.
func (e *Event) Validate() error {
	switch {
	case e.ID == "":
		return fmt.Errorf("event: id is required")
	case e.TS.IsZero():
		return fmt.Errorf("event: ts is required")
	case e.IngestedAt.IsZero():
		return fmt.Errorf("event: ingested_at is required")
	case e.Title == "":
		return fmt.Errorf("event: title is required")
	case e.DedupKey == "":
		return fmt.Errorf("event: dedup_key is required")
	}
	if !ValidSource(e.Source) {
		return fmt.Errorf("event: invalid source %q", e.Source)
	}
	if !ValidKind(e.Kind) {
		return fmt.Errorf("event: invalid kind %q", e.Kind)
	}
	if !ValidStatus(e.Status) {
		return fmt.Errorf("event: invalid status %q", e.Status)
	}
	return nil
}

// NewID returns a fresh ULID string.
func NewID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
}

// tsLayout is RFC3339 with fixed millisecond precision so stored strings sort
// lexicographically. Mixed-precision RFC3339 (some values with fractional
// seconds, some without) does NOT sort correctly as text.
const tsLayout = "2006-01-02T15:04:05.000Z07:00"

// FormatTS renders t as the canonical stored timestamp (UTC, milliseconds).
func FormatTS(t time.Time) string { return t.UTC().Format(tsLayout) }

// ParseTS parses a stored or user-supplied RFC3339 timestamp.
func ParseTS(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}

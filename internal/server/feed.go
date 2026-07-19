package server

import (
	"crypto/subtle"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/migueljfsc/wtc/internal/model"
	"github.com/migueljfsc/wtc/internal/store"
)

// Atom feed (pull-based awareness): a read-only subscription surface for
// feed readers, no sink or webhook required. Registered at /feed (not under
// /api — it serves XML and is not part of the JSON contract). Feed readers
// cannot set headers, so the api_token may arrive as ?token= instead of the
// Authorization header; both are constant-time compared. The token lands in
// reader config/history — acceptable for the same reason bearer tokens in CI
// config are: rotate api_tokens to revoke.

const feedDefaultLimit = 50
const feedMaxLimit = 200

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	NS      string      `xml:"xmlns,attr"`
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Author  atomAuthor  `xml:"author"`
	Entries []atomEntry `xml:"entry"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

type atomEntry struct {
	Title   string    `xml:"title"`
	ID      string    `xml:"id"`
	Updated string    `xml:"updated"`
	Link    *atomLink `xml:"link,omitempty"`
	Content string    `xml:"content"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
}

// feedAuthorized accepts the api_token from the Authorization header or the
// ?token= query parameter. Same fail-closed semantics as requireBearer.
func (s *Server) feedAuthorized(r *http.Request) bool {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		token = r.URL.Query().Get("token")
	}
	if token == "" {
		return false
	}
	for _, want := range s.tokens {
		if subtle.ConstantTimeCompare([]byte(token), []byte(want)) == 1 {
			return true
		}
	}
	return false
}

// handleFeed serves recent events as Atom. Filters mirror the /events facet
// params (env, service, repo, kind, status, source) plus limit.
func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	if !s.feedAuthorized(r) {
		s.writeError(w, http.StatusUnauthorized, "missing or invalid token (Authorization header or ?token=)")
		return
	}

	q := r.URL.Query()
	f := store.Filter{Limit: feedDefaultLimit}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			s.writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		f.Limit = min(n, feedMaxLimit)
	}
	set := func(dst *[]string, key string) {
		if v := q.Get(key); v != "" {
			*dst = []string{v}
		}
	}
	set(&f.Envs, "env")
	set(&f.Services, "service")
	set(&f.Repos, "repo")
	set(&f.Kinds, "kind")
	set(&f.Statuses, "status")
	set(&f.Sources, "source")

	events, _, err := s.store.ListEvents(r.Context(), f)
	if err != nil {
		s.log.Error("feed: list events", "error", err)
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	updated := time.Now().UTC()
	if len(events) > 0 {
		updated = events[0].TS // newest first
	}
	feed := atomFeed{
		NS:      "http://www.w3.org/2005/Atom",
		Title:   "wtc changes",
		ID:      "urn:wtc:feed",
		Updated: updated.Format(time.RFC3339),
		Author:  atomAuthor{Name: "wtc"},
		Entries: make([]atomEntry, 0, len(events)),
	}
	for _, ev := range events {
		feed.Entries = append(feed.Entries, feedEntry(ev))
	}

	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	_, _ = w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(feed); err != nil {
		s.log.Error("feed: encode", "error", err)
	}
}

func feedEntry(ev model.Event) atomEntry {
	env := ev.Env
	if env == "" {
		env = "unmapped"
	}
	subject := ev.Service
	if subject == "" {
		subject = ev.Repo
	}
	title := fmt.Sprintf("[%s] %s", env, ev.Kind)
	if subject != "" {
		title += " " + subject
	}
	title += fmt.Sprintf(" %s — %s", ev.Status, ev.Title)

	e := atomEntry{
		Title: title,
		// Feed readers dedupe on entry id, and a status transition updates the
		// row in place (same ULID) — so the id carries the status, mirroring
		// the (event id, status) notification idempotency key. A transition
		// then appears as a new entry instead of being swallowed.
		ID:      fmt.Sprintf("urn:wtc:%s:%s", ev.ID, ev.Status),
		Updated: ev.TS.Format(time.RFC3339),
		Content: fmt.Sprintf("source=%s env=%s service=%s repo=%s kind=%s status=%s ref=%s",
			ev.Source, ev.Env, ev.Service, ev.Repo, ev.Kind, ev.Status, ev.Ref),
	}
	if ev.URL != "" {
		e.Link = &atomLink{Href: ev.URL}
	}
	return e
}

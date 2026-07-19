package normalize

import (
	"regexp"
	"sort"
	"strings"
)

// Poller scope-glob helpers. They live next to CompileGlob because they
// are the same dialect: `*` matches one path segment, `**` any depth. Both
// pollers and config validation share these — one glob behavior product-wide.

// IsGlob reports whether a repo/project scope entry is a pattern rather than
// an exact name.
func IsGlob(entry string) bool { return strings.Contains(entry, "*") }

// SplitScope partitions scope entries into exact names and compiled glob
// patterns. Patterns were validated at config load, so a compile failure here
// is a programming error and the entry is dropped (never silently matched).
func SplitScope(entries []string) (exact []string, globs []*regexp.Regexp) {
	for _, e := range entries {
		if !IsGlob(e) {
			exact = append(exact, e)
			continue
		}
		if re, err := CompileGlob(e); err == nil {
			globs = append(globs, re)
		}
	}
	return exact, globs
}

// ResolveScope unions the exact entries with the discovered candidates that
// match any glob, deduplicated and sorted — the effective poll list for one
// sweep.
func ResolveScope(exact []string, globs []*regexp.Regexp, candidates []string) []string {
	set := map[string]bool{}
	for _, e := range exact {
		set[e] = true
	}
	for _, c := range candidates {
		for _, re := range globs {
			if re.MatchString(c) {
				set[c] = true
				break
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ScopeNamespace returns the static namespace prefix of a glob pattern — the
// segments before the first glob-bearing one. On GitLab this prefix IS the
// bounded discovery call (list that group's/user's projects), so ok=false
// (no static prefix, e.g. "*" or "*/x") means the pattern cannot be resolved
// there; config load rejects it. GitHub never needs this: its discovery is
// the affiliation-bounded /user/repos set and any glob is just a filter.
func ScopeNamespace(pattern string) (string, bool) {
	var static []string
	for _, seg := range strings.Split(pattern, "/") {
		if strings.Contains(seg, "*") {
			break
		}
		static = append(static, seg)
	}
	if len(static) == 0 {
		return "", false
	}
	return strings.Join(static, "/"), true
}

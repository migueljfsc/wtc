package normalize

import (
	"fmt"
	"regexp"
	"strings"
)

// DefaultTagPatterns cover the operator's two conventions (SPEC §2). Each
// pattern must expose a `sha` named capture group.
var DefaultTagPatterns = []string{
	`^sha-(?P<sha>[0-9a-f]{7,40})$`,             // sha-abc1234
	`^v?\d+\.\d+\.\d+-(?P<sha>[0-9a-f]{7,40})$`, // 1.4.2-abc1234
}

// TagResolver extracts git shas from image tags via an ordered pattern list.
// This is the tag↔sha join `wtc where` traverses — configurable so no single
// tagging convention is hardcoded (CLAUDE.md trap #8).
type TagResolver struct {
	patterns []*regexp.Regexp
}

// NewTagResolver compiles patterns (nil/empty → defaults). Every pattern must
// contain a `sha` named group; errors surface at startup.
func NewTagResolver(patterns []string) (*TagResolver, error) {
	if len(patterns) == 0 {
		patterns = DefaultTagPatterns
	}
	r := &TagResolver{}
	for i, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("tag_patterns[%d]: %w", i, err)
		}
		if re.SubexpIndex("sha") < 0 {
			return nil, fmt.Errorf("tag_patterns[%d]: missing (?P<sha>...) capture group", i)
		}
		r.patterns = append(r.patterns, re)
	}
	return r, nil
}

// Resolve returns the sha embedded in an image tag (or full image ref — the
// part after the last ':' is tried too), or "" when no pattern matches.
func (r *TagResolver) Resolve(tag string) string {
	candidates := []string{tag}
	if i := strings.LastIndex(tag, ":"); i >= 0 && i < len(tag)-1 {
		candidates = append(candidates, tag[i+1:])
	}
	for _, c := range candidates {
		for _, re := range r.patterns {
			if m := re.FindStringSubmatch(c); m != nil {
				return m[re.SubexpIndex("sha")]
			}
		}
	}
	return ""
}

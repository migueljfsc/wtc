// Package catalog resolves the owning team of a service from an external
// service catalog. It parses the popular formats — Backstage `catalog-info.yaml`,
// Datadog service catalog, a plain wtc `services.yaml`, and CODEOWNERS — into a
// service→owner map (with a repo→owner fallback for CODEOWNERS), scanned in a
// fixed priority order. The result feeds the normalization engine so every
// ingested event is stamped with its owner at ingest time.
package catalog

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Source is one configured catalog input. Path may be a glob. Repo applies to
// the codeowners type only — the repo whose default owner the file supplies.
type Source struct {
	Type string `yaml:"type"` // backstage | datadog | services | codeowners
	Path string `yaml:"path"` // file or glob
	Repo string `yaml:"repo"` // codeowners only: the repo this file governs
}

// ValidTypes are the recognized catalog source types, in scan-priority order.
var ValidTypes = []string{"backstage", "datadog", "services", "codeowners"}

// typePriority ranks source types so that, regardless of config listing order,
// a service defined in more than one catalog takes the higher-priority owner:
// backstage > datadog > services.yaml > codeowners.
var typePriority = map[string]int{"backstage": 0, "datadog": 1, "services": 2, "codeowners": 3}

// Catalog maps a service (and, as a fallback, a repo) to its owning team.
type Catalog struct {
	byService map[string]string
	byRepo    map[string]string
}

// Owner returns the team owning (service, repo). A service match wins; repo is
// the fallback (CODEOWNERS default owners key by repo). "" when neither is known.
func (c *Catalog) Owner(service, repo string) string {
	if c == nil {
		return ""
	}
	if service != "" {
		if o := c.byService[service]; o != "" {
			return o
		}
	}
	if repo != "" {
		if o := c.byRepo[repo]; o != "" {
			return o
		}
	}
	return ""
}

// Len reports how many service + repo mappings were loaded (for doctor / config
// visibility).
func (c *Catalog) Len() int {
	if c == nil {
		return 0
	}
	return len(c.byService) + len(c.byRepo)
}

// setService / setRepo are first-writer-wins: an earlier (higher-priority)
// source keeps its mapping.
func (c *Catalog) setService(service, owner string) {
	if service == "" || owner == "" {
		return
	}
	if _, ok := c.byService[service]; !ok {
		c.byService[service] = owner
	}
}

func (c *Catalog) setRepo(repo, owner string) {
	if repo == "" || owner == "" {
		return
	}
	if _, ok := c.byRepo[repo]; !ok {
		c.byRepo[repo] = owner
	}
}

// Load reads every source into one Catalog. Sources are processed in priority
// order (typePriority, stable within a type) so higher-priority catalogs win a
// contested service. A missing file or an unreadable/malformed one is a fatal
// config error — a silently-empty catalog would mislabel every event as unowned.
func Load(sources []Source) (*Catalog, error) {
	c := &Catalog{byService: map[string]string{}, byRepo: map[string]string{}}

	ordered := append([]Source(nil), sources...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return typePriority[ordered[i].Type] < typePriority[ordered[j].Type]
	})

	for _, s := range ordered {
		files, err := expand(s.Path)
		if err != nil {
			return nil, fmt.Errorf("catalog %s %q: %w", s.Type, s.Path, err)
		}
		for _, f := range files {
			if err := c.parseFile(s, f); err != nil {
				return nil, fmt.Errorf("catalog %s %s: %w", s.Type, f, err)
			}
		}
	}
	return c, nil
}

func (c *Catalog) parseFile(s Source, path string) error {
	f, err := os.Open(path) //nolint:gosec // operator-configured catalog path
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	switch s.Type {
	case "backstage":
		return parseBackstage(f, c)
	case "datadog":
		return parseDatadog(f, c)
	case "services":
		return parseServices(f, c)
	case "codeowners":
		if s.Repo == "" {
			return fmt.Errorf("a codeowners source needs `repo:` — the repo whose default owner it supplies")
		}
		return parseCodeowners(f, s.Repo, c)
	default:
		return fmt.Errorf("unknown catalog type %q (want one of %s)", s.Type, strings.Join(ValidTypes, ", "))
	}
}

// expand resolves a path that may be a glob. A literal path that does not exist
// is an error; a glob that matches nothing is also an error (a scope typo must
// not pass silently).
func expand(path string) ([]string, error) {
	if !strings.ContainsAny(path, "*?[") {
		if _, err := os.Stat(path); err != nil {
			return nil, err
		}
		return []string{path}, nil
	}
	matches, err := filepath.Glob(path)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("glob matched no files")
	}
	sort.Strings(matches)
	return matches, nil
}

// ---- Backstage catalog-info.yaml -------------------------------------------

type backstageDoc struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Owner string `yaml:"owner"`
	} `yaml:"spec"`
}

func parseBackstage(r io.Reader, c *Catalog) error {
	dec := yaml.NewDecoder(r)
	for {
		var d backstageDoc
		if err := dec.Decode(&d); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if !strings.EqualFold(d.Kind, "Component") {
			continue
		}
		c.setService(d.Metadata.Name, normalizeRef(d.Spec.Owner))
	}
}

// ---- Datadog service catalog -----------------------------------------------

type datadogDoc struct {
	DDService string `yaml:"dd-service"` // v2
	Kind      string `yaml:"kind"`       // v3: "service"
	Team      string `yaml:"team"`       // v2 + v3
	Metadata  struct {
		Name string `yaml:"name"` // v3
	} `yaml:"metadata"`
	Spec struct {
		Owner string `yaml:"owner"`
	} `yaml:"spec"`
}

func parseDatadog(r io.Reader, c *Catalog) error {
	dec := yaml.NewDecoder(r)
	for {
		var d datadogDoc
		if err := dec.Decode(&d); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		// v3 entities carry many kinds; only `service` names an owner here.
		if d.Kind != "" && !strings.EqualFold(d.Kind, "service") {
			continue
		}
		service := firstNonEmpty(d.DDService, d.Metadata.Name)
		owner := firstNonEmpty(d.Team, d.Spec.Owner)
		c.setService(service, normalizeRef(owner))
	}
}

// ---- wtc services.yaml -----------------------------------------------------

type servicesDoc struct {
	Services []struct {
		Service string `yaml:"service"`
		Name    string `yaml:"name"`
		Owner   string `yaml:"owner"`
		Team    string `yaml:"team"`
		Repo    string `yaml:"repo"`
	} `yaml:"services"`
}

func parseServices(r io.Reader, c *Catalog) error {
	var doc servicesDoc
	if err := yaml.NewDecoder(r).Decode(&doc); err != nil {
		if err == io.EOF {
			return nil
		}
		return err
	}
	for _, s := range doc.Services {
		owner := firstNonEmpty(s.Owner, s.Team)
		c.setService(firstNonEmpty(s.Service, s.Name), owner)
		c.setRepo(s.Repo, owner)
	}
	return nil
}

// ---- CODEOWNERS ------------------------------------------------------------

// parseCodeowners keys the repo to the owner of its broadest rule. CODEOWNERS
// resolves last-match-wins, so the effective default is the last `*` line; its
// first owner token (leading @ stripped) is the repo's default team.
func parseCodeowners(r io.Reader, repo string, c *Catalog) error {
	var owner string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "*" {
			continue
		}
		owner = strings.TrimPrefix(fields[1], "@") // last * rule wins
	}
	if err := sc.Err(); err != nil {
		return err
	}
	c.setRepo(repo, owner)
	return nil
}

// ---- helpers ---------------------------------------------------------------

// normalizeRef drops a Backstage entity-ref kind prefix (group:/user:/team:) so
// `group:platform` and `platform` fold to the same owner value.
func normalizeRef(owner string) string {
	owner = strings.TrimSpace(owner)
	for _, p := range []string{"group:", "user:", "team:"} {
		if strings.HasPrefix(strings.ToLower(owner), p) {
			return owner[len(p):]
		}
	}
	return owner
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

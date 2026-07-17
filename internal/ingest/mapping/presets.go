package mapping

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// presetFS holds the shipped preset mappings — a mapping + fixture, tested like
// any parser. A preset supplies the field/dedup/facts templates for a known
// tool; the operator's config still supplies the source name and auth (its own
// secret), and may override any preset field. Auth is intentionally NOT baked
// into a preset: a shared secret is per-installation.
//
//go:embed presets/*.yaml
var presetFS embed.FS

var (
	presetsOnce sync.Once
	presets     map[string]Webhook
	presetsErr  error
)

func loadPresets() {
	presets = map[string]Webhook{}
	entries, err := fs.ReadDir(presetFS, "presets")
	if err != nil {
		presetsErr = err
		return
	}
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".yaml")
		raw, rerr := presetFS.ReadFile("presets/" + e.Name())
		if rerr != nil {
			presetsErr = rerr
			return
		}
		var w Webhook
		if uerr := yaml.Unmarshal(raw, &w); uerr != nil {
			presetsErr = fmt.Errorf("preset %s: %w", name, uerr)
			return
		}
		presets[name] = w
	}
}

// Preset returns the shipped preset by name (grafana, jenkins, …).
func Preset(name string) (Webhook, bool) {
	presetsOnce.Do(loadPresets)
	if presetsErr != nil {
		return Webhook{}, false
	}
	w, ok := presets[name]
	return w, ok
}

// PresetNames lists the shipped preset names (for docs/CLI listing).
func PresetNames() []string {
	presetsOnce.Do(loadPresets)
	names := make([]string, 0, len(presets))
	for n := range presets {
		names = append(names, n)
	}
	return names
}

// merge overlays an operator's Webhook onto a preset base: any non-empty field
// in over wins; Facts maps merge key-by-key (override keys win). Name and Auth
// come from the operator (a preset carries neither). The preset supplies the
// template surface; the operator supplies identity + credentials + any tweaks.
func merge(base, over Webhook) Webhook {
	out := base
	out.Name = over.Name
	out.Preset = over.Preset
	out.Auth = over.Auth
	pick := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	pick(&out.DedupKey, over.DedupKey)
	pick(&out.Mapping.Kind, over.Mapping.Kind)
	pick(&out.Mapping.Status, over.Mapping.Status)
	pick(&out.Mapping.Env, over.Mapping.Env)
	pick(&out.Mapping.Cluster, over.Mapping.Cluster)
	pick(&out.Mapping.Namespace, over.Mapping.Namespace)
	pick(&out.Mapping.Service, over.Mapping.Service)
	pick(&out.Mapping.Actor, over.Mapping.Actor)
	pick(&out.Mapping.Ref, over.Mapping.Ref)
	pick(&out.Mapping.Artifact, over.Mapping.Artifact)
	pick(&out.Mapping.Title, over.Mapping.Title)
	pick(&out.Mapping.URL, over.Mapping.URL)
	pick(&out.Mapping.TS, over.Mapping.TS)
	pick(&out.Mapping.DurationMS, over.Mapping.DurationMS)
	if len(over.Facts) > 0 {
		out.Facts = map[string]string{}
		for k, v := range base.Facts {
			out.Facts[k] = v
		}
		for k, v := range over.Facts {
			out.Facts[k] = v
		}
	}
	return out
}

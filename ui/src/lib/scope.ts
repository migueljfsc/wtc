import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import { daysAgoISO } from "@/lib/format";

/**
 * The global scope: the filter every analytical page shares, persisted in the
 * URL so it survives navigation and stays shareable. Facet fields are
 * comma-joined OR-sets (matching the events API); the time range resolves to
 * a since/until window (`custom` reads explicit since/until params).
 */
export type RangePreset = "24h" | "7d" | "30d" | "90d" | "custom";

export const RANGE_PRESETS: { key: Exclude<RangePreset, "custom">; label: string; days: number }[] = [
  { key: "24h", label: "24h", days: 1 },
  { key: "7d", label: "7d", days: 7 },
  { key: "30d", label: "30d", days: 30 },
  { key: "90d", label: "90d", days: 90 },
];

const DEFAULT_RANGE: RangePreset = "7d";

export interface Scope {
  env: string;
  service: string;
  owner: string;
  repo: string;
  q: string;
  range: RangePreset;
  since: string; // resolved ISO
  until: string; // resolved ISO
}

/** The scope keys owned in the URL. Other params (e.g. a changeset `ref`, the
 *  Where `ref`) are preserved untouched. */
const FACET_KEYS = ["env", "service", "owner", "repo", "q"] as const;
type FacetKey = (typeof FACET_KEYS)[number];

function resolve(params: URLSearchParams): Scope {
  const range = ((params.get("range") as RangePreset) || DEFAULT_RANGE);
  let since: string;
  let until: string;
  if (range === "custom") {
    since = params.get("since") || daysAgoISO(7);
    until = params.get("until") || new Date().toISOString();
  } else {
    const days = RANGE_PRESETS.find((p) => p.key === range)?.days ?? 7;
    since = daysAgoISO(days);
    until = new Date().toISOString();
  }
  return {
    env: params.get("env") ?? "",
    service: params.get("service") ?? "",
    owner: params.get("owner") ?? "",
    repo: params.get("repo") ?? "",
    q: params.get("q") ?? "",
    range,
    since,
    until,
  };
}

export interface ScopePatch {
  env?: string;
  service?: string;
  owner?: string;
  repo?: string;
  q?: string;
  range?: RangePreset;
  since?: string;
  until?: string;
}

export function useScope() {
  const [params, setParams] = useSearchParams();
  const scope = useMemo(() => resolve(params), [params]);

  const setScope = useCallback(
    (patch: ScopePatch) => {
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          for (const [k, v] of Object.entries(patch)) {
            if (v) next.set(k, v);
            else next.delete(k);
          }
          // A non-custom range makes since/until implicit — drop the stale absolutes.
          if (patch.range && patch.range !== "custom") {
            next.delete("since");
            next.delete("until");
          }
          return next;
        },
        { replace: false },
      );
    },
    [setParams],
  );

  /** Clear the facet filters but keep the time range. */
  const clearFacets = useCallback(() => {
    const empty: ScopePatch = {};
    for (const k of FACET_KEYS) empty[k as FacetKey] = "";
    setScope(empty);
  }, [setScope]);

  const hasFacets = FACET_KEYS.some((k) => scope[k] !== "");

  return { scope, setScope, clearFacets, hasFacets };
}

import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import type { components } from "@/api/schema";
import { api } from "@/lib/api";

/** The timeline's faceted filter state. Empty fields mean "no constraint". */
export interface EventFilters {
  source?: string;
  env?: string;
  service?: string;
  repo?: string;
  owner?: string;
  kind?: string;
  status?: string;
  actor?: string;
  ref?: string; // exact sha/revision OR-set — scopes the timeline to one changeset
  q?: string;
  since?: string; // RFC3339 window bounds (from the global scope's time range)
  until?: string;
}

// Drop empty values so the client omits them rather than sending `env=`.
function clean(f: EventFilters): EventFilters {
  return Object.fromEntries(
    Object.entries(f).filter(([, v]) => v !== undefined && v !== ""),
  );
}

// Thin typed wrappers around the generated client. Each throws on transport or
// API error so TanStack Query surfaces it via `error`.

export function useActivity(sinceISO: string, bucket: "day" | "hour") {
  return useQuery({
    queryKey: ["stats", "activity", sinceISO, bucket],
    queryFn: async () => {
      const { data, error } = await api.GET("/api/v1/stats/activity", {
        params: { query: { since: sinceISO, bucket } },
      });
      if (error) throw new Error("activity stats request failed");
      return data;
    },
  });
}

export function useDeployStats(sinceISO: string) {
  return useQuery({
    queryKey: ["stats", "deploys", sinceISO],
    queryFn: async () => {
      const { data, error } = await api.GET("/api/v1/stats/deploys", {
        params: { query: { since: sinceISO } },
      });
      if (error) throw new Error("deploy stats request failed");
      return data;
    },
  });
}

/** Change-failure rate + MTTR over a window (DORA), overall and by env/owner. */
export function useDORA(sinceISO: string) {
  return useQuery({
    queryKey: ["dora", sinceISO],
    queryFn: async () => {
      const { data, error } = await api.GET("/api/v1/dora", {
        params: { query: { since: sinceISO } },
      });
      if (error) throw new Error("dora request failed");
      return data;
    },
  });
}

/** Logical changes (build → merge → per-env deploys, grouped by app sha). */
export function useChangesets(sinceISO: string) {
  return useQuery({
    queryKey: ["changesets", sinceISO],
    queryFn: async () => {
      const { data, error } = await api.GET("/api/v1/changesets", {
        params: { query: { since: sinceISO } },
      });
      if (error) throw new Error("changesets request failed");
      return data;
    },
  });
}

export function useRecentEvents(limit: number) {
  return useQuery({
    queryKey: ["events", "recent", limit],
    queryFn: async () => {
      const { data, error } = await api.GET("/api/v1/events", {
        params: { query: { limit } },
      });
      if (error) throw new Error("events request failed");
      return data.events ?? [];
    },
  });
}

const PAGE = 50;

/** Cursor-paginated events for the timeline (infinite scroll). */
export function useEventsInfinite(filters: EventFilters) {
  const query = clean(filters);
  return useInfiniteQuery({
    queryKey: ["events", "list", query],
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) => {
      const { data, error } = await api.GET("/api/v1/events", {
        params: { query: { ...query, cursor: pageParam, limit: PAGE } },
      });
      if (error) throw new Error("events request failed");
      return data;
    },
    getNextPageParam: (last) => last.next_cursor || undefined,
  });
}

export function useDoctor() {
  return useQuery({
    queryKey: ["doctor"],
    queryFn: async () => {
      const { data, error } = await api.GET("/api/v1/doctor", {});
      if (error) throw new Error("doctor request failed");
      return data;
    },
  });
}

export function useConfig() {
  return useQuery({
    queryKey: ["config"],
    queryFn: async () => {
      const { data, error } = await api.GET("/api/v1/config", {});
      if (error) throw new Error("config request failed");
      return data;
    },
    staleTime: 60_000,
  });
}

/** Server build version — static per process, cache aggressively. */
export function useVersion() {
  return useQuery({
    queryKey: ["version"],
    queryFn: async () => {
      const { data, error } = await api.GET("/api/v1/version", {});
      if (error) throw new Error("version request failed");
      return data;
    },
    staleTime: Infinity,
  });
}

/** Save edited rules (validated + hot-reloaded server-side). Throws the
 *  server's message on 400 so the editor can surface it. */
export async function putRules(rules: unknown[]): Promise<void> {
  // The server re-validates by compiling; cast the parsed JSON to the wire type.
  const { error } = await api.PUT("/api/v1/config/rules", {
    body: { rules: rules as components["schemas"]["Rule"][] },
  });
  if (error) throw new Error(error.error ?? "rules rejected");
}

export async function putTagPatterns(tagPatterns: string[]): Promise<void> {
  const { error } = await api.PUT("/api/v1/config/tag_patterns", {
    body: { tag_patterns: tagPatterns },
  });
  if (error) throw new Error(error.error ?? "tag patterns rejected");
}

export async function resetRules(): Promise<void> {
  const { error } = await api.DELETE("/api/v1/config/rules", {});
  if (error) throw new Error(error.error ?? "reset failed");
}

export async function resetTagPatterns(): Promise<void> {
  const { error } = await api.DELETE("/api/v1/config/tag_patterns", {});
  if (error) throw new Error(error.error ?? "reset failed");
}

export function useFacets() {
  return useQuery({
    queryKey: ["facets"],
    queryFn: async () => {
      const { data, error } = await api.GET("/api/v1/facets", {});
      if (error) throw new Error("facets request failed");
      return data;
    },
    staleTime: 60_000, // facets change slowly; don't refetch on every open
  });
}

/**
 * Services × environments deploy grid (Diff view). `at` (RFC3339) reconstructs
 * the grid as of a past instant; omitted/undefined shows current state.
 */
export function useMatrix(envs: string[], at?: string) {
  return useQuery({
    queryKey: ["matrix", envs, at ?? ""],
    queryFn: async () => {
      const query: { envs?: string; at?: string } = {};
      if (envs.length) query.envs = envs.join(",");
      if (at) query.at = at;
      const { data, error } = await api.GET("/api/v1/matrix", {
        params: { query },
      });
      if (error) throw new Error("matrix request failed");
      return data;
    },
  });
}

/** Recent deploys for one service, for the service-detail page + its metrics. */
export function useServiceDeploys(service: string | null) {
  return useQuery({
    queryKey: ["events", "service-deploys", service],
    enabled: !!service,
    queryFn: async () => {
      const { data, error } = await api.GET("/api/v1/events", {
        params: { query: { service: service!, kind: "deploy", limit: 100 } },
      });
      if (error) throw new Error("service deploys request failed");
      return data.events ?? [];
    },
  });
}

/** Ranked suspect changes for an alert (or alerts following a change). */
export function useBlast(id: string | null, window: string) {
  return useQuery({
    queryKey: ["blast", id, window],
    enabled: !!id,
    queryFn: async () => {
      const { data, error } = await api.GET("/api/v1/blast", {
        params: { query: { id: id!, window } },
      });
      if (error) throw new Error("blast request failed");
      return data;
    },
  });
}

/** The where-journey for a ref (git sha / tag / artifact), for the drawer. */
export function useWhere(ref: string | null) {
  return useQuery({
    queryKey: ["where", ref],
    enabled: !!ref,
    queryFn: async () => {
      const { data, error } = await api.GET("/api/v1/where/{ref}", {
        params: { path: { ref: ref! } },
      });
      if (error) throw new Error("where request failed");
      return data;
    },
  });
}

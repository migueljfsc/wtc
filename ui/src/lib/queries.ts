import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

/** The timeline's faceted filter state. Empty fields mean "no constraint". */
export interface EventFilters {
  env?: string;
  service?: string;
  kind?: string;
  status?: string;
  actor?: string;
  q?: string;
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

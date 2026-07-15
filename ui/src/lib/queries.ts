import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

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

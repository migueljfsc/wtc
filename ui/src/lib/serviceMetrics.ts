import type { components } from "@/api/schema";

type Event = components["schemas"]["Event"];

export interface ServiceMetrics {
  total: number;
  failed: number;
  /** failed / total, or null when there are no deploys. */
  failureRate: number | null;
  /** Deploys per week over the observed span, or null when < 2 deploys. */
  perWeek: number | null;
  /** Mean time between failed deploys, ms — null with < 2 failures. */
  mtbfMs: number | null;
}

const WEEK = 7 * 86_400_000;

/**
 * Client-side DORA-ish metrics from a service's deploy events. Lead-time
 * (build → prod deploy) needs a per-change build↔deploy join and is left to a
 * future backend metric; these four are computable from the deploy list alone.
 */
export function serviceMetrics(deploys: Event[]): ServiceMetrics {
  const total = deploys.length;
  const failedEvents = deploys.filter((d) => d.status === "failed");
  const failed = failedEvents.length;

  const times = deploys.map((d) => new Date(d.ts).getTime()).sort((a, b) => a - b);
  const span = times.length >= 2 ? times[times.length - 1] - times[0] : 0;
  const perWeek = span > 0 ? (total / span) * WEEK : null;

  const failTimes = failedEvents.map((d) => new Date(d.ts).getTime()).sort((a, b) => a - b);
  let mtbfMs: number | null = null;
  if (failTimes.length >= 2) {
    let sum = 0;
    for (let i = 1; i < failTimes.length; i++) sum += failTimes[i] - failTimes[i - 1];
    mtbfMs = sum / (failTimes.length - 1);
  }

  return {
    total,
    failed,
    failureRate: total > 0 ? failed / total : null,
    perWeek,
    mtbfMs,
  };
}

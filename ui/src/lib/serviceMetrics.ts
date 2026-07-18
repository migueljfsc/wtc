import type { components } from "@/api/schema";

type Event = components["schemas"]["Event"];

export interface ServiceMetrics {
  total: number;
  failed: number;
  /** failed / total, or null when there are no deploys. */
  failureRate: number | null;
  /**
   * Deploys per week since the first observed deploy, or null with none.
   * The window is floored at one week: a young service reads as "N this
   * week" instead of extrapolating a few closely-spaced deploys into an
   * absurd rate (3 deploys 50ms apart is not 30M/wk).
   */
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
  // Observation window: first deploy → now, never less than a week (also
  // guards future timestamps from clock skew).
  const span = total > 0 ? Math.max(Date.now() - times[0], WEEK) : 0;
  const perWeek = total > 0 ? (total / span) * WEEK : null;

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

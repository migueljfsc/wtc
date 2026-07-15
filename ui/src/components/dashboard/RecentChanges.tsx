import type { components } from "@/api/schema";
import { StatusDot } from "@/components/StatusBadge";
import { relativeTime } from "@/lib/format";

type Event = components["schemas"]["Event"];

const KIND_LABEL: Record<string, string> = {
  build: "build",
  merge: "merge",
  push: "push",
  deploy: "deploy",
  config_change: "config",
  infra_change: "infra",
  rollback: "rollback",
  alert: "alert",
  manual: "manual",
};

/** Compact recent-activity feed. Rows link out later (P8 timeline drawer). */
export function RecentChanges({ events }: { events: Event[] }) {
  if (events.length === 0) {
    return <p className="text-sm text-muted-foreground">No recent events.</p>;
  }
  return (
    <ul className="divide-y">
      {events.map((e) => (
        <li key={e.id} className="flex items-center gap-3 py-2.5 text-sm">
          <StatusDot status={e.status} />
          <span className="w-16 shrink-0 text-xs uppercase tracking-wide text-muted-foreground">
            {KIND_LABEL[e.kind] ?? e.kind}
          </span>
          <span className="min-w-0 flex-1 truncate">{e.title}</span>
          {e.env && (
            <span className="shrink-0 rounded bg-secondary px-1.5 py-0.5 font-mono text-xs text-secondary-foreground">
              {e.env}
            </span>
          )}
          <span className="w-16 shrink-0 text-right text-xs text-muted-foreground">
            {relativeTime(e.ts)}
          </span>
        </li>
      ))}
    </ul>
  );
}

import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { useChangesets } from "@/lib/queries";
import { daysAgoISO } from "@/lib/format";

const WINDOWS = [
  { label: "24h", days: 1 },
  { label: "7d", days: 7 },
  { label: "30d", days: 30 },
];

function Chips({ values, to }: { values: string[]; to?: (v: string) => string }) {
  if (values.length === 0) return <span className="text-muted-foreground">—</span>;
  const cls = "rounded-full border px-1.5 py-0.5 font-mono text-xs";
  return (
    <span className="flex flex-wrap gap-1">
      {values.map((v) =>
        to ? (
          <Link key={v} to={to(v)} className={cls + " hover:bg-accent"}>
            {v}
          </Link>
        ) : (
          <span key={v} className={cls}>
            {v}
          </span>
        ),
      )}
    </span>
  );
}

export function Changes() {
  const [days, setDays] = useState(7);
  const since = useMemo(() => daysAgoISO(days), [days]);
  const changes = useChangesets(since);

  return (
    <div className="mx-auto max-w-7xl space-y-4">
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Changes</h1>
          <p className="text-sm text-muted-foreground">
            Every build → merge → per-env deploy carrying one commit, collapsed into
            a single change. A change spans all the envs it reached.
          </p>
        </div>
        <div className="flex gap-1 rounded-md border p-0.5">
          {WINDOWS.map((w) => (
            <Button
              key={w.days}
              size="sm"
              variant={days === w.days ? "secondary" : "ghost"}
              onClick={() => setDays(w.days)}
            >
              {w.label}
            </Button>
          ))}
        </div>
      </div>

      {changes.isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {changes.error && <p className="text-sm text-destructive">Couldn’t load changes.</p>}
      {changes.data && changes.data.changesets.length === 0 && (
        <p className="text-sm text-muted-foreground">
          No changes with a resolvable commit sha in this window.
        </p>
      )}

      <div className="space-y-2">
        {changes.data?.changesets.map((cs) => {
          const status = cs.failed ? "failed" : cs.deployed ? "deployed" : "in progress";
          const statusClass = cs.failed
            ? "text-red-600 dark:text-red-500"
            : cs.deployed
              ? "text-emerald-600 dark:text-emerald-500"
              : "text-muted-foreground";
          // The change's events, filtered in the Timeline by the service(s) it
          // touched (or its repo when it maps to no service — e.g. CI-only builds).
          const filter = cs.services.length
            ? `service=${cs.services.map(encodeURIComponent).join(",")}`
            : cs.repos.length
              ? `repo=${cs.repos.map(encodeURIComponent).join(",")}`
              : "";
          return (
            <Card key={cs.sha}>
              <CardContent className="space-y-2 py-3">
                <div className="flex flex-wrap items-baseline justify-between gap-2">
                  <div className="flex min-w-0 items-baseline gap-2">
                    <Link
                      to={`/where?ref=${cs.sha}`}
                      title="Trace this change's journey"
                      className="font-mono text-sm text-primary hover:underline"
                    >
                      {cs.sha.slice(0, 7)}
                    </Link>
                    <Link
                      to={filter ? `/timeline?${filter}` : "/timeline"}
                      title="See this change's events in the timeline"
                      className="truncate text-sm hover:underline"
                    >
                      {cs.title || "—"}
                    </Link>
                  </div>
                  <span className={"text-xs font-medium " + statusClass}>{status}</span>
                </div>
                <div className="grid gap-2 text-xs text-muted-foreground sm:grid-cols-[auto_1fr] sm:gap-x-4">
                  <span className="pt-0.5">services</span>
                  <Chips values={cs.services} to={(s) => `/services?service=${encodeURIComponent(s)}`} />
                  <span className="pt-0.5">envs</span>
                  <Chips values={cs.envs} />
                  {cs.owners.length > 0 && (
                    <>
                      <span className="pt-0.5">teams</span>
                      <Chips values={cs.owners} />
                    </>
                  )}
                </div>
                <p className="text-xs text-muted-foreground">
                  {cs.events} events · latest {new Date(cs.last_ts).toLocaleString()}
                </p>
              </CardContent>
            </Card>
          );
        })}
      </div>
    </div>
  );
}

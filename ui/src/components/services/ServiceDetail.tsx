import { useMemo } from "react";
import { Link } from "react-router-dom";
import type { components } from "@/api/schema";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { StatusDot } from "@/components/StatusBadge";
import { useMatrix, useServiceDeploys, useFacets } from "@/lib/queries";
import { serviceMetrics } from "@/lib/serviceMetrics";
import { isEphemeral, orderEnvs } from "@/lib/envOrder";
import { duration, relativeTime } from "@/lib/format";

type Cell = components["schemas"]["MatrixCell"];

function cellVersion(c: Cell): string {
  return c.artifact || (c.ref ? c.ref.slice(0, 7) : "—");
}

function Tile({ label, value }: { label: string; value: string }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardDescription>{label}</CardDescription>
        <CardTitle className="text-2xl tabular-nums">{value}</CardTitle>
      </CardHeader>
    </Card>
  );
}

export interface ServiceScope {
  env: string;
  owner: string;
  since: string;
  until: string;
  at?: string;
}

export function ServiceDetail({ service, scope }: { service: string; scope: ServiceScope }) {
  const facets = useFacets();
  // Envs shown = non-ephemeral, narrowed to the scope-bar env facet when set.
  const envs = useMemo(() => {
    const pick = new Set(scope.env ? scope.env.split(",").filter(Boolean) : []);
    const base = (facets.data?.envs ?? []).filter(
      (e) => !isEphemeral(e) && (pick.size === 0 || pick.has(e)),
    );
    return orderEnvs(base);
  }, [facets.data, scope.env]);
  const matrix = useMatrix(envs, scope.at, { owner: scope.owner });
  const deploys = useServiceDeploys(service, {
    env: scope.env,
    since: scope.since,
    until: scope.until,
  });

  const row = matrix.data?.services.find((s) => s.service === service);
  const events = useMemo(() => deploys.data ?? [], [deploys.data]);
  const m = useMemo(() => serviceMetrics(events), [events]);
  const failures = events.filter((e) => e.status === "failed").slice(0, 5);

  return (
    <div className="space-y-6">
      {/* Current version across environments */}
      <section>
        <h2 className="mb-2 text-sm font-medium text-muted-foreground">
          {scope.at ? "Deployed as of the range end" : "Deployed now"}
        </h2>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {envs.map((env) => {
            const cell = row?.cells[env];
            return (
              <Card key={env}>
                <CardHeader className="pb-2">
                  <CardDescription className="font-mono">{env}</CardDescription>
                  {cell ? (
                    <CardTitle className="truncate text-sm">
                      <Link
                        to={`/where?ref=${encodeURIComponent(cell.ref || cell.artifact || "")}`}
                        className="font-mono hover:underline"
                        title={cell.artifact || cell.ref}
                      >
                        {cellVersion(cell)}
                      </Link>
                    </CardTitle>
                  ) : (
                    <CardTitle className="text-sm text-muted-foreground">not deployed</CardTitle>
                  )}
                </CardHeader>
                {cell && (
                  <CardContent className="pt-0 text-xs text-muted-foreground">
                    {relativeTime(cell.ts)}
                  </CardContent>
                )}
              </Card>
            );
          })}
        </div>
      </section>

      {/* Metrics */}
      <section>
        <h2 className="mb-2 text-sm font-medium text-muted-foreground">
          Metrics <span className="font-normal">· last {events.length} deploys</span>
        </h2>
        <div className="grid gap-3 sm:grid-cols-4">
          <Tile label="Deploy freq" value={m.perWeek != null ? `${m.perWeek.toFixed(1)}/wk` : "—"} />
          <Tile label="Failures" value={String(m.failed)} />
          <Tile label="Failure rate" value={m.failureRate != null ? `${Math.round(m.failureRate * 100)}%` : "—"} />
          <Tile label="MTBF" value={m.mtbfMs != null ? duration(m.mtbfMs) : "—"} />
        </div>
        <p className="mt-1 text-xs text-muted-foreground">
          Lead-time (build → prod) needs a build↔deploy join — a future metric.
        </p>
      </section>

      {/* Recent failures */}
      {failures.length > 0 && (
        <section>
          <h2 className="mb-2 text-sm font-medium text-muted-foreground">Recent failures</h2>
          <ul className="divide-y rounded-lg border">
            {failures.map((e) => (
              <li key={e.id} className="flex items-center gap-3 px-3 py-2 text-sm">
                <StatusDot status={e.status} />
                <span className="min-w-0 flex-1 truncate">{e.title}</span>
                {e.env && <span className="font-mono text-xs text-muted-foreground">{e.env}</span>}
                <span className="text-xs text-muted-foreground">{relativeTime(e.ts)}</span>
              </li>
            ))}
          </ul>
        </section>
      )}

      {/* Deploy history */}
      <section>
        <h2 className="mb-2 text-sm font-medium text-muted-foreground">Deploy history</h2>
        {deploys.isLoading ? (
          <p className="text-sm text-muted-foreground">Loading…</p>
        ) : events.length === 0 ? (
          <p className="text-sm text-muted-foreground">No deploys recorded.</p>
        ) : (
          <ul className="divide-y rounded-lg border">
            {events.slice(0, 20).map((e) => (
              <li key={e.id} className="flex items-center gap-3 px-3 py-2 text-sm">
                <StatusDot status={e.status} />
                {e.env && (
                  <span className="w-20 shrink-0 font-mono text-xs text-muted-foreground">{e.env}</span>
                )}
                <span className="min-w-0 flex-1 truncate font-mono text-xs">
                  {e.artifact || e.ref?.slice(0, 7) || e.title}
                </span>
                <span className="text-xs text-muted-foreground">{relativeTime(e.ts)}</span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}

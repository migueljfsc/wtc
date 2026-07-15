import type { components } from "@/api/schema";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useConfig, useDoctor } from "@/lib/queries";
import { bytes, relativeTime } from "@/lib/format";

type Rule = components["schemas"]["Rule"];

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

function SourceHealth() {
  const doctor = useDoctor();
  if (doctor.isLoading) return <p className="text-sm text-muted-foreground">Loading…</p>;
  if (doctor.error || !doctor.data)
    return <p className="text-sm text-destructive">Couldn’t load source health.</p>;
  const d = doctor.data;

  return (
    <div className="space-y-3">
      <div className="grid gap-3 sm:grid-cols-4">
        <Tile label="Total events" value={d.total_events.toLocaleString()} />
        <Tile label="DB size" value={bytes(d.db_size_bytes)} />
        <Tile label="Unmapped (24h)" value={d.unmapped_24h.toLocaleString()} />
        <Tile label="Clock skew (24h)" value={d.clock_skew_24h.toLocaleString()} />
      </div>

      <div className="overflow-x-auto rounded-lg border">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b bg-muted/40 text-left">
              <th className="px-3 py-2 font-medium">Source</th>
              <th className="px-3 py-2 font-medium">Events (24h)</th>
              <th className="px-3 py-2 font-medium">Last event</th>
            </tr>
          </thead>
          <tbody>
            {d.sources.length === 0 ? (
              <tr>
                <td colSpan={3} className="px-3 py-2 text-muted-foreground">
                  No sources have reported yet.
                </td>
              </tr>
            ) : (
              d.sources.map((s) => (
                <tr key={s.source} className="border-b last:border-0">
                  <td className="px-3 py-2 font-mono">{s.source}</td>
                  <td className="px-3 py-2 tabular-nums">{s.count_24h}</td>
                  <td className="px-3 py-2 text-muted-foreground">{relativeTime(s.last_ts)}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {d.unmapped_samples && d.unmapped_samples.length > 0 && (
        <p className="text-xs text-muted-foreground">
          Unmapped samples: {d.unmapped_samples.join(" · ")}
        </p>
      )}
    </div>
  );
}

function RuleView({ rule }: { rule: Rule }) {
  const match = Object.entries(rule.match).filter(([, v]) => v != null && String(v).length > 0);
  const set = Object.entries(rule.set).filter(([, v]) => v != null && String(v).length > 0);
  return (
    <div className="flex flex-wrap items-center gap-2 rounded-md border px-3 py-2 text-xs">
      <span className="text-muted-foreground">when</span>
      {match.length === 0 ? (
        <span className="italic text-muted-foreground">any</span>
      ) : (
        match.map(([k, v]) => (
          <span key={k} className="rounded bg-muted px-1.5 py-0.5 font-mono">
            {k}={Array.isArray(v) ? v.join("|") : String(v)}
          </span>
        ))
      )}
      <span className="text-muted-foreground">→ set</span>
      {set.map(([k, v]) => (
        <span key={k} className="rounded bg-secondary px-1.5 py-0.5 font-mono text-secondary-foreground">
          {k}={String(v)}
        </span>
      ))}
    </div>
  );
}

function ConfigView() {
  const cfg = useConfig();
  if (cfg.isLoading) return <p className="text-sm text-muted-foreground">Loading…</p>;
  if (cfg.error || !cfg.data)
    return <p className="text-sm text-destructive">Couldn’t load config.</p>;

  return (
    <div className="space-y-4">
      <div>
        <h3 className="mb-2 text-sm font-medium">Inference rules ({cfg.data.rules.length})</h3>
        {cfg.data.rules.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No rules configured — events land with <code>env=""</code> (see source health).
          </p>
        ) : (
          <div className="space-y-1.5">
            {cfg.data.rules.map((r, i) => (
              <RuleView key={i} rule={r} />
            ))}
          </div>
        )}
      </div>

      <div>
        <h3 className="mb-2 text-sm font-medium">Tag patterns ({cfg.data.tag_patterns.length})</h3>
        <ul className="space-y-1">
          {cfg.data.tag_patterns.map((p, i) => (
            <li key={i} className="rounded bg-muted px-2 py-1 font-mono text-xs">
              {p}
            </li>
          ))}
        </ul>
      </div>

      <p className="text-xs text-muted-foreground">
        Read-only for now — in-UI editing (with live re-routing) is the next step.
      </p>
    </div>
  );
}

export function Settings() {
  return (
    <div className="mx-auto max-w-4xl space-y-8">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>
        <p className="text-sm text-muted-foreground">Source health and the normalization config in use.</p>
      </div>

      <section>
        <h2 className="mb-3 text-sm font-medium text-muted-foreground">Source health</h2>
        <SourceHealth />
      </section>

      <section>
        <h2 className="mb-3 text-sm font-medium text-muted-foreground">Normalization</h2>
        <CardWrap>
          <ConfigView />
        </CardWrap>
      </section>
    </div>
  );
}

function CardWrap({ children }: { children: React.ReactNode }) {
  return (
    <Card>
      <CardContent className="pt-6">{children}</CardContent>
    </Card>
  );
}

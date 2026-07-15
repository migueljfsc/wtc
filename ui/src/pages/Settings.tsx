import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { components } from "@/api/schema";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import {
  putRules,
  putTagPatterns,
  resetRules,
  resetTagPatterns,
  useConfig,
  useDoctor,
} from "@/lib/queries";
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

function OverrideBadge({ on }: { on: boolean }) {
  return on ? (
    <span className="rounded-full bg-amber-500/15 px-2 py-0.5 text-[10px] font-medium uppercase text-amber-600 dark:text-amber-500">
      overridden
    </span>
  ) : (
    <span className="text-[10px] uppercase text-muted-foreground">from file</span>
  );
}

function ConfigView() {
  const qc = useQueryClient();
  const cfg = useConfig();
  const [editing, setEditing] = useState(false);
  const [rulesText, setRulesText] = useState("");
  const [tagsText, setTagsText] = useState("");

  function startEdit() {
    if (!cfg.data) return;
    setRulesText(JSON.stringify(cfg.data.rules, null, 2));
    setTagsText(JSON.stringify(cfg.data.tag_patterns, null, 2));
    save.reset();
    setEditing(true);
  }

  const save = useMutation({
    mutationFn: async () => {
      let rules: unknown, tags: unknown;
      try {
        rules = JSON.parse(rulesText);
      } catch {
        throw new Error("Rules: invalid JSON");
      }
      try {
        tags = JSON.parse(tagsText);
      } catch {
        throw new Error("Tag patterns: invalid JSON");
      }
      if (!Array.isArray(rules) || !Array.isArray(tags))
        throw new Error("Rules and tag patterns must be JSON arrays");
      await putRules(rules);
      await putTagPatterns(tags as string[]);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["config"] });
      setEditing(false);
    },
  });

  const reset = useMutation({
    mutationFn: async () => {
      await resetRules();
      await resetTagPatterns();
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["config"] });
      setEditing(false);
    },
  });

  if (cfg.isLoading) return <p className="text-sm text-muted-foreground">Loading…</p>;
  if (cfg.error || !cfg.data)
    return <p className="text-sm text-destructive">Couldn’t load config.</p>;

  if (editing) {
    return (
      <div className="space-y-4">
        <div>
          <h3 className="mb-1 text-sm font-medium">Inference rules</h3>
          <Textarea rows={12} value={rulesText} onChange={(e) => setRulesText(e.target.value)} />
        </div>
        <div>
          <h3 className="mb-1 text-sm font-medium">Tag patterns</h3>
          <Textarea rows={4} value={tagsText} onChange={(e) => setTagsText(e.target.value)} />
        </div>
        {save.error && <p className="text-sm text-destructive">{(save.error as Error).message}</p>}
        <div className="flex gap-2">
          <Button size="sm" onClick={() => save.mutate()} disabled={save.isPending}>
            {save.isPending ? "Saving…" : "Save & apply"}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setEditing(false)}>
            Cancel
          </Button>
          <Button
            size="sm"
            variant="outline"
            className="ml-auto"
            onClick={() => reset.mutate()}
            disabled={reset.isPending}
          >
            Reset to file
          </Button>
        </div>
        <p className="text-xs text-muted-foreground">
          Saved rules are validated, persisted, and hot-reloaded — the next ingested
          event is routed by them, no restart.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-medium">Inference rules ({cfg.data.rules.length})</h3>
          <OverrideBadge on={cfg.data.rules_overridden} />
        </div>
        <Button size="sm" variant="outline" onClick={startEdit}>
          Edit
        </Button>
      </div>
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

      <div className="flex items-center gap-2">
        <h3 className="text-sm font-medium">Tag patterns ({cfg.data.tag_patterns.length})</h3>
        <OverrideBadge on={cfg.data.tag_patterns_overridden} />
      </div>
      <ul className="space-y-1">
        {cfg.data.tag_patterns.map((p, i) => (
          <li key={i} className="rounded bg-muted px-2 py-1 font-mono text-xs">
            {p}
          </li>
        ))}
      </ul>
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

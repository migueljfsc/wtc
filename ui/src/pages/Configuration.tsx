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
type ConfigResponse = components["schemas"]["ConfigResponse"];
type SourceHealthRow = components["schemas"]["SourceHealth"];

// ---------- shared bits ----------

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

/** Ingest-mode badge: green when at least one path is live. */
function ModeBadge({ modes }: { modes: string[] }) {
  if (modes.length === 0)
    return (
      <span className="rounded-full bg-muted px-2 py-0.5 text-[10px] font-medium uppercase text-muted-foreground">
        off
      </span>
    );
  return (
    <span className="rounded-full bg-emerald-500/15 px-2 py-0.5 text-[10px] font-medium uppercase text-emerald-600 dark:text-emerald-500">
      {modes.join(" + ")}
    </span>
  );
}

/** Last-event chip client-joined from doctor — config says wired, this says alive. */
function HealthChip({ health }: { health?: SourceHealthRow }) {
  if (!health) return null;
  return (
    <span className="ml-auto text-[11px] text-muted-foreground">
      last event {relativeTime(health.last_ts)} · {health.count_24h} in 24h
    </span>
  );
}

/** One label/value line inside a source card; values are pre-masked by the server. */
function KV({ k, v, mono = true }: { k: string; v: string; mono?: boolean }) {
  if (!v) return null;
  return (
    <div className="flex items-baseline gap-2 text-xs">
      <span className="w-32 shrink-0 text-muted-foreground">{k}</span>
      <span className={mono ? "font-mono break-all" : ""}>{v}</span>
    </div>
  );
}

/** Compact one-line rendering of a scope match, e.g. "ns=prod-* kind=HelmRelease". */
function scopeMatch(m: components["schemas"]["ConfigScopeMatch"]): string {
  const parts: string[] = [];
  if (m.namespace) parts.push(`ns=${m.namespace}`);
  if (m.object_name) parts.push(`name=${m.object_name}`);
  if (m.object_kind) parts.push(`kind=${m.object_kind}`);
  if (m.cluster) parts.push(`cluster=${m.cluster}`);
  if (m.project) parts.push(`project=${m.project}`);
  return parts.join(" ");
}

/** Ingest allow/deny lists; renders nothing when scope is unset (ingest all). */
function ScopeKV({ scope }: { scope: components["schemas"]["ConfigScope"] }) {
  const { allow, deny } = scope;
  if (allow.length === 0 && deny.length === 0) return null;
  return (
    <>
      {allow.length > 0 && <KV k="scope allow" v={allow.map(scopeMatch).join("  ·  ")} />}
      {deny.length > 0 && <KV k="scope deny" v={deny.map(scopeMatch).join("  ·  ")} />}
    </>
  );
}

function SourceCard({
  name,
  modes,
  health,
  children,
}: {
  name: string;
  modes: string[];
  health?: SourceHealthRow;
  children: React.ReactNode;
}) {
  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center gap-2">
          <CardTitle className="font-mono text-sm">{name}</CardTitle>
          <ModeBadge modes={modes} />
          <HealthChip health={health} />
        </div>
      </CardHeader>
      <CardContent className="space-y-1.5">{children}</CardContent>
    </Card>
  );
}

// ---------- sources ----------

function Sources({ cfg }: { cfg: ConfigResponse }) {
  const doctor = useDoctor();
  const healthBy = new Map((doctor.data?.sources ?? []).map((s) => [s.source, s]));
  const gh = cfg.sources.github;
  const gl = cfg.sources.gitlab;
  const flux = cfg.sources.flux;
  const argo = cfg.sources.argocd;

  return (
    <div className="grid gap-3 lg:grid-cols-2">
      <SourceCard
        name="github"
        modes={[gh.poller_enabled && "poller", gh.webhook_secret && "webhook"].filter(Boolean) as string[]}
        health={healthBy.get("github")}
      >
        <KV k="api token" v={gh.api_token} />
        <KV k="webhook secret" v={gh.webhook_secret} />
        {gh.poller_enabled && <KV k="poll interval" v={gh.poll_interval} />}
        {gh.poller_enabled && (
          <KV k="repos" v={gh.repos.length > 0 ? gh.repos.join(", ") : "auto-discover (all accessible)"} />
        )}
        {gh.poller_enabled && <KV k="backfill" v={gh.backfill} />}
        <KV k="infra path" v={gh.infra_path} />
      </SourceCard>

      <SourceCard
        name="gitlab"
        modes={[gl.poller_enabled && "poller", gl.webhook_secret && "webhook"].filter(Boolean) as string[]}
        health={healthBy.get("gitlab")}
      >
        <KV k="base url" v={gl.base_url} />
        <KV k="api token" v={gl.api_token} />
        <KV k="webhook secret" v={gl.webhook_secret} />
        {gl.poller_enabled && <KV k="poll interval" v={gl.poll_interval} />}
        {gl.projects.length > 0 && <KV k="projects" v={gl.projects.join(", ")} />}
        {gl.poller_enabled && <KV k="backfill" v={gl.backfill} />}
        <KV k="infra path" v={gl.infra_path} />
      </SourceCard>

      <SourceCard name="flux" modes={flux.hmac_key ? ["webhook"] : []} health={healthBy.get("flux")}>
        <KV k="hmac key" v={flux.hmac_key} />
        <KV k="suppression" v={flux.suppression_window} />
        <ScopeKV scope={flux.scope} />
      </SourceCard>

      <SourceCard name="argocd" modes={argo.webhook_secret ? ["webhook"] : []} health={healthBy.get("argocd")}>
        <KV k="webhook secret" v={argo.webhook_secret} />
        <KV k="suppression" v={argo.suppression_window} />
        <ScopeKV scope={argo.scope} />
      </SourceCard>

      {cfg.sources.webhooks.map((wh) => (
        <SourceCard key={wh.name} name={wh.name} modes={["webhook"]} health={healthBy.get(wh.name)}>
          {wh.preset && <KV k="preset" v={wh.preset} />}
          <KV k="auth" v={wh.auth.mode + (wh.auth.header ? ` (${wh.auth.header})` : "")} />
          {wh.auth.algo && <KV k="hmac algo" v={wh.auth.algo} />}
          <KV k="secret" v={wh.auth.secret} />
          <KV k="dedup key" v={wh.dedup_key} />
          {Object.entries(wh.mapping).map(([field, tpl]) => (
            <KV key={field} k={`map ${field}`} v={tpl} />
          ))}
          {Object.entries(wh.facts ?? {}).map(([fact, tpl]) => (
            <KV key={fact} k={`fact ${fact}`} v={tpl} />
          ))}
        </SourceCard>
      ))}
    </div>
  );
}

// ---------- storage & server / jobs ----------

function StorageServer({ cfg }: { cfg: ConfigResponse }) {
  const s = cfg.storage;
  const location = s.host ? `${s.host}:${s.port}/${s.database}` : "";
  return (
    <div className="grid gap-3 lg:grid-cols-2">
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm">Storage</CardTitle>
        </CardHeader>
        <CardContent className="space-y-1.5">
          <KV k="backend" v={s.backend} />
          <KV k="database" v={location} />
          <KV k="dsn" v={s.dsn} />
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm">Server</CardTitle>
        </CardHeader>
        <CardContent className="space-y-1.5">
          <KV k="listen" v={cfg.server.listen} />
          <KV k="cors origins" v={cfg.server.cors_origins.join(", ")} />
          <KV k="api tokens" v={`${cfg.auth.api_tokens.length} configured`} mono={false} />
          {cfg.metrics.listen && <KV k="metrics listener" v={`${cfg.metrics.listen} (unauthenticated)`} />}
          {cfg.server.capture_enabled && (
            <p className="rounded-md bg-amber-500/10 px-2 py-1 text-xs text-amber-600 dark:text-amber-500">
              Capture mode is ON — raw ingest bodies are written to disk (dev only).
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function Jobs({ cfg }: { cfg: ConfigResponse }) {
  return (
    <div className="grid gap-3 lg:grid-cols-2">
      <Card>
        <CardHeader className="pb-3">
          <div className="flex items-center gap-2">
            <CardTitle className="text-sm">Retention</CardTitle>
            <ModeBadge modes={cfg.retention.enabled ? ["on"] : []} />
          </div>
        </CardHeader>
        <CardContent className="space-y-1.5">
          {cfg.retention.enabled ? (
            <>
              <KV k="keep" v={cfg.retention.keep} />
              <KV k="ephemeral envs" v={`${cfg.retention.ephemeral_env_pattern} → keep ${cfg.retention.ephemeral_keep}`} />
              <KV k="prune every" v={cfg.retention.interval ?? ""} />
            </>
          ) : (
            <p className="text-xs text-muted-foreground">Off — nothing is pruned.</p>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="pb-3">
          <div className="flex items-center gap-2">
            <CardTitle className="text-sm">Slack digest</CardTitle>
            <ModeBadge modes={cfg.digest.enabled ? ["on"] : []} />
          </div>
        </CardHeader>
        <CardContent className="space-y-1.5">
          {cfg.digest.enabled ? (
            <>
              <KV k="every" v={cfg.digest.interval} />
              <KV k="window" v={cfg.digest.window} />
              <KV k="slack webhook" v={cfg.digest.slack_webhook} />
            </>
          ) : (
            <p className="text-xs text-muted-foreground">Off.</p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

// ---------- source health (doctor) ----------

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

// ---------- normalization (live-editable) ----------

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

function Normalization() {
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

// ---------- page ----------

export function Configuration() {
  const cfg = useConfig();

  return (
    <div className="mx-auto max-w-7xl space-y-8">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Configuration</h1>
        <p className="text-sm text-muted-foreground">
          What this wtc has configured — ingest sources, storage, jobs, and the
          normalization rules in use. Secrets are masked by the server and never
          leave it.
        </p>
      </div>

      {cfg.isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {(cfg.error || (!cfg.isLoading && !cfg.data)) && (
        <p className="text-sm text-destructive">Couldn’t load configuration.</p>
      )}

      {cfg.data && (
        <>
          <section>
            <h2 className="mb-3 text-sm font-medium text-muted-foreground">Sources</h2>
            <Sources cfg={cfg.data} />
          </section>

          <section>
            <h2 className="mb-3 text-sm font-medium text-muted-foreground">Storage &amp; server</h2>
            <StorageServer cfg={cfg.data} />
          </section>

          <section>
            <h2 className="mb-3 text-sm font-medium text-muted-foreground">Retention &amp; jobs</h2>
            <Jobs cfg={cfg.data} />
          </section>

          <section>
            <h2 className="mb-3 text-sm font-medium text-muted-foreground">Normalization</h2>
            <Card>
              <CardContent className="pt-6">
                <Normalization />
              </CardContent>
            </Card>
          </section>

          <section>
            <h2 className="mb-3 text-sm font-medium text-muted-foreground">Source health</h2>
            <SourceHealth />
          </section>
        </>
      )}
    </div>
  );
}

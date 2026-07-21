import { useMemo, useState } from "react";
import { CalendarClock } from "lucide-react";
import { useFacets, useMatrix } from "@/lib/queries";
import { isEphemeral, orderEnvs } from "@/lib/envOrder";
import { DiffMatrix } from "@/components/diff/DiffMatrix";
import { cn } from "@/lib/utils";

export function Diff() {
  const facets = useFacets();
  const allEnvs = useMemo(
    () => orderEnvs((facets.data?.envs ?? []).filter((e) => !isEphemeral(e))),
    [facets.data],
  );

  // Default selection = every non-ephemeral env, in promotion order. Once the
  // user deselects, keep their choice (don't clobber on refetch).
  const [chosen, setChosen] = useState<string[] | null>(null);
  const selected = chosen ?? allEnvs;

  // Point-in-time: an empty input means "current state". datetime-local is in
  // the viewer's local zone; new Date(...).toISOString() converts to the UTC
  // RFC3339 instant the API expects.
  const [atLocal, setAtLocal] = useState("");
  const atISO = atLocal ? new Date(atLocal).toISOString() : undefined;

  const matrix = useMatrix(selected, atISO);

  function toggle(env: string) {
    const set = new Set(selected);
    if (set.has(env)) set.delete(env);
    else set.add(env);
    setChosen(orderEnvs([...set]));
  }

  return (
    <div className="mx-auto max-w-7xl space-y-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Diff</h1>
        <p className="text-sm text-muted-foreground">
          {atISO ? "The version of every service that was running" : "Current version of every service"}{" "}
          across environments. Drift and not-yet-promoted services are flagged;
          the rightmost column is the promotion target.
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <label htmlFor="asof" className="text-sm font-medium">
          As of
        </label>
        <div className="relative">
          <CalendarClock className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <input
            id="asof"
            type="datetime-local"
            value={atLocal}
            onChange={(e) => setAtLocal(e.target.value)}
            // Open the native picker from anywhere on the field, not just the
            // tiny calendar glyph.
            onClick={(e) => {
              try {
                e.currentTarget.showPicker();
              } catch {
                // showPicker() unsupported on older browsers — the field still works.
              }
            }}
            className="h-9 min-w-[15rem] cursor-pointer rounded-md border bg-background pl-9 pr-3 text-sm hover:bg-accent"
          />
        </div>
        {atLocal ? (
          <button
            onClick={() => setAtLocal("")}
            className="rounded-md border px-3 py-1.5 text-sm text-muted-foreground hover:bg-accent"
          >
            Now
          </button>
        ) : (
          <span className="text-sm text-muted-foreground">showing current state</span>
        )}
      </div>

      {allEnvs.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-xs text-muted-foreground">Environments:</span>
          {allEnvs.map((e) => {
            const on = selected.includes(e);
            return (
              <button
                key={e}
                onClick={() => toggle(e)}
                className={cn(
                  "rounded-full border px-2.5 py-0.5 font-mono text-xs transition-colors",
                  on
                    ? "border-primary bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-accent",
                )}
              >
                {e}
              </button>
            );
          })}
        </div>
      )}

      {matrix.isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {matrix.error && <p className="text-sm text-destructive">Couldn’t load the matrix.</p>}
      {matrix.data && selected.length > 0 ? (
        <DiffMatrix matrix={matrix.data} />
      ) : (
        !matrix.isLoading && (
          <p className="text-sm text-muted-foreground">Select at least one environment.</p>
        )
      )}

      <div className="space-y-1 text-xs text-muted-foreground">
        <p>
          An <span className="text-amber-600 dark:text-amber-500">amber</span> cell is behind
          the newest deploy in its row — the laggard to promote.
        </p>
        <p>
          State:{" "}
          <span className="text-emerald-600 dark:text-emerald-500">in sync</span> (all match) ·{" "}
          <span className="text-amber-600 dark:text-amber-500">drift</span> (versions differ) ·{" "}
          partial (not deployed in every env).
        </p>
        <p>
          <sup>rev</sup> = revision-only (no artifact reported). Click a version to trace it —
          OCI-artifact and chart-version cells carry no git lineage and aren’t linked.
        </p>
      </div>
    </div>
  );
}

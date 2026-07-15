import { useMemo, useState } from "react";
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

  const matrix = useMatrix(selected);

  function toggle(env: string) {
    const set = new Set(selected);
    if (set.has(env)) set.delete(env);
    else set.add(env);
    setChosen(orderEnvs([...set]));
  }

  return (
    <div className="mx-auto max-w-5xl space-y-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Diff</h1>
        <p className="text-sm text-muted-foreground">
          Current version of every service across environments. Drift and
          not-yet-promoted services are flagged; the rightmost column is the
          promotion target.
        </p>
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

      <p className="text-xs text-muted-foreground">
        <span className="text-amber-600 dark:text-amber-500">drift</span> = versions differ ·{" "}
        <span className="text-muted-foreground">partial</span> = not deployed everywhere ·{" "}
        <span className="text-emerald-600 dark:text-emerald-500">in sync</span> = all match ·{" "}
        <sup>rev</sup> = revision-only (no artifact reported). Click a version to trace it.
      </p>
    </div>
  );
}

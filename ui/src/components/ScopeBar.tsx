import { useEffect, useState } from "react";
import { Clock, Search } from "lucide-react";
import { useScope, RANGE_PRESETS } from "@/lib/scope";
import { useFacets } from "@/lib/queries";
import { useDebouncedValue } from "@/lib/useDebouncedValue";
import { MultiSelect } from "@/components/timeline/MultiSelect";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

/**
 * The global scope bar: one shared filter (time range + facets + search) that
 * every analytical page reads from the URL. Rendered once in the app shell.
 * `disabled` greys it on pages that don't scope (Where / Configuration /
 * Settings) so the active filter is still visible but not editable.
 */
export function ScopeBar({ disabled = false }: { disabled?: boolean }) {
  const { scope, setScope, clearFacets, hasFacets } = useScope();
  const facets = useFacets();

  // Debounce the search box so typing doesn't churn the URL/refetch per key.
  const [search, setSearch] = useState(scope.q);
  // Reflect an externally-set q (e.g. clicking a change) back into the box —
  // the "adjust state when a value changes" pattern, no effect needed. The
  // debounce always pushes the current search, so this never clobbers typing.
  const [prevQ, setPrevQ] = useState(scope.q);
  if (scope.q !== prevQ) {
    setPrevQ(scope.q);
    setSearch(scope.q);
  }
  const debounced = useDebouncedValue(search, 350);
  useEffect(() => {
    if (debounced !== scope.q) setScope({ q: debounced });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debounced]);

  return (
    <div
      aria-disabled={disabled}
      className={cn(
        "flex flex-wrap items-center gap-2 border-b bg-card/40 px-4 py-2",
        disabled && "pointer-events-none select-none opacity-50",
      )}
    >
      <div className="relative w-56 max-w-full">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          className="h-8 pl-8"
          placeholder="Search all changes…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      </div>

      <MultiSelect label="env" value={scope.env} options={facets.data?.envs ?? []} onChange={(v) => setScope({ env: v })} />
      <MultiSelect label="service" value={scope.service} options={facets.data?.services ?? []} onChange={(v) => setScope({ service: v })} searchable />
      <MultiSelect label="owner" value={scope.owner} options={facets.data?.owners ?? []} onChange={(v) => setScope({ owner: v })} searchable />
      <MultiSelect label="repo" value={scope.repo} options={facets.data?.repos ?? []} onChange={(v) => setScope({ repo: v })} searchable />

      {hasFacets && (
        <button
          onClick={clearFacets}
          className="text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
        >
          Clear
        </button>
      )}

      {/* Time range — right-aligned, Grafana-style. */}
      <div className="ml-auto flex items-center gap-0.5 rounded-md border p-0.5">
        <Clock className="mx-1 size-3.5 text-muted-foreground" />
        {RANGE_PRESETS.map((p) => (
          <button
            key={p.key}
            onClick={() => setScope({ range: p.key })}
            className={cn(
              "rounded px-2 py-0.5 text-xs font-medium transition-colors",
              scope.range === p.key
                ? "bg-secondary text-secondary-foreground"
                : "text-muted-foreground hover:bg-accent",
            )}
          >
            {p.label}
          </button>
        ))}
      </div>
    </div>
  );
}

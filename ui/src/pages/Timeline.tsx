import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { Star, X } from "lucide-react";
import type { components } from "@/api/schema";
import { FilterBar } from "@/components/timeline/FilterBar";
import { EventRow } from "@/components/timeline/EventRow";
import { EventDrawer } from "@/components/timeline/EventDrawer";
import { Button } from "@/components/ui/button";
import { useEventsInfinite, useFacets, type EventFilters } from "@/lib/queries";
import { useScope } from "@/lib/scope";
import {
  deleteFilter,
  loadSavedFilters,
  saveFilter,
  type SavedFilter,
} from "@/lib/savedFilters";

type Event = components["schemas"]["Event"];
type AdvancedKey = "source" | "kind" | "status" | "actor";
const ADVANCED_KEYS: AdvancedKey[] = ["source", "kind", "status", "actor"];

export function Timeline() {
  const { scope } = useScope();
  const [params, setParams] = useSearchParams();
  const [selected, setSelected] = useState<Event | null>(null);
  const [saved, setSaved] = useState<SavedFilter[]>(() => loadSavedFilters());

  // Advanced facets + the changeset `ref` live in the URL, refining the global
  // scope (env/service/owner/repo/time/search, which come from the scope bar).
  const advanced = useMemo<EventFilters>(() => {
    const f: EventFilters = {};
    for (const k of ADVANCED_KEYS) {
      const v = params.get(k);
      if (v) f[k] = v;
    }
    return f;
  }, [params]);
  const ref = params.get("ref") || undefined;

  const effective = useMemo<EventFilters>(
    () => ({
      env: scope.env || undefined,
      service: scope.service || undefined,
      owner: scope.owner || undefined,
      repo: scope.repo || undefined,
      q: scope.q || undefined,
      since: scope.since,
      until: scope.until,
      ref,
      ...advanced,
    }),
    [scope, advanced, ref],
  );

  const facets = useFacets();
  const events = useEventsInfinite(effective);
  const rows = useMemo(
    () => events.data?.pages.flatMap((p) => p.events ?? []) ?? [],
    [events.data],
  );

  // Infinite scroll: load the next page when the sentinel scrolls into view.
  const sentinel = useRef<HTMLDivElement>(null);
  const { fetchNextPage, hasNextPage, isFetchingNextPage } = events;
  useEffect(() => {
    const el = sentinel.current;
    if (!el || !hasNextPage) return;
    const obs = new IntersectionObserver((entries) => {
      if (entries[0].isIntersecting && !isFetchingNextPage) fetchNextPage();
    });
    obs.observe(el);
    return () => obs.disconnect();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

  const patchParams = useCallback(
    (mut: (p: URLSearchParams) => void) => {
      setParams((prev) => {
        const n = new URLSearchParams(prev);
        mut(n);
        return n;
      });
    },
    [setParams],
  );

  const onSelect = useCallback(
    (key: AdvancedKey, value: string) =>
      patchParams((p) => {
        if (value) p.set(key, value);
        else p.delete(key);
      }),
    [patchParams],
  );

  const refineActive = ADVANCED_KEYS.some((k) => params.get(k)) || !!ref;

  const onClear = useCallback(
    () =>
      patchParams((p) => {
        [...ADVANCED_KEYS, "ref"].forEach((k) => p.delete(k));
      }),
    [patchParams],
  );

  const onSave = useCallback(() => {
    const name = window.prompt("Name this refinement:")?.trim();
    if (name) setSaved(saveFilter(name, advanced));
  }, [advanced]);

  const onApply = useCallback(
    (f: SavedFilter) =>
      patchParams((p) => {
        ADVANCED_KEYS.forEach((k) => p.delete(k));
        for (const [k, v] of Object.entries(f.filters)) if (v) p.set(k, v as string);
      }),
    [patchParams],
  );

  const onDelete = useCallback((name: string) => setSaved(deleteFilter(name)), []);

  return (
    <div className="mx-auto max-w-7xl space-y-4">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Timeline</h1>
          <p className="text-sm text-muted-foreground">Every change in scope, newest first.</p>
        </div>
        {refineActive && (
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="sm" onClick={onClear}>
              <X className="mr-1 size-3.5" /> Clear
            </Button>
            <Button variant="outline" size="sm" onClick={onSave}>
              <Star className="mr-1 size-3.5" /> Save
            </Button>
          </div>
        )}
      </div>

      <FilterBar
        filters={advanced}
        onSelect={onSelect}
        facets={facets.data}
        saved={saved}
        onApply={onApply}
        onDelete={onDelete}
      />

      <div className="rounded-lg border">
        {events.isLoading ? (
          <p className="p-4 text-sm text-muted-foreground">Loading…</p>
        ) : events.error ? (
          <p className="p-4 text-sm text-destructive">Couldn’t load events.</p>
        ) : rows.length === 0 ? (
          <p className="p-4 text-sm text-muted-foreground">No events match the current scope.</p>
        ) : (
          <div className="divide-y">
            {rows.map((e) => (
              <EventRow key={e.id} event={e} onSelect={setSelected} />
            ))}
          </div>
        )}

        {hasNextPage && (
          <div ref={sentinel} className="p-3 text-center">
            <Button variant="ghost" size="sm" onClick={() => fetchNextPage()} disabled={isFetchingNextPage}>
              {isFetchingNextPage ? "Loading…" : "Load more"}
            </Button>
          </div>
        )}
      </div>

      <EventDrawer event={selected} onClose={() => setSelected(null)} />
    </div>
  );
}

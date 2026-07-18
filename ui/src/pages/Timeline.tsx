import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Star, X } from "lucide-react";
import type { components } from "@/api/schema";
import { FilterBar } from "@/components/timeline/FilterBar";
import { EventRow } from "@/components/timeline/EventRow";
import { EventDrawer } from "@/components/timeline/EventDrawer";
import { Button } from "@/components/ui/button";
import { useEventsInfinite, useFacets, type EventFilters } from "@/lib/queries";
import { useDebouncedValue } from "@/lib/useDebouncedValue";
import {
  deleteFilter,
  loadSavedFilters,
  saveFilter,
  type SavedFilter,
} from "@/lib/savedFilters";

type Event = components["schemas"]["Event"];
type SelectKey = "source" | "env" | "service" | "kind" | "status" | "actor";

export function Timeline() {
  const [filters, setFilters] = useState<EventFilters>({});
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<Event | null>(null);
  const [saved, setSaved] = useState<SavedFilter[]>(() => loadSavedFilters());

  const debouncedSearch = useDebouncedValue(search, 300);
  const effective = useMemo<EventFilters>(
    () => ({ ...filters, q: debouncedSearch || undefined }),
    [filters, debouncedSearch],
  );

  const facets = useFacets();
  const events = useEventsInfinite(effective);
  const rows = useMemo(
    () => events.data?.pages.flatMap((p) => p.events ?? []) ?? [],
    [events.data],
  );

  const hasActive =
    Object.values(filters).some(Boolean) || search.trim() !== "";

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

  const onSelect = useCallback((key: SelectKey, value: string) => {
    setFilters((prev) => ({ ...prev, [key]: value || undefined }));
  }, []);

  const onClear = useCallback(() => {
    setFilters({});
    setSearch("");
  }, []);

  const onSave = useCallback(() => {
    const name = window.prompt("Name this filter set:")?.trim();
    if (name) setSaved(saveFilter(name, effective));
  }, [effective]);

  const onApply = useCallback((f: SavedFilter) => {
    const { q, ...rest } = f.filters;
    setFilters(rest);
    setSearch(q ?? "");
  }, []);

  const onDelete = useCallback((name: string) => setSaved(deleteFilter(name)), []);

  return (
    <div className="mx-auto max-w-7xl space-y-4">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Timeline</h1>
          <p className="text-sm text-muted-foreground">Every change, newest first.</p>
        </div>
        {hasActive && (
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
        filters={filters}
        onSelect={onSelect}
        search={search}
        onSearch={setSearch}
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
          <p className="p-4 text-sm text-muted-foreground">No events match these filters.</p>
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

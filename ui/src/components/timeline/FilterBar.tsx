import { X } from "lucide-react";
import type { components } from "@/api/schema";
import type { EventFilters } from "@/lib/queries";
import type { SavedFilter } from "@/lib/savedFilters";
import { MultiSelect } from "@/components/timeline/MultiSelect";

type Facets = components["schemas"]["Facets"];

const KINDS = [
  "build", "merge", "push", "deploy", "config_change",
  "infra_change", "rollback", "alert", "manual",
];
const STATUSES = ["started", "succeeded", "failed", "degraded", "unknown"];

// Advanced facets that refine within the global scope (env/service/owner/repo/
// time/search live in the global scope bar).
type SelectKey = "source" | "kind" | "status" | "actor";

export function FilterBar({
  filters,
  onSelect,
  facets,
  saved,
  onApply,
  onDelete,
}: {
  filters: EventFilters;
  onSelect: (key: SelectKey, value: string) => void;
  facets?: Facets;
  saved: SavedFilter[];
  onApply: (f: SavedFilter) => void;
  onDelete: (name: string) => void;
}) {
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs text-muted-foreground">Refine:</span>
        <MultiSelect label="source" value={filters.source} options={facets?.sources ?? []} onChange={(v) => onSelect("source", v)} />
        <MultiSelect label="kind" value={filters.kind} options={KINDS} onChange={(v) => onSelect("kind", v)} />
        <MultiSelect label="status" value={filters.status} options={STATUSES} onChange={(v) => onSelect("status", v)} />
        <MultiSelect label="actor" value={filters.actor} options={facets?.actors ?? []} onChange={(v) => onSelect("actor", v)} searchable />
      </div>

      {saved.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-xs text-muted-foreground">Saved:</span>
          {saved.map((f) => (
            <span
              key={f.name}
              className="inline-flex items-center gap-1 rounded-full border bg-secondary py-0.5 pl-2.5 pr-1 text-xs"
            >
              <button className="hover:underline" onClick={() => onApply(f)}>
                {f.name}
              </button>
              <button
                aria-label={`Delete ${f.name}`}
                className="rounded-full p-0.5 text-muted-foreground hover:text-foreground"
                onClick={() => onDelete(f.name)}
              >
                <X className="size-3" />
              </button>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

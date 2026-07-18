import { Search, Star, X } from "lucide-react";
import type { components } from "@/api/schema";
import type { EventFilters } from "@/lib/queries";
import type { SavedFilter } from "@/lib/savedFilters";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Button } from "@/components/ui/button";

type Facets = components["schemas"]["Facets"];

const KINDS = [
  "build", "merge", "push", "deploy", "config_change",
  "infra_change", "rollback", "alert", "manual",
];
const STATUSES = ["started", "succeeded", "failed", "degraded", "unknown"];

type SelectKey = "env" | "service" | "kind" | "status" | "actor";

function Facet({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string | undefined;
  options: string[];
  onChange: (v: string) => void;
}) {
  return (
    <Select
      aria-label={label}
      value={value ?? ""}
      onChange={(e) => onChange(e.target.value)}
      title={value || label}
      className="w-auto min-w-[6.5rem] max-w-[11rem] truncate"
    >
      <option value="">{label}</option>
      {options.map((o) => (
        <option key={o} value={o}>
          {o}
        </option>
      ))}
    </Select>
  );
}

export function FilterBar({
  filters,
  onSelect,
  search,
  onSearch,
  onClear,
  hasActive,
  facets,
  saved,
  onSave,
  onApply,
  onDelete,
}: {
  filters: EventFilters;
  onSelect: (key: SelectKey, value: string) => void;
  search: string;
  onSearch: (v: string) => void;
  onClear: () => void;
  hasActive: boolean;
  facets?: Facets;
  saved: SavedFilter[];
  onSave: () => void;
  onApply: (f: SavedFilter) => void;
  onDelete: (name: string) => void;
}) {
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-[12rem] flex-1">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="h-8 pl-8"
            placeholder="Search events…"
            value={search}
            onChange={(e) => onSearch(e.target.value)}
          />
        </div>
        <Facet label="env" value={filters.env} options={facets?.envs ?? []} onChange={(v) => onSelect("env", v)} />
        <Facet label="service" value={filters.service} options={facets?.services ?? []} onChange={(v) => onSelect("service", v)} />
        <Facet label="kind" value={filters.kind} options={KINDS} onChange={(v) => onSelect("kind", v)} />
        <Facet label="status" value={filters.status} options={STATUSES} onChange={(v) => onSelect("status", v)} />
        <Facet label="actor" value={filters.actor} options={facets?.actors ?? []} onChange={(v) => onSelect("actor", v)} />
        {hasActive && (
          <>
            <Button variant="ghost" size="sm" onClick={onClear}>
              <X className="mr-1 size-3.5" /> Clear
            </Button>
            <Button variant="outline" size="sm" onClick={onSave}>
              <Star className="mr-1 size-3.5" /> Save
            </Button>
          </>
        )}
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

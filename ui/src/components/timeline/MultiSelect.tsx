import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown } from "lucide-react";
import { cn } from "@/lib/utils";

/**
 * Compact multi-select for a timeline facet. The selection is a comma-joined
 * OR-set string (so the whole filter/saved-filter plumbing stays string-based);
 * onChange returns the new comma-joined value. A trigger button shows the label
 * + a count badge; the panel is a checkbox list, searchable for long facets
 * (service/actor). No popover dependency — outside-click / Escape close it.
 */
export function MultiSelect({
  label,
  value,
  options,
  onChange,
  searchable = false,
}: {
  label: string;
  value: string | undefined;
  options: string[];
  onChange: (v: string) => void;
  searchable?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const ref = useRef<HTMLDivElement>(null);
  const selected = new Set((value ?? "").split(",").filter(Boolean));

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDoc);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  function toggle(o: string) {
    const next = new Set(selected);
    if (next.has(o)) next.delete(o);
    else next.add(o);
    onChange([...next].join(","));
  }

  const filtered =
    searchable && q
      ? options.filter((o) => o.toLowerCase().includes(q.toLowerCase()))
      : options;

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className={cn(
          "flex h-8 items-center gap-1.5 rounded-md border border-input bg-transparent px-2.5 text-sm shadow-sm transition-colors hover:bg-accent",
          selected.size > 0 && "border-primary",
        )}
      >
        <span className={cn(selected.size === 0 && "text-muted-foreground")}>{label}</span>
        {selected.size > 0 && (
          <span className="rounded-full bg-primary px-1.5 text-[10px] font-medium leading-4 text-primary-foreground">
            {selected.size}
          </span>
        )}
        <ChevronDown className="size-3.5 text-muted-foreground" />
      </button>

      {open && (
        <div className="absolute left-0 z-20 mt-1 max-h-72 w-48 overflow-auto rounded-md border bg-background p-1 shadow-md">
          {searchable && (
            <input
              autoFocus
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder={`Filter ${label}…`}
              className="mb-1 w-full rounded border border-input bg-transparent px-2 py-1 text-xs outline-none focus-visible:ring-1 focus-visible:ring-ring"
            />
          )}
          {filtered.length === 0 ? (
            <p className="px-2 py-1 text-xs text-muted-foreground">No match.</p>
          ) : (
            filtered.map((o) => {
              const on = selected.has(o);
              return (
                <button
                  key={o}
                  type="button"
                  onClick={() => toggle(o)}
                  className={cn(
                    "flex w-full items-center gap-2 rounded px-2 py-1 text-left text-xs transition-colors hover:bg-accent",
                    on ? "text-foreground" : "text-muted-foreground",
                  )}
                >
                  <Check className={cn("size-3.5 shrink-0", on ? "opacity-100" : "opacity-0")} />
                  <span className="truncate font-mono">{o}</span>
                </button>
              );
            })
          )}
        </div>
      )}
    </div>
  );
}

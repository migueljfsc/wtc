import type { components } from "@/api/schema";
import { SourceIcon } from "@/components/SourceIcon";
import { StatusDot } from "@/components/StatusBadge";
import { relativeTime } from "@/lib/format";

type Event = components["schemas"]["Event"];

export function EventRow({ event, onSelect }: { event: Event; onSelect: (e: Event) => void }) {
  return (
    <button
      onClick={() => onSelect(event)}
      className="flex w-full items-center gap-3 px-2 py-2.5 text-left text-sm transition-colors hover:bg-accent"
    >
      <StatusDot status={event.status} />
      {/* Fixed-width slot so titles align whether or not a mark exists. */}
      <span className="flex w-3.5 shrink-0 justify-center">
        <SourceIcon source={event.source} />
      </span>
      <span className="w-16 shrink-0 text-xs uppercase tracking-wide text-muted-foreground">
        {event.kind.replace("_", " ")}
      </span>
      <span className="min-w-0 flex-1 truncate">{event.title}</span>
      {event.service && (
        <span className="hidden shrink-0 font-mono text-xs text-muted-foreground sm:inline">
          {event.service}
        </span>
      )}
      {event.env && (
        <span className="shrink-0 rounded bg-secondary px-1.5 py-0.5 font-mono text-xs text-secondary-foreground">
          {event.env}
        </span>
      )}
      <span className="w-16 shrink-0 text-right text-xs text-muted-foreground">
        {relativeTime(event.ts)}
      </span>
    </button>
  );
}

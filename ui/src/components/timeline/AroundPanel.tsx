import { useState } from "react";
import { AlertTriangle } from "lucide-react";
import type { components } from "@/api/schema";
import { StatusDot } from "@/components/StatusBadge";
import { useAround } from "@/lib/queries";
import { duration } from "@/lib/format";
import { cn } from "@/lib/utils";

type Event = components["schemas"]["Event"];

const WINDOWS = ["30m", "2h", "6h", "24h"];

/**
 * Alert correlation: a compact timeline centred on an alert, listing the
 * changes in the preceding window (closest to the alert first, subtly
 * highlighted — the likeliest culprit). The window mirrors `wtc around --window`.
 */
export function AroundPanel({ alert }: { alert: Event }) {
  const [window, setWindow] = useState("6h");
  const around = useAround(alert.id, window);
  const anchor = new Date(alert.ts).getTime();

  // The window includes the alert itself; drop it from the "changes" list.
  const changes = (around.data ?? []).filter((e) => e.id !== alert.id);

  return (
    <div>
      <div className="mb-2 flex items-center justify-between">
        <h3 className="text-sm font-semibold">Preceding changes</h3>
        <div className="flex gap-0.5 rounded-md border p-0.5">
          {WINDOWS.map((w) => (
            <button
              key={w}
              onClick={() => setWindow(w)}
              className={cn(
                "rounded px-2 py-0.5 text-xs transition-colors",
                w === window ? "bg-secondary font-medium" : "text-muted-foreground hover:bg-accent",
              )}
            >
              {w}
            </button>
          ))}
        </div>
      </div>

      {around.isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {around.error && <p className="text-sm text-muted-foreground">Couldn’t correlate changes.</p>}
      {around.data && (
        <div>
          {changes.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No changes in the {window} before this alert.
            </p>
          ) : (
            <ul className="border-l pl-3">
              {changes.map((e, i) => {
                const before = anchor - new Date(e.ts).getTime();
                return (
                  <li
                    key={e.id}
                    className={cn(
                      "relative flex items-center gap-2 py-1.5 text-sm",
                      i === 0 && "font-medium",
                    )}
                  >
                    <span className="absolute -left-[17px] size-2 rounded-full bg-border" />
                    <StatusDot status={e.status} />
                    <span className="min-w-0 flex-1 truncate">{e.title}</span>
                    {e.env && (
                      <span className="shrink-0 rounded bg-secondary px-1.5 py-0.5 font-mono text-xs">
                        {e.env}
                      </span>
                    )}
                    <span className="w-20 shrink-0 text-right text-xs text-muted-foreground">
                      {duration(before)} before
                    </span>
                  </li>
                );
              })}
            </ul>
          )}
          <div className="mt-1 flex items-center gap-2 border-l border-destructive pl-3 text-sm text-destructive">
            <AlertTriangle className="size-4" />
            <span className="font-medium">alert fired</span>
          </div>
        </div>
      )}
    </div>
  );
}

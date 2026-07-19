import { useState } from "react";
import { Link } from "react-router-dom";
import { AlertTriangle } from "lucide-react";
import type { components } from "@/api/schema";
import { StatusDot } from "@/components/StatusBadge";
import { useBlast } from "@/lib/queries";
import { duration } from "@/lib/format";
import { cn } from "@/lib/utils";

type Event = components["schemas"]["Event"];

const WINDOWS = ["30m", "2h", "6h", "24h"];

/**
 * Likely causes: the changes in the window before an alert, ranked by
 * the deterministic blast score (recency, same env/service, kind, failed
 * state) — best suspect first. Rows with a ref link into Where. The window
 * mirrors `wtc blast --window`.
 */
export function AroundPanel({ alert, onNavigate }: { alert: Event; onNavigate?: () => void }) {
  const [window, setWindow] = useState("6h");
  const blast = useBlast(alert.id, window);
  const suspects = blast.data?.suspects ?? [];

  return (
    <div>
      <div className="mb-2 flex items-center justify-between">
        <h3 className="text-sm font-semibold">Likely causes</h3>
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

      {blast.isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {blast.error && <p className="text-sm text-muted-foreground">Couldn’t rank suspects.</p>}
      {blast.data && (
        <div>
          {suspects.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No changes in the {window} before this alert.
            </p>
          ) : (
            <ul className="border-l pl-3">
              {suspects.map((s, i) => {
                const e = s.event;
                const before = new Date(alert.ts).getTime() - new Date(e.ts).getTime();
                const title = e.ref ? (
                  <Link
                    to={`/where?ref=${encodeURIComponent(e.ref)}`}
                    onClick={onNavigate}
                    title="Trace this change in Where"
                    className="underline-offset-4 hover:underline"
                  >
                    {e.title}
                  </Link>
                ) : (
                  e.title
                );
                return (
                  <li
                    key={e.id}
                    title={s.reasons.join(" · ")}
                    className={cn(
                      "relative flex items-center gap-2 py-1.5 text-sm",
                      i === 0 && "font-medium",
                    )}
                  >
                    <span className="absolute -left-[17px] size-2 rounded-full bg-border" />
                    <span
                      className={cn(
                        "w-8 shrink-0 rounded bg-secondary px-1 py-0.5 text-center font-mono text-xs",
                        i === 0 && "bg-primary text-primary-foreground",
                      )}
                    >
                      {s.score}
                    </span>
                    <StatusDot status={e.status} />
                    <span className="min-w-0 flex-1 truncate">{title}</span>
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
          {(blast.data.notes ?? []).map((n) => (
            <p key={n} className="mt-1 text-xs italic text-muted-foreground">
              {n}
            </p>
          ))}
        </div>
      )}
    </div>
  );
}

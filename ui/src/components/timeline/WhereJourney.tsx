import type { components } from "@/api/schema";
import { StatusDot } from "@/components/StatusBadge";
import { duration, relativeTime } from "@/lib/format";

type WhereReport = components["schemas"]["WhereReport"];

/**
 * Compact BUILD → INTENT → APPLIED journey for the drawer. The full pipeline
 * visualization is P9; this is the inline summary.
 */
export function WhereJourney({ report }: { report: WhereReport }) {
  // Servers ≤ v0.20.0 marshalled empty lists as null (fixed since); guard so
  // one skewed API version can't take down the whole timeline page.
  const builds = report.builds ?? [];
  const intents = report.intents ?? [];
  const envs = report.envs ?? [];
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-3 text-xs text-muted-foreground">
        <span>
          sha <code className="text-foreground">{report.sha.slice(0, 10)}</code>
        </span>
        <span>{builds.length} build{builds.length === 1 ? "" : "s"}</span>
        <span>{intents.length} intent{intents.length === 1 ? "" : "s"}</span>
      </div>

      {envs.length === 0 ? (
        <p className="text-sm text-muted-foreground">Not yet applied to any environment.</p>
      ) : (
        <ul className="space-y-1.5">
          {envs.map((e) => (
            <li key={e.env} className="flex items-center gap-2 text-sm">
              <span className="w-20 shrink-0 font-mono text-xs">{e.env}</span>
              {e.applied ? (
                <>
                  <StatusDot status={e.applied.status} />
                  {e.applied.url ? (
                    <a
                      href={e.applied.url}
                      target="_blank"
                      rel="noreferrer"
                      className="text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
                    >
                      applied {relativeTime(e.applied.ts)}
                    </a>
                  ) : (
                    <span className="text-muted-foreground">
                      applied {relativeTime(e.applied.ts)}
                    </span>
                  )}
                  {typeof e.lag_ms === "number" && (
                    <span className="text-xs text-muted-foreground">
                      · lag {duration(e.lag_ms)}
                    </span>
                  )}
                </>
              ) : (
                <span className="text-xs text-muted-foreground">intent only, not applied</span>
              )}
            </li>
          ))}
        </ul>
      )}

      {report.notes && report.notes.length > 0 && (
        <ul className="list-disc pl-4 text-xs text-muted-foreground">
          {report.notes.map((n, i) => (
            <li key={i}>{n}</li>
          ))}
        </ul>
      )}
    </div>
  );
}

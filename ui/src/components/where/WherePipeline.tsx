import { ArrowRight, CircleDashed, ExternalLink } from "lucide-react";
import type { components } from "@/api/schema";
import { StatusDot } from "@/components/StatusBadge";
import { cn } from "@/lib/utils";
import { duration, relativeTime } from "@/lib/format";

type WhereReport = components["schemas"]["WhereReport"];
type Event = components["schemas"]["Event"];

function Stage({
  title,
  event,
  gap,
}: {
  title: string;
  event?: Event | null;
  gap?: string;
}) {
  const frame = cn(
    "min-w-[8.5rem] rounded-md border px-3 py-2",
    !event && "border-dashed bg-muted/30",
  );
  const body = (
    <>
      <div className="mb-1 text-[10px] uppercase tracking-wide text-muted-foreground">{title}</div>
      {event ? (
        <div className="flex items-center gap-1.5 text-xs">
          <StatusDot status={event.status} />
          <span className="truncate">{relativeTime(event.ts)}</span>
          {event.url && <ExternalLink className="size-3 shrink-0 text-muted-foreground" />}
        </div>
      ) : (
        <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <CircleDashed className="size-3" />
          {gap ?? "unknown"}
        </div>
      )}
    </>
  );

  // P18: a stage with a source URL is a real link — the run, PR/commit, or
  // deploy on the system that produced it. No URL (flux reconciles, most
  // deploys) → the plain card, never a dead link.
  if (event?.url) {
    return (
      <a
        href={event.url}
        target="_blank"
        rel="noreferrer"
        title={event.title}
        className={cn(frame, "block transition-colors hover:border-primary/50 hover:bg-accent")}
      >
        {body}
      </a>
    );
  }
  return <div className={frame}>{body}</div>;
}

function Arrow({ lag }: { lag?: number }) {
  return (
    <div className="flex flex-col items-center px-1 text-muted-foreground">
      <ArrowRight className="size-4" />
      {typeof lag === "number" && <span className="text-[10px]">{duration(lag)}</span>}
    </div>
  );
}

export function WherePipeline({ report }: { report: WhereReport }) {
  const builds = report.builds ?? [];
  const intents = report.intents ?? [];
  const envs = report.envs ?? [];
  const build = builds[0] ?? null;

  return (
    <div className="space-y-4">
      {report.sha && (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
          <span className="text-muted-foreground">
            resolved sha <code className="text-foreground">{report.sha.slice(0, 12)}</code>
          </span>
          <span className="text-muted-foreground">
            {builds.length} build{builds.length === 1 ? "" : "s"} ·{" "}
            {intents.length} intent{intents.length === 1 ? "" : "s"}
          </span>
        </div>
      )}

      {envs.length === 0 ? (
        <p className="text-sm text-muted-foreground">Not applied to any environment yet.</p>
      ) : (
        <div className="space-y-3">
          {envs.map((e) => (
            <div key={e.env} className="rounded-lg border p-3">
              <div className="mb-2 font-mono text-sm">{e.env}</div>
              <div className="flex flex-wrap items-center gap-1">
                <Stage title="build" event={build} gap="no build" />
                <Arrow />
                <Stage title="intent" event={e.intent} gap="no intent" />
                <Arrow lag={e.lag_ms ?? undefined} />
                <Stage title="applied" event={e.applied} gap="not applied" />
              </div>
              {e.unknown && e.unknown.length > 0 && (
                <p className="mt-2 text-xs text-muted-foreground">
                  gaps: {e.unknown.join(", ")}
                </p>
              )}
            </div>
          ))}
        </div>
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

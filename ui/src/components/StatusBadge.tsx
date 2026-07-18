import { cn } from "@/lib/utils";
import { statusStyle } from "@/lib/status";

/**
 * The dot core shared by StatusDot and StatusBadge. "started" is the one
 * in-flight status, so it pulses (an expanding ping ring behind the solid
 * dot) to read as "ongoing" against the static settled colors. motion-safe:
 * keeps it static for prefers-reduced-motion users; the title/label still
 * carries the state, never color or motion alone.
 */
function Dot({ status, className }: { status: string; className?: string }) {
  const s = statusStyle(status);
  return (
    <span className={cn("relative inline-flex size-2 shrink-0", className)} title={status}>
      {status === "started" && (
        <span
          className={cn(
            "absolute inline-flex h-full w-full rounded-full opacity-75 motion-safe:animate-ping",
            s.dot,
          )}
        />
      )}
      <span className={cn("relative inline-flex size-2 rounded-full", s.dot)} />
    </span>
  );
}

/** A colored dot only — for compact rows and env-health cards. */
export function StatusDot({ status, className }: { status: string; className?: string }) {
  return <Dot status={status} className={className} />;
}

/** Dot + label pill. */
export function StatusBadge({ status }: { status: string }) {
  const s = statusStyle(status);
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-xs font-medium", s.text)}>
      <Dot status={status} />
      {s.label}
    </span>
  );
}

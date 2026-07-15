import { cn } from "@/lib/utils";
import { statusStyle } from "@/lib/status";

/** A colored dot only — for compact rows and env-health cards. */
export function StatusDot({ status, className }: { status: string; className?: string }) {
  return (
    <span
      className={cn("inline-block size-2 shrink-0 rounded-full", statusStyle(status).dot, className)}
      title={status}
    />
  );
}

/** Dot + label pill. */
export function StatusBadge({ status }: { status: string }) {
  const s = statusStyle(status);
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-xs font-medium", s.text)}>
      <span className={cn("size-2 rounded-full", s.dot)} />
      {s.label}
    </span>
  );
}

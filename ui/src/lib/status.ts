// Reserved status palette (dataviz): state, not series identity — always
// rendered with a label/dot, never color alone. Tailwind classes stay
// theme-aware.
const STATUS: Record<string, { dot: string; text: string; label: string }> = {
  succeeded: { dot: "bg-emerald-500", text: "text-emerald-600 dark:text-emerald-500", label: "succeeded" },
  failed: { dot: "bg-red-500", text: "text-red-600 dark:text-red-500", label: "failed" },
  started: { dot: "bg-blue-500", text: "text-blue-600 dark:text-blue-500", label: "started" },
  unknown: { dot: "bg-zinc-400", text: "text-muted-foreground", label: "unknown" },
};

export function statusStyle(status: string) {
  return STATUS[status] ?? STATUS.unknown;
}

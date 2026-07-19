// The API emits UTC ISO-8601; the UI renders local time (timelines
// sort by ts; CLI/UI render local).

const DAY = 86_400_000;

/** "Jun 1" — short local date, for chart axes and cards. */
export function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });
}

/** "Jun 1, 14:32" — local date + time. */
export function dateTime(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** "just now" / "5m ago" / "3h ago" / "2d ago". */
export function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  if (diff < 60_000) return "just now";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < DAY) return `${Math.floor(diff / 3_600_000)}h ago`;
  return `${Math.floor(diff / DAY)}d ago`;
}

/** Percentage from a ratio, no decimals: pct(2, 8) => "25%". */
export function pct(part: number, whole: number): string {
  if (whole === 0) return "—";
  return `${Math.round((part / whole) * 100)}%`;
}

/** ISO timestamp for N days before now — for stats ?since= params. */
export function daysAgoISO(days: number): string {
  return new Date(Date.now() - days * DAY).toISOString();
}

/** Human byte size: 1536 => "1.5 KB". */
export function bytes(n: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${i === 0 ? v : v.toFixed(1)} ${units[i]}`;
}

/** Compact duration from milliseconds: 1500 => "2s", 900000 => "15m". */
export function duration(ms: number): string {
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h}h`;
  return `${Math.round(h / 24)}d`;
}

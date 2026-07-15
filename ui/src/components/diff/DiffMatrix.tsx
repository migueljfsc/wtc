import { Link } from "react-router-dom";
import type { components } from "@/api/schema";
import { cn } from "@/lib/utils";
import { relativeTime } from "@/lib/format";

type Matrix = components["schemas"]["Matrix"];
type Cell = components["schemas"]["MatrixCell"];

/** Display version for a cell: artifact, else short ref. */
function version(cell: Cell): string {
  return cell.artifact || (cell.ref ? cell.ref.slice(0, 7) : "—");
}

function rowStatus(envs: string[], cells: Matrix["services"][number]["cells"]) {
  const present = envs.filter((e) => cells[e]);
  const versions = new Set(present.map((e) => version(cells[e])));
  if (versions.size > 1) return { label: "drift", tone: "drift" as const };
  if (present.length < envs.length) return { label: "partial", tone: "partial" as const };
  return { label: "in sync", tone: "sync" as const };
}

/**
 * The version deployed most recently across the row (by ts) — the "current"
 * version other envs are measured against. Envs not on it are behind; ts is
 * sortable RFC3339, so string comparison finds the newest.
 */
function leadVersion(envs: string[], cells: Matrix["services"][number]["cells"]): string | null {
  let leadTs = "";
  let lead: string | null = null;
  for (const e of envs) {
    const c = cells[e];
    if (c && c.ts > leadTs) {
      leadTs = c.ts;
      lead = version(c);
    }
  }
  return lead;
}

const TONE = {
  drift: "text-amber-600 dark:text-amber-500",
  partial: "text-muted-foreground",
  sync: "text-emerald-600 dark:text-emerald-500",
};

export function DiffMatrix({ matrix }: { matrix: Matrix }) {
  const envs = matrix.envs;
  const target = envs[envs.length - 1]; // rightmost column = promotion target

  if (matrix.services.length === 0) {
    return <p className="text-sm text-muted-foreground">No deploys across these environments.</p>;
  }

  return (
    <div className="overflow-x-auto rounded-lg border">
      <table className="w-full border-collapse text-sm">
        <thead>
          <tr className="border-b bg-muted/40">
            <th className="px-3 py-2 text-left font-medium">Service</th>
            {envs.map((e) => (
              <th key={e} className="px-3 py-2 text-left font-mono text-xs font-medium">
                {e}
                {e === target && envs.length > 1 && (
                  <span className="ml-1 font-sans text-[10px] uppercase text-muted-foreground">target</span>
                )}
              </th>
            ))}
            <th className="px-3 py-2 text-left font-medium">State</th>
          </tr>
        </thead>
        <tbody>
          {matrix.services.map((row) => {
            const st = rowStatus(envs, row.cells);
            const lead = leadVersion(envs, row.cells);
            return (
              <tr key={row.service} className="border-b last:border-0">
                <td className="px-3 py-2 font-medium">{row.service}</td>
                {envs.map((e) => {
                  const cell = row.cells[e];
                  if (!cell) {
                    return (
                      <td key={e} className="px-3 py-2 text-muted-foreground">
                        —
                      </td>
                    );
                  }
                  const ver = version(cell);
                  // Amber marks an env that is BEHIND the newest deploy — the
                  // laggard needing promotion, not the up-to-date envs.
                  const behind = lead !== null && ver !== lead;
                  const revOnly = !cell.artifact && !!cell.ref;
                  return (
                    <td key={e} className={cn("px-3 py-2", behind && "bg-amber-500/10")}>
                      <Link
                        to={`/where?ref=${encodeURIComponent(cell.ref || cell.artifact || "")}`}
                        className={cn("font-mono text-xs hover:underline", behind && TONE.drift)}
                        title={
                          behind
                            ? `behind ${lead}`
                            : revOnly
                              ? "revision-only (no artifact reported)"
                              : cell.artifact || cell.ref
                        }
                      >
                        {ver}
                        {revOnly && <sup className="ml-0.5 text-[9px] text-muted-foreground">rev</sup>}
                      </Link>
                      <div className="text-[10px] text-muted-foreground">{relativeTime(cell.ts)}</div>
                    </td>
                  );
                })}
                <td className={cn("px-3 py-2 text-xs font-medium", TONE[st.tone])}>{st.label}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

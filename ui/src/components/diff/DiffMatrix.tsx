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

const GIT_SHA = /^[0-9a-f]{7,40}$/;

/** The ref `wtc where` would trace for this cell (ref wins over artifact). */
function whereRef(cell: Cell): string {
  return cell.ref || cell.artifact || "";
}

/**
 * Can `where` resolve this cell to a git sha? It traces git-sha lineage only, so
 * a cell is traceable when its ref is a git sha, or (no ref) its artifact is an
 * image tag that embeds one via tag_patterns. Flux OCI/helm reconciles surface
 * as a `name@revision` artifact — an OCI content digest or chart version with no
 * git sha — so they must NOT link (clicking dead-ends on a 400 in Where).
 */
function traceable(cell: Cell): boolean {
  if (cell.ref) return GIT_SHA.test(cell.ref);
  return !!cell.artifact && !cell.artifact.includes("@");
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
                  const canTrace = traceable(cell);
                  const label = (
                    <>
                      {ver}
                      {revOnly && <sup className="ml-0.5 text-[9px] text-muted-foreground">rev</sup>}
                    </>
                  );
                  return (
                    <td key={e} className={cn("px-3 py-2", behind && "bg-amber-500/10")}>
                      {canTrace ? (
                        <Link
                          to={`/where?ref=${encodeURIComponent(whereRef(cell))}`}
                          className={cn("font-mono text-xs hover:underline", behind && TONE.drift)}
                          title={
                            behind
                              ? `behind ${lead}`
                              : revOnly
                                ? "revision-only (no artifact reported)"
                                : cell.artifact || cell.ref
                          }
                        >
                          {label}
                        </Link>
                      ) : (
                        <span
                          className={cn(
                            "font-mono text-xs text-muted-foreground",
                            behind && TONE.drift,
                          )}
                          title="OCI artifact or chart version — no git lineage to trace"
                        >
                          {label}
                        </span>
                      )}
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

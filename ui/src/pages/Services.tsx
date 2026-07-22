import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { Search } from "lucide-react";
import { useFacets } from "@/lib/queries";
import { ServiceDetail } from "@/components/services/ServiceDetail";
import { useScope } from "@/lib/scope";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

export function Services() {
  const facets = useFacets();
  const { scope } = useScope();
  // The bar's `service` facet narrows which services are pickable here; `?svc=`
  // (this page's own param) picks the one whose detail is open — the two don't
  // collide. env/owner/time from the bar flow into the detail below.
  const services = useMemo(() => {
    const all = facets.data?.services ?? [];
    const pick = new Set(scope.service ? scope.service.split(",").filter(Boolean) : []);
    return pick.size === 0 ? all : all.filter((s) => pick.has(s));
  }, [facets.data, scope.service]);

  const [params, setParams] = useSearchParams();
  const selected = params.get("svc");
  const [query, setQuery] = useState("");

  // Merge-write: preserve the rest of the scope (env/owner/time/…) in the URL.
  const selectService = (svc: string, replace = false) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.set("svc", svc);
        return next;
      },
      { replace },
    );

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return q ? services.filter((s) => s.toLowerCase().includes(q)) : services;
  }, [services, query]);

  // Default to the first service once facets load (or when the scope narrowing
  // drops the currently-selected one out of the list).
  useEffect(() => {
    if (services.length > 0 && (!selected || !services.includes(selected))) {
      selectService(services[0], true);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected, services]);

  return (
    <div className="mx-auto max-w-7xl space-y-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Services</h1>
        <p className="text-sm text-muted-foreground">
          Where each service is deployed, how often, and how it fails.
        </p>
      </div>

      {facets.isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {services.length === 0 && !facets.isLoading && (
        <p className="text-sm text-muted-foreground">No services seen yet.</p>
      )}

      {services.length > 0 && (
        <div className="grid gap-4 md:grid-cols-[16rem_minmax(0,1fr)]">
          {/* Searchable, scrollable list — scales past a wrapping pill wall. */}
          <aside className="space-y-2">
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                className="h-8 pl-8"
                placeholder={`Filter ${services.length} services…`}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </div>
            <div className="max-h-[70vh] overflow-auto rounded-md border">
              {filtered.length === 0 ? (
                <p className="px-3 py-2 text-sm text-muted-foreground">No match.</p>
              ) : (
                <ul>
                  {filtered.map((s) => (
                    <li key={s}>
                      <button
                        onClick={() => selectService(s)}
                        title={s}
                        className={cn(
                          "block w-full truncate px-3 py-1.5 text-left text-sm transition-colors",
                          s === selected
                            ? "bg-primary text-primary-foreground"
                            : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
                        )}
                      >
                        {s}
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </aside>

          <div className="min-w-0">
            {selected && (
              <ServiceDetail
                key={selected}
                service={selected}
                scope={{
                  env: scope.env,
                  owner: scope.owner,
                  since: scope.since,
                  until: scope.until,
                  at: scope.range === "custom" ? scope.until : undefined,
                }}
              />
            )}
          </div>
        </div>
      )}
    </div>
  );
}

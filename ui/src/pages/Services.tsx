import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { Search } from "lucide-react";
import { useFacets } from "@/lib/queries";
import { ServiceDetail } from "@/components/services/ServiceDetail";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

export function Services() {
  const facets = useFacets();
  const services = useMemo(() => facets.data?.services ?? [], [facets.data]);

  const [params, setParams] = useSearchParams();
  const selected = params.get("service");
  const [query, setQuery] = useState("");

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return q ? services.filter((s) => s.toLowerCase().includes(q)) : services;
  }, [services, query]);

  // Default to the first service once facets load.
  useEffect(() => {
    if (!selected && services.length > 0) {
      setParams({ service: services[0] }, { replace: true });
    }
  }, [selected, services, setParams]);

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
                        onClick={() => setParams({ service: s })}
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
            {selected && <ServiceDetail key={selected} service={selected} />}
          </div>
        </div>
      )}
    </div>
  );
}

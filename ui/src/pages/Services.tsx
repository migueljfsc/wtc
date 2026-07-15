import { useEffect, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import { useFacets } from "@/lib/queries";
import { ServiceDetail } from "@/components/services/ServiceDetail";
import { cn } from "@/lib/utils";

export function Services() {
  const facets = useFacets();
  const services = useMemo(() => facets.data?.services ?? [], [facets.data]);

  const [params, setParams] = useSearchParams();
  const selected = params.get("service");

  // Default to the first service once facets load.
  useEffect(() => {
    if (!selected && services.length > 0) {
      setParams({ service: services[0] }, { replace: true });
    }
  }, [selected, services, setParams]);

  return (
    <div className="mx-auto max-w-5xl space-y-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Services</h1>
        <p className="text-sm text-muted-foreground">
          Where each service is deployed, how often, and how it fails.
        </p>
      </div>

      {services.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {services.map((s) => (
            <button
              key={s}
              onClick={() => setParams({ service: s })}
              className={cn(
                "rounded-full border px-3 py-1 text-sm transition-colors",
                s === selected
                  ? "border-primary bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:bg-accent",
              )}
            >
              {s}
            </button>
          ))}
        </div>
      )}

      {facets.isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {services.length === 0 && !facets.isLoading && (
        <p className="text-sm text-muted-foreground">No services seen yet.</p>
      )}
      {selected && <ServiceDetail key={selected} service={selected} />}
    </div>
  );
}

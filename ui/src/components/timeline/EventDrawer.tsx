import type { components } from "@/api/schema";
import { Drawer } from "@/components/Drawer";
import { StatusBadge } from "@/components/StatusBadge";
import { WhereJourney } from "@/components/timeline/WhereJourney";
import { AroundPanel } from "@/components/timeline/AroundPanel";
import { useWhere } from "@/lib/queries";
import { dateTime } from "@/lib/format";

type Event = components["schemas"]["Event"];

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[7rem_1fr] gap-2 py-1 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </div>
  );
}

function prettyPayload(raw: string): string {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

export function EventDrawer({ event, onClose }: { event: Event | null; onClose: () => void }) {
  // Resolve the where-journey by the event's git ref (fallback: artifact).
  const ref = event ? event.ref || event.artifact || null : null;
  const where = useWhere(event ? ref : null);

  return (
    <Drawer open={!!event} onClose={onClose} title={event?.title ?? ""}>
      {event && (
        <div className="space-y-6">
          <section>
            <div className="mb-2 flex items-center gap-3">
              <StatusBadge status={event.status} />
              <span className="text-xs uppercase tracking-wide text-muted-foreground">
                {event.kind.replace("_", " ")} · {event.source}
              </span>
            </div>
            <dl className="divide-y">
              {event.env && <Field label="env">{event.env}</Field>}
              {event.service && <Field label="service">{event.service}</Field>}
              {event.cluster && <Field label="cluster">{event.cluster}</Field>}
              {event.namespace && <Field label="namespace">{event.namespace}</Field>}
              {event.actor && <Field label="actor">{event.actor}</Field>}
              {event.ref && <Field label="ref"><code className="text-xs">{event.ref}</code></Field>}
              {event.artifact && <Field label="artifact"><code className="text-xs break-all">{event.artifact}</code></Field>}
              <Field label="time">{dateTime(event.ts)}</Field>
              {event.url && (
                <Field label="source">
                  <a href={event.url} target="_blank" rel="noreferrer" className="text-primary underline-offset-4 hover:underline">
                    open ↗
                  </a>
                </Field>
              )}
            </dl>
          </section>

          {event.kind === "alert" && <AroundPanel alert={event} />}

          {ref && event.kind !== "alert" && (
            <section>
              <h3 className="mb-2 text-sm font-semibold">Journey</h3>
              {where.isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
              {where.error && <p className="text-sm text-muted-foreground">Couldn’t resolve journey.</p>}
              {where.data && <WhereJourney report={where.data} />}
            </section>
          )}

          {event.payload && (
            <section>
              <h3 className="mb-2 text-sm font-semibold">Payload (redacted)</h3>
              <pre className="max-h-80 overflow-auto rounded-md border bg-muted/40 p-3 text-xs">
                {prettyPayload(event.payload)}
              </pre>
            </section>
          )}
        </div>
      )}
    </Drawer>
  );
}

import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, XCircle } from "lucide-react";
import { api } from "@/lib/api";
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

function Stat({ label, value }: { label: string; value: string | number }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardDescription>{label}</CardDescription>
        <CardTitle className="text-3xl tabular-nums">{value}</CardTitle>
      </CardHeader>
    </Card>
  );
}

export function Dashboard() {
  // A live, typed, bearer-authed call — proves the whole client stack (token +
  // CORS + generated types) against the real server. The rich dashboard lands
  // in P8; for now this is the connection-health probe.
  const { data, isLoading, error } = useQuery({
    queryKey: ["doctor"],
    queryFn: async () => {
      const { data, error } = await api.GET("/api/v1/doctor");
      if (error) throw new Error("doctor request failed");
      return data;
    },
  });

  return (
    <div className="mx-auto max-w-4xl">
      <h1 className="mb-1 text-2xl font-semibold tracking-tight">Dashboard</h1>
      <p className="mb-6 text-sm text-muted-foreground">
        Overview and DORA-style metrics land in P8. This is the live
        connection probe.
      </p>

      {isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}

      {error && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-destructive">
              <XCircle className="size-5" /> API unreachable
            </CardTitle>
            <CardDescription>
              The portal is authenticated but the doctor endpoint failed. Check
              the server logs.
            </CardDescription>
          </CardHeader>
        </Card>
      )}

      {data && (
        <div className="space-y-4">
          <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-500">
            <CheckCircle2 className="size-4" /> Connected to the wtc API.
          </div>
          <div className="grid gap-4 sm:grid-cols-3">
            <Stat label="Total events" value={data.total_events.toLocaleString()} />
            <Stat label="Sources" value={data.sources.length} />
            <Stat
              label="Unmapped (24h)"
              value={data.unmapped_24h.toLocaleString()}
            />
          </div>
        </div>
      )}
    </div>
  );
}

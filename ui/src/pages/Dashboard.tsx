import { useMemo, useState } from "react";
import { AlertCircle } from "lucide-react";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { ActivityChart } from "@/components/dashboard/ActivityChart";
import { EnvHealthCards } from "@/components/dashboard/EnvHealthCards";
import { RecentChanges } from "@/components/dashboard/RecentChanges";
import { useActivity, useDeployStats, useRecentEvents } from "@/lib/queries";
import { daysAgoISO, pct } from "@/lib/format";

const WINDOWS = [
  { label: "14d", days: 14 },
  { label: "30d", days: 30 },
  { label: "90d", days: 90 },
];

function StatTile({ label, value, tone }: { label: string; value: string; tone?: "danger" }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardDescription>{label}</CardDescription>
        <CardTitle
          className={"text-3xl tabular-nums " + (tone === "danger" ? "text-red-600 dark:text-red-500" : "")}
        >
          {value}
        </CardTitle>
      </CardHeader>
    </Card>
  );
}

function ErrorCard({ what }: { what: string }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-destructive">
          <AlertCircle className="size-5" /> Couldn’t load {what}
        </CardTitle>
        <CardDescription>Check that the API is reachable and the token is valid.</CardDescription>
      </CardHeader>
    </Card>
  );
}

export function Dashboard() {
  const [days, setDays] = useState(30);
  const since = useMemo(() => daysAgoISO(days), [days]);

  const activity = useActivity(since, "day");
  const deploys = useDeployStats(since);
  const recent = useRecentEvents(12);

  const totals = useMemo(() => {
    const buckets = activity.data?.buckets ?? [];
    const events = buckets.reduce((n, b) => n + b.total, 0);
    const envs = deploys.data?.envs ?? [];
    const deployTotal = envs.reduce((n, e) => n + e.total, 0);
    const deployFailed = envs.reduce((n, e) => n + e.failed, 0);
    return { events, deployTotal, deployFailed };
  }, [activity.data, deploys.data]);

  return (
    <div className="mx-auto max-w-5xl space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Dashboard</h1>
          <p className="text-sm text-muted-foreground">Change activity across your environments.</p>
        </div>
        <div className="flex gap-1 rounded-md border p-0.5">
          {WINDOWS.map((w) => (
            <Button
              key={w.days}
              size="sm"
              variant={days === w.days ? "secondary" : "ghost"}
              onClick={() => setDays(w.days)}
            >
              {w.label}
            </Button>
          ))}
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-3">
        <StatTile label={`Events (${days}d)`} value={totals.events.toLocaleString()} />
        <StatTile label="Deploys" value={totals.deployTotal.toLocaleString()} />
        <StatTile
          label="Deploy failure rate"
          value={pct(totals.deployFailed, totals.deployTotal)}
          tone={totals.deployFailed > 0 ? "danger" : undefined}
        />
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-base">Activity</CardTitle>
          <CardDescription>Events per day, failures highlighted.</CardDescription>
        </CardHeader>
        <CardContent>
          {activity.isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
          {activity.error && <p className="text-sm text-destructive">Couldn’t load activity.</p>}
          {activity.data && <ActivityChart data={activity.data} />}
        </CardContent>
      </Card>

      <section className="space-y-3">
        <h2 className="text-sm font-medium text-muted-foreground">Environments</h2>
        {deploys.isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
        {deploys.error && <ErrorCard what="deploy stats" />}
        {deploys.data && <EnvHealthCards data={deploys.data} />}
      </section>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-base">Recent changes</CardTitle>
        </CardHeader>
        <CardContent>
          {recent.isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
          {recent.error && <p className="text-sm text-destructive">Couldn’t load recent changes.</p>}
          {recent.data && <RecentChanges events={recent.data} />}
        </CardContent>
      </Card>
    </div>
  );
}

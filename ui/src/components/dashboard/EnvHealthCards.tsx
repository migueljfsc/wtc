import type { components } from "@/api/schema";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { StatusBadge } from "@/components/StatusBadge";
import { pct, relativeTime } from "@/lib/format";

type DeployStats = components["schemas"]["DeployStats"];
type EnvDeployStats = components["schemas"]["EnvDeployStats"];

function Metric({ label, value, tone }: { label: string; value: string; tone?: "danger" }) {
  return (
    <div>
      <div className={"text-lg font-semibold tabular-nums " + (tone === "danger" ? "text-red-600 dark:text-red-500" : "")}>
        {value}
      </div>
      <div className="text-xs text-muted-foreground">{label}</div>
    </div>
  );
}

function EnvCard({ env }: { env: EnvDeployStats }) {
  const rate = env.total > 0 ? env.failed / env.total : 0;
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-3">
        <CardTitle className="font-mono text-base">{env.env}</CardTitle>
        {env.last_status ? (
          <StatusBadge status={env.last_status} />
        ) : (
          <span className="text-xs text-muted-foreground">no deploys</span>
        )}
      </CardHeader>
      <CardContent className="grid grid-cols-3 gap-2">
        <Metric label="deploys" value={String(env.total)} />
        <Metric label="failure rate" value={pct(env.failed, env.total)} tone={rate > 0 ? "danger" : undefined} />
        <Metric label="services" value={String(env.services)} />
        {env.last_ts && (
          <div className="col-span-3 text-xs text-muted-foreground">
            last deploy {relativeTime(env.last_ts)}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export function EnvHealthCards({ data }: { data: DeployStats }) {
  if (data.envs.length === 0) {
    return <p className="text-sm text-muted-foreground">No deploys in this window.</p>;
  }
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      {data.envs.map((e) => (
        <EnvCard key={e.env} env={e} />
      ))}
    </div>
  );
}

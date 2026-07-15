import {
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import type { components } from "@/api/schema";
import { useChartColors } from "@/lib/useChartColors";
import { shortDate } from "@/lib/format";

type ActivityStats = components["schemas"]["ActivityStats"];

interface Row {
  ts: string;
  ok: number;
  failed: number;
  total: number;
}

// Themed tooltip — Recharts' default is light-only, so we render our own with
// the app's surface tokens.
function ChartTooltip({
  active,
  payload,
  label,
}: {
  active?: boolean;
  payload?: { payload: Row }[];
  label?: string;
}) {
  if (!active || !payload?.length) return null;
  const row = payload[0].payload;
  return (
    <div className="rounded-md border bg-card px-3 py-2 text-xs shadow-md">
      <div className="mb-1 font-medium text-foreground">
        {label ? shortDate(label) : ""}
      </div>
      <div className="text-muted-foreground">
        {row.total} event{row.total === 1 ? "" : "s"}
        {row.failed > 0 && (
          <span className="ml-1 text-red-600 dark:text-red-500">
            · {row.failed} failed
          </span>
        )}
      </div>
    </div>
  );
}

export function ActivityChart({ data }: { data: ActivityStats }) {
  const c = useChartColors();
  const rows: Row[] = data.buckets.map((b) => ({
    ts: b.ts,
    ok: b.total - b.failed,
    failed: b.failed,
    total: b.total,
  }));

  return (
    <ResponsiveContainer width="100%" height={220}>
      <BarChart data={rows} margin={{ top: 4, right: 4, bottom: 0, left: -16 }} barCategoryGap="20%">
        <CartesianGrid vertical={false} stroke={c.grid} />
        <XAxis
          dataKey="ts"
          tickFormatter={shortDate}
          tick={{ fill: c.axis, fontSize: 11 }}
          tickLine={false}
          axisLine={{ stroke: c.grid }}
          minTickGap={24}
        />
        <YAxis
          allowDecimals={false}
          width={44}
          tick={{ fill: c.axis, fontSize: 11 }}
          tickLine={false}
          axisLine={false}
        />
        <Tooltip cursor={{ fill: c.grid, opacity: 0.4 }} content={<ChartTooltip />} />
        <Legend
          iconType="circle"
          wrapperStyle={{ fontSize: 12, color: c.axis }}
          formatter={(v) => <span className="text-muted-foreground">{v}</span>}
        />
        {/* 1px surface stroke gives the 2px gap between stacked fills. */}
        <Bar dataKey="ok" name="Events" stackId="a" fill={c.events} stroke={c.surface} strokeWidth={1} />
        <Bar
          dataKey="failed"
          name="Failed"
          stackId="a"
          fill={c.failed}
          stroke={c.surface}
          strokeWidth={1}
          radius={[3, 3, 0, 0]}
        />
      </BarChart>
    </ResponsiveContainer>
  );
}

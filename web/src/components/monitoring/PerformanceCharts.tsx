import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
} from "recharts";
import type { Tier1Snapshot } from "../../types/monitoring";

interface Props {
  history: Tier1Snapshot[];
}

export function PerformanceCharts({ history }: Props) {
  const data = history.slice(-120).map((s) => ({
    time: new Date(s.timestamp).toLocaleTimeString([], {
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    }),
    tps: s.database.txn_commit_rate + s.database.txn_rollback_rate,
    cache_hit: s.database.cache_hit_ratio * 100,
    connections: s.activity.active_queries,
    blk_reads: s.database.blk_read_rate,
  }));

  if (data.length < 2) {
    return (
      <div
        className="rounded-lg border p-8 text-center text-sm"
        style={{
          backgroundColor: "var(--color-surface)",
          borderColor: "var(--color-border)",
          color: "var(--color-text-muted)",
        }}
      >
        Collecting data... charts will appear after a few samples.
      </div>
    );
  }

  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
      <ChartCard title="Transactions / sec" dataKey="tps" data={data} color="#3b82f6" />
      <ChartCard
        title="Cache Hit Ratio %"
        dataKey="cache_hit"
        data={data}
        color="#10b981"
        domain={[
          Math.max(0, Math.min(...data.map((d) => d.cache_hit)) - 2),
          100,
        ]}
      />
      <ChartCard title="Active Queries" dataKey="connections" data={data} color="#f59e0b" />
      <ChartCard title="Block Reads / sec" dataKey="blk_reads" data={data} color="#ef4444" />
    </div>
  );
}

function ChartCard({
  title,
  dataKey,
  data,
  color,
  domain,
}: {
  title: string;
  dataKey: string;
  data: Record<string, unknown>[];
  color: string;
  domain?: [number, number];
}) {
  return (
    <div
      className="rounded-lg border p-4"
      style={{
        backgroundColor: "var(--color-surface)",
        borderColor: "var(--color-border)",
      }}
    >
      <p
        className="text-[11px] font-medium uppercase tracking-wide mb-3"
        style={{ color: "var(--color-text-muted)" }}
      >
        {title}
      </p>
      <ResponsiveContainer width="100%" height={160}>
        <LineChart data={data}>
          <CartesianGrid strokeDasharray="3 3" stroke="var(--color-border)" />
          <XAxis
            dataKey="time"
            tick={{ fontSize: 9, fill: "var(--color-text-muted)" }}
            interval="preserveStartEnd"
          />
          <YAxis
            tick={{ fontSize: 9, fill: "var(--color-text-muted)" }}
            width={40}
            domain={domain}
          />
          <Tooltip
            contentStyle={{
              backgroundColor: "var(--color-surface)",
              borderColor: "var(--color-border)",
              fontSize: 11,
            }}
          />
          <Line
            type="monotone"
            dataKey={dataKey}
            stroke={color}
            strokeWidth={1.5}
            dot={false}
            isAnimationActive={false}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}

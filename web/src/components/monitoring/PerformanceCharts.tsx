import { useState } from "react";
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
  AreaChart,
  Area,
} from "recharts";
import type { Tier1Snapshot } from "../../types/monitoring";

interface Props {
  history: Tier1Snapshot[];
}

type ChartGroup = "overview" | "throughput" | "io" | "wal" | "checkpointer";

const groups: { id: ChartGroup; label: string }[] = [
  { id: "overview", label: "Overview" },
  { id: "throughput", label: "Throughput" },
  { id: "io", label: "I/O & Cache" },
  { id: "wal", label: "WAL" },
  { id: "checkpointer", label: "Checkpointer" },
];

export function PerformanceCharts({ history }: Props) {
  const [group, setGroup] = useState<ChartGroup>("overview");

  const data = history.slice(-300).map((s) => ({
    time: new Date(s.timestamp).toLocaleTimeString([], {
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    }),
    tps: s.database.txn_commit_rate + s.database.txn_rollback_rate,
    commits: s.database.txn_commit_rate,
    rollbacks: s.database.txn_rollback_rate,
    cache_hit: s.database.cache_hit_ratio * 100,
    active: s.activity.active_queries,
    idle: s.activity.idle_connections,
    idle_in_tx: s.activity.idle_in_transaction,
    total_conn: s.activity.total_connections,
    max_conn: s.activity.max_connections,
    waiting: s.activity.waiting_on_lock,
    blk_reads: s.database.blk_read_rate,
    blk_hits: s.database.blk_hit_rate,
    tup_fetched: s.database.tup_fetched_rate,
    tup_inserted: s.database.tup_inserted_rate,
    tup_updated: s.database.tup_updated_rate,
    tup_deleted: s.database.tup_deleted_rate,
    temp_bytes: s.database.temp_bytes_rate,
    deadlocks: s.database.deadlocks_rate,
    wal_bytes: (s.wal?.wal_bytes_rate ?? 0) / 1024 / 1024,
    wal_records: s.wal?.wal_records_rate ?? 0,
    wal_fpi: s.wal?.wal_fpi_rate ?? 0,
    ckpt_timed: s.checkpointer.checkpoints_timed_rate,
    ckpt_req: s.checkpointer.checkpoints_req_rate,
    buffers_ckpt: s.checkpointer.buffers_checkpoint_rate,
    buffers_clean: s.checkpointer.buffers_clean_rate,
    buffers_backend: s.checkpointer.buffers_backend_rate,
    longest_query: s.activity.longest_query_sec,
    repl_lag: s.replication.replay_lag_sec ?? 0,
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
    <div className="space-y-3">
      <div className="flex gap-1 rounded-lg p-1" style={{ backgroundColor: "var(--color-surface)" }}>
        {groups.map((g) => (
          <button
            key={g.id}
            onClick={() => setGroup(g.id)}
            className="px-3 py-1.5 rounded-md text-xs font-medium transition-colors"
            style={{
              backgroundColor: group === g.id ? "var(--color-accent)" : "transparent",
              color: group === g.id ? "#fff" : "var(--color-text-secondary)",
            }}
          >
            {g.label}
          </button>
        ))}
      </div>

      {group === "overview" && (
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
          <ChartCard title="Active Connections" dataKey="active" data={data} color="#f59e0b" />
          <ChartCard title="Longest Query (sec)" dataKey="longest_query" data={data} color="#ef4444" />
        </div>
      )}

      {group === "throughput" && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
          <StackedAreaCard
            title="Transactions / sec"
            data={data}
            areas={[
              { key: "commits", color: "#10b981", label: "Commits" },
              { key: "rollbacks", color: "#ef4444", label: "Rollbacks" },
            ]}
          />
          <StackedAreaCard
            title="Tuple Operations / sec"
            data={data}
            areas={[
              { key: "tup_fetched", color: "#3b82f6", label: "Fetched" },
              { key: "tup_inserted", color: "#10b981", label: "Inserted" },
              { key: "tup_updated", color: "#f59e0b", label: "Updated" },
              { key: "tup_deleted", color: "#ef4444", label: "Deleted" },
            ]}
          />
          <ChartCard title="Temp Bytes / sec" dataKey="temp_bytes" data={data} color="#f59e0b" />
          <ChartCard title="Deadlocks / sec" dataKey="deadlocks" data={data} color="#ef4444" />
        </div>
      )}

      {group === "io" && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
          <ChartCard title="Block Reads / sec" dataKey="blk_reads" data={data} color="#ef4444" />
          <ChartCard title="Block Hits / sec" dataKey="blk_hits" data={data} color="#10b981" />
          <StackedAreaCard
            title="Connections Breakdown"
            data={data}
            areas={[
              { key: "active", color: "#10b981", label: "Active" },
              { key: "idle", color: "#6b7280", label: "Idle" },
              { key: "idle_in_tx", color: "#f59e0b", label: "Idle in Tx" },
              { key: "waiting", color: "#ef4444", label: "Waiting" },
            ]}
          />
          <ChartCard
            title="Replication Lag (sec)"
            dataKey="repl_lag"
            data={data}
            color="#8b5cf6"
          />
        </div>
      )}

      {group === "wal" && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
          <ChartCard title="WAL Generated (MB/sec)" dataKey="wal_bytes" data={data} color="#8b5cf6" />
          <ChartCard title="WAL Records / sec" dataKey="wal_records" data={data} color="#3b82f6" />
          <ChartCard title="Full Page Images / sec" dataKey="wal_fpi" data={data} color="#f59e0b" />
          <div
            className="rounded-lg border p-4 flex items-center justify-center"
            style={{
              backgroundColor: "var(--color-surface)",
              borderColor: "var(--color-border)",
            }}
          >
            <p className="text-xs" style={{ color: "var(--color-text-muted)" }}>
              WAL metrics require PostgreSQL 14+
            </p>
          </div>
        </div>
      )}

      {group === "checkpointer" && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
          <StackedAreaCard
            title="Checkpoints / sec"
            data={data}
            areas={[
              { key: "ckpt_timed", color: "#3b82f6", label: "Timed" },
              { key: "ckpt_req", color: "#ef4444", label: "Requested" },
            ]}
          />
          <StackedAreaCard
            title="Buffers Written / sec"
            data={data}
            areas={[
              { key: "buffers_ckpt", color: "#3b82f6", label: "Checkpoint" },
              { key: "buffers_clean", color: "#10b981", label: "Clean" },
              { key: "buffers_backend", color: "#ef4444", label: "Backend" },
            ]}
          />
        </div>
      )}
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
            width={45}
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

function StackedAreaCard({
  title,
  data,
  areas,
}: {
  title: string;
  data: Record<string, unknown>[];
  areas: { key: string; color: string; label: string }[];
}) {
  return (
    <div
      className="rounded-lg border p-4"
      style={{
        backgroundColor: "var(--color-surface)",
        borderColor: "var(--color-border)",
      }}
    >
      <div className="flex items-center justify-between mb-3">
        <p
          className="text-[11px] font-medium uppercase tracking-wide"
          style={{ color: "var(--color-text-muted)" }}
        >
          {title}
        </p>
        <div className="flex gap-3">
          {areas.map((a) => (
            <div key={a.key} className="flex items-center gap-1">
              <div className="w-2 h-2 rounded-full" style={{ backgroundColor: a.color }} />
              <span className="text-[9px]" style={{ color: "var(--color-text-muted)" }}>{a.label}</span>
            </div>
          ))}
        </div>
      </div>
      <ResponsiveContainer width="100%" height={160}>
        <AreaChart data={data}>
          <CartesianGrid strokeDasharray="3 3" stroke="var(--color-border)" />
          <XAxis
            dataKey="time"
            tick={{ fontSize: 9, fill: "var(--color-text-muted)" }}
            interval="preserveStartEnd"
          />
          <YAxis
            tick={{ fontSize: 9, fill: "var(--color-text-muted)" }}
            width={45}
          />
          <Tooltip
            contentStyle={{
              backgroundColor: "var(--color-surface)",
              borderColor: "var(--color-border)",
              fontSize: 11,
            }}
          />
          {areas.map((a) => (
            <Area
              key={a.key}
              type="monotone"
              dataKey={a.key}
              stackId="1"
              stroke={a.color}
              fill={a.color}
              fillOpacity={0.3}
              strokeWidth={1.5}
              isAnimationActive={false}
            />
          ))}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}

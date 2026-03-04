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

type ChartGroup = "overview" | "sessions" | "throughput" | "io" | "health" | "wal" | "checkpointer";

const groups: { id: ChartGroup; label: string }[] = [
  { id: "overview", label: "Overview" },
  { id: "sessions", label: "Sessions" },
  { id: "throughput", label: "Throughput" },
  { id: "io", label: "I/O & Cache" },
  { id: "health", label: "Health" },
  { id: "wal", label: "WAL" },
  { id: "checkpointer", label: "Checkpointer" },
];

const stateKeys = ["active", "idle", "idle_in_tx", "idle_in_tx_aborted", "waiting", "disabled", "fastpath"] as const;
const stateColors: Record<string, string> = {
  active: "#10b981",
  idle: "#6b7280",
  idle_in_tx: "#f59e0b",
  idle_in_tx_aborted: "#ef4444",
  waiting: "#8b5cf6",
  disabled: "#374151",
  fastpath: "#06b6d4",
};
const stateLabels: Record<string, string> = {
  active: "Active",
  idle: "Idle",
  idle_in_tx: "Idle in Tx",
  idle_in_tx_aborted: "Idle in Tx (aborted)",
  waiting: "Waiting",
  disabled: "Disabled",
  fastpath: "Fastpath",
};

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(1024));
  const idx = Math.min(i, units.length - 1);
  return `${(bytes / Math.pow(1024, idx)).toFixed(1)} ${units[idx]}`;
}

function formatUTC(ts: string): string {
  const d = new Date(ts);
  const pad = (n: number) => n.toString().padStart(2, "0");
  return `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}`;
}

export function PerformanceCharts({ history }: Props) {
  const [group, setGroup] = useState<ChartGroup>("overview");

  const data = history.slice(-300).map((s) => {
    const byState = s.activity.by_state ?? {};
    return {
      time: formatUTC(s.timestamp),
      tps: s.database.txn_commit_rate + s.database.txn_rollback_rate,
      commits: s.database.txn_commit_rate,
      rollbacks: s.database.txn_rollback_rate,
      cache_hit: s.database.cache_hit_ratio * 100,
      active: byState["active"] ?? s.activity.active_queries,
      idle: byState["idle"] ?? s.activity.idle_connections,
      idle_in_tx: (byState["idle in transaction"] ?? 0),
      idle_in_tx_aborted: (byState["idle in transaction (aborted)"] ?? 0),
      waiting: s.activity.waiting_on_lock,
      disabled: byState["disabled"] ?? 0,
      fastpath: byState["fastpath function call"] ?? 0,
      total_conn: s.activity.total_connections,
      max_conn: s.activity.max_connections,
      blocked_locks: s.activity.blocked_locks ?? 0,
      blk_reads: s.database.blk_read_rate,
      blk_hits: s.database.blk_hit_rate,
      tup_fetched: s.database.tup_fetched_rate,
      tup_inserted: s.database.tup_inserted_rate,
      tup_updated: s.database.tup_updated_rate,
      tup_deleted: s.database.tup_deleted_rate,
      temp_files: s.database.temp_files_rate,
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
      txid_age_pct: (s.health?.txid_age_pct ?? 0) * 100,
      mxid_age_pct: (s.health?.mxid_age_pct ?? 0) * 100,
    };
  });

  const waitEventData = history.slice(-300).map((s) => {
    const obj: Record<string, unknown> = {
      time: formatUTC(s.timestamp),
    };
    for (const [k, v] of Object.entries(s.activity.by_wait_event ?? {})) {
      obj[k] = v;
    }
    return obj;
  });

  const allWaitEvents = Array.from(
    new Set(history.flatMap((s) => Object.keys(s.activity.by_wait_event ?? {})))
  );
  const waitEventColors = ["#3b82f6", "#10b981", "#f59e0b", "#ef4444", "#8b5cf6", "#06b6d4", "#ec4899", "#84cc16"];

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

  const usedStates = stateKeys.filter((k) => data.some((d) => (d[k] as number) > 0));

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
          <StackedAreaCard
            title="Connection States"
            data={data}
            areas={usedStates.map((k) => ({
              key: k,
              color: stateColors[k] ?? "#6b7280",
              label: stateLabels[k] ?? k,
            }))}
          />
          <ChartCard title="Longest Query (sec)" dataKey="longest_query" data={data} color="#ef4444" />
        </div>
      )}

      {group === "sessions" && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
          <StackedAreaCard
            title="Connection States"
            data={data}
            areas={usedStates.map((k) => ({
              key: k,
              color: stateColors[k] ?? "#6b7280",
              label: stateLabels[k] ?? k,
            }))}
          />
          {allWaitEvents.length > 0 && (
            <StackedAreaCard
              title="Wait Events (active sessions)"
              data={waitEventData}
              areas={allWaitEvents.map((evt, i) => ({
                key: evt,
                color: waitEventColors[i % waitEventColors.length],
                label: evt,
              }))}
            />
          )}
          <ChartCard title="Blocked Locks" dataKey="blocked_locks" data={data} color="#ef4444" />
          <ChartCard title="Longest Query (sec)" dataKey="longest_query" data={data} color="#f59e0b" />
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
          <ChartCard title="Temp Files / sec" dataKey="temp_files" data={data} color="#f59e0b" />
          <ChartCard
            title="Temp Bytes Written / sec"
            dataKey="temp_bytes"
            data={data}
            color="#ef4444"
            formatValue={formatBytes}
          />
        </div>
      )}

      {group === "io" && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
          <ChartCard title="Block Reads / sec" dataKey="blk_reads" data={data} color="#ef4444" />
          <ChartCard title="Block Hits / sec" dataKey="blk_hits" data={data} color="#10b981" />
          <ChartCard title="Deadlocks / sec" dataKey="deadlocks" data={data} color="#ef4444" />
          <ChartCard
            title="Replication Lag (sec)"
            dataKey="repl_lag"
            data={data}
            color="#8b5cf6"
          />
        </div>
      )}

      {group === "health" && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
          <ChartCard
            title="Transaction ID Age %"
            dataKey="txid_age_pct"
            data={data}
            color="#f59e0b"
            domain={[0, Math.max(100, ...data.map((d) => d.txid_age_pct))]}
            warnThreshold={50}
          />
          <ChartCard
            title="Multi-Transaction ID Age %"
            dataKey="mxid_age_pct"
            data={data}
            color="#8b5cf6"
            domain={[0, Math.max(100, ...data.map((d) => d.mxid_age_pct))]}
            warnThreshold={50}
          />
          <ChartCard
            title="Replication Lag (sec)"
            dataKey="repl_lag"
            data={data}
            color="#3b82f6"
          />
          <ChartCard title="Blocked Locks" dataKey="blocked_locks" data={data} color="#ef4444" />
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
  formatValue,
  warnThreshold,
}: {
  title: string;
  dataKey: string;
  data: Record<string, unknown>[];
  color: string;
  domain?: [number, number];
  formatValue?: (v: number) => string;
  warnThreshold?: number;
}) {
  const lastValue = data.length > 0 ? (data[data.length - 1][dataKey] as number) : 0;
  const isWarning = warnThreshold !== undefined && lastValue > warnThreshold;

  return (
    <div
      className="rounded-lg border p-4"
      style={{
        backgroundColor: "var(--color-surface)",
        borderColor: isWarning ? "#f59e0b" : "var(--color-border)",
      }}
    >
      <div className="flex items-center justify-between mb-3">
        <p
          className="text-[11px] font-medium uppercase tracking-wide"
          style={{ color: "var(--color-text-muted)" }}
        >
          {title}
        </p>
        {data.length > 0 && (
          <span className="text-xs font-mono tabular-nums" style={{ color: isWarning ? "#f59e0b" : "var(--color-text-secondary)" }}>
            {formatValue ? formatValue(lastValue) : Number.isFinite(lastValue) ? lastValue.toFixed(1) : "—"}
          </span>
        )}
      </div>
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
            tickFormatter={formatValue}
          />
          <Tooltip
            contentStyle={{
              backgroundColor: "var(--color-surface)",
              borderColor: "var(--color-border)",
              fontSize: 11,
            }}
            formatter={(value: number) => [formatValue ? formatValue(value) : value.toFixed(2), title]}
          />
          <Line
            type="monotone"
            dataKey={dataKey}
            stroke={isWarning ? "#f59e0b" : color}
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
        <div className="flex gap-3 flex-wrap justify-end">
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

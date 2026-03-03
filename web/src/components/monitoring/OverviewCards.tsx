import {
  Users,
  Zap,
  Database,
  GitBranch,
} from "lucide-react";
import type { NodeMonitoringSnapshot } from "../../types/monitoring";

interface Props {
  node: NodeMonitoringSnapshot;
}

export function OverviewCards({ node }: Props) {
  const t1 = node.tier1;
  const db = t1?.database;
  const act = t1?.activity;
  const repl = t1?.replication;

  const cards = [
    {
      label: "Connections",
      value: act ? `${act.active_queries} / ${act.total_connections}` : "—",
      sub: act ? `of ${act.max_connections} max` : "",
      icon: Users,
      color: act && act.total_connections / act.max_connections > 0.8 ? "#ef4444" : "#3b82f6",
    },
    {
      label: "TPS",
      value: db ? `${(db.txn_commit_rate + db.txn_rollback_rate).toFixed(0)}` : "—",
      sub: db ? `${db.txn_rollback_rate.toFixed(1)} rollback/s` : "",
      icon: Zap,
      color: "#10b981",
    },
    {
      label: "Cache Hit Ratio",
      value: db ? `${(db.cache_hit_ratio * 100).toFixed(1)}%` : "—",
      sub: db ? `${db.blk_read_rate.toFixed(0)} blk reads/s` : "",
      icon: Database,
      color: db && db.cache_hit_ratio < 0.95 ? "#f59e0b" : "#10b981",
    },
    {
      label: "Replication",
      value: repl?.is_replica
        ? `${(repl.replay_lag_sec ?? 0).toFixed(1)}s lag`
        : repl?.standbys?.length
          ? `${repl.standbys.length} standby`
          : "Primary",
      sub: repl?.is_replica
        ? formatBytes(repl.replay_lag_bytes ?? 0)
        : "",
      icon: GitBranch,
      color: repl?.is_replica && (repl.replay_lag_sec ?? 0) > 10 ? "#ef4444" : "#6b7280",
    },
  ];

  return (
    <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
      {cards.map((c) => (
        <div
          key={c.label}
          className="rounded-lg border p-4"
          style={{
            backgroundColor: "var(--color-surface)",
            borderColor: "var(--color-border)",
          }}
        >
          <div className="flex items-center gap-2 mb-2">
            <c.icon className="w-3.5 h-3.5" style={{ color: c.color }} />
            <span
              className="text-[11px] font-medium uppercase tracking-wide"
              style={{ color: "var(--color-text-muted)" }}
            >
              {c.label}
            </span>
          </div>
          <div className="text-xl font-semibold" style={{ color: "var(--color-text)" }}>
            {c.value}
          </div>
          {c.sub && (
            <div className="text-xs mt-0.5" style={{ color: "var(--color-text-muted)" }}>
              {c.sub}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "kB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`;
}

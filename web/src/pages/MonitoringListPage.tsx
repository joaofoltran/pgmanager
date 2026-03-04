import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  Activity,
  Loader2,
  WifiOff,
  RefreshCw,
  Database,
  ChevronRight,
  AlertTriangle,
} from "lucide-react";
import type { MonitoringClusterSummary } from "../types/monitoring";
import { fetchMonitoredClusters } from "../api/client";

export function MonitoringListPage() {
  const navigate = useNavigate();
  const [clusters, setClusters] = useState<MonitoringClusterSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [apiDown, setApiDown] = useState(false);

  async function loadData() {
    try {
      const data = await fetchMonitoredClusters();
      setClusters(data || []);
      setApiDown(false);
    } catch {
      setClusters([]);
      setApiDown(true);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadData();
    const interval = setInterval(loadData, 5000);
    return () => clearInterval(interval);
  }, []);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="w-8 h-8 animate-spin" style={{ color: "var(--color-accent)" }} />
      </div>
    );
  }

  return (
    <div className="space-y-4 max-w-5xl">
      <div>
        <h2 className="text-lg font-semibold" style={{ color: "var(--color-text)" }}>
          Monitoring
        </h2>
        <p className="text-sm mt-0.5" style={{ color: "var(--color-text-muted)" }}>
          Real-time PostgreSQL metrics and performance insights
        </p>
      </div>

      {apiDown && (
        <div
          className="rounded-lg border p-8 text-center"
          style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}
        >
          <WifiOff className="w-8 h-8 mx-auto mb-3" style={{ color: "var(--color-text-muted)" }} />
          <p className="font-medium" style={{ color: "var(--color-text)" }}>Unable to reach API</p>
          <p className="text-sm mt-1" style={{ color: "var(--color-text-muted)" }}>
            Check that the pgmanager process is running.
          </p>
          <button
            onClick={() => { setLoading(true); setApiDown(false); loadData(); }}
            className="mt-4 flex items-center gap-2 mx-auto px-4 py-2 rounded-lg text-sm transition-colors hover:bg-white/5"
            style={{ color: "var(--color-accent)" }}
          >
            <RefreshCw className="w-4 h-4" /> Retry
          </button>
        </div>
      )}

      {!apiDown && clusters.length === 0 && (
        <div
          className="rounded-lg border p-12 text-center"
          style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}
        >
          <Database className="w-12 h-12 mx-auto mb-4" style={{ color: "var(--color-text-muted)" }} />
          <h3 className="text-sm font-medium mb-1" style={{ color: "var(--color-text)" }}>
            No clusters monitored
          </h3>
          <p className="text-sm" style={{ color: "var(--color-text-muted)" }}>
            Enable monitoring on nodes via the Cluster settings page.
          </p>
        </div>
      )}

      {!apiDown && clusters.length > 0 && (
        <div className="space-y-2">
          {clusters.map((c) => {
            const connPct = c.max_connections > 0 ? (c.total_connections / c.max_connections) * 100 : 0;
            const hasWarning = c.txid_age_pct > 0.5 || c.blocked_locks > 0 || (c.cache_hit_ratio > 0 && c.cache_hit_ratio < 0.95);
            return (
              <div
                key={c.cluster_id}
                className="rounded-lg border cursor-pointer transition-colors hover:border-[var(--color-accent)]/30"
                style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}
                onClick={() => navigate(`/monitoring/${c.cluster_id}`)}
              >
                <div className="flex items-center gap-3 p-4">
                  <div
                    className="w-8 h-8 rounded-lg flex items-center justify-center shrink-0"
                    style={{ backgroundColor: hasWarning ? "#f59e0b" : "var(--color-accent)" }}
                  >
                    {hasWarning ? (
                      <AlertTriangle className="w-4 h-4 text-white" />
                    ) : (
                      <Activity className="w-4 h-4 text-white" />
                    )}
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="text-sm font-medium" style={{ color: "var(--color-text)" }}>
                      {c.cluster_name || c.cluster_id}
                    </div>
                    <div className="text-xs" style={{ color: "var(--color-text-muted)" }}>
                      {c.nodes_ok}/{c.nodes_total} node{c.nodes_total !== 1 ? "s" : ""} healthy
                    </div>
                  </div>

                  <div className="hidden sm:flex items-center gap-4 text-xs shrink-0">
                    <MetricPill label="TPS" value={c.tps.toFixed(0)} />
                    <MetricPill
                      label="Cache"
                      value={c.cache_hit_ratio > 0 ? `${(c.cache_hit_ratio * 100).toFixed(1)}%` : "—"}
                      warn={c.cache_hit_ratio > 0 && c.cache_hit_ratio < 0.95}
                    />
                    <MetricPill
                      label="Conn"
                      value={`${c.total_connections}/${c.max_connections}`}
                      warn={connPct > 80}
                    />
                    {c.replication_lag_sec > 0 && (
                      <MetricPill
                        label="Lag"
                        value={`${c.replication_lag_sec.toFixed(1)}s`}
                        warn={c.replication_lag_sec > 5}
                      />
                    )}
                    {c.blocked_locks > 0 && (
                      <MetricPill label="Blocked" value={String(c.blocked_locks)} warn />
                    )}
                    {c.txid_age_pct > 0.3 && (
                      <MetricPill
                        label="TXID"
                        value={`${(c.txid_age_pct * 100).toFixed(0)}%`}
                        warn={c.txid_age_pct > 0.5}
                      />
                    )}
                  </div>

                  <ChevronRight className="w-4 h-4 shrink-0" style={{ color: "var(--color-text-muted)" }} />
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function MetricPill({ label, value, warn }: { label: string; value: string; warn?: boolean }) {
  return (
    <div className="flex flex-col items-center min-w-[48px]">
      <span
        className="text-[10px] uppercase tracking-wide"
        style={{ color: "var(--color-text-muted)" }}
      >
        {label}
      </span>
      <span
        className="font-mono tabular-nums text-xs font-medium"
        style={{ color: warn ? "#f59e0b" : "var(--color-text)" }}
      >
        {value}
      </span>
    </div>
  );
}

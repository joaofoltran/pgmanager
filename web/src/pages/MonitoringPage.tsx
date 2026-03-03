import { useEffect, useState } from "react";
import {
  Activity,
  Loader2,
  WifiOff,
  RefreshCw,
  Database,
  ChevronRight,
} from "lucide-react";
import type { Cluster } from "../types/cluster";
import {
  fetchClusters,
  fetchMonitoringStatus,
} from "../api/client";
import { useMonitoring } from "../hooks/useMonitoring";
import { PerformanceCharts } from "../components/monitoring/PerformanceCharts";
import { ActivitySection } from "../components/monitoring/ActivitySection";
import { TableStatsSection } from "../components/monitoring/TableStatsSection";
import { SizesSection } from "../components/monitoring/SizesSection";
import { SlowQueryLog } from "../components/monitoring/SlowQueryLog";

export function MonitoringPage() {
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [monitoredIds, setMonitoredIds] = useState<string[]>([]);
  const [selectedCluster, setSelectedCluster] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [apiDown, setApiDown] = useState(false);

  const { overview, tier2, loading: metricsLoading } = useMonitoring(
    selectedCluster || null
  );

  async function loadData() {
    try {
      const [data, status] = await Promise.all([
        fetchClusters(),
        fetchMonitoringStatus(),
      ]);
      setClusters(data || []);
      const ids = status.monitored_clusters || [];
      setMonitoredIds(ids);
      setApiDown(false);

      if (ids.length > 0 && !selectedCluster) {
        setSelectedCluster(ids[0]);
      }
    } catch {
      setClusters([]);
      setApiDown(true);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    loadData();
    const interval = setInterval(async () => {
      try {
        const status = await fetchMonitoringStatus();
        setMonitoredIds(status.monitored_clusters || []);
      } catch {}
    }, 10000);
    return () => clearInterval(interval);
  }, []);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="w-8 h-8 animate-spin" style={{ color: "var(--color-accent)" }} />
      </div>
    );
  }

  const monitoredClusters = clusters.filter((c) => monitoredIds.includes(c.id));
  const primaryNode = overview?.nodes?.find(
    (n) => n.tier1 && n.status === "ok"
  ) ?? overview?.nodes?.[0];

  return (
    <div className="space-y-4 max-w-5xl">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold" style={{ color: "var(--color-text)" }}>
            Monitoring
          </h2>
          <p className="text-sm mt-0.5" style={{ color: "var(--color-text-muted)" }}>
            Real-time PostgreSQL metrics and performance insights
          </p>
        </div>
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

      {!apiDown && monitoredClusters.length === 0 && (
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

      {!apiDown && monitoredClusters.length > 0 && !selectedCluster && (
        <div className="space-y-2">
          {monitoredClusters.map((c) => (
            <button
              key={c.id}
              onClick={() => setSelectedCluster(c.id)}
              className="w-full rounded-lg border p-4 flex items-center gap-3 text-left transition-colors hover:border-[var(--color-accent)]/30"
              style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}
            >
              <div className="w-8 h-8 rounded-lg flex items-center justify-center" style={{ backgroundColor: "var(--color-accent)" }}>
                <Activity className="w-4 h-4 text-white" />
              </div>
              <div className="flex-1 min-w-0">
                <div className="text-sm font-medium" style={{ color: "var(--color-text)" }}>{c.name}</div>
                <div className="text-xs" style={{ color: "var(--color-text-muted)" }}>
                  {c.nodes.filter((n) => n.monitoring_enabled).length} node{c.nodes.filter((n) => n.monitoring_enabled).length !== 1 ? "s" : ""} monitored
                </div>
              </div>
              <ChevronRight className="w-4 h-4" style={{ color: "var(--color-text-muted)" }} />
            </button>
          ))}
        </div>
      )}

      {!apiDown && selectedCluster && (
        <>
          {monitoredClusters.length > 1 && (
            <div className="flex items-center gap-2">
              {monitoredClusters.map((c) => (
                <button
                  key={c.id}
                  onClick={() => setSelectedCluster(c.id)}
                  className="px-3 py-1.5 rounded-lg text-xs font-medium transition-colors"
                  style={{
                    backgroundColor: selectedCluster === c.id ? "var(--color-accent)" : "var(--color-surface)",
                    color: selectedCluster === c.id ? "#fff" : "var(--color-text-secondary)",
                    borderWidth: 1,
                    borderColor: selectedCluster === c.id ? "var(--color-accent)" : "var(--color-border)",
                  }}
                >
                  {c.name}
                </button>
              ))}
            </div>
          )}

          {!overview && metricsLoading && (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="w-6 h-6 animate-spin" style={{ color: "var(--color-accent)" }} />
            </div>
          )}

          {!overview && !metricsLoading && (
            <div
              className="rounded-lg border p-12 text-center"
              style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}
            >
              <Activity className="w-12 h-12 mx-auto mb-4" style={{ color: "var(--color-text-muted)" }} />
              <h3 className="text-sm font-medium mb-1" style={{ color: "var(--color-text)" }}>
                Connecting...
              </h3>
              <p className="text-sm" style={{ color: "var(--color-text-muted)" }}>
                Waiting for first metrics from cluster nodes.
              </p>
            </div>
          )}

          {overview && primaryNode && (
            <>
              {overview.nodes.length > 1 && (
                <div className="flex gap-2">
                  {overview.nodes.map((n) => (
                    <div
                      key={n.node_id}
                      className="flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-full"
                      style={{
                        backgroundColor: "var(--color-surface)",
                        borderColor: "var(--color-border)",
                        color: n.status === "ok" ? "var(--color-text)" : "var(--color-text-muted)",
                      }}
                    >
                      <span
                        className="w-1.5 h-1.5 rounded-full"
                        style={{
                          backgroundColor:
                            n.status === "ok"
                              ? "#10b981"
                              : n.status === "error"
                                ? "#ef4444"
                                : "#6b7280",
                        }}
                      />
                      {n.node_name || n.node_id}
                    </div>
                  ))}
                </div>
              )}

              <PerformanceCharts history={overview.history ?? []} />

              {primaryNode.tier1?.activity && (
                <ActivitySection activity={primaryNode.tier1.activity} />
              )}

              <SlowQueryLog clusterId={selectedCluster} nodeId={primaryNode.node_id} />

              <TableStatsSection tier2={tier2[primaryNode.node_id] ?? null} />

              <SizesSection clusterId={selectedCluster} nodeId={primaryNode.node_id} />
            </>
          )}
        </>
      )}
    </div>
  );
}

import { useEffect, useState } from "react";
import {
  Activity,
  Loader2,
  WifiOff,
  RefreshCw,
  ChevronDown,
  Database,
  Play,
  Square,
} from "lucide-react";
import type { Cluster } from "../types/cluster";
import {
  fetchClusters,
  startMonitoring,
  stopMonitoring,
  fetchMonitoringStatus,
} from "../api/client";
import { useMonitoring } from "../hooks/useMonitoring";
import { OverviewCards } from "../components/monitoring/OverviewCards";
import { ActivitySection } from "../components/monitoring/ActivitySection";
import { PerformanceCharts } from "../components/monitoring/PerformanceCharts";
import { TableStatsSection } from "../components/monitoring/TableStatsSection";
import { SizesSection } from "../components/monitoring/SizesSection";

export function MonitoringPage() {
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [selectedCluster, setSelectedCluster] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [apiDown, setApiDown] = useState(false);
  const [isMonitored, setIsMonitored] = useState(false);
  const [starting, setStarting] = useState(false);

  const { overview, tier2, loading: metricsLoading } = useMonitoring(
    isMonitored ? selectedCluster : null
  );

  async function loadClusters() {
    try {
      const data = await fetchClusters();
      setClusters(data || []);
      setApiDown(false);
      if (data && data.length > 0 && !selectedCluster) {
        setSelectedCluster(data[0].id);
      }
    } catch {
      setClusters([]);
      setApiDown(true);
    } finally {
      setLoading(false);
    }
  }

  async function checkMonitoringStatus(clusterId: string) {
    try {
      const status = await fetchMonitoringStatus();
      setIsMonitored(status.monitored_clusters?.includes(clusterId) ?? false);
    } catch {
      setIsMonitored(false);
    }
  }

  useEffect(() => {
    loadClusters();
  }, []);

  useEffect(() => {
    if (selectedCluster) {
      checkMonitoringStatus(selectedCluster);
    }
  }, [selectedCluster]);

  async function handleStart() {
    if (!selectedCluster) return;
    setStarting(true);
    try {
      await startMonitoring(selectedCluster);
      setIsMonitored(true);
    } catch (err: unknown) {
      alert(err instanceof Error ? err.message : "Failed to start monitoring");
    } finally {
      setStarting(false);
    }
  }

  async function handleStop() {
    if (!selectedCluster) return;
    try {
      await stopMonitoring(selectedCluster);
      setIsMonitored(false);
    } catch (err: unknown) {
      alert(err instanceof Error ? err.message : "Failed to stop monitoring");
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="w-8 h-8 animate-spin" style={{ color: "var(--color-accent)" }} />
      </div>
    );
  }

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
            onClick={() => { setLoading(true); setApiDown(false); loadClusters(); }}
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
            No clusters registered
          </h3>
          <p className="text-sm" style={{ color: "var(--color-text-muted)" }}>
            Register a cluster first to start monitoring.
          </p>
        </div>
      )}

      {!apiDown && clusters.length > 0 && (
        <>
          <div
            className="rounded-lg border p-4"
            style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}
          >
            <div className="flex items-center gap-4">
              <div className="flex-1">
                <label className="block text-xs mb-1.5" style={{ color: "var(--color-text-secondary)" }}>
                  Cluster
                </label>
                <div className="relative">
                  <select
                    value={selectedCluster}
                    onChange={(e) => setSelectedCluster(e.target.value)}
                    className="w-full rounded-md border px-3 py-2 text-sm appearance-none pr-8"
                    style={{
                      backgroundColor: "var(--color-bg)",
                      borderColor: "var(--color-border)",
                      color: "var(--color-text)",
                    }}
                  >
                    {clusters.map((c) => (
                      <option key={c.id} value={c.id}>
                        {c.name} ({c.nodes.length} node{c.nodes.length !== 1 ? "s" : ""})
                      </option>
                    ))}
                  </select>
                  <ChevronDown
                    className="w-4 h-4 absolute right-2 top-1/2 -translate-y-1/2 pointer-events-none"
                    style={{ color: "var(--color-text-muted)" }}
                  />
                </div>
              </div>
              <div className="pt-5">
                {isMonitored ? (
                  <button
                    onClick={handleStop}
                    className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors hover:bg-red-500/10"
                    style={{ color: "#ef4444" }}
                  >
                    <Square className="w-4 h-4" />
                    Stop
                  </button>
                ) : (
                  <button
                    onClick={handleStart}
                    disabled={starting}
                    className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors"
                    style={{ backgroundColor: "var(--color-accent)", color: "#fff" }}
                  >
                    {starting ? (
                      <Loader2 className="w-4 h-4 animate-spin" />
                    ) : (
                      <Play className="w-4 h-4" />
                    )}
                    Start Monitoring
                  </button>
                )}
              </div>
            </div>
          </div>

          {isMonitored && !overview && metricsLoading && (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="w-6 h-6 animate-spin" style={{ color: "var(--color-accent)" }} />
            </div>
          )}

          {isMonitored && !overview && !metricsLoading && (
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

          {isMonitored && overview && primaryNode && (
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

              <OverviewCards node={primaryNode} />

              {primaryNode.tier1?.activity && (
                <ActivitySection activity={primaryNode.tier1.activity} />
              )}

              <PerformanceCharts history={overview.history ?? []} />

              <TableStatsSection tier2={tier2[primaryNode.node_id] ?? null} />

              <SizesSection clusterId={selectedCluster} nodeId={primaryNode.node_id} />
            </>
          )}

          {!isMonitored && (
            <div
              className="rounded-lg border p-12 text-center"
              style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}
            >
              <Activity className="w-12 h-12 mx-auto mb-4" style={{ color: "var(--color-text-muted)" }} />
              <h3 className="text-sm font-medium mb-1" style={{ color: "var(--color-text)" }}>
                Monitoring not active
              </h3>
              <p className="text-sm" style={{ color: "var(--color-text-muted)" }}>
                Click "Start Monitoring" to begin collecting metrics from this cluster.
              </p>
            </div>
          )}
        </>
      )}
    </div>
  );
}

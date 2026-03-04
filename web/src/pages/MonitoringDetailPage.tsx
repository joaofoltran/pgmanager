import { useEffect, useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import {
  Activity,
  Loader2,
  WifiOff,
  RefreshCw,
  ArrowLeft,
} from "lucide-react";
import { fetchMonitoringStatus } from "../api/client";
import { useMonitoring } from "../hooks/useMonitoring";
import { PerformanceCharts } from "../components/monitoring/PerformanceCharts";
import { ActivitySection } from "../components/monitoring/ActivitySection";
import { TableStatsSection } from "../components/monitoring/TableStatsSection";
import { SizesSection } from "../components/monitoring/SizesSection";
import { SlowQueryLog } from "../components/monitoring/SlowQueryLog";
import { TimeRangePicker, type TimeRange } from "../components/monitoring/TimeRangePicker";

export function MonitoringDetailPage() {
  const { clusterId } = useParams<{ clusterId: string }>();
  const navigate = useNavigate();
  const [apiDown, setApiDown] = useState(false);
  const [timeRange, setTimeRange] = useState<TimeRange | null>(null);

  const { overview, tier2, loading: metricsLoading, isLive } = useMonitoring(
    clusterId || null,
    timeRange,
  );

  useEffect(() => {
    if (!clusterId) return;
    fetchMonitoringStatus()
      .then((s) => {
        if (!s.monitored_clusters?.includes(clusterId)) {
          navigate("/monitoring", { replace: true });
        }
        setApiDown(false);
      })
      .catch(() => setApiDown(true));
  }, [clusterId]);

  const primaryNode = overview?.nodes?.find(
    (n) => n.tier1 && n.status === "ok"
  ) ?? overview?.nodes?.[0];

  return (
    <div className="space-y-4 max-w-5xl">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <button
            onClick={() => navigate("/monitoring")}
            className="p-1.5 rounded-md transition-colors hover:bg-white/5"
            style={{ color: "var(--color-text-muted)" }}
          >
            <ArrowLeft className="w-4 h-4" />
          </button>
          <div>
            <h2 className="text-lg font-semibold" style={{ color: "var(--color-text)" }}>
              {overview?.cluster_name || clusterId}
            </h2>
            <p className="text-sm mt-0.5" style={{ color: "var(--color-text-muted)" }}>
              {isLive ? "Real-time monitoring" : "Historical view (UTC)"}
            </p>
          </div>
        </div>
        <TimeRangePicker value={timeRange} onChange={setTimeRange} />
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
            onClick={() => setApiDown(false)}
            className="mt-4 flex items-center gap-2 mx-auto px-4 py-2 rounded-lg text-sm transition-colors hover:bg-white/5"
            style={{ color: "var(--color-accent)" }}
          >
            <RefreshCw className="w-4 h-4" /> Retry
          </button>
        </div>
      )}

      {!apiDown && !overview && metricsLoading && (
        <div className="flex items-center justify-center py-12">
          <Loader2 className="w-6 h-6 animate-spin" style={{ color: "var(--color-accent)" }} />
        </div>
      )}

      {!apiDown && !overview && !metricsLoading && (
        <div
          className="rounded-lg border p-12 text-center"
          style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}
        >
          <Activity className="w-12 h-12 mx-auto mb-4" style={{ color: "var(--color-text-muted)" }} />
          <h3 className="text-sm font-medium mb-1" style={{ color: "var(--color-text)" }}>
            {isLive ? "Connecting..." : "No data"}
          </h3>
          <p className="text-sm" style={{ color: "var(--color-text-muted)" }}>
            {isLive
              ? "Waiting for first metrics from cluster nodes."
              : "No snapshots found for the selected time range."}
          </p>
        </div>
      )}

      {!apiDown && overview && primaryNode && (
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

          {isLive && primaryNode.tier1?.activity && (
            <ActivitySection activity={primaryNode.tier1.activity} />
          )}

          {isLive && (
            <SlowQueryLog clusterId={clusterId!} nodeId={primaryNode.node_id} />
          )}

          {isLive && (
            <TableStatsSection tier2={tier2[primaryNode.node_id] ?? null} />
          )}

          {isLive && (
            <SizesSection clusterId={clusterId!} nodeId={primaryNode.node_id} />
          )}
        </>
      )}
    </div>
  );
}

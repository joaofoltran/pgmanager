import { useState } from "react";
import { ChevronDown, RefreshCw } from "lucide-react";
import type { Tier3Snapshot } from "../../types/monitoring";
import { fetchNodeSizes, refreshNodeSizes } from "../../api/client";

interface Props {
  clusterId: string;
  nodeId: string;
}

export function SizesSection({ clusterId, nodeId }: Props) {
  const [expanded, setExpanded] = useState(false);
  const [tier3, setTier3] = useState<Tier3Snapshot | null>(null);
  const [loading, setLoading] = useState(false);
  const [tab, setTab] = useState<"sizes" | "bloat" | "queries">("sizes");

  async function handleRefresh() {
    setLoading(true);
    try {
      await refreshNodeSizes(clusterId, nodeId);
      const data = await fetchNodeSizes(clusterId, nodeId);
      setTier3(data);
    } catch {
      // Silently fail — data might not be ready yet.
    } finally {
      setLoading(false);
    }
  }

  async function handleExpand() {
    const next = !expanded;
    setExpanded(next);
    if (next && !tier3) {
      try {
        const data = await fetchNodeSizes(clusterId, nodeId);
        setTier3(data);
      } catch {
        // Will show empty state.
      }
    }
  }

  return (
    <div
      className="rounded-lg border"
      style={{
        backgroundColor: "var(--color-surface)",
        borderColor: "var(--color-border)",
      }}
    >
      <div className="flex items-center">
        <button
          className="flex-1 flex items-center gap-2 p-4 text-left"
          onClick={handleExpand}
        >
          <ChevronDown
            className={`w-4 h-4 transition-transform ${expanded ? "" : "-rotate-90"}`}
            style={{ color: "var(--color-text-muted)" }}
          />
          <span className="text-sm font-medium" style={{ color: "var(--color-text)" }}>
            Sizes, Bloat & Top Queries
          </span>
          <span className="text-[10px] px-1.5 py-0.5 rounded font-medium" style={{
            backgroundColor: "var(--color-border)",
            color: "var(--color-text-muted)",
          }}>
            On-demand
          </span>
        </button>
        {expanded && (
          <button
            onClick={handleRefresh}
            className="mr-4 flex items-center gap-1.5 px-3 py-1.5 rounded text-xs transition-colors hover:bg-white/5"
            style={{ color: "var(--color-text-secondary)" }}
          >
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? "animate-spin" : ""}`} />
            Refresh
          </button>
        )}
      </div>

      {expanded && (
        <div className="border-t px-4 pb-4" style={{ borderColor: "var(--color-border)" }}>
          <div className="flex gap-1 mt-3 mb-3">
            {(["sizes", "bloat", "queries"] as const).map((t) => (
              <button
                key={t}
                onClick={() => setTab(t)}
                className="px-3 py-1 rounded text-xs font-medium transition-colors"
                style={{
                  backgroundColor: tab === t ? "var(--color-accent)" : "transparent",
                  color: tab === t ? "#fff" : "var(--color-text-secondary)",
                }}
              >
                {t === "queries" ? "Top Queries" : t.charAt(0).toUpperCase() + t.slice(1)}
              </button>
            ))}
          </div>

          {!tier3 ? (
            <div className="py-6 text-center text-xs" style={{ color: "var(--color-text-muted)" }}>
              Click Refresh to collect size data. This runs heavier queries.
            </div>
          ) : (
            <>
              {tab === "sizes" && <SizesTab sizes={tier3.sizes ?? []} />}
              {tab === "bloat" && <BloatTab bloat={tier3.bloat ?? []} />}
              {tab === "queries" && <QueriesTab queries={tier3.top_queries ?? []} />}
            </>
          )}
        </div>
      )}
    </div>
  );
}

function SizesTab({ sizes }: { sizes: NonNullable<Tier3Snapshot["sizes"]> }) {
  if (sizes.length === 0) return <Empty text="No size data" />;
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-xs">
        <thead>
          <tr style={{ color: "var(--color-text-muted)" }}>
            <th className="text-left py-1.5 px-2 font-medium">Relation</th>
            <th className="text-right py-1.5 px-2 font-medium">Total</th>
            <th className="text-right py-1.5 px-2 font-medium">Data</th>
            <th className="text-right py-1.5 px-2 font-medium">Indexes</th>
            <th className="text-right py-1.5 px-2 font-medium">Toast</th>
          </tr>
        </thead>
        <tbody>
          {sizes.map((s) => (
            <tr key={`${s.schema}.${s.name}`} className="border-t" style={{ borderColor: "var(--color-border)" }}>
              <td className="py-1.5 px-2 font-mono" style={{ color: "var(--color-text)" }}>
                {s.schema}.{s.name}
              </td>
              <td className="py-1.5 px-2 text-right font-medium" style={{ color: "var(--color-text)" }}>
                {s.total_size}
              </td>
              <td className="py-1.5 px-2 text-right tabular-nums" style={{ color: "var(--color-text-secondary)" }}>
                {formatBytes(s.data_bytes)}
              </td>
              <td className="py-1.5 px-2 text-right tabular-nums" style={{ color: "var(--color-text-secondary)" }}>
                {formatBytes(s.index_bytes)}
              </td>
              <td className="py-1.5 px-2 text-right tabular-nums" style={{ color: "var(--color-text-muted)" }}>
                {s.toast_bytes > 0 ? formatBytes(s.toast_bytes) : "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function BloatTab({ bloat }: { bloat: NonNullable<Tier3Snapshot["bloat"]> }) {
  if (bloat.length === 0) return <Empty text="No bloat data (tables may be too small)" />;
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-xs">
        <thead>
          <tr style={{ color: "var(--color-text-muted)" }}>
            <th className="text-left py-1.5 px-2 font-medium">Relation</th>
            <th className="text-right py-1.5 px-2 font-medium">Total</th>
            <th className="text-right py-1.5 px-2 font-medium">Bloat</th>
            <th className="text-right py-1.5 px-2 font-medium">Bloat %</th>
          </tr>
        </thead>
        <tbody>
          {bloat.map((b) => (
            <tr key={`${b.schema}.${b.name}`} className="border-t" style={{ borderColor: "var(--color-border)" }}>
              <td className="py-1.5 px-2 font-mono" style={{ color: "var(--color-text)" }}>
                {b.schema}.{b.name}
              </td>
              <td className="py-1.5 px-2 text-right tabular-nums" style={{ color: "var(--color-text-secondary)" }}>
                {formatBytes(b.total_bytes)}
              </td>
              <td className="py-1.5 px-2 text-right tabular-nums" style={{ color: "var(--color-text-secondary)" }}>
                {formatBytes(b.bloat_bytes)}
              </td>
              <td className="py-1.5 px-2 text-right tabular-nums" style={{
                color: b.bloat_ratio > 0.3 ? "#ef4444" : b.bloat_ratio > 0.15 ? "#f59e0b" : "var(--color-text-secondary)",
              }}>
                {(b.bloat_ratio * 100).toFixed(1)}%
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function QueriesTab({ queries }: { queries: NonNullable<Tier3Snapshot["top_queries"]> }) {
  if (queries.length === 0) return <Empty text="pg_stat_statements not available or no queries recorded" />;
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-xs">
        <thead>
          <tr style={{ color: "var(--color-text-muted)" }}>
            <th className="text-left py-1.5 px-2 font-medium w-1/2">Query</th>
            <th className="text-right py-1.5 px-2 font-medium">Calls</th>
            <th className="text-right py-1.5 px-2 font-medium">Total (ms)</th>
            <th className="text-right py-1.5 px-2 font-medium">Mean (ms)</th>
            <th className="text-right py-1.5 px-2 font-medium">Hit %</th>
          </tr>
        </thead>
        <tbody>
          {queries.map((q) => (
            <tr key={q.query_id} className="border-t" style={{ borderColor: "var(--color-border)" }}>
              <td className="py-1.5 px-2">
                <pre
                  className="truncate max-w-md font-mono"
                  style={{ color: "var(--color-text-secondary)" }}
                  title={q.query}
                >
                  {q.query}
                </pre>
              </td>
              <td className="py-1.5 px-2 text-right tabular-nums" style={{ color: "var(--color-text)" }}>
                {q.calls.toLocaleString()}
              </td>
              <td className="py-1.5 px-2 text-right tabular-nums" style={{ color: "var(--color-text)" }}>
                {q.total_time_ms.toFixed(0)}
              </td>
              <td className="py-1.5 px-2 text-right tabular-nums" style={{ color: "var(--color-text-secondary)" }}>
                {q.mean_time_ms.toFixed(2)}
              </td>
              <td className="py-1.5 px-2 text-right tabular-nums" style={{
                color: q.hit_ratio < 0.95 ? "#f59e0b" : "var(--color-text-secondary)",
              }}>
                {(q.hit_ratio * 100).toFixed(1)}%
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Empty({ text }: { text: string }) {
  return (
    <div className="py-6 text-center text-xs" style={{ color: "var(--color-text-muted)" }}>
      {text}
    </div>
  );
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "kB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`;
}

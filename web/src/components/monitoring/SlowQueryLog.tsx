import { useState, useEffect, useCallback } from "react";
import { ChevronDown, Clock } from "lucide-react";
import type { SlowQueryEntry } from "../../types/monitoring";
import { fetchSlowQueries } from "../../api/client";

interface Props {
  clusterId: string;
  nodeId: string;
}

export function SlowQueryLog({ clusterId, nodeId }: Props) {
  const [expanded, setExpanded] = useState(false);
  const [entries, setEntries] = useState<SlowQueryEntry[]>([]);
  const [loading, setLoading] = useState(false);

  const poll = useCallback(async () => {
    try {
      const data = await fetchSlowQueries(clusterId, nodeId);
      setEntries(data || []);
    } catch {}
  }, [clusterId, nodeId]);

  useEffect(() => {
    if (!expanded) return;
    setLoading(true);
    poll().finally(() => setLoading(false));
    const id = setInterval(poll, 10000);
    return () => clearInterval(id);
  }, [expanded, poll]);

  return (
    <div
      className="rounded-lg border"
      style={{
        backgroundColor: "var(--color-surface)",
        borderColor: "var(--color-border)",
      }}
    >
      <button
        className="w-full flex items-center gap-2 p-4 text-left"
        onClick={() => setExpanded(!expanded)}
      >
        <ChevronDown
          className={`w-4 h-4 transition-transform ${expanded ? "" : "-rotate-90"}`}
          style={{ color: "var(--color-text-muted)" }}
        />
        <Clock className="w-3.5 h-3.5" style={{ color: "#f59e0b" }} />
        <span className="text-sm font-medium" style={{ color: "var(--color-text)" }}>
          Slow Query Log
        </span>
        <span className="text-[10px] px-1.5 py-0.5 rounded font-medium" style={{
          backgroundColor: entries.length > 0 ? "#f59e0b20" : "var(--color-border)",
          color: entries.length > 0 ? "#f59e0b" : "var(--color-text-muted)",
        }}>
          {entries.length} {entries.length === 1 ? "query" : "queries"} &gt;5s
        </span>
      </button>

      {expanded && (
        <div className="border-t px-4 pb-4" style={{ borderColor: "var(--color-border)" }}>
          {loading && entries.length === 0 ? (
            <div className="py-6 text-center text-xs" style={{ color: "var(--color-text-muted)" }}>
              Loading...
            </div>
          ) : entries.length === 0 ? (
            <div className="py-6 text-center text-xs" style={{ color: "var(--color-text-muted)" }}>
              No slow queries detected (threshold: 5 seconds)
            </div>
          ) : (
            <div className="overflow-x-auto mt-3">
              <table className="w-full text-xs">
                <thead>
                  <tr style={{ color: "var(--color-text-muted)" }}>
                    <th className="text-left py-1.5 px-2 font-medium">Time</th>
                    <th className="text-left py-1.5 px-2 font-medium">Database</th>
                    <th className="text-left py-1.5 px-2 font-medium">User</th>
                    <th className="text-right py-1.5 px-2 font-medium">Duration</th>
                    <th className="text-left py-1.5 px-2 font-medium w-1/2">Query</th>
                  </tr>
                </thead>
                <tbody>
                  {entries.slice().reverse().map((e, i) => (
                    <tr
                      key={`${e.pid}-${e.timestamp}-${i}`}
                      className="border-t"
                      style={{ borderColor: "var(--color-border)" }}
                    >
                      <td className="py-1.5 px-2 whitespace-nowrap" style={{ color: "var(--color-text-muted)" }}>
                        {new Date(e.timestamp).toLocaleTimeString([], {
                          hour: "2-digit",
                          minute: "2-digit",
                          second: "2-digit",
                        })}
                      </td>
                      <td className="py-1.5 px-2 font-mono" style={{ color: "var(--color-text-secondary)" }}>
                        {e.datname}
                      </td>
                      <td className="py-1.5 px-2 font-mono" style={{ color: "var(--color-text-secondary)" }}>
                        {e.usename}
                      </td>
                      <td className="py-1.5 px-2 text-right tabular-nums font-mono" style={{
                        color: e.duration_sec > 30 ? "#ef4444" : e.duration_sec > 10 ? "#f59e0b" : "var(--color-text)",
                      }}>
                        {e.duration_sec.toFixed(1)}s
                      </td>
                      <td className="py-1.5 px-2">
                        <pre
                          className="truncate max-w-md font-mono"
                          style={{ color: "var(--color-text-secondary)" }}
                          title={e.query}
                        >
                          {e.query}
                        </pre>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

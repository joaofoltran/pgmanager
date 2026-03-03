import { Clock } from "lucide-react";
import type { ActivitySnapshot } from "../../types/monitoring";

interface Props {
  activity: ActivitySnapshot;
}

const stateColors: Record<string, string> = {
  active: "#10b981",
  idle: "#6b7280",
  "idle in transaction": "#f59e0b",
  "idle in transaction (aborted)": "#ef4444",
  disabled: "#374151",
};

export function ActivitySection({ activity }: Props) {
  const states = Object.entries(activity.by_state).sort(
    ([, a], [, b]) => b - a
  );
  const waitEvents = Object.entries(activity.by_wait_event ?? {}).sort(
    ([, a], [, b]) => b - a
  );

  return (
    <div
      className="rounded-lg border p-4 space-y-4"
      style={{
        backgroundColor: "var(--color-surface)",
        borderColor: "var(--color-border)",
      }}
    >
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium" style={{ color: "var(--color-text)" }}>
          Activity
        </h3>
        {activity.longest_query_sec > 0 && (
          <div className="flex items-center gap-1.5 text-xs" style={{ color: "var(--color-text-muted)" }}>
            <Clock className="w-3 h-3" />
            Longest query: {activity.longest_query_sec.toFixed(1)}s
            {activity.longest_query_pid ? ` (PID ${activity.longest_query_pid})` : ""}
          </div>
        )}
      </div>

      <div className="flex gap-6">
        <div className="flex-1">
          <p className="text-[10px] uppercase tracking-wide mb-2" style={{ color: "var(--color-text-muted)" }}>
            Connection States
          </p>
          <div className="space-y-1.5">
            {states.map(([state, count]) => {
              const pct = activity.total_connections > 0
                ? (count / activity.total_connections) * 100
                : 0;
              return (
                <div key={state} className="flex items-center gap-2 text-xs">
                  <div
                    className="w-2 h-2 rounded-full shrink-0"
                    style={{ backgroundColor: stateColors[state] ?? "#6b7280" }}
                  />
                  <span className="flex-1 truncate" style={{ color: "var(--color-text-secondary)" }}>
                    {state}
                  </span>
                  <span className="font-mono tabular-nums" style={{ color: "var(--color-text)" }}>
                    {count}
                  </span>
                  <span
                    className="w-12 text-right"
                    style={{ color: "var(--color-text-muted)" }}
                  >
                    {pct.toFixed(0)}%
                  </span>
                </div>
              );
            })}
          </div>
        </div>

        {waitEvents.length > 0 && (
          <div className="flex-1">
            <p className="text-[10px] uppercase tracking-wide mb-2" style={{ color: "var(--color-text-muted)" }}>
              Wait Events (active)
            </p>
            <div className="space-y-1.5">
              {waitEvents.map(([event, count]) => (
                <div key={event} className="flex items-center gap-2 text-xs">
                  <span className="flex-1 truncate" style={{ color: "var(--color-text-secondary)" }}>
                    {event}
                  </span>
                  <span className="font-mono tabular-nums" style={{ color: "var(--color-text)" }}>
                    {count}
                  </span>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>

      {activity.longest_query && (
        <div>
          <p className="text-[10px] uppercase tracking-wide mb-1" style={{ color: "var(--color-text-muted)" }}>
            Longest Running Query
          </p>
          <pre
            className="text-xs p-2 rounded-md overflow-x-auto"
            style={{
              backgroundColor: "var(--color-bg)",
              color: "var(--color-text-secondary)",
            }}
          >
            {activity.longest_query}
          </pre>
        </div>
      )}
    </div>
  );
}

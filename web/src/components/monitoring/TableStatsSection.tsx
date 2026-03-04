import { ChevronDown, AlertTriangle, Database } from "lucide-react";
import { useState, useMemo } from "react";
import type { Tier2Snapshot } from "../../types/monitoring";

interface Props {
  tier2: Tier2Snapshot | null;
}

export function TableStatsSection({ tier2 }: Props) {
  const [expanded, setExpanded] = useState(false);
  const [tab, setTab] = useState<"tables" | "indexes" | "locks" | "vacuum">("tables");
  const [selectedDB, setSelectedDB] = useState<string | null>(null);

  const databases = useMemo(() => {
    if (!tier2) return [];
    if (tier2.databases?.length) return tier2.databases;
    const dbs = new Set<string>();
    tier2.tables?.forEach((t) => t.database && dbs.add(t.database));
    tier2.indexes?.forEach((i) => i.database && dbs.add(i.database));
    return Array.from(dbs).sort();
  }, [tier2]);

  const activeDB = selectedDB ?? databases[0] ?? null;

  const filteredTables = useMemo(() => {
    const tables = tier2?.tables ?? [];
    if (!activeDB || databases.length <= 1) return tables;
    return tables.filter((t) => t.database === activeDB);
  }, [tier2?.tables, activeDB, databases.length]);

  const filteredIndexes = useMemo(() => {
    const indexes = tier2?.indexes ?? [];
    if (!activeDB || databases.length <= 1) return indexes;
    return indexes.filter((i) => i.database === activeDB);
  }, [tier2?.indexes, activeDB, databases.length]);

  if (!tier2) return null;

  const vacuums = tier2.vacuum_progress ?? [];

  const unusedIndexes = filteredIndexes.filter((i) => i.idx_scan === 0);
  const deadTupTables = filteredTables.filter(
    (t) => t.n_dead_tup > 1000 && t.n_live_tup > 0 && t.n_dead_tup / t.n_live_tup > 0.1
  );

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
        <span className="text-sm font-medium" style={{ color: "var(--color-text)" }}>
          Table & Index Stats
        </span>
        <span className="text-xs ml-auto" style={{ color: "var(--color-text-muted)" }}>
          {filteredTables.length} tables &middot; {filteredIndexes.length} indexes
          {tier2.locks?.length ? ` · ${tier2.locks.length} blocked` : ""}
        </span>
        {(unusedIndexes.length > 0 || deadTupTables.length > 0) && (
          <AlertTriangle className="w-3.5 h-3.5 text-amber-400" />
        )}
      </button>

      {expanded && (
        <div className="border-t px-4 pb-4" style={{ borderColor: "var(--color-border)" }}>
          <div className="flex items-center justify-between mt-3 mb-3">
            <div className="flex gap-1">
              {(["tables", "indexes", "locks", "vacuum"] as const).map((t) => (
                <button
                  key={t}
                  onClick={() => setTab(t)}
                  className="px-3 py-1 rounded text-xs font-medium transition-colors"
                  style={{
                    backgroundColor: tab === t ? "var(--color-accent)" : "transparent",
                    color: tab === t ? "#fff" : "var(--color-text-secondary)",
                  }}
                >
                  {t.charAt(0).toUpperCase() + t.slice(1)}
                  {t === "locks" && tier2.locks?.length ? ` (${tier2.locks.length})` : ""}
                  {t === "vacuum" && vacuums.length ? ` (${vacuums.length})` : ""}
                </button>
              ))}
            </div>
            {databases.length > 1 && (
              <div className="flex items-center gap-1.5">
                <Database className="w-3.5 h-3.5" style={{ color: "var(--color-text-muted)" }} />
                <select
                  value={activeDB ?? ""}
                  onChange={(e) => setSelectedDB(e.target.value)}
                  className="text-xs rounded px-2 py-1 border"
                  style={{
                    backgroundColor: "var(--color-bg)",
                    borderColor: "var(--color-border)",
                    color: "var(--color-text)",
                  }}
                >
                  {databases.map((db) => (
                    <option key={db} value={db}>{db}</option>
                  ))}
                </select>
              </div>
            )}
          </div>

          {tab === "tables" && <TablesTab tables={filteredTables} showDB={databases.length > 1} />}
          {tab === "indexes" && <IndexesTab indexes={filteredIndexes} showDB={databases.length > 1} />}
          {tab === "locks" && <LocksTab locks={tier2.locks ?? []} />}
          {tab === "vacuum" && <VacuumTab vacuums={vacuums} />}
        </div>
      )}
    </div>
  );
}

function TablesTab({ tables, showDB }: { tables: Props["tier2"] extends null ? never : NonNullable<Props["tier2"]>["tables"]; showDB: boolean }) {
  if (tables.length === 0) {
    return <EmptyState text="No table stats collected yet" />;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-xs">
        <thead>
          <tr style={{ color: "var(--color-text-muted)" }}>
            {showDB && <th className="text-left py-1.5 px-2 font-medium">DB</th>}
            <th className="text-left py-1.5 px-2 font-medium">Table</th>
            <th className="text-right py-1.5 px-2 font-medium">Seq Scans</th>
            <th className="text-right py-1.5 px-2 font-medium">Idx Usage</th>
            <th className="text-right py-1.5 px-2 font-medium">Live Rows</th>
            <th className="text-right py-1.5 px-2 font-medium">Dead Rows</th>
            <th className="text-right py-1.5 px-2 font-medium">Last Vacuum</th>
          </tr>
        </thead>
        <tbody>
          {tables.map((t) => (
            <tr
              key={`${t.database}.${t.schema}.${t.name}`}
              className="border-t"
              style={{ borderColor: "var(--color-border)" }}
            >
              {showDB && (
                <td className="py-1.5 px-2 font-mono" style={{ color: "var(--color-text-muted)" }}>
                  {t.database}
                </td>
              )}
              <td className="py-1.5 px-2 font-mono" style={{ color: "var(--color-text)" }}>
                {t.schema}.{t.name}
              </td>
              <td className="py-1.5 px-2 text-right tabular-nums" style={{ color: "var(--color-text-secondary)" }}>
                {t.seq_scan.toLocaleString()}
              </td>
              <td className="py-1.5 px-2 text-right tabular-nums" style={{
                color: t.index_usage_ratio < 0.5 ? "#f59e0b" : "var(--color-text-secondary)",
              }}>
                {(t.index_usage_ratio * 100).toFixed(0)}%
              </td>
              <td className="py-1.5 px-2 text-right tabular-nums" style={{ color: "var(--color-text-secondary)" }}>
                {t.n_live_tup.toLocaleString()}
              </td>
              <td className="py-1.5 px-2 text-right tabular-nums" style={{
                color: t.n_dead_tup > 1000 && t.n_live_tup > 0 && t.n_dead_tup / t.n_live_tup > 0.1
                  ? "#ef4444"
                  : "var(--color-text-secondary)",
              }}>
                {t.n_dead_tup.toLocaleString()}
              </td>
              <td className="py-1.5 px-2 text-right" style={{ color: "var(--color-text-muted)" }}>
                {t.last_autovacuum
                  ? timeAgo(t.last_autovacuum)
                  : t.last_vacuum
                    ? timeAgo(t.last_vacuum)
                    : "never"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function IndexesTab({ indexes, showDB }: { indexes: NonNullable<NonNullable<Props["tier2"]>>["indexes"]; showDB: boolean }) {
  if (indexes.length === 0) {
    return <EmptyState text="No index stats collected yet" />;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-xs">
        <thead>
          <tr style={{ color: "var(--color-text-muted)" }}>
            {showDB && <th className="text-left py-1.5 px-2 font-medium">DB</th>}
            <th className="text-left py-1.5 px-2 font-medium">Index</th>
            <th className="text-left py-1.5 px-2 font-medium">Table</th>
            <th className="text-right py-1.5 px-2 font-medium">Scans</th>
            <th className="text-right py-1.5 px-2 font-medium">Size</th>
          </tr>
        </thead>
        <tbody>
          {indexes.map((i) => (
            <tr
              key={`${i.database}.${i.schema}.${i.name}`}
              className="border-t"
              style={{ borderColor: "var(--color-border)" }}
            >
              {showDB && (
                <td className="py-1.5 px-2 font-mono" style={{ color: "var(--color-text-muted)" }}>
                  {i.database}
                </td>
              )}
              <td className="py-1.5 px-2 font-mono" style={{
                color: i.idx_scan === 0 ? "#f59e0b" : "var(--color-text)",
              }}>
                {i.name}
                {i.idx_scan === 0 && (
                  <span className="ml-1 text-[9px] px-1 py-0.5 rounded bg-amber-500/20 text-amber-400">
                    unused
                  </span>
                )}
              </td>
              <td className="py-1.5 px-2 font-mono" style={{ color: "var(--color-text-secondary)" }}>
                {i.schema}.{i.table}
              </td>
              <td className="py-1.5 px-2 text-right tabular-nums" style={{ color: "var(--color-text-secondary)" }}>
                {i.idx_scan.toLocaleString()}
              </td>
              <td className="py-1.5 px-2 text-right" style={{ color: "var(--color-text-muted)" }}>
                {i.size}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function LocksTab({ locks }: { locks: NonNullable<NonNullable<Props["tier2"]>>["locks"] }) {
  if (locks.length === 0) {
    return <EmptyState text="No lock contention detected" />;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-xs">
        <thead>
          <tr style={{ color: "var(--color-text-muted)" }}>
            <th className="text-left py-1.5 px-2 font-medium">Waiting PID</th>
            <th className="text-left py-1.5 px-2 font-medium">Blocking PID</th>
            <th className="text-left py-1.5 px-2 font-medium">Mode</th>
            <th className="text-left py-1.5 px-2 font-medium">Relation</th>
          </tr>
        </thead>
        <tbody>
          {locks.map((l, i) => (
            <tr
              key={i}
              className="border-t"
              style={{ borderColor: "var(--color-border)" }}
            >
              <td className="py-1.5 px-2 font-mono" style={{ color: "#ef4444" }}>
                {l.waiting_pid}
              </td>
              <td className="py-1.5 px-2 font-mono" style={{ color: "#f59e0b" }}>
                {l.blocking_pid}
              </td>
              <td className="py-1.5 px-2" style={{ color: "var(--color-text-secondary)" }}>
                {l.mode}
              </td>
              <td className="py-1.5 px-2 font-mono" style={{ color: "var(--color-text-muted)" }}>
                {l.relation || "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function VacuumTab({ vacuums }: { vacuums: NonNullable<NonNullable<Props["tier2"]>["vacuum_progress"]> }) {
  if (vacuums.length === 0) {
    return <EmptyState text="No vacuum operations in progress" />;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-xs">
        <thead>
          <tr style={{ color: "var(--color-text-muted)" }}>
            <th className="text-left py-1.5 px-2 font-medium">PID</th>
            <th className="text-left py-1.5 px-2 font-medium">Table</th>
            <th className="text-left py-1.5 px-2 font-medium">Phase</th>
            <th className="text-right py-1.5 px-2 font-medium">Progress</th>
          </tr>
        </thead>
        <tbody>
          {vacuums.map((v) => (
            <tr
              key={v.pid}
              className="border-t"
              style={{ borderColor: "var(--color-border)" }}
            >
              <td className="py-1.5 px-2 font-mono" style={{ color: "var(--color-text)" }}>
                {v.pid}
              </td>
              <td className="py-1.5 px-2 font-mono" style={{ color: "var(--color-text-secondary)" }}>
                {v.schema}.{v.table}
              </td>
              <td className="py-1.5 px-2" style={{ color: "var(--color-text-secondary)" }}>
                {v.phase}
              </td>
              <td className="py-1.5 px-2 text-right">
                <div className="flex items-center gap-2 justify-end">
                  <div className="w-20 h-1.5 rounded-full overflow-hidden" style={{ backgroundColor: "var(--color-border)" }}>
                    <div
                      className="h-full rounded-full"
                      style={{ width: `${v.percent_done}%`, backgroundColor: "var(--color-accent)" }}
                    />
                  </div>
                  <span className="tabular-nums font-mono" style={{ color: "var(--color-text)" }}>
                    {v.percent_done.toFixed(1)}%
                  </span>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function EmptyState({ text }: { text: string }) {
  return (
    <div className="py-6 text-center text-xs" style={{ color: "var(--color-text-muted)" }}>
      {text}
    </div>
  );
}

function timeAgo(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

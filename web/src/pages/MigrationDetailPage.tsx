import { useEffect, useState, useCallback, useRef } from "react";
import { useParams, useNavigate } from "react-router-dom";
import {
  ArrowLeft,
  Play,
  Square,
  ArrowLeftRight,
  Loader2,
  CheckCircle2,
  XCircle,
  Clock,
  Zap,
  Database,
  RefreshCw,
  Trash2,
  Table2,
  AlertTriangle,
  Terminal,
  Timer,
  FileCode,
  ChevronDown,
  ChevronUp,
  Activity,
} from "lucide-react";
import type { Migration, MigrationStatus } from "../types/migration";
import type { TableProgress, LogEntry, ErrorEntry } from "../types/metrics";
import {
  fetchMigration,
  fetchMigrationLogs,
  startMigration,
  stopMigration,
  switchoverMigration,
  removeMigration,
} from "../api/client";

const statusConfig: Record<
  MigrationStatus,
  { label: string; color: string; icon: React.ComponentType<{ className?: string }> }
> = {
  created: { label: "Created", color: "#6b7280", icon: Clock },
  running: { label: "Running", color: "#3b82f6", icon: Play },
  streaming: { label: "Streaming", color: "#8b5cf6", icon: Zap },
  switchover: { label: "Switchover", color: "#f59e0b", icon: ArrowLeftRight },
  completed: { label: "Completed", color: "#10b981", icon: CheckCircle2 },
  failed: { label: "Failed", color: "#ef4444", icon: XCircle },
  stopped: { label: "Stopped", color: "#6b7280", icon: Square },
};

export function MigrationDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [migration, setMigration] = useState<Migration | null>(null);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showLogs, setShowLogs] = useState(true);
  const [showEvents, setShowEvents] = useState(false);
  const [showSchemaDetails, setShowSchemaDetails] = useState(false);
  const logEndRef = useRef<HTMLDivElement>(null);
  const logContainerRef = useRef<HTMLDivElement>(null);
  const userScrolledUp = useRef(false);

  const load = useCallback(async () => {
    if (!id) return;
    try {
      const [data, logData] = await Promise.all([
        fetchMigration(id),
        fetchMigrationLogs(id),
      ]);
      setMigration(data);
      setLogs(logData);
      setError(null);
    } catch {
      if (!migration) setError("Migration not found");
    } finally {
      setLoading(false);
    }
  }, [id]);

  useEffect(() => {
    load();
    const isActive = migration?.status === "running" || migration?.status === "streaming" || migration?.status === "switchover";
    const interval = setInterval(load, isActive ? 2000 : 10000);
    return () => clearInterval(interval);
  }, [load, migration?.status]);

  useEffect(() => {
    if (logEndRef.current && !userScrolledUp.current) {
      logEndRef.current.scrollIntoView({ behavior: "smooth" });
    }
  }, [logs]);

  const handleLogScroll = useCallback(() => {
    const el = logContainerRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    userScrolledUp.current = distanceFromBottom > 40;
  }, []);

  async function handleAction(action: () => Promise<void>) {
    setActionLoading(true);
    setError(null);
    try {
      await action();
      await load();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Action failed");
    } finally {
      setActionLoading(false);
    }
  }

  async function handleDelete() {
    if (!id) return;
    const active = migration?.status === "running" || migration?.status === "streaming" || migration?.status === "switchover";
    const msg = active
      ? "This migration appears to be running. Force delete it? This will stop it and cannot be undone."
      : "Delete this migration? This cannot be undone.";
    if (!confirm(msg)) return;
    try {
      await removeMigration(id, active);
      navigate("/migration");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to delete");
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="w-8 h-8 animate-spin" style={{ color: "var(--color-accent)" }} />
      </div>
    );
  }

  if (!migration) {
    return (
      <div className="space-y-4 max-w-4xl">
        <button onClick={() => navigate("/migration")} className="flex items-center gap-2 text-sm" style={{ color: "var(--color-text-muted)" }}>
          <ArrowLeft className="w-4 h-4" /> Back to migrations
        </button>
        <div className="rounded-lg border p-8 text-center" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
          <p style={{ color: "var(--color-text)" }}>Migration not found</p>
        </div>
      </div>
    );
  }

  const sc = statusConfig[migration.status] || statusConfig.created;
  const StatusIcon = sc.icon;
  const isActive = migration.status === "running" || migration.status === "streaming" || migration.status === "switchover";
  const canStart = migration.status === "created" || migration.status === "failed" || migration.status === "stopped";
  const canStop = isActive;
  const canSwitchover = migration.status === "streaming" && migration.mode !== "clone_only";

  const phase = migration.live_phase || migration.phase;
  const tablesTotal = migration.live_tables_total || migration.tables_total;
  const tablesCopied = migration.live_tables_copied || migration.tables_copied;
  const liveTables = migration.live_tables || [];
  const rowsPerSec = migration.live_rows_per_sec || 0;
  const bytesPerSec = migration.live_bytes_per_sec || 0;
  const lagBytes = migration.live_lag_bytes || 0;
  const lagFormatted = migration.live_lag_formatted || "";
  const events = migration.live_events || [];
  const phases = migration.live_phases || [];
  const errorHistory = migration.live_error_history || [];
  const schemaStats = migration.live_schema_stats;
  const errorCount = migration.live_error_count || 0;
  const elapsed = migration.live_elapsed_sec || 0;

  const totalRowsAll = liveTables.reduce((s, t) => s + t.rows_total, 0);
  const copiedRowsAll = liveTables.reduce((s, t) => s + t.rows_copied, 0);
  const copyPercent = liveTables.length > 0 && totalRowsAll > 0
    ? Math.round((copiedRowsAll / totalRowsAll) * 100)
    : tablesTotal > 0
      ? Math.round((tablesCopied / tablesTotal) * 100)
      : 0;

  const modeLabelMap: Record<string, string> = {
    clone_only: "Copy Only",
    clone_and_follow: "Copy + Stream",
    clone_follow_switchover: "Full Migration",
  };

  return (
    <div className="space-y-6 max-w-5xl">
      {/* Header */}
      <div className="flex items-center gap-3">
        <button
          onClick={() => navigate("/migration")}
          className="p-1.5 rounded-md transition-colors hover:bg-white/5"
          style={{ color: "var(--color-text-muted)" }}
        >
          <ArrowLeft className="w-4 h-4" />
        </button>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-3">
            <h2 className="text-lg font-semibold" style={{ color: "var(--color-text)" }}>
              {migration.name}
            </h2>
            <span
              className="text-[10px] px-2 py-0.5 rounded-full font-medium flex items-center gap-1"
              style={{ backgroundColor: sc.color + "20", color: sc.color }}
            >
              <StatusIcon className="w-3 h-3" />
              {sc.label}
            </span>
          </div>
          <p className="text-sm mt-0.5" style={{ color: "var(--color-text-muted)" }}>
            {migration.id}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {canStart && (
            <button
              onClick={() => handleAction(() => startMigration(migration.id))}
              disabled={actionLoading}
              className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors disabled:opacity-40"
              style={{ backgroundColor: "#10b981", color: "#fff" }}
            >
              {actionLoading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Play className="w-3.5 h-3.5" />}
              Start
            </button>
          )}
          {canStop && (
            <button
              onClick={() => handleAction(() => stopMigration(migration.id))}
              disabled={actionLoading}
              className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors disabled:opacity-40"
              style={{ backgroundColor: "#ef4444", color: "#fff" }}
            >
              {actionLoading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Square className="w-3.5 h-3.5" />}
              Stop
            </button>
          )}
          {canSwitchover && (
            <button
              onClick={() => { if (confirm("Initiate switchover?")) handleAction(() => switchoverMigration(migration.id)); }}
              disabled={actionLoading}
              className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors disabled:opacity-40"
              style={{ backgroundColor: "#f59e0b", color: "#fff" }}
            >
              <ArrowLeftRight className="w-3.5 h-3.5" />
              Switchover
            </button>
          )}
          <button
            onClick={handleDelete}
            className="p-1.5 rounded-md hover:bg-red-500/10 transition-colors"
          >
            <Trash2 className="w-4 h-4 text-red-400" />
          </button>
          <button
            onClick={load}
            className="p-1.5 rounded-md transition-colors hover:bg-white/5"
            style={{ color: "var(--color-text-muted)" }}
          >
            <RefreshCw className="w-4 h-4" />
          </button>
        </div>
      </div>

      {error && (
        <div className="flex items-center gap-2 text-xs text-red-400 rounded-lg border border-red-400/20 p-3" style={{ backgroundColor: "var(--color-surface)" }}>
          <XCircle className="w-3.5 h-3.5 shrink-0" /> {error}
        </div>
      )}

      {migration.error_message && (
        <div className="rounded-lg border border-red-400/20 p-4" style={{ backgroundColor: "#ef444410" }}>
          <p className="text-sm font-medium text-red-400 mb-1">Error</p>
          <p className="text-xs font-mono text-red-300">{migration.error_message}</p>
        </div>
      )}

      {/* Stat cards */}
      <div className="grid grid-cols-2 lg:grid-cols-5 gap-3">
        <StatCard label="Status" value={sc.label} icon={StatusIcon} color={sc.color} />
        <StatCard label="Phase" value={phase || "\u2014"} icon={Zap} />
        <StatCard label="Tables" value={tablesTotal > 0 ? `${tablesCopied}/${tablesTotal}` : "\u2014"} icon={Database} />
        <StatCard label="Lag" value={lagFormatted || (lagBytes > 0 ? formatBytes(lagBytes) : "\u2014")} icon={Activity} color={lagBytes > 10 * 1024 * 1024 ? "#ef4444" : lagBytes > 1024 * 1024 ? "#f59e0b" : undefined} />
        <StatCard label="Elapsed" value={elapsed > 0 ? formatDuration(elapsed) : "\u2014"} icon={Timer} />
      </div>

      {/* Lag + throughput + errors row */}
      {isActive && (
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
          <StatCard label="Rows/s" value={rowsPerSec > 0 ? formatNumber(rowsPerSec) : "\u2014"} icon={Zap} />
          <StatCard label="Throughput" value={bytesPerSec > 0 ? formatBytes(bytesPerSec) + "/s" : "\u2014"} icon={Database} />
          <StatCard label="Rows" value={migration.live_total_rows ? formatNumber(migration.live_total_rows) : "\u2014"} icon={Table2} />
          <StatCard label="Errors" value={String(errorCount)} icon={AlertTriangle} color={errorCount > 0 ? "#ef4444" : undefined} />
        </div>
      )}

      {/* Copy progress */}
      {tablesTotal > 0 && (
        <div className="rounded-lg border p-4" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
          <div className="flex items-center justify-between mb-2">
            <span className="text-xs font-medium" style={{ color: "var(--color-text)" }}>Copy Progress</span>
            <div className="flex items-center gap-3">
              {rowsPerSec > 0 && (
                <span className="text-[10px] font-mono" style={{ color: "var(--color-text-muted)" }}>
                  {formatNumber(rowsPerSec)} rows/s
                </span>
              )}
              {bytesPerSec > 0 && (
                <span className="text-[10px] font-mono" style={{ color: "var(--color-text-muted)" }}>
                  {formatBytes(bytesPerSec)}/s
                </span>
              )}
              <span className="text-xs" style={{ color: "var(--color-text-muted)" }}>{copyPercent}%</span>
            </div>
          </div>
          <div className="h-2 rounded-full" style={{ backgroundColor: "var(--color-border)" }}>
            <div
              className="h-full rounded-full transition-all duration-500"
              style={{ width: `${copyPercent}%`, backgroundColor: "var(--color-accent)" }}
            />
          </div>
          {liveTables.length > 0 && (
            <div className="mt-3 space-y-1.5">
              {liveTables.map((t) => (
                <TableRow key={`${t.schema}.${t.name}`} table={t} />
              ))}
            </div>
          )}
        </div>
      )}

      {/* Phase Timeline */}
      {phases.length > 0 && (
        <div className="rounded-lg border p-4" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
          <div className="flex items-center gap-2 mb-3">
            <Timer className="w-3.5 h-3.5" style={{ color: "var(--color-text-muted)" }} />
            <p className="text-xs font-medium" style={{ color: "var(--color-text)" }}>Phase Timeline</p>
          </div>
          <div className="flex items-center gap-1 h-6">
            {phases.map((p, i) => {
              const totalDuration = phases.reduce((s, ph) => s + ph.duration_sec, 0);
              const pct = totalDuration > 0 ? Math.max((p.duration_sec / totalDuration) * 100, 2) : 100 / phases.length;
              return (
                <div
                  key={i}
                  className="h-full rounded-sm relative group cursor-default"
                  style={{
                    width: `${pct}%`,
                    backgroundColor: phaseColor(p.phase),
                    minWidth: "8px",
                  }}
                  title={`${p.phase}: ${formatDuration(p.duration_sec)}`}
                >
                  <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-1 hidden group-hover:block z-10">
                    <div className="bg-black/90 text-white text-[10px] px-2 py-1 rounded whitespace-nowrap">
                      {p.phase}: {formatDuration(p.duration_sec)}
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
          <div className="flex flex-wrap gap-3 mt-2">
            {phases.map((p, i) => (
              <span key={i} className="text-[10px] flex items-center gap-1" style={{ color: "var(--color-text-muted)" }}>
                <span className="w-2 h-2 rounded-sm inline-block" style={{ backgroundColor: phaseColor(p.phase) }} />
                {p.phase} ({formatDuration(p.duration_sec)})
              </span>
            ))}
          </div>
        </div>
      )}

      {/* Schema Stats */}
      {schemaStats && (
        <div className="rounded-lg border p-4" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
          <div className="flex items-center gap-2 mb-3">
            <FileCode className="w-3.5 h-3.5" style={{ color: "var(--color-text-muted)" }} />
            <p className="text-xs font-medium" style={{ color: "var(--color-text)" }}>Schema Application</p>
          </div>
          <div className="grid grid-cols-4 gap-3">
            <MiniStat label="Total" value={schemaStats.statements_total} />
            <MiniStat label="Applied" value={schemaStats.statements_applied} color="#10b981" />
            <MiniStat label="Skipped" value={schemaStats.statements_skipped} color="#6b7280" />
            <MiniStat label="Errors Tolerated" value={schemaStats.errors_tolerated} color={schemaStats.errors_tolerated > 0 ? "#f59e0b" : undefined} />
          </div>
          {((schemaStats.skipped_details && schemaStats.skipped_details.length > 0) || (schemaStats.errored_details && schemaStats.errored_details.length > 0)) && (
            <div className="mt-3 border-t pt-3" style={{ borderColor: "var(--color-border)" }}>
              <button
                className="flex items-center gap-1.5 text-[11px]"
                style={{ color: "var(--color-text-muted)" }}
                onClick={() => setShowSchemaDetails(!showSchemaDetails)}
              >
                {showSchemaDetails ? <ChevronUp className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
                {showSchemaDetails ? "Hide" : "Show"} statement details
              </button>
              {showSchemaDetails && (
                <div className="mt-2 space-y-2 max-h-48 overflow-y-auto">
                  {schemaStats.skipped_details && schemaStats.skipped_details.length > 0 && (
                    <div>
                      <p className="text-[10px] uppercase tracking-wider mb-1" style={{ color: "var(--color-text-muted)" }}>
                        Skipped ({schemaStats.skipped_details.length})
                      </p>
                      <div className="space-y-0.5">
                        {schemaStats.skipped_details.map((d, i) => (
                          <div key={i} className="text-[11px] font-mono py-0.5 px-2 rounded" style={{ backgroundColor: "var(--color-bg)" }}>
                            <span style={{ color: "var(--color-text-muted)" }}>{d.statement}</span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                  {schemaStats.errored_details && schemaStats.errored_details.length > 0 && (
                    <div>
                      <p className="text-[10px] uppercase tracking-wider mb-1 text-yellow-500">
                        Errors Tolerated ({schemaStats.errored_details.length})
                      </p>
                      <div className="space-y-0.5">
                        {schemaStats.errored_details.map((d, i) => (
                          <div key={i} className="text-[11px] font-mono py-0.5 px-2 rounded" style={{ backgroundColor: "#ef444408" }}>
                            <span style={{ color: "var(--color-text)" }}>{d.statement}</span>
                            <span className="ml-2 text-red-400">{d.reason}</span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {/* Error History */}
      {errorHistory.length > 0 && (
        <div className="rounded-lg border p-4" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
          <div className="flex items-center gap-2 mb-3">
            <AlertTriangle className="w-3.5 h-3.5 text-red-400" />
            <p className="text-xs font-medium" style={{ color: "var(--color-text)" }}>Error History ({errorHistory.length})</p>
          </div>
          <div className="space-y-1.5 max-h-48 overflow-y-auto">
            {errorHistory.map((e, i) => (
              <ErrorRow key={i} entry={e} />
            ))}
          </div>
        </div>
      )}

      {/* Log Viewer */}
      <div className="rounded-lg border" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
        <button
          className="flex items-center justify-between w-full p-4"
          onClick={() => setShowLogs(!showLogs)}
        >
          <div className="flex items-center gap-2">
            <Terminal className="w-3.5 h-3.5" style={{ color: "var(--color-text-muted)" }} />
            <p className="text-xs font-medium" style={{ color: "var(--color-text)" }}>
              Logs ({logs.length})
            </p>
          </div>
          {showLogs ? <ChevronUp className="w-3.5 h-3.5" style={{ color: "var(--color-text-muted)" }} /> : <ChevronDown className="w-3.5 h-3.5" style={{ color: "var(--color-text-muted)" }} />}
        </button>
        {showLogs && (
          <div className="border-t px-4 pb-4" style={{ borderColor: "var(--color-border)" }}>
            <div ref={logContainerRef} onScroll={handleLogScroll} className="max-h-80 overflow-y-auto font-mono text-[11px] mt-2 space-y-0.5">
              {logs.length === 0 ? (
                <p className="text-xs py-4 text-center" style={{ color: "var(--color-text-muted)" }}>
                  {isActive ? "Waiting for logs..." : "No logs available"}
                </p>
              ) : (
                logs.map((log, i) => <LogRow key={i} entry={log} />)
              )}
              <div ref={logEndRef} />
            </div>
          </div>
        )}
      </div>

      {/* Event Log */}
      {events.length > 0 && (
        <div className="rounded-lg border" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
          <button
            className="flex items-center justify-between w-full p-4"
            onClick={() => setShowEvents(!showEvents)}
          >
            <div className="flex items-center gap-2">
              <Activity className="w-3.5 h-3.5" style={{ color: "var(--color-text-muted)" }} />
              <p className="text-xs font-medium" style={{ color: "var(--color-text)" }}>
                Events ({events.length})
              </p>
            </div>
            {showEvents ? <ChevronUp className="w-3.5 h-3.5" style={{ color: "var(--color-text-muted)" }} /> : <ChevronDown className="w-3.5 h-3.5" style={{ color: "var(--color-text-muted)" }} />}
          </button>
          {showEvents && (
            <div className="border-t px-4 pb-4" style={{ borderColor: "var(--color-border)" }}>
              <div className="max-h-60 overflow-y-auto text-[11px] mt-2 space-y-1">
                {events.map((ev, i) => (
                  <div key={i} className="flex items-start gap-2 py-0.5">
                    <span className="text-[10px] shrink-0 font-mono" style={{ color: "var(--color-text-muted)" }}>
                      {new Date(ev.time).toLocaleTimeString()}
                    </span>
                    <span
                      className="text-[9px] px-1.5 py-0.5 rounded shrink-0"
                      style={{ backgroundColor: eventTypeColor(ev.type) + "20", color: eventTypeColor(ev.type) }}
                    >
                      {ev.type}
                    </span>
                    <span style={{ color: "var(--color-text)" }}>{ev.message}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* Config + Clusters */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <InfoCard title="Configuration">
          <InfoRow label="Strategy" value={modeLabelMap[migration.mode] || migration.mode} />
          <InfoRow label="Fallback" value={migration.fallback ? "Enabled" : "Disabled"} />
          <InfoRow label="Copy Workers" value={String(migration.copy_workers)} />
          <InfoRow label="Slot Name" value={migration.slot_name} mono />
          <InfoRow label="Publication" value={migration.publication} mono />
        </InfoCard>

        <InfoCard title="Clusters">
          <InfoRow label="Source Cluster" value={migration.source_cluster_id} />
          <InfoRow label="Source Node" value={migration.source_node_id} />
          <InfoRow label="Dest Cluster" value={migration.dest_cluster_id} />
          <InfoRow label="Dest Node" value={migration.dest_node_id} />
        </InfoCard>
      </div>

      {/* Timeline */}
      <div className="rounded-lg border p-4" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
        <p className="text-xs font-medium mb-3" style={{ color: "var(--color-text)" }}>Timeline</p>
        <div className="space-y-2 text-xs">
          <TimelineRow label="Created" time={migration.created_at} />
          <TimelineRow label="Started" time={migration.started_at} />
          <TimelineRow label="Last Updated" time={migration.updated_at} />
          <TimelineRow label="Finished" time={migration.finished_at} />
        </div>
      </div>
    </div>
  );
}

function StatCard({ label, value, icon: Icon, color, mono }: {
  label: string; value: string;
  icon: React.ComponentType<{ className?: string; style?: React.CSSProperties }>;
  color?: string; mono?: boolean;
}) {
  return (
    <div className="rounded-lg border p-3" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
      <div className="flex items-center gap-2 mb-1">
        <Icon className="w-3.5 h-3.5" style={{ color: color || "var(--color-text-muted)" }} />
        <span className="text-[10px] uppercase tracking-wider" style={{ color: "var(--color-text-muted)" }}>{label}</span>
      </div>
      <p className={`text-sm font-medium truncate ${mono ? "font-mono text-xs" : ""}`} style={{ color: "var(--color-text)" }}>
        {value}
      </p>
    </div>
  );
}

function MiniStat({ label, value, color }: { label: string; value: number; color?: string }) {
  return (
    <div className="text-center">
      <p className="text-lg font-semibold font-mono" style={{ color: color || "var(--color-text)" }}>{value}</p>
      <p className="text-[10px]" style={{ color: "var(--color-text-muted)" }}>{label}</p>
    </div>
  );
}

function InfoCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border p-4" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
      <p className="text-xs font-medium mb-3" style={{ color: "var(--color-text)" }}>{title}</p>
      <div className="space-y-2">{children}</div>
    </div>
  );
}

function InfoRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-center justify-between text-xs">
      <span style={{ color: "var(--color-text-muted)" }}>{label}</span>
      <span className={mono ? "font-mono" : ""} style={{ color: "var(--color-text)" }}>{value}</span>
    </div>
  );
}

function TimelineRow({ label, time }: { label: string; time?: string | null }) {
  return (
    <div className="flex items-center justify-between">
      <span style={{ color: "var(--color-text-muted)" }}>{label}</span>
      <span className="font-mono" style={{ color: time ? "var(--color-text)" : "var(--color-text-muted)" }}>
        {time ? new Date(time).toLocaleString() : "\u2014"}
      </span>
    </div>
  );
}

const logLevelColors: Record<string, string> = {
  debug: "#6b7280",
  info: "#3b82f6",
  warn: "#f59e0b",
  error: "#ef4444",
  fatal: "#ef4444",
};

function LogRow({ entry }: { entry: LogEntry }) {
  const color = logLevelColors[entry.level] || "#6b7280";
  return (
    <div className="flex items-start gap-2 py-0.5 hover:bg-white/5 px-1 rounded">
      <span className="text-[10px] shrink-0" style={{ color: "var(--color-text-muted)" }}>
        {new Date(entry.time).toLocaleTimeString()}
      </span>
      <span
        className="text-[9px] px-1 rounded shrink-0 uppercase font-semibold"
        style={{ color }}
      >
        {entry.level.slice(0, 3)}
      </span>
      <span className="flex-1" style={{ color: "var(--color-text)" }}>{entry.message}</span>
      {entry.fields && Object.keys(entry.fields).length > 0 && (
        <span className="shrink-0" style={{ color: "var(--color-text-muted)" }}>
          {Object.entries(entry.fields).map(([k, v]) => `${k}=${v}`).join(" ")}
        </span>
      )}
    </div>
  );
}

function ErrorRow({ entry }: { entry: ErrorEntry }) {
  return (
    <div className="flex items-start gap-2 text-xs py-1 px-2 rounded" style={{ backgroundColor: "#ef444408" }}>
      <span className="text-[10px] font-mono shrink-0" style={{ color: "var(--color-text-muted)" }}>
        {new Date(entry.time).toLocaleTimeString()}
      </span>
      <span
        className="text-[9px] px-1.5 py-0.5 rounded shrink-0"
        style={{ backgroundColor: "#6b728020", color: "#6b7280" }}
      >
        {entry.phase}
      </span>
      <span className="flex-1 text-red-400 font-mono">{entry.message}</span>
      {entry.retryable && (
        <span className="text-[9px] px-1.5 py-0.5 rounded shrink-0 bg-yellow-500/10 text-yellow-500">
          retryable
        </span>
      )}
    </div>
  );
}

const tableStatusColors: Record<string, { bg: string; text: string; label: string }> = {
  pending:   { bg: "#6b728020", text: "#6b7280", label: "Pending" },
  copying:   { bg: "#3b82f620", text: "#3b82f6", label: "Copying" },
  copied:    { bg: "#10b98120", text: "#10b981", label: "Copied" },
  streaming: { bg: "#8b5cf620", text: "#8b5cf6", label: "Streaming" },
};

function TableRow({ table: t }: { table: TableProgress }) {
  const sc = tableStatusColors[t.status] || tableStatusColors.pending;
  const tableName = t.schema !== "public" ? `${t.schema}.${t.name}` : t.name;
  const pct = Math.round(t.percent);

  return (
    <div className="flex items-center gap-3 py-1.5 px-2 rounded-md" style={{ backgroundColor: "var(--color-bg)" }}>
      <Table2 className="w-3 h-3 shrink-0" style={{ color: "var(--color-text-muted)" }} />
      <span className="text-xs font-mono truncate min-w-0 flex-1" style={{ color: "var(--color-text)" }}>
        {tableName}
      </span>
      <span
        className="text-[9px] px-1.5 py-0.5 rounded font-medium shrink-0"
        style={{ backgroundColor: sc.bg, color: sc.text }}
      >
        {sc.label}
      </span>
      {(t.rows_total > 0 || t.rows_copied > 0) && (
        <span className="text-[10px] font-mono shrink-0 w-24 text-right" style={{ color: "var(--color-text-muted)" }}>
          {t.rows_total > 0 ? `${formatNumber(t.rows_copied)}/${formatNumber(t.rows_total)}` : `${formatNumber(t.rows_copied)} rows`}
        </span>
      )}
      {t.size_bytes > 0 && (
        <span className="text-[10px] font-mono shrink-0 w-16 text-right" style={{ color: "var(--color-text-muted)" }}>
          {formatBytes(t.size_bytes)}
        </span>
      )}
      <div className="w-16 shrink-0">
        <div className="h-1 rounded-full" style={{ backgroundColor: "var(--color-border)" }}>
          <div
            className="h-full rounded-full transition-all duration-300"
            style={{ width: `${pct}%`, backgroundColor: sc.text }}
          />
        </div>
      </div>
      <span className="text-[10px] font-mono shrink-0 w-8 text-right" style={{ color: "var(--color-text-muted)" }}>
        {pct}%
      </span>
    </div>
  );
}

function phaseColor(phase: string): string {
  const colors: Record<string, string> = {
    connecting: "#6b7280",
    schema: "#8b5cf6",
    copy: "#3b82f6",
    streaming: "#10b981",
    resuming: "#f59e0b",
    switchover: "#f59e0b",
    done: "#10b981",
    "switchover-complete": "#10b981",
    idle: "#374151",
  };
  return colors[phase] || "#6b7280";
}

function eventTypeColor(type: string): string {
  const colors: Record<string, string> = {
    phase_change: "#8b5cf6",
    error: "#ef4444",
    retry: "#f59e0b",
    copy_complete: "#10b981",
    schema_applied: "#3b82f6",
  };
  return colors[type] || "#6b7280";
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const val = bytes / Math.pow(1024, i);
  return `${val < 10 ? val.toFixed(1) : Math.round(val)} ${units[i]}`;
}

function formatNumber(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + "M";
  if (n >= 1_000) return (n / 1_000).toFixed(1) + "K";
  return String(Math.round(n));
}

function formatDuration(seconds: number): string {
  if (seconds < 1) return "<1s";
  if (seconds < 60) return Math.round(seconds) + "s";
  if (seconds < 3600) {
    const m = Math.floor(seconds / 60);
    const s = Math.round(seconds % 60);
    return s > 0 ? `${m}m ${s}s` : `${m}m`;
  }
  const h = Math.floor(seconds / 3600);
  const m = Math.round((seconds % 3600) / 60);
  return m > 0 ? `${h}h ${m}m` : `${h}h`;
}

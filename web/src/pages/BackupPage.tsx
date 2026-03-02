import { useEffect, useState } from "react";
import {
  HardDrive,
  Loader2,
  WifiOff,
  RefreshCw,
  Trash2,
  CheckCircle2,
  XCircle,
  Clock,
  Play,
  ChevronDown,
  Archive,
  Database,
} from "lucide-react";
import type { Cluster } from "../types/cluster";
import type { Backup, BackupStatus } from "../types/backup";
import { fetchClusters, fetchBackups, removeBackup } from "../api/client";

const statusConfig: Record<
  BackupStatus,
  { label: string; color: string; icon: React.ComponentType<{ className?: string }> }
> = {
  pending: { label: "Pending", color: "#6b7280", icon: Clock },
  running: { label: "Running", color: "#3b82f6", icon: Play },
  complete: { label: "Complete", color: "#10b981", icon: CheckCircle2 },
  failed: { label: "Failed", color: "#ef4444", icon: XCircle },
};

const typeLabels: Record<string, string> = {
  full: "Full",
  diff: "Differential",
  incr: "Incremental",
};

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "kB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`;
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  const remaining = sec % 60;
  if (min < 60) return `${min}m ${remaining}s`;
  const hr = Math.floor(min / 60);
  return `${hr}h ${min % 60}m`;
}

function formatDate(iso?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return d.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function BackupPage() {
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [selectedCluster, setSelectedCluster] = useState<string>("");
  const [backups, setBackups] = useState<Backup[]>([]);
  const [loading, setLoading] = useState(true);
  const [backupsLoading, setBackupsLoading] = useState(false);
  const [apiDown, setApiDown] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);

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

  async function loadBackups(clusterId: string) {
    if (!clusterId) return;
    setBackupsLoading(true);
    try {
      const data = await fetchBackups(clusterId);
      setBackups(data || []);
    } catch {
      setBackups([]);
    } finally {
      setBackupsLoading(false);
    }
  }

  useEffect(() => {
    loadClusters();
  }, []);

  useEffect(() => {
    if (selectedCluster) {
      loadBackups(selectedCluster);
      const interval = setInterval(() => loadBackups(selectedCluster), 10000);
      return () => clearInterval(interval);
    }
  }, [selectedCluster]);

  async function handleRemove(e: React.MouseEvent, id: string) {
    e.stopPropagation();
    try {
      await removeBackup(id);
      setBackups((prev) => prev.filter((b) => b.id !== id));
    } catch (err: unknown) {
      alert(err instanceof Error ? err.message : "Failed to remove backup");
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="w-8 h-8 animate-spin" style={{ color: "var(--color-accent)" }} />
      </div>
    );
  }

  const cluster = clusters.find((c) => c.id === selectedCluster);

  return (
    <div className="space-y-6 max-w-4xl">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold" style={{ color: "var(--color-text)" }}>
            Backup & Restore
          </h2>
          <p className="text-sm mt-0.5" style={{ color: "var(--color-text-muted)" }}>
            pgBackRest-powered backups with point-in-time recovery
          </p>
        </div>
        <button
          onClick={() => loadBackups(selectedCluster)}
          className="flex items-center gap-2 px-3 py-2 rounded-lg text-sm transition-colors hover:bg-white/5"
          style={{ color: "var(--color-text-secondary)" }}
        >
          <RefreshCw className={`w-4 h-4 ${backupsLoading ? "animate-spin" : ""}`} />
          Refresh
        </button>
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
            Register a cluster first to manage backups.
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
                        {c.name} ({c.id})
                      </option>
                    ))}
                  </select>
                  <ChevronDown
                    className="w-4 h-4 absolute right-2 top-1/2 -translate-y-1/2 pointer-events-none"
                    style={{ color: "var(--color-text-muted)" }}
                  />
                </div>
              </div>
              {cluster?.backup_path && (
                <div className="flex-1">
                  <label className="block text-xs mb-1.5" style={{ color: "var(--color-text-secondary)" }}>
                    Repository Path
                  </label>
                  <div
                    className="rounded-md border px-3 py-2 text-sm font-mono"
                    style={{
                      backgroundColor: "var(--color-bg)",
                      borderColor: "var(--color-border)",
                      color: "var(--color-text-muted)",
                    }}
                  >
                    {cluster.backup_path}
                  </div>
                </div>
              )}
            </div>
          </div>

          {backupsLoading && backups.length === 0 ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="w-6 h-6 animate-spin" style={{ color: "var(--color-accent)" }} />
            </div>
          ) : backups.length === 0 ? (
            <div
              className="rounded-lg border p-12 text-center"
              style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}
            >
              <HardDrive className="w-12 h-12 mx-auto mb-4" style={{ color: "var(--color-text-muted)" }} />
              <h3 className="text-sm font-medium mb-1" style={{ color: "var(--color-text)" }}>
                No backups yet
              </h3>
              <p className="text-sm" style={{ color: "var(--color-text-muted)" }}>
                Configure the pgmanager agent on this cluster's nodes to start syncing backup metadata.
              </p>
            </div>
          ) : (
            <div className="space-y-2">
              {backups.map((b) => {
                const sc = statusConfig[b.status] || statusConfig.pending;
                const StatusIcon = sc.icon;
                const isExpanded = expanded === b.id;
                return (
                  <div
                    key={b.id}
                    className="rounded-lg border transition-colors"
                    style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}
                  >
                    <div
                      className="flex items-center gap-3 p-4 cursor-pointer"
                      onClick={() => setExpanded(isExpanded ? null : b.id)}
                    >
                      <div
                        className="w-8 h-8 rounded-lg flex items-center justify-center"
                        style={{ backgroundColor: sc.color + "20", color: sc.color }}
                      >
                        <StatusIcon className="w-4 h-4" />
                      </div>
                      <div className="flex-1 min-w-0">
                        <div className="text-sm font-medium" style={{ color: "var(--color-text)" }}>
                          {typeLabels[b.backup_type] || b.backup_type} Backup
                          {b.backup_label && (
                            <span
                              className="ml-2 text-xs font-mono"
                              style={{ color: "var(--color-text-muted)" }}
                            >
                              {b.backup_label}
                            </span>
                          )}
                        </div>
                        <div className="text-xs" style={{ color: "var(--color-text-muted)" }}>
                          {formatDate(b.started_at)}
                          {b.duration_ms > 0 && <> &middot; {formatDuration(b.duration_ms)}</>}
                          {b.size_bytes > 0 && <> &middot; {formatBytes(b.size_bytes)}</>}
                        </div>
                      </div>
                      <span
                        className="text-[10px] px-2 py-0.5 rounded-full font-medium"
                        style={{ backgroundColor: sc.color + "20", color: sc.color }}
                      >
                        {sc.label}
                      </span>
                      <button
                        onClick={(e) => handleRemove(e, b.id)}
                        className="p-1.5 rounded-md hover:bg-red-500/10 transition-colors"
                      >
                        <Trash2 className="w-4 h-4 text-red-400" />
                      </button>
                    </div>

                    {isExpanded && (
                      <div
                        className="border-t px-4 py-3 space-y-3"
                        style={{ borderColor: "var(--color-border)" }}
                      >
                        <div className="grid grid-cols-2 gap-4 text-xs">
                          <div>
                            <span style={{ color: "var(--color-text-muted)" }}>Stanza</span>
                            <p className="font-mono mt-0.5" style={{ color: "var(--color-text)" }}>
                              {b.stanza}
                            </p>
                          </div>
                          <div>
                            <span style={{ color: "var(--color-text-muted)" }}>Type</span>
                            <p className="mt-0.5" style={{ color: "var(--color-text)" }}>
                              {typeLabels[b.backup_type] || b.backup_type}
                            </p>
                          </div>
                          <div>
                            <span style={{ color: "var(--color-text-muted)" }}>Original Size</span>
                            <p className="font-mono mt-0.5" style={{ color: "var(--color-text)" }}>
                              {formatBytes(b.size_bytes)}
                            </p>
                          </div>
                          <div>
                            <span style={{ color: "var(--color-text-muted)" }}>Repository Size</span>
                            <p className="font-mono mt-0.5" style={{ color: "var(--color-text)" }}>
                              {formatBytes(b.repo_size_bytes)}
                            </p>
                          </div>
                          {b.wal_start && (
                            <div>
                              <span style={{ color: "var(--color-text-muted)" }}>WAL Start</span>
                              <p className="font-mono mt-0.5" style={{ color: "var(--color-text)" }}>
                                {b.wal_start}
                              </p>
                            </div>
                          )}
                          {b.wal_stop && (
                            <div>
                              <span style={{ color: "var(--color-text-muted)" }}>WAL Stop</span>
                              <p className="font-mono mt-0.5" style={{ color: "var(--color-text)" }}>
                                {b.wal_stop}
                              </p>
                            </div>
                          )}
                          <div>
                            <span style={{ color: "var(--color-text-muted)" }}>Started</span>
                            <p className="mt-0.5" style={{ color: "var(--color-text)" }}>
                              {formatDate(b.started_at)}
                            </p>
                          </div>
                          <div>
                            <span style={{ color: "var(--color-text-muted)" }}>Finished</span>
                            <p className="mt-0.5" style={{ color: "var(--color-text)" }}>
                              {formatDate(b.finished_at)}
                            </p>
                          </div>
                        </div>

                        {b.database_list && b.database_list.length > 0 && (
                          <div>
                            <span className="text-xs" style={{ color: "var(--color-text-muted)" }}>
                              Databases
                            </span>
                            <div className="flex flex-wrap gap-1 mt-1">
                              {b.database_list.map((db) => (
                                <span
                                  key={db}
                                  className="text-[10px] px-2 py-0.5 rounded-full flex items-center gap-1"
                                  style={{
                                    backgroundColor: "var(--color-border)",
                                    color: "var(--color-text-muted)",
                                  }}
                                >
                                  <Archive className="w-3 h-3" />
                                  {db}
                                </span>
                              ))}
                            </div>
                          </div>
                        )}

                        {b.error_message && (
                          <div
                            className="rounded-md p-3 text-xs"
                            style={{ backgroundColor: "#ef444420", color: "#ef4444" }}
                          >
                            {b.error_message}
                          </div>
                        )}

                        {b.synced_at && (
                          <div className="text-[10px] text-right" style={{ color: "var(--color-text-muted)" }}>
                            Last synced {formatDate(b.synced_at)}
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </>
      )}
    </div>
  );
}

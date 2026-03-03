import type { Snapshot, LogEntry } from "../types/metrics";
import type { Cluster, ConnTestResult, ClusterInfo } from "../types/cluster";
import type { Migration, CreateMigrationRequest } from "../types/migration";
import type { Backup } from "../types/backup";
import type {
  MonitoringOverview,
  Tier2Snapshot,
  Tier3Snapshot,
  SlowQueryEntry,
} from "../types/monitoring";

const BASE = "";

export async function fetchStatus(): Promise<Snapshot> {
  const res = await fetch(`${BASE}/api/v1/status`);
  return res.json();
}

export async function fetchTables(): Promise<Snapshot["tables"]> {
  const res = await fetch(`${BASE}/api/v1/tables`);
  return res.json();
}

export async function fetchLogs(): Promise<LogEntry[]> {
  const res = await fetch(`${BASE}/api/v1/logs`);
  return res.json();
}

export async function submitClone(payload?: {
  source_uri?: string;
  dest_uri?: string;
  follow?: boolean;
  workers?: number;
}): Promise<void> {
  const res = await fetch(`${BASE}/api/v1/jobs/clone`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload || {}),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: `HTTP ${res.status}` }));
    throw new Error(body.error || `HTTP ${res.status}`);
  }
}

export async function submitFollow(payload?: {
  source_uri?: string;
  dest_uri?: string;
  start_lsn?: string;
}): Promise<void> {
  const res = await fetch(`${BASE}/api/v1/jobs/follow`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload || {}),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: `HTTP ${res.status}` }));
    throw new Error(body.error || `HTTP ${res.status}`);
  }
}

export async function stopJob(): Promise<void> {
  const res = await fetch(`${BASE}/api/v1/jobs/stop`, { method: "POST" });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
}

export async function fetchClusters(): Promise<Cluster[]> {
  const res = await fetch(`${BASE}/api/v1/clusters`);
  return res.json();
}

export async function fetchCluster(id: string): Promise<Cluster> {
  const res = await fetch(`${BASE}/api/v1/clusters/${encodeURIComponent(id)}`);
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

export async function addCluster(cluster: {
  id: string;
  name: string;
  nodes: Cluster["nodes"];
  tags?: string[];
}): Promise<Cluster> {
  const res = await fetch(`${BASE}/api/v1/clusters`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(cluster),
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
  return res.json();
}

export async function updateCluster(
  id: string,
  data: { name: string; nodes: Cluster["nodes"]; tags?: string[] }
): Promise<Cluster> {
  const res = await fetch(`${BASE}/api/v1/clusters/${encodeURIComponent(id)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(data),
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
  return res.json();
}

export async function removeCluster(id: string): Promise<void> {
  const res = await fetch(`${BASE}/api/v1/clusters/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
}

export async function testConnection(dsn: string): Promise<ConnTestResult> {
  const res = await fetch(`${BASE}/api/v1/clusters/test-connection`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ dsn }),
  });
  return res.json();
}

export async function introspectCluster(
  id: string,
  nodeId?: string
): Promise<ClusterInfo> {
  const params = nodeId ? `?node=${encodeURIComponent(nodeId)}` : "";
  const res = await fetch(
    `${BASE}/api/v1/clusters/${encodeURIComponent(id)}/introspect${params}`
  );
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
  return res.json();
}

// --- Migrations ---

export async function fetchMigrations(): Promise<Migration[]> {
  const res = await fetch(`${BASE}/api/v1/migrations`);
  return res.json();
}

export async function fetchMigration(id: string): Promise<Migration> {
  const res = await fetch(`${BASE}/api/v1/migrations/${encodeURIComponent(id)}`);
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

export async function createMigration(req: CreateMigrationRequest): Promise<Migration> {
  const res = await fetch(`${BASE}/api/v1/migrations`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
  return res.json();
}

export async function removeMigration(id: string, force?: boolean): Promise<void> {
  const params = force ? "?force=true" : "";
  const res = await fetch(`${BASE}/api/v1/migrations/${encodeURIComponent(id)}${params}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
}

export async function startMigration(id: string): Promise<void> {
  const res = await fetch(`${BASE}/api/v1/migrations/${encodeURIComponent(id)}/start`, {
    method: "POST",
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
}

export async function stopMigration(id: string): Promise<void> {
  const res = await fetch(`${BASE}/api/v1/migrations/${encodeURIComponent(id)}/stop`, {
    method: "POST",
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
}

export async function switchoverMigration(id: string): Promise<void> {
  const res = await fetch(`${BASE}/api/v1/migrations/${encodeURIComponent(id)}/switchover`, {
    method: "POST",
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
}

export async function fetchMigrationLogs(id: string): Promise<LogEntry[]> {
  const res = await fetch(`${BASE}/api/v1/migrations/${encodeURIComponent(id)}/logs`);
  if (!res.ok) return [];
  return res.json();
}

// --- Backups ---

export async function fetchBackups(clusterId: string): Promise<Backup[]> {
  const res = await fetch(
    `${BASE}/api/v1/backups?cluster_id=${encodeURIComponent(clusterId)}`
  );
  return res.json();
}

export async function fetchBackup(id: string): Promise<Backup> {
  const res = await fetch(`${BASE}/api/v1/backups/${encodeURIComponent(id)}`);
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

export async function fetchLatestBackup(clusterId: string): Promise<Backup | null> {
  const res = await fetch(
    `${BASE}/api/v1/backups/latest?cluster_id=${encodeURIComponent(clusterId)}`
  );
  if (res.status === 404) return null;
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

export async function removeBackup(id: string): Promise<void> {
  const res = await fetch(`${BASE}/api/v1/backups/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
}

// --- Monitoring ---

export async function fetchMonitoringOverview(
  clusterId: string
): Promise<MonitoringOverview> {
  const res = await fetch(
    `${BASE}/api/v1/monitoring/${encodeURIComponent(clusterId)}`
  );
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

export async function fetchNodeTableStats(
  clusterId: string,
  nodeId: string
): Promise<Tier2Snapshot> {
  const res = await fetch(
    `${BASE}/api/v1/monitoring/${encodeURIComponent(clusterId)}/nodes/${encodeURIComponent(nodeId)}/tables`
  );
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

export async function fetchNodeSizes(
  clusterId: string,
  nodeId: string
): Promise<Tier3Snapshot> {
  const res = await fetch(
    `${BASE}/api/v1/monitoring/${encodeURIComponent(clusterId)}/nodes/${encodeURIComponent(nodeId)}/sizes`
  );
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

export async function refreshNodeSizes(
  clusterId: string,
  nodeId: string
): Promise<void> {
  const res = await fetch(
    `${BASE}/api/v1/monitoring/${encodeURIComponent(clusterId)}/nodes/${encodeURIComponent(nodeId)}/refresh-sizes`,
    { method: "POST" }
  );
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
}

export async function startMonitoring(clusterId: string): Promise<void> {
  const res = await fetch(`${BASE}/api/v1/monitoring/start`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ cluster_id: clusterId }),
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
}

export async function stopMonitoring(clusterId: string): Promise<void> {
  const res = await fetch(`${BASE}/api/v1/monitoring/stop`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ cluster_id: clusterId }),
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
}

export async function fetchMonitoringStatus(): Promise<{
  monitored_clusters: string[];
}> {
  const res = await fetch(`${BASE}/api/v1/monitoring/status`);
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

export async function toggleNodeMonitoring(
  clusterId: string,
  nodeId: string,
  enabled: boolean
): Promise<void> {
  const res = await fetch(`${BASE}/api/v1/monitoring/toggle`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ cluster_id: clusterId, node_id: nodeId, enabled }),
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(body || `HTTP ${res.status}`);
  }
}

export async function fetchSlowQueries(
  clusterId: string,
  nodeId: string
): Promise<SlowQueryEntry[]> {
  const res = await fetch(
    `${BASE}/api/v1/monitoring/${encodeURIComponent(clusterId)}/nodes/${encodeURIComponent(nodeId)}/slow-queries`
  );
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

// ---------------------------------------------------------------------------
// Tier 1 — lightweight, polled every 2s
// ---------------------------------------------------------------------------

export interface ActivitySnapshot {
  total_connections: number;
  active_queries: number;
  idle_connections: number;
  idle_in_transaction: number;
  waiting_on_lock: number;
  max_connections: number;
  blocked_locks: number;
  longest_query_sec: number;
  longest_query?: string;
  longest_query_pid?: number;
  by_state: Record<string, number>;
  by_wait_event?: Record<string, number>;
}

export interface DatabaseStats {
  cache_hit_ratio: number;
  txn_commit_rate: number;
  txn_rollback_rate: number;
  tup_inserted_rate: number;
  tup_updated_rate: number;
  tup_deleted_rate: number;
  tup_fetched_rate: number;
  temp_files_rate: number;
  temp_bytes_rate: number;
  temp_files_total: number;
  temp_bytes_total: number;
  deadlocks_rate: number;
  blk_read_rate: number;
  blk_hit_rate: number;
}

export interface CheckpointerStats {
  checkpoints_timed_rate: number;
  checkpoints_req_rate: number;
  buffers_checkpoint_rate: number;
  buffers_clean_rate: number;
  buffers_backend_rate: number;
  maxwritten_clean_rate: number;
}

export interface WALStats {
  wal_bytes_rate: number;
  wal_records_rate: number;
  wal_fpi_rate: number;
  archive_pending: number;
  archive_fail_rate: number;
}

export interface StandbyInfo {
  application_name: string;
  state: string;
  sent_lag: string;
  write_lag: string;
  flush_lag: string;
  replay_lag: string;
}

export interface ReplicationInfo {
  is_replica: boolean;
  replay_lag_bytes?: number;
  replay_lag_sec?: number;
  standbys?: StandbyInfo[];
}

export interface DatabaseHealth {
  txid_age_pct: number;
  txid_age: number;
  txid_max_age: number;
  mxid_age_pct: number;
  mxid_age: number;
  mxid_max_age: number;
}

export interface Tier1Snapshot {
  timestamp: string;
  node_id: string;
  activity: ActivitySnapshot;
  database: DatabaseStats;
  checkpointer: CheckpointerStats;
  wal: WALStats;
  replication: ReplicationInfo;
  health: DatabaseHealth;
}

// ---------------------------------------------------------------------------
// Tier 2 — medium weight, polled every 30s
// ---------------------------------------------------------------------------

export interface TableStat {
  database: string;
  schema: string;
  name: string;
  seq_scan: number;
  seq_tup_read: number;
  idx_scan: number;
  idx_tup_fetch: number;
  n_tup_ins: number;
  n_tup_upd: number;
  n_tup_del: number;
  n_live_tup: number;
  n_dead_tup: number;
  last_vacuum?: string;
  last_autovacuum?: string;
  last_analyze?: string;
  last_autoanalyze?: string;
  index_usage_ratio: number;
}

export interface IndexStat {
  database: string;
  schema: string;
  table: string;
  name: string;
  idx_scan: number;
  idx_tup_read: number;
  idx_tup_fetch: number;
  size_bytes: number;
  size: string;
}

export interface LockInfo {
  waiting_pid: number;
  blocking_pid: number;
  mode: string;
  relation?: string;
  granted: boolean;
}

export interface VacuumProgress {
  pid: number;
  schema: string;
  table: string;
  phase: string;
  heap_blks_total: number;
  heap_blks_scanned: number;
  percent_done: number;
}

export interface Tier2Snapshot {
  timestamp: string;
  node_id: string;
  databases?: string[];
  tables: TableStat[];
  indexes: IndexStat[];
  locks: LockInfo[];
  vacuum_progress?: VacuumProgress[];
}

// ---------------------------------------------------------------------------
// Tier 3 — heavy, on-demand
// ---------------------------------------------------------------------------

export interface RelationSize {
  database: string;
  schema: string;
  name: string;
  total_bytes: number;
  total_size: string;
  data_bytes: number;
  index_bytes: number;
  toast_bytes: number;
}

export interface BloatEstimate {
  database: string;
  schema: string;
  name: string;
  total_bytes: number;
  bloat_bytes: number;
  bloat_ratio: number;
}

export interface TopQuery {
  database: string;
  query_id: number;
  query: string;
  calls: number;
  total_time_ms: number;
  mean_time_ms: number;
  rows: number;
  shared_blks_hit: number;
  shared_blks_read: number;
  hit_ratio: number;
}

export interface Tier3Snapshot {
  timestamp: string;
  node_id: string;
  databases?: string[];
  sizes?: RelationSize[];
  bloat?: BloatEstimate[];
  top_queries?: TopQuery[];
}

// ---------------------------------------------------------------------------
// Slow query log
// ---------------------------------------------------------------------------

export interface SlowQueryEntry {
  timestamp: string;
  pid: number;
  datname: string;
  usename: string;
  duration_sec: number;
  query: string;
  wait_event?: string;
  state: string;
}

// ---------------------------------------------------------------------------
// Composite — API responses
// ---------------------------------------------------------------------------

export type NodeStatus = "ok" | "connecting" | "error" | "disconnected";

export interface NodeMonitoringSnapshot {
  node_id: string;
  node_name: string;
  cluster_id: string;
  status: NodeStatus;
  error?: string;
  tier1?: Tier1Snapshot;
  tier2?: Tier2Snapshot;
  tier3?: Tier3Snapshot;
}

export interface MonitoringOverview {
  cluster_id: string;
  cluster_name: string;
  nodes: NodeMonitoringSnapshot[];
  history?: Tier1Snapshot[];
}

export interface MonitoringClusterSummary {
  cluster_id: string;
  cluster_name: string;
  nodes_total: number;
  nodes_ok: number;
  tps: number;
  active_queries: number;
  cache_hit_ratio: number;
  replication_lag_sec: number;
  txid_age_pct: number;
  blocked_locks: number;
  total_connections: number;
  max_connections: number;
}

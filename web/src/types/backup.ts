export type BackupType = "full" | "diff" | "incr";
export type BackupStatus = "pending" | "running" | "complete" | "failed";

export interface Backup {
  id: string;
  cluster_id: string;
  node_id: string;
  stanza: string;
  backup_type: BackupType;
  status: BackupStatus;
  error_message?: string;
  backup_label?: string;
  wal_start?: string;
  wal_stop?: string;
  size_bytes: number;
  delta_bytes: number;
  repo_size_bytes: number;
  database_list?: string[];
  duration_ms: number;
  started_at?: string;
  finished_at?: string;
  synced_at?: string;
  created_at: string;
}

export interface TableProgress {
  schema: string;
  name: string;
  status: "pending" | "copying" | "copied" | "streaming";
  rows_total: number;
  rows_copied: number;
  size_bytes: number;
  bytes_copied: number;
  percent: number;
  elapsed_sec: number;
}

export interface Snapshot {
  timestamp: string;
  phase: string;
  elapsed_sec: number;

  applied_lsn: string;
  confirmed_lsn: string;
  lag_bytes: number;
  lag_formatted: string;

  tables_total: number;
  tables_copied: number;
  tables: TableProgress[];

  rows_per_sec: number;
  bytes_per_sec: number;
  total_rows: number;
  total_bytes: number;

  error_count: number;
  last_error?: string;

  events?: MigrationEvent[];
  phases?: PhaseEntry[];
  error_history?: ErrorEntry[];
  schema_stats?: SchemaStats;
}

export interface LogEntry {
  time: string;
  level: string;
  message: string;
  fields?: Record<string, string>;
}

export interface MigrationEvent {
  time: string;
  type: string;
  message: string;
  fields?: Record<string, string>;
}

export interface PhaseEntry {
  phase: string;
  started_at: string;
  ended_at?: string;
  duration_sec: number;
}

export interface ErrorEntry {
  time: string;
  phase: string;
  message: string;
  retryable: boolean;
}

export interface SchemaStatementDetail {
  statement: string;
  reason: string;
}

export interface SchemaStats {
  statements_total: number;
  statements_applied: number;
  statements_skipped: number;
  errors_tolerated: number;
  skipped_details?: SchemaStatementDetail[];
  errored_details?: SchemaStatementDetail[];
}

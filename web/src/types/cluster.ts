export type NodeRole = "primary" | "replica" | "standby";

export interface ClusterNode {
  id: string;
  name: string;
  host: string;
  port: number;
  role: NodeRole;
  user?: string;
  password?: string;
  dbname?: string;
  agent_url?: string;
  monitoring_enabled: boolean;
}

export interface Cluster {
  id: string;
  name: string;
  nodes: ClusterNode[];
  tags?: string[];
  backup_path?: string;
  created_at: string;
  updated_at: string;
}

export interface ConnTestResult {
  reachable: boolean;
  version?: string;
  is_replica: boolean;
  privileges?: Record<string, boolean>;
  latency_ns: number;
  error?: string;
}

export interface ClusterInfo {
  version: string;
  is_replica: boolean;
  uptime: string;
  started_at: string;
  max_connections: number;
  cluster_size: string;
  cluster_bytes: number;
  databases: DBInfo[];
  parameters: ParameterInfo[];
}

export interface DBInfo {
  name: string;
  size: string;
  size_bytes: number;
  owner: string;
  schemas?: SchemaInfo[];
}

export interface SchemaInfo {
  name: string;
  tables: TableInfo[];
  table_count: number;
  total_size: string;
  total_bytes: number;
}

export interface TableInfo {
  schema: string;
  name: string;
  row_count: number;
  total_size: string;
  total_bytes: number;
  data_size: string;
  data_bytes: number;
  index_size: string;
  index_bytes: number;
}

export interface ParameterInfo {
  name: string;
  setting: string;
  unit?: string;
  source: string;
}

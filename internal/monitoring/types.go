package monitoring

import "time"

// TierConfig controls polling intervals per tier.
type TierConfig struct {
	Tier1Interval time.Duration // default 2s — shared memory stats
	Tier2Interval time.Duration // default 30s — table/index stats with LIMIT
	Tier3Interval time.Duration // default 5min — sizes, bloat, top queries (or 0 for on-demand only)
}

// DefaultTierConfig returns conservative defaults.
func DefaultTierConfig() TierConfig {
	return TierConfig{
		Tier1Interval: 2 * time.Second,
		Tier2Interval: 30 * time.Second,
		Tier3Interval: 5 * time.Minute,
	}
}

// ---------------------------------------------------------------------------
// Tier 1 — lightweight, high-frequency (shared memory reads only)
// ---------------------------------------------------------------------------

// ActivitySnapshot captures aggregated pg_stat_activity state.
type ActivitySnapshot struct {
	TotalConnections int            `json:"total_connections"`
	ActiveQueries    int            `json:"active_queries"`
	IdleConnections  int            `json:"idle_connections"`
	IdleInTx         int            `json:"idle_in_transaction"`
	WaitingOnLock    int            `json:"waiting_on_lock"`
	MaxConnections   int            `json:"max_connections"`
	BlockedLocks     int            `json:"blocked_locks"`
	LongestQuerySec  float64        `json:"longest_query_sec"`
	LongestQuery     string         `json:"longest_query,omitempty"`
	LongestQueryPID  int32          `json:"longest_query_pid,omitempty"`
	ByState          map[string]int `json:"by_state"`
	ByWaitEvent      map[string]int `json:"by_wait_event,omitempty"`
}

// DatabaseStats captures pg_stat_database deltas (rates per second).
type DatabaseStats struct {
	CacheHitRatio   float64 `json:"cache_hit_ratio"`   // 0.0–1.0
	TxnCommitRate   float64 `json:"txn_commit_rate"`   // per second
	TxnRollbackRate float64 `json:"txn_rollback_rate"` // per second
	TupInsertedRate float64 `json:"tup_inserted_rate"`
	TupUpdatedRate  float64 `json:"tup_updated_rate"`
	TupDeletedRate  float64 `json:"tup_deleted_rate"`
	TupFetchedRate  float64 `json:"tup_fetched_rate"`
	TempFilesRate   float64 `json:"temp_files_rate"`
	TempBytesRate   float64 `json:"temp_bytes_rate"`
	TempFilesTotal  int64   `json:"temp_files_total"`
	TempBytesTotal  int64   `json:"temp_bytes_total"`
	DeadlocksRate   float64 `json:"deadlocks_rate"`
	BlkReadRate     float64 `json:"blk_read_rate"`
	BlkHitRate      float64 `json:"blk_hit_rate"`
}

// CheckpointerStats captures checkpoint/bgwriter deltas.
type CheckpointerStats struct {
	CheckpointsTimedRate  float64 `json:"checkpoints_timed_rate"`
	CheckpointsReqRate    float64 `json:"checkpoints_req_rate"`
	BuffersCheckpointRate float64 `json:"buffers_checkpoint_rate"`
	BuffersCleanRate      float64 `json:"buffers_clean_rate"`
	BuffersBackendRate    float64 `json:"buffers_backend_rate"`
	MaxWrittenCleanRate   float64 `json:"maxwritten_clean_rate"`
}

// WALStats captures WAL generation and archiving metrics.
type WALStats struct {
	WALBytesRate    float64 `json:"wal_bytes_rate"`
	WALRecordsRate  float64 `json:"wal_records_rate"`
	WALFpiRate      float64 `json:"wal_fpi_rate"`
	ArchivePending  int     `json:"archive_pending"`
	ArchiveFailRate float64 `json:"archive_fail_rate"`
}

// ReplicationInfo captures replication state.
type ReplicationInfo struct {
	IsReplica      bool          `json:"is_replica"`
	ReplayLagBytes int64         `json:"replay_lag_bytes,omitempty"`
	ReplayLagSec   float64       `json:"replay_lag_sec,omitempty"`
	Standbys       []StandbyInfo `json:"standbys,omitempty"` // populated on primaries
}

// StandbyInfo describes a connected standby (from pg_stat_replication).
type StandbyInfo struct {
	ApplicationName string `json:"application_name"`
	State           string `json:"state"`
	SentLag         string `json:"sent_lag"`
	WriteLag        string `json:"write_lag"`
	FlushLag        string `json:"flush_lag"`
	ReplayLag       string `json:"replay_lag"`
}

// DatabaseHealth captures database-level health indicators.
type DatabaseHealth struct {
	TxIDAgePct  float64 `json:"txid_age_pct"`  // 0.0–1.0
	TxIDAge     int64   `json:"txid_age"`
	TxIDMaxAge  int64   `json:"txid_max_age"`
	MxIDAgePct  float64 `json:"mxid_age_pct"`  // 0.0–1.0
	MxIDAge     int64   `json:"mxid_age"`
	MxIDMaxAge  int64   `json:"mxid_max_age"`
}

// Tier1Snapshot is the complete Tier 1 snapshot for a single node.
type Tier1Snapshot struct {
	Timestamp    time.Time          `json:"timestamp"`
	NodeID       string             `json:"node_id"`
	Activity     ActivitySnapshot   `json:"activity"`
	Database     DatabaseStats      `json:"database"`
	Checkpointer CheckpointerStats `json:"checkpointer"`
	WAL          WALStats           `json:"wal"`
	Replication  ReplicationInfo    `json:"replication"`
	Health       DatabaseHealth     `json:"health"`
}

// ---------------------------------------------------------------------------
// Tier 2 — medium weight, bounded by LIMIT + schema filter
// ---------------------------------------------------------------------------

// TableStat captures per-table statistics from pg_stat_user_tables.
type TableStat struct {
	Database        string     `json:"database"`
	Schema          string     `json:"schema"`
	Name            string     `json:"name"`
	SeqScan         int64      `json:"seq_scan"`
	SeqTupRead      int64      `json:"seq_tup_read"`
	IdxScan         int64      `json:"idx_scan"`
	IdxTupFetch     int64      `json:"idx_tup_fetch"`
	NTupIns         int64      `json:"n_tup_ins"`
	NTupUpd         int64      `json:"n_tup_upd"`
	NTupDel         int64      `json:"n_tup_del"`
	NLiveTup        int64      `json:"n_live_tup"`
	NDeadTup        int64      `json:"n_dead_tup"`
	LastVacuum      *time.Time `json:"last_vacuum,omitempty"`
	LastAutoVacuum  *time.Time `json:"last_autovacuum,omitempty"`
	LastAnalyze     *time.Time `json:"last_analyze,omitempty"`
	LastAutoAnalyze *time.Time `json:"last_autoanalyze,omitempty"`
	IndexUsageRatio float64    `json:"index_usage_ratio"` // idx_scan / (seq_scan + idx_scan)
}

// IndexStat captures per-index statistics from pg_stat_user_indexes.
type IndexStat struct {
	Database    string `json:"database"`
	Schema      string `json:"schema"`
	Table       string `json:"table"`
	Name        string `json:"name"`
	IdxScan     int64  `json:"idx_scan"`
	IdxTupRead  int64  `json:"idx_tup_read"`
	IdxTupFetch int64  `json:"idx_tup_fetch"`
	SizeBytes   int64  `json:"size_bytes"`
	Size        string `json:"size"`
}

// LockInfo captures a blocking lock relationship.
type LockInfo struct {
	WaitingPID  int32  `json:"waiting_pid"`
	BlockingPID int32  `json:"blocking_pid"`
	Mode        string `json:"mode"`
	Relation    string `json:"relation,omitempty"`
	Granted     bool   `json:"granted"`
}

// VacuumProgress captures in-progress vacuum operations.
type VacuumProgress struct {
	PID           int32   `json:"pid"`
	Schema        string  `json:"schema"`
	Table         string  `json:"table"`
	Phase         string  `json:"phase"`
	HeapBlksTotal int64   `json:"heap_blks_total"`
	HeapBlksScanned int64 `json:"heap_blks_scanned"`
	PercentDone   float64 `json:"percent_done"`
}

// Tier2Snapshot is the complete Tier 2 snapshot for a single node.
type Tier2Snapshot struct {
	Timestamp       time.Time        `json:"timestamp"`
	NodeID          string           `json:"node_id"`
	Databases       []string         `json:"databases,omitempty"`
	Tables          []TableStat      `json:"tables"`
	Indexes         []IndexStat      `json:"indexes"`
	Locks           []LockInfo       `json:"locks"`
	VacuumProgress  []VacuumProgress `json:"vacuum_progress,omitempty"`
}

// ---------------------------------------------------------------------------
// Tier 3 — heavy, infrequent or on-demand
// ---------------------------------------------------------------------------

// RelationSize captures size breakdown for a relation.
type RelationSize struct {
	Database   string `json:"database"`
	Schema     string `json:"schema"`
	Name       string `json:"name"`
	TotalBytes int64  `json:"total_bytes"`
	TotalSize  string `json:"total_size"`
	DataBytes  int64  `json:"data_bytes"`
	IndexBytes int64  `json:"index_bytes"`
	ToastBytes int64  `json:"toast_bytes"`
}

// BloatEstimate captures statistics-based bloat estimation.
type BloatEstimate struct {
	Database   string  `json:"database"`
	Schema     string  `json:"schema"`
	Name       string  `json:"name"`
	TotalBytes int64   `json:"total_bytes"`
	BloatBytes int64   `json:"bloat_bytes"`
	BloatRatio float64 `json:"bloat_ratio"` // 0.0–1.0
}

// TopQuery captures a top query from pg_stat_statements.
type TopQuery struct {
	Database       string  `json:"database"`
	QueryID        int64   `json:"query_id"`
	Query          string  `json:"query"`
	Calls          int64   `json:"calls"`
	TotalTimeMs    float64 `json:"total_time_ms"`
	MeanTimeMs     float64 `json:"mean_time_ms"`
	Rows           int64   `json:"rows"`
	SharedBlksHit  int64   `json:"shared_blks_hit"`
	SharedBlksRead int64   `json:"shared_blks_read"`
	HitRatio       float64 `json:"hit_ratio"` // 0.0–1.0
}

// Tier3Snapshot is the complete Tier 3 snapshot for a single node.
type Tier3Snapshot struct {
	Timestamp  time.Time       `json:"timestamp"`
	NodeID     string          `json:"node_id"`
	Databases  []string        `json:"databases,omitempty"`
	Sizes      []RelationSize  `json:"sizes,omitempty"`
	Bloat      []BloatEstimate `json:"bloat,omitempty"`
	TopQueries []TopQuery      `json:"top_queries,omitempty"`
}

// ---------------------------------------------------------------------------
// Slow query log — kept in memory ring buffer, exposed via API
// ---------------------------------------------------------------------------

// SlowQueryEntry captures a query seen running longer than a threshold.
type SlowQueryEntry struct {
	Timestamp    time.Time `json:"timestamp"`
	PID          int32     `json:"pid"`
	Datname      string    `json:"datname"`
	Usename      string    `json:"usename"`
	DurationSec  float64   `json:"duration_sec"`
	Query        string    `json:"query"`
	WaitEvent    string    `json:"wait_event,omitempty"`
	State        string    `json:"state"`
}

// ---------------------------------------------------------------------------
// Composite types — API responses
// ---------------------------------------------------------------------------

// NodeMonitoringSnapshot is the combined state for a single monitored node.
type NodeMonitoringSnapshot struct {
	NodeID    string         `json:"node_id"`
	NodeName  string         `json:"node_name"`
	ClusterID string         `json:"cluster_id"`
	Status    string         `json:"status"` // "ok", "connecting", "error", "disconnected"
	Error     string         `json:"error,omitempty"`
	Tier1     *Tier1Snapshot `json:"tier1,omitempty"`
	Tier2     *Tier2Snapshot `json:"tier2,omitempty"`
	Tier3     *Tier3Snapshot `json:"tier3,omitempty"`
}

// MonitoringOverview is the top-level API response for a cluster.
type MonitoringOverview struct {
	ClusterID   string                   `json:"cluster_id"`
	ClusterName string                   `json:"cluster_name"`
	Nodes       []NodeMonitoringSnapshot `json:"nodes"`
	History     []Tier1Snapshot          `json:"history,omitempty"`
}

// MonitoringClusterSummary provides a quick overview of a monitored cluster for listing pages.
type MonitoringClusterSummary struct {
	ClusterID        string  `json:"cluster_id"`
	ClusterName      string  `json:"cluster_name"`
	NodesTotal       int     `json:"nodes_total"`
	NodesOK          int     `json:"nodes_ok"`
	TPS              float64 `json:"tps"`
	ActiveQueries    int     `json:"active_queries"`
	CacheHitRatio    float64 `json:"cache_hit_ratio"`
	ReplicationLag   float64 `json:"replication_lag_sec"`
	TxIDAgePct       float64 `json:"txid_age_pct"`
	BlockedLocks     int     `json:"blocked_locks"`
	TotalConnections int     `json:"total_connections"`
	MaxConnections   int     `json:"max_connections"`
}

// ---------------------------------------------------------------------------
// Internal types — raw counters for delta computation
// ---------------------------------------------------------------------------

// rawDBStats holds the raw cumulative counters from pg_stat_database.
// Used internally for delta computation; never exposed via API.
type rawDBStats struct {
	XactCommit   int64
	XactRollback int64
	BlksRead     int64
	BlksHit      int64
	TupReturned  int64
	TupFetched   int64
	TupInserted  int64
	TupUpdated   int64
	TupDeleted   int64
	TempFiles    int64
	TempBytes    int64
	Deadlocks    int64
	StatsReset   float64 // epoch of stats_reset, for detecting resets
}

// rawCheckpointerStats holds raw cumulative checkpoint counters.
type rawCheckpointerStats struct {
	CheckpointsTimed  int64
	CheckpointsReq    int64
	BuffersCheckpoint int64
	BuffersClean      int64
	BuffersBackend    int64
	MaxWrittenClean   int64
	StatsReset        float64
}

// rawWALStats holds raw cumulative WAL counters.
type rawWALStats struct {
	WALRecords   int64
	WALFpi       int64
	WALBytes     int64
	ArchiveFail  int64
	StatsReset   float64
}

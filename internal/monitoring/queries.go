package monitoring

// ---------------------------------------------------------------------------
// Tier 1 — shared memory stats, safe at any frequency
// ---------------------------------------------------------------------------

// ActivityQuery aggregates pg_stat_activity in a single row.
// pg_stat_activity reads from shared memory (PgBackendStatus array), not shared_buffers.
const ActivityQuery = `
SELECT
    count(*) AS total,
    count(*) FILTER (WHERE state = 'active') AS active,
    count(*) FILTER (WHERE state = 'idle') AS idle,
    count(*) FILTER (WHERE state = 'idle in transaction') AS idle_in_tx,
    count(*) FILTER (WHERE wait_event_type = 'Lock') AS waiting_lock,
    COALESCE(max(EXTRACT(EPOCH FROM clock_timestamp() - query_start))
        FILTER (WHERE state = 'active' AND query NOT LIKE 'autovacuum:%'), 0) AS longest_sec,
    (SELECT setting::int FROM pg_settings WHERE name = 'max_connections') AS max_conn
FROM pg_stat_activity
WHERE backend_type = 'client backend'`

// ActivityByStateQuery returns connection counts grouped by state.
const ActivityByStateQuery = `
SELECT COALESCE(state, 'unknown'), count(*)
FROM pg_stat_activity
WHERE backend_type = 'client backend'
GROUP BY state`

// ActivityByWaitEventQuery returns connection counts grouped by wait_event_type.
const ActivityByWaitEventQuery = `
SELECT COALESCE(wait_event_type, 'CPU'), count(*)
FROM pg_stat_activity
WHERE backend_type = 'client backend' AND state = 'active'
GROUP BY wait_event_type`

// LongestQueryQuery returns the text + PID of the longest running active query.
const LongestQueryQuery = `
SELECT pid, left(query, 200)
FROM pg_stat_activity
WHERE backend_type = 'client backend'
  AND state = 'active'
  AND query NOT LIKE 'autovacuum:%'
ORDER BY query_start ASC NULLS LAST
LIMIT 1`

// DatabaseStatsQuery reads raw cumulative counters from pg_stat_database.
// We store previous values and compute deltas in Go.
const DatabaseStatsQuery = `
SELECT
    xact_commit, xact_rollback,
    blks_read, blks_hit,
    tup_returned, tup_fetched,
    tup_inserted, tup_updated, tup_deleted,
    temp_files, temp_bytes,
    deadlocks,
    COALESCE(EXTRACT(EPOCH FROM stats_reset), 0) AS stats_reset_epoch
FROM pg_stat_database
WHERE datname = current_database()`

// CheckpointerQueryPG17 reads from pg_stat_checkpointer (PG17+).
const CheckpointerQueryPG17 = `
SELECT
    num_timed, num_requested,
    buffers_written, 0::bigint, 0::bigint, 0::bigint,
    COALESCE(EXTRACT(EPOCH FROM stats_reset), 0) AS stats_reset_epoch
FROM pg_stat_checkpointer`

// CheckpointerQueryLegacy reads checkpoint columns from pg_stat_bgwriter (PG < 17).
const CheckpointerQueryLegacy = `
SELECT
    checkpoints_timed, checkpoints_req,
    buffers_checkpoint, buffers_clean, buffers_backend,
    maxwritten_clean,
    COALESCE(EXTRACT(EPOCH FROM stats_reset), 0) AS stats_reset_epoch
FROM pg_stat_bgwriter`

// WALStatsQueryPG14 reads from pg_stat_wal (PG14+).
const WALStatsQueryPG14 = `
SELECT
    wal_records, wal_fpi, wal_bytes,
    COALESCE(EXTRACT(EPOCH FROM stats_reset), 0) AS stats_reset_epoch
FROM pg_stat_wal`

// ArchiveStatsQuery reads archive pending/fail counts.
const ArchiveStatsQuery = `
SELECT
    COALESCE(last_failed_count, 0)
FROM pg_stat_archiver`

// ArchivePendingQuery counts ready WAL files.
const ArchivePendingQuery = `
SELECT count(*) FROM pg_ls_archive_statusdir() WHERE name LIKE '%.ready'`

// ReplicationLagQuery checks replica lag from shared memory functions.
const ReplicationLagQuery = `
SELECT
    pg_is_in_recovery() AS is_replica,
    CASE WHEN pg_is_in_recovery()
        THEN COALESCE(pg_wal_lsn_diff(pg_last_wal_receive_lsn(), pg_last_wal_replay_lsn()), 0)
        ELSE 0
    END AS replay_lag_bytes,
    CASE WHEN pg_is_in_recovery()
        THEN COALESCE(EXTRACT(EPOCH FROM now() - pg_last_xact_replay_timestamp()), 0)
        ELSE 0
    END AS replay_lag_sec`

// StandbyInfoQuery lists connected standbys on a primary.
const StandbyInfoQuery = `
SELECT
    application_name, state,
    COALESCE(sent_lsn::text, ''),
    COALESCE(write_lag::text, '0'),
    COALESCE(flush_lag::text, '0'),
    COALESCE(replay_lag::text, '0')
FROM pg_stat_replication`

// SlowQueriesQuery returns currently running queries above a duration threshold.
// $1 = minimum seconds, $2 = max rows.
const SlowQueriesQuery = `
SELECT
    pid,
    COALESCE(datname, ''),
    COALESCE(usename, ''),
    EXTRACT(EPOCH FROM clock_timestamp() - query_start) AS duration_sec,
    left(query, 100),
    COALESCE(wait_event_type || ':' || wait_event, ''),
    COALESCE(state, 'unknown')
FROM pg_stat_activity
WHERE backend_type = 'client backend'
  AND state = 'active'
  AND query NOT LIKE 'autovacuum:%'
  AND pid != pg_backend_pid()
  AND EXTRACT(EPOCH FROM clock_timestamp() - query_start) > $1
ORDER BY query_start ASC
LIMIT $2`

// ---------------------------------------------------------------------------
// Tier 2 — shared memory views, but bounded by LIMIT + schema filter
// ---------------------------------------------------------------------------

// TableStatsQuery reads pg_stat_user_tables with optional schema filter and LIMIT.
// $1 = text[] of schema names (NULL = all schemas), $2 = max rows.
const TableStatsQuery = `
SELECT
    schemaname, relname,
    COALESCE(seq_scan, 0), COALESCE(seq_tup_read, 0),
    COALESCE(idx_scan, 0), COALESCE(idx_tup_fetch, 0),
    COALESCE(n_tup_ins, 0), COALESCE(n_tup_upd, 0), COALESCE(n_tup_del, 0),
    COALESCE(n_live_tup, 0), COALESCE(n_dead_tup, 0),
    last_vacuum, last_autovacuum, last_analyze, last_autoanalyze
FROM pg_stat_user_tables
WHERE ($1::text[] IS NULL OR schemaname = ANY($1))
ORDER BY COALESCE(seq_scan, 0) + COALESCE(idx_scan, 0) DESC
LIMIT $2`

// IndexStatsQuery reads pg_stat_user_indexes. Includes pg_relation_size
// which reads catalog, hence Tier 2 not Tier 1. Sorted by scan count
// ascending to surface unused indexes first.
// $1 = text[] of schema names (NULL = all), $2 = max rows.
const IndexStatsQuery = `
SELECT
    schemaname, relname, indexrelname,
    COALESCE(idx_scan, 0), COALESCE(idx_tup_read, 0), COALESCE(idx_tup_fetch, 0),
    pg_relation_size(indexrelid) AS size_bytes,
    pg_size_pretty(pg_relation_size(indexrelid)) AS size
FROM pg_stat_user_indexes
WHERE ($1::text[] IS NULL OR schemaname = ANY($1))
ORDER BY idx_scan ASC
LIMIT $2`

// LockContentionQuery returns only blocked lock relationships.
// pg_locks reads from the in-memory lock table. We filter to
// only conflicting locks (NOT granted) so the result set is small.
const LockContentionQuery = `
SELECT
    blocked.pid AS waiting_pid,
    blocking.pid AS blocking_pid,
    blocked.mode,
    COALESCE(blocked.relation::regclass::text, '') AS relation,
    blocked.granted
FROM pg_locks blocked
JOIN pg_locks blocking
    ON blocking.locktype = blocked.locktype
    AND blocking.database IS NOT DISTINCT FROM blocked.database
    AND blocking.relation IS NOT DISTINCT FROM blocked.relation
    AND blocking.page IS NOT DISTINCT FROM blocked.page
    AND blocking.tuple IS NOT DISTINCT FROM blocked.tuple
    AND blocking.virtualxid IS NOT DISTINCT FROM blocked.virtualxid
    AND blocking.transactionid IS NOT DISTINCT FROM blocked.transactionid
    AND blocking.classid IS NOT DISTINCT FROM blocked.classid
    AND blocking.objid IS NOT DISTINCT FROM blocked.objid
    AND blocking.objsubid IS NOT DISTINCT FROM blocked.objsubid
    AND blocking.pid != blocked.pid
    AND blocking.granted
WHERE NOT blocked.granted
LIMIT 50`

// VacuumProgressQuery captures currently running vacuum operations.
const VacuumProgressQuery = `
SELECT
    p.pid,
    n.nspname,
    c.relname,
    p.phase,
    p.heap_blks_total,
    p.heap_blks_scanned
FROM pg_stat_progress_vacuum p
JOIN pg_class c ON c.oid = p.relid
JOIN pg_namespace n ON n.oid = c.relnamespace
ORDER BY p.pid`

// ---------------------------------------------------------------------------
// Tier 3 — expensive, infrequent or on-demand only
// ---------------------------------------------------------------------------

// RelationSizesQuery uses pg_total_relation_size which stats() files and
// reads catalog. ORDER BY relpages (cached estimate, no I/O) then LIMIT.
// $1 = text[] of schema names (NULL = all), $2 = max rows.
const RelationSizesQuery = `
SELECT
    n.nspname, c.relname,
    pg_total_relation_size(c.oid) AS total_bytes,
    pg_size_pretty(pg_total_relation_size(c.oid)) AS total_size,
    pg_relation_size(c.oid) AS data_bytes,
    pg_indexes_size(c.oid) AS index_bytes,
    COALESCE(pg_total_relation_size(c.reltoastrelid), 0) AS toast_bytes
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind = 'r'
    AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
    AND ($1::text[] IS NULL OR n.nspname = ANY($1))
ORDER BY c.relpages DESC
LIMIT $2`

// BloatEstimateQuery uses statistics-based estimation without pgstattuple.
// Joins pg_stats for avg_width which is maintained by ANALYZE.
// $1 = text[] of schema names (NULL = all), $2 = max rows.
const BloatEstimateQuery = `
WITH table_bloat AS (
    SELECT
        s.schemaname, s.relname,
        pg_total_relation_size(s.relid) AS total_bytes,
        GREATEST(
            pg_total_relation_size(s.relid) -
            (s.n_live_tup * (
                SELECT COALESCE(sum(avg_width), 0) + 24
                FROM pg_stats ps
                WHERE ps.schemaname = s.schemaname AND ps.tablename = s.relname
            ))::bigint,
            0
        ) AS bloat_bytes
    FROM pg_stat_user_tables s
    WHERE s.n_live_tup > 1000
        AND ($1::text[] IS NULL OR s.schemaname = ANY($1))
)
SELECT schemaname, relname, total_bytes, bloat_bytes
FROM table_bloat
WHERE total_bytes > 0
ORDER BY total_bytes DESC
LIMIT $2`

// TopQueriesQuery reads from pg_stat_statements (shared memory hash table).
// Gracefully fails if extension is not installed.
// $1 = max rows.
const TopQueriesQuery = `
SELECT
    queryid, left(query, 500), calls,
    total_exec_time AS total_time_ms,
    mean_exec_time AS mean_time_ms,
    rows,
    shared_blks_hit, shared_blks_read
FROM pg_stat_statements
WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
ORDER BY total_exec_time DESC
LIMIT $1`

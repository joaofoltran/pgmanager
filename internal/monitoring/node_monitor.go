package monitoring

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmanager/internal/cluster"
)

const (
	tier1RingCap = 600 // ~20 min at 2s
	tier2RingCap = 60  // ~30 min at 30s
	tier3RingCap = 12  // ~1 hr at 5min
	slowQueryCap = 500 // max slow query entries kept in memory
)

type nodeMonitor struct {
	clusterID   string
	clusterName string
	node        cluster.Node
	cfg         TierConfig
	store       *Store
	logger      zerolog.Logger

	mu     sync.RWMutex
	conn   *pgx.Conn
	status string // "connecting", "ok", "error", "disconnected"
	lastErr string
	pgVersion int // e.g. 170000 for PG17

	// Discovered databases on this instance.
	databases []string

	// Latest snapshots.
	tier1 *Tier1Snapshot
	tier2 *Tier2Snapshot
	tier3 *Tier3Snapshot

	// Ring buffers for history.
	tier1Ring []Tier1Snapshot
	tier2Ring []Tier2Snapshot
	tier3Ring []Tier3Snapshot

	// Slow query log ring buffer.
	slowQueries []SlowQueryEntry
	seenSlowPIDs map[int32]time.Time

	// Previous raw counters for delta computation.
	prevDB          *rawDBStats
	prevDBTime      time.Time
	prevCkpt        *rawCheckpointerStats
	prevCkptTime    time.Time
	prevWAL         *rawWALStats
	prevWALTime     time.Time

	cancel context.CancelFunc
}

func newNodeMonitor(clusterID, clusterName string, node cluster.Node, cfg TierConfig, store *Store, logger zerolog.Logger) *nodeMonitor {
	return &nodeMonitor{
		clusterID:    clusterID,
		clusterName:  clusterName,
		node:         node,
		cfg:          cfg,
		store:        store,
		logger:       logger.With().Str("node", node.ID).Str("host", node.Host).Logger(),
		status:       "connecting",
		tier1Ring:    make([]Tier1Snapshot, 0, tier1RingCap),
		tier2Ring:    make([]Tier2Snapshot, 0, tier2RingCap),
		tier3Ring:    make([]Tier3Snapshot, 0, tier3RingCap),
		slowQueries:  make([]SlowQueryEntry, 0, slowQueryCap),
		seenSlowPIDs: make(map[int32]time.Time),
	}
}

func (nm *nodeMonitor) run(ctx context.Context) {
	ctx, nm.cancel = context.WithCancel(ctx)

	backoff := time.Second
	for {
		if err := nm.connect(ctx); err != nil {
			nm.mu.Lock()
			nm.status = "error"
			nm.lastErr = err.Error()
			nm.mu.Unlock()
			nm.logger.Warn().Err(err).Dur("retry_in", backoff).Msg("monitoring connection failed")

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		backoff = time.Second

		nm.mu.Lock()
		nm.status = "ok"
		nm.lastErr = ""
		nm.mu.Unlock()

		nm.logger.Info().Int("pg_version", nm.pgVersion).Msg("monitoring connected")
		nm.runTickers(ctx)

		// If we get here, connection was lost or context cancelled.
		if ctx.Err() != nil {
			return
		}
		nm.mu.Lock()
		nm.status = "disconnected"
		nm.mu.Unlock()
	}
}

func (nm *nodeMonitor) connect(ctx context.Context) error {
	dsn := nm.node.DSN()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}
	cfg.RuntimeParams["application_name"] = "pgmanager_monitoring"
	cfg.RuntimeParams["statement_timeout"] = "5000"
	cfg.RuntimeParams["idle_in_transaction_session_timeout"] = "10000"

	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// Detect PG version (SHOW returns text, so query the function instead).
	var versionNum int
	if err := conn.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&versionNum); err != nil {
		conn.Close(ctx)
		return fmt.Errorf("detect version: %w", err)
	}

	nm.mu.Lock()
	nm.conn = conn
	nm.pgVersion = versionNum
	nm.mu.Unlock()
	return nil
}

func (nm *nodeMonitor) runTickers(ctx context.Context) {
	t1 := time.NewTicker(nm.cfg.Tier1Interval)
	t2 := time.NewTicker(nm.cfg.Tier2Interval)
	defer t1.Stop()
	defer t2.Stop()

	// Discover databases before first Tier2/Tier3 collection.
	nm.discoverDatabases(ctx)

	// Collect immediately on start.
	nm.collectTier1(ctx)
	nm.collectTier2(ctx)

	var t3 <-chan time.Time
	if nm.cfg.Tier3Interval > 0 {
		ticker := time.NewTicker(nm.cfg.Tier3Interval)
		defer ticker.Stop()
		t3 = ticker.C
		nm.collectTier3(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			nm.closeConn(ctx)
			return
		case <-t1.C:
			if err := nm.collectTier1(ctx); err != nil {
				nm.logger.Warn().Err(err).Msg("tier1 collection failed")
				nm.closeConn(ctx)
				return
			}
		case <-t2.C:
			nm.discoverDatabases(ctx)
			if err := nm.collectTier2(ctx); err != nil {
				nm.logger.Warn().Err(err).Msg("tier2 collection failed")
			}
		case <-t3:
			if err := nm.collectTier3(ctx); err != nil {
				nm.logger.Warn().Err(err).Msg("tier3 collection failed")
			}
		}
	}
}

func (nm *nodeMonitor) closeConn(ctx context.Context) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	if nm.conn != nil {
		nm.conn.Close(ctx)
		nm.conn = nil
	}
}

func (nm *nodeMonitor) stop() {
	if nm.cancel != nil {
		nm.cancel()
	}
}

// discoverDatabases queries pg_database to find all non-template databases.
func (nm *nodeMonitor) discoverDatabases(ctx context.Context) {
	nm.mu.RLock()
	conn := nm.conn
	nm.mu.RUnlock()
	if conn == nil {
		return
	}

	rows, err := conn.Query(ctx, ListDatabasesQuery)
	if err != nil {
		nm.logger.Debug().Err(err).Msg("database discovery failed")
		return
	}
	defer rows.Close()

	var dbs []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			break
		}
		dbs = append(dbs, name)
	}
	if len(dbs) == 0 {
		return
	}

	nm.mu.Lock()
	nm.databases = dbs
	nm.mu.Unlock()
	nm.logger.Debug().Strs("databases", dbs).Msg("discovered databases")
}

// connectToDB opens a short-lived connection to a specific database on the same instance.
func (nm *nodeMonitor) connectToDB(ctx context.Context, dbname string) (*pgx.Conn, error) {
	dsn := nm.node.DSN()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.Database = dbname
	cfg.RuntimeParams["application_name"] = "pgmanager_monitoring"
	cfg.RuntimeParams["statement_timeout"] = "10000"

	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", dbname, err)
	}
	return conn, nil
}

// connDBName returns the database name the main connection is connected to.
func (nm *nodeMonitor) connDBName() string {
	dbname := nm.node.DBName
	if dbname == "" {
		dbname = "postgres"
	}
	return dbname
}

// ---------------------------------------------------------------------------
// Tier 1 collection
// ---------------------------------------------------------------------------

func (nm *nodeMonitor) collectTier1(ctx context.Context) error {
	nm.mu.RLock()
	conn := nm.conn
	nm.mu.RUnlock()
	if conn == nil {
		return fmt.Errorf("no connection")
	}

	snap := Tier1Snapshot{
		Timestamp: time.Now(),
		NodeID:    nm.node.ID,
	}

	// Activity.
	var a ActivitySnapshot
	err := conn.QueryRow(ctx, ActivityQuery).Scan(
		&a.TotalConnections, &a.ActiveQueries, &a.IdleConnections,
		&a.IdleInTx, &a.WaitingOnLock, &a.LongestQuerySec, &a.MaxConnections,
	)
	if err != nil {
		return fmt.Errorf("activity query: %w", err)
	}

	// By state.
	a.ByState = make(map[string]int)
	rows, err := conn.Query(ctx, ActivityByStateQuery)
	if err != nil {
		return fmt.Errorf("activity by state: %w", err)
	}
	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			rows.Close()
			return err
		}
		a.ByState[state] = count
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// By wait event (only for active queries).
	a.ByWaitEvent = make(map[string]int)
	rows, err = conn.Query(ctx, ActivityByWaitEventQuery)
	if err != nil {
		return fmt.Errorf("activity by wait event: %w", err)
	}
	for rows.Next() {
		var event string
		var count int
		if err := rows.Scan(&event, &count); err != nil {
			rows.Close()
			return err
		}
		a.ByWaitEvent[event] = count
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Longest query detail.
	_ = conn.QueryRow(ctx, LongestQueryQuery).Scan(&a.LongestQueryPID, &a.LongestQuery)

	// Blocked locks count.
	_ = conn.QueryRow(ctx, BlockedLocksCountQuery).Scan(&a.BlockedLocks)

	snap.Activity = a

	// Database stats (delta computation).
	var raw rawDBStats
	err = conn.QueryRow(ctx, DatabaseStatsQuery).Scan(
		&raw.XactCommit, &raw.XactRollback,
		&raw.BlksRead, &raw.BlksHit,
		&raw.TupReturned, &raw.TupFetched,
		&raw.TupInserted, &raw.TupUpdated, &raw.TupDeleted,
		&raw.TempFiles, &raw.TempBytes,
		&raw.Deadlocks, &raw.StatsReset,
	)
	if err != nil {
		return fmt.Errorf("database stats: %w", err)
	}
	dbStats := nm.computeDBDeltas(raw)
	dbStats.TempFilesTotal = raw.TempFiles
	dbStats.TempBytesTotal = raw.TempBytes
	snap.Database = dbStats

	// Checkpointer stats (delta).
	var rawCk rawCheckpointerStats
	ckQuery := CheckpointerQueryLegacy
	if nm.pgVersion >= 170000 {
		ckQuery = CheckpointerQueryPG17
	}
	err = conn.QueryRow(ctx, ckQuery).Scan(
		&rawCk.CheckpointsTimed, &rawCk.CheckpointsReq,
		&rawCk.BuffersCheckpoint, &rawCk.BuffersClean,
		&rawCk.BuffersBackend, &rawCk.MaxWrittenClean,
		&rawCk.StatsReset,
	)
	if err != nil {
		return fmt.Errorf("checkpointer stats: %w", err)
	}
	snap.Checkpointer = nm.computeCkptDeltas(rawCk)

	// WAL stats (PG14+).
	if nm.pgVersion >= 140000 {
		var rawW rawWALStats
		err = conn.QueryRow(ctx, WALStatsQueryPG14).Scan(
			&rawW.WALRecords, &rawW.WALFpi, &rawW.WALBytes,
			&rawW.StatsReset,
		)
		if err != nil {
			nm.logger.Debug().Err(err).Msg("wal stats query failed")
		} else {
			snap.WAL = nm.computeWALDeltas(rawW)
		}

		// Archive pending (best-effort, may fail without superuser).
		var pending int
		if err := conn.QueryRow(ctx, ArchivePendingQuery).Scan(&pending); err == nil {
			snap.WAL.ArchivePending = pending
		}
	}

	// Replication.
	var repl ReplicationInfo
	err = conn.QueryRow(ctx, ReplicationLagQuery).Scan(
		&repl.IsReplica, &repl.ReplayLagBytes, &repl.ReplayLagSec,
	)
	if err != nil {
		return fmt.Errorf("replication lag: %w", err)
	}
	if !repl.IsReplica {
		rows, err := conn.Query(ctx, StandbyInfoQuery)
		if err == nil {
			for rows.Next() {
				var s StandbyInfo
				if err := rows.Scan(&s.ApplicationName, &s.State, &s.SentLag, &s.WriteLag, &s.FlushLag, &s.ReplayLag); err != nil {
					rows.Close()
					break
				}
				repl.Standbys = append(repl.Standbys, s)
			}
			rows.Close()
		}
	}
	snap.Replication = repl

	// Database health: Transaction ID age.
	var health DatabaseHealth
	var txidAge, txidMaxAge int64
	if err := conn.QueryRow(ctx, TxIDAgeQuery).Scan(&txidAge, &txidMaxAge); err == nil && txidMaxAge > 0 {
		health.TxIDAge = txidAge
		health.TxIDMaxAge = txidMaxAge
		health.TxIDAgePct = float64(txidAge) / float64(txidMaxAge)
	}

	// Database health: Multi-transaction ID age.
	var mxidAge, mxidMaxAge int64
	if err := conn.QueryRow(ctx, MxIDAgeQuery).Scan(&mxidAge, &mxidMaxAge); err == nil && mxidMaxAge > 0 {
		health.MxIDAge = mxidAge
		health.MxIDMaxAge = mxidMaxAge
		health.MxIDAgePct = float64(mxidAge) / float64(mxidMaxAge)
	}
	snap.Health = health

	// Collect slow queries (>5 seconds) into log.
	nm.collectSlowQueries(ctx, conn)

	// Store in memory ring buffer.
	nm.mu.Lock()
	nm.tier1 = &snap
	nm.tier1Ring = appendRing(nm.tier1Ring, snap, tier1RingCap)
	nm.mu.Unlock()

	// Persist to DB asynchronously.
	if nm.store != nil {
		go nm.store.Insert(context.Background(), snap, nm.clusterID)
	}

	return nil
}

func (nm *nodeMonitor) collectSlowQueries(ctx context.Context, conn *pgx.Conn) {
	rows, err := conn.Query(ctx, SlowQueriesQuery, 5.0, 50)
	if err != nil {
		return
	}
	defer rows.Close()

	now := time.Now()
	for rows.Next() {
		var e SlowQueryEntry
		if err := rows.Scan(&e.PID, &e.Datname, &e.Usename, &e.DurationSec, &e.Query, &e.WaitEvent, &e.State); err != nil {
			break
		}
		e.Timestamp = now

		nm.mu.Lock()
		// Only log if we haven't seen this PID in the last 30 seconds (avoid duplicates).
		if lastSeen, ok := nm.seenSlowPIDs[e.PID]; !ok || now.Sub(lastSeen) > 30*time.Second {
			nm.slowQueries = appendRing(nm.slowQueries, e, slowQueryCap)
			nm.seenSlowPIDs[e.PID] = now
		}
		nm.mu.Unlock()
	}

	// Cleanup old PID entries.
	nm.mu.Lock()
	for pid, t := range nm.seenSlowPIDs {
		if now.Sub(t) > 5*time.Minute {
			delete(nm.seenSlowPIDs, pid)
		}
	}
	nm.mu.Unlock()
}

func (nm *nodeMonitor) computeDBDeltas(raw rawDBStats) DatabaseStats {
	now := time.Now()
	defer func() {
		nm.prevDB = &raw
		nm.prevDBTime = now
	}()

	if nm.prevDB == nil {
		return DatabaseStats{}
	}

	// Detect stats reset.
	if raw.StatsReset != nm.prevDB.StatsReset {
		return DatabaseStats{}
	}

	elapsed := now.Sub(nm.prevDBTime).Seconds()
	if elapsed < 0.1 {
		return DatabaseStats{}
	}

	prev := nm.prevDB
	blksRead := float64(raw.BlksRead - prev.BlksRead)
	blksHit := float64(raw.BlksHit - prev.BlksHit)
	var hitRatio float64
	if blksRead+blksHit > 0 {
		hitRatio = blksHit / (blksRead + blksHit)
	}

	return DatabaseStats{
		CacheHitRatio:   hitRatio,
		TxnCommitRate:   float64(raw.XactCommit-prev.XactCommit) / elapsed,
		TxnRollbackRate: float64(raw.XactRollback-prev.XactRollback) / elapsed,
		TupInsertedRate: float64(raw.TupInserted-prev.TupInserted) / elapsed,
		TupUpdatedRate:  float64(raw.TupUpdated-prev.TupUpdated) / elapsed,
		TupDeletedRate:  float64(raw.TupDeleted-prev.TupDeleted) / elapsed,
		TupFetchedRate:  float64(raw.TupFetched-prev.TupFetched) / elapsed,
		TempFilesRate:   float64(raw.TempFiles-prev.TempFiles) / elapsed,
		TempBytesRate:   float64(raw.TempBytes-prev.TempBytes) / elapsed,
		DeadlocksRate:   float64(raw.Deadlocks-prev.Deadlocks) / elapsed,
		BlkReadRate:     blksRead / elapsed,
		BlkHitRate:      blksHit / elapsed,
	}
}

func (nm *nodeMonitor) computeCkptDeltas(raw rawCheckpointerStats) CheckpointerStats {
	now := time.Now()
	defer func() {
		nm.prevCkpt = &raw
		nm.prevCkptTime = now
	}()

	if nm.prevCkpt == nil {
		return CheckpointerStats{}
	}
	if raw.StatsReset != nm.prevCkpt.StatsReset {
		return CheckpointerStats{}
	}

	elapsed := now.Sub(nm.prevCkptTime).Seconds()
	if elapsed < 0.1 {
		return CheckpointerStats{}
	}

	prev := nm.prevCkpt
	return CheckpointerStats{
		CheckpointsTimedRate:  float64(raw.CheckpointsTimed-prev.CheckpointsTimed) / elapsed,
		CheckpointsReqRate:    float64(raw.CheckpointsReq-prev.CheckpointsReq) / elapsed,
		BuffersCheckpointRate: float64(raw.BuffersCheckpoint-prev.BuffersCheckpoint) / elapsed,
		BuffersCleanRate:      float64(raw.BuffersClean-prev.BuffersClean) / elapsed,
		BuffersBackendRate:    float64(raw.BuffersBackend-prev.BuffersBackend) / elapsed,
		MaxWrittenCleanRate:   float64(raw.MaxWrittenClean-prev.MaxWrittenClean) / elapsed,
	}
}

func (nm *nodeMonitor) computeWALDeltas(raw rawWALStats) WALStats {
	now := time.Now()
	defer func() {
		nm.prevWAL = &raw
		nm.prevWALTime = now
	}()

	if nm.prevWAL == nil {
		return WALStats{}
	}
	if raw.StatsReset != nm.prevWAL.StatsReset {
		return WALStats{}
	}

	elapsed := now.Sub(nm.prevWALTime).Seconds()
	if elapsed < 0.1 {
		return WALStats{}
	}

	prev := nm.prevWAL
	return WALStats{
		WALBytesRate:   float64(raw.WALBytes-prev.WALBytes) / elapsed,
		WALRecordsRate: float64(raw.WALRecords-prev.WALRecords) / elapsed,
		WALFpiRate:     float64(raw.WALFpi-prev.WALFpi) / elapsed,
	}
}

// ---------------------------------------------------------------------------
// Tier 2 collection
// ---------------------------------------------------------------------------

func (nm *nodeMonitor) collectTier2(ctx context.Context) error {
	nm.mu.RLock()
	conn := nm.conn
	dbs := nm.databases
	nm.mu.RUnlock()
	if conn == nil {
		return fmt.Errorf("no connection")
	}

	snap := Tier2Snapshot{
		Timestamp: time.Now(),
		NodeID:    nm.node.ID,
		Databases: dbs,
	}

	mainDB := nm.connDBName()

	// Collect per-database stats (tables + indexes).
	dbsToQuery := dbs
	if len(dbsToQuery) == 0 {
		dbsToQuery = []string{mainDB}
	}

	for _, db := range dbsToQuery {
		var c *pgx.Conn
		if db == mainDB {
			c = conn
		} else {
			var err error
			c, err = nm.connectToDB(ctx, db)
			if err != nil {
				nm.logger.Debug().Err(err).Str("database", db).Msg("skipping database for tier2")
				continue
			}
		}

		nm.collectTier2Tables(ctx, c, db, &snap)
		nm.collectTier2Indexes(ctx, c, db, &snap)

		if db != mainDB {
			c.Close(ctx)
		}
	}

	// Lock contention (instance-wide, from main conn).
	rows, err := conn.Query(ctx, LockContentionQuery)
	if err != nil {
		return fmt.Errorf("lock contention: %w", err)
	}
	for rows.Next() {
		var l LockInfo
		if err := rows.Scan(&l.WaitingPID, &l.BlockingPID, &l.Mode, &l.Relation, &l.Granted); err != nil {
			rows.Close()
			return fmt.Errorf("scan lock: %w", err)
		}
		snap.Locks = append(snap.Locks, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Vacuum progress (instance-wide, from main conn).
	rows, err = conn.Query(ctx, VacuumProgressQuery)
	if err != nil {
		nm.logger.Debug().Err(err).Msg("vacuum progress query failed")
	} else {
		for rows.Next() {
			var v VacuumProgress
			if err := rows.Scan(&v.PID, &v.Schema, &v.Table, &v.Phase, &v.HeapBlksTotal, &v.HeapBlksScanned); err != nil {
				rows.Close()
				break
			}
			if v.HeapBlksTotal > 0 {
				v.PercentDone = float64(v.HeapBlksScanned) / float64(v.HeapBlksTotal) * 100
			}
			snap.VacuumProgress = append(snap.VacuumProgress, v)
		}
		rows.Close()
	}

	nm.mu.Lock()
	nm.tier2 = &snap
	nm.tier2Ring = appendRing(nm.tier2Ring, snap, tier2RingCap)
	nm.mu.Unlock()
	return nil
}

func (nm *nodeMonitor) collectTier2Tables(ctx context.Context, conn *pgx.Conn, dbname string, snap *Tier2Snapshot) {
	rows, err := conn.Query(ctx, TableStatsQuery, nil, 100)
	if err != nil {
		nm.logger.Debug().Err(err).Str("database", dbname).Msg("table stats query failed")
		return
	}
	for rows.Next() {
		var t TableStat
		if err := rows.Scan(
			&t.Schema, &t.Name,
			&t.SeqScan, &t.SeqTupRead, &t.IdxScan, &t.IdxTupFetch,
			&t.NTupIns, &t.NTupUpd, &t.NTupDel,
			&t.NLiveTup, &t.NDeadTup,
			&t.LastVacuum, &t.LastAutoVacuum, &t.LastAnalyze, &t.LastAutoAnalyze,
		); err != nil {
			rows.Close()
			nm.logger.Debug().Err(err).Str("database", dbname).Msg("scan table stat failed")
			return
		}
		t.Database = dbname
		total := t.SeqScan + t.IdxScan
		if total > 0 {
			t.IndexUsageRatio = float64(t.IdxScan) / float64(total)
		}
		snap.Tables = append(snap.Tables, t)
	}
	rows.Close()
}

func (nm *nodeMonitor) collectTier2Indexes(ctx context.Context, conn *pgx.Conn, dbname string, snap *Tier2Snapshot) {
	rows, err := conn.Query(ctx, IndexStatsQuery, nil, 50)
	if err != nil {
		nm.logger.Debug().Err(err).Str("database", dbname).Msg("index stats query failed")
		return
	}
	for rows.Next() {
		var idx IndexStat
		if err := rows.Scan(
			&idx.Schema, &idx.Table, &idx.Name,
			&idx.IdxScan, &idx.IdxTupRead, &idx.IdxTupFetch,
			&idx.SizeBytes, &idx.Size,
		); err != nil {
			rows.Close()
			nm.logger.Debug().Err(err).Str("database", dbname).Msg("scan index stat failed")
			return
		}
		idx.Database = dbname
		snap.Indexes = append(snap.Indexes, idx)
	}
	rows.Close()
}

// ---------------------------------------------------------------------------
// Tier 3 collection
// ---------------------------------------------------------------------------

func (nm *nodeMonitor) collectTier3(ctx context.Context) error {
	nm.mu.RLock()
	conn := nm.conn
	dbs := nm.databases
	nm.mu.RUnlock()
	if conn == nil {
		return fmt.Errorf("no connection")
	}

	snap := Tier3Snapshot{
		Timestamp: time.Now(),
		NodeID:    nm.node.ID,
		Databases: dbs,
	}

	mainDB := nm.connDBName()

	dbsToQuery := dbs
	if len(dbsToQuery) == 0 {
		dbsToQuery = []string{mainDB}
	}

	for _, db := range dbsToQuery {
		var c *pgx.Conn
		if db == mainDB {
			c = conn
		} else {
			var err error
			c, err = nm.connectToDB(ctx, db)
			if err != nil {
				nm.logger.Debug().Err(err).Str("database", db).Msg("skipping database for tier3")
				continue
			}
		}

		nm.collectTier3Sizes(ctx, c, db, &snap)
		nm.collectTier3Bloat(ctx, c, db, &snap)
		nm.collectTier3TopQueries(ctx, c, db, &snap)

		if db != mainDB {
			c.Close(ctx)
		}
	}

	nm.mu.Lock()
	nm.tier3 = &snap
	nm.tier3Ring = appendRing(nm.tier3Ring, snap, tier3RingCap)
	nm.mu.Unlock()
	return nil
}

func (nm *nodeMonitor) collectTier3Sizes(ctx context.Context, conn *pgx.Conn, dbname string, snap *Tier3Snapshot) {
	rows, err := conn.Query(ctx, RelationSizesQuery, nil, 50)
	if err != nil {
		nm.logger.Debug().Err(err).Str("database", dbname).Msg("relation sizes query failed")
		return
	}
	for rows.Next() {
		var s RelationSize
		if err := rows.Scan(
			&s.Schema, &s.Name,
			&s.TotalBytes, &s.TotalSize,
			&s.DataBytes, &s.IndexBytes, &s.ToastBytes,
		); err != nil {
			rows.Close()
			return
		}
		s.Database = dbname
		snap.Sizes = append(snap.Sizes, s)
	}
	rows.Close()
}

func (nm *nodeMonitor) collectTier3Bloat(ctx context.Context, conn *pgx.Conn, dbname string, snap *Tier3Snapshot) {
	rows, err := conn.Query(ctx, BloatEstimateQuery, nil, 50)
	if err != nil {
		nm.logger.Debug().Err(err).Str("database", dbname).Msg("bloat estimation skipped")
		return
	}
	for rows.Next() {
		var b BloatEstimate
		if err := rows.Scan(&b.Schema, &b.Name, &b.TotalBytes, &b.BloatBytes); err != nil {
			rows.Close()
			return
		}
		b.Database = dbname
		if b.TotalBytes > 0 {
			b.BloatRatio = float64(b.BloatBytes) / float64(b.TotalBytes)
		}
		snap.Bloat = append(snap.Bloat, b)
	}
	rows.Close()
}

func (nm *nodeMonitor) collectTier3TopQueries(ctx context.Context, conn *pgx.Conn, dbname string, snap *Tier3Snapshot) {
	rows, err := conn.Query(ctx, TopQueriesQuery, 20)
	if err != nil {
		nm.logger.Debug().Err(err).Str("database", dbname).Msg("pg_stat_statements not available")
		return
	}
	for rows.Next() {
		var q TopQuery
		if err := rows.Scan(
			&q.QueryID, &q.Query, &q.Calls,
			&q.TotalTimeMs, &q.MeanTimeMs, &q.Rows,
			&q.SharedBlksHit, &q.SharedBlksRead,
		); err != nil {
			rows.Close()
			return
		}
		q.Database = dbname
		total := q.SharedBlksHit + q.SharedBlksRead
		if total > 0 {
			q.HitRatio = float64(q.SharedBlksHit) / float64(total)
		}
		snap.TopQueries = append(snap.TopQueries, q)
	}
	rows.Close()
}

// ---------------------------------------------------------------------------
// Snapshot accessors (thread-safe)
// ---------------------------------------------------------------------------

func (nm *nodeMonitor) snapshot() NodeMonitoringSnapshot {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return NodeMonitoringSnapshot{
		NodeID:    nm.node.ID,
		NodeName:  nm.node.Name,
		ClusterID: nm.clusterID,
		Status:    nm.status,
		Error:     nm.lastErr,
		Tier1:     nm.tier1,
		Tier2:     nm.tier2,
		Tier3:     nm.tier3,
	}
}

func (nm *nodeMonitor) latestTier2() *Tier2Snapshot {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return nm.tier2
}

func (nm *nodeMonitor) latestTier3() *Tier3Snapshot {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return nm.tier3
}

func (nm *nodeMonitor) tier1History() []Tier1Snapshot {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	out := make([]Tier1Snapshot, len(nm.tier1Ring))
	copy(out, nm.tier1Ring)
	return out
}

func (nm *nodeMonitor) getSlowQueries() []SlowQueryEntry {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	out := make([]SlowQueryEntry, len(nm.slowQueries))
	copy(out, nm.slowQueries)
	return out
}

// ---------------------------------------------------------------------------
// Ring buffer helper
// ---------------------------------------------------------------------------

func appendRing[T any](ring []T, item T, cap int) []T {
	ring = append(ring, item)
	if len(ring) > cap {
		ring = ring[len(ring)-cap:]
	}
	return ring
}

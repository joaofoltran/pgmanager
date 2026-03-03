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
)

type nodeMonitor struct {
	clusterID   string
	clusterName string
	node        cluster.Node
	cfg         TierConfig
	logger      zerolog.Logger

	mu     sync.RWMutex
	conn   *pgx.Conn
	status string // "connecting", "ok", "error", "disconnected"
	lastErr string
	pgVersion int // e.g. 170000 for PG17

	// Latest snapshots.
	tier1 *Tier1Snapshot
	tier2 *Tier2Snapshot
	tier3 *Tier3Snapshot

	// Ring buffers for history.
	tier1Ring []Tier1Snapshot
	tier2Ring []Tier2Snapshot
	tier3Ring []Tier3Snapshot

	// Previous raw counters for delta computation.
	prevDB          *rawDBStats
	prevDBTime      time.Time
	prevCkpt        *rawCheckpointerStats
	prevCkptTime    time.Time

	cancel context.CancelFunc
}

func newNodeMonitor(clusterID, clusterName string, node cluster.Node, cfg TierConfig, logger zerolog.Logger) *nodeMonitor {
	return &nodeMonitor{
		clusterID:   clusterID,
		clusterName: clusterName,
		node:        node,
		cfg:         cfg,
		logger:      logger.With().Str("node", node.ID).Str("host", node.Host).Logger(),
		status:      "connecting",
		tier1Ring:   make([]Tier1Snapshot, 0, tier1RingCap),
		tier2Ring:   make([]Tier2Snapshot, 0, tier2RingCap),
		tier3Ring:   make([]Tier3Snapshot, 0, tier3RingCap),
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
			if err := nm.collectTier2(ctx); err != nil {
				nm.logger.Warn().Err(err).Msg("tier2 collection failed")
				// Tier 2 failure is not fatal — keep going.
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
	snap.Database = nm.computeDBDeltas(raw)

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

	// Store.
	nm.mu.Lock()
	nm.tier1 = &snap
	nm.tier1Ring = appendRing(nm.tier1Ring, snap, tier1RingCap)
	nm.mu.Unlock()
	return nil
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

// ---------------------------------------------------------------------------
// Tier 2 collection
// ---------------------------------------------------------------------------

func (nm *nodeMonitor) collectTier2(ctx context.Context) error {
	nm.mu.RLock()
	conn := nm.conn
	nm.mu.RUnlock()
	if conn == nil {
		return fmt.Errorf("no connection")
	}

	snap := Tier2Snapshot{
		Timestamp: time.Now(),
		NodeID:    nm.node.ID,
	}

	// Tables.
	rows, err := conn.Query(ctx, TableStatsQuery, nil, 100)
	if err != nil {
		return fmt.Errorf("table stats: %w", err)
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
			return fmt.Errorf("scan table stat: %w", err)
		}
		total := t.SeqScan + t.IdxScan
		if total > 0 {
			t.IndexUsageRatio = float64(t.IdxScan) / float64(total)
		}
		snap.Tables = append(snap.Tables, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Indexes.
	rows, err = conn.Query(ctx, IndexStatsQuery, nil, 50)
	if err != nil {
		return fmt.Errorf("index stats: %w", err)
	}
	for rows.Next() {
		var idx IndexStat
		if err := rows.Scan(
			&idx.Schema, &idx.Table, &idx.Name,
			&idx.IdxScan, &idx.IdxTupRead, &idx.IdxTupFetch,
			&idx.SizeBytes, &idx.Size,
		); err != nil {
			rows.Close()
			return fmt.Errorf("scan index stat: %w", err)
		}
		snap.Indexes = append(snap.Indexes, idx)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Lock contention.
	rows, err = conn.Query(ctx, LockContentionQuery)
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

	nm.mu.Lock()
	nm.tier2 = &snap
	nm.tier2Ring = appendRing(nm.tier2Ring, snap, tier2RingCap)
	nm.mu.Unlock()
	return nil
}

// ---------------------------------------------------------------------------
// Tier 3 collection
// ---------------------------------------------------------------------------

func (nm *nodeMonitor) collectTier3(ctx context.Context) error {
	nm.mu.RLock()
	conn := nm.conn
	nm.mu.RUnlock()
	if conn == nil {
		return fmt.Errorf("no connection")
	}

	snap := Tier3Snapshot{
		Timestamp: time.Now(),
		NodeID:    nm.node.ID,
	}

	// Relation sizes.
	rows, err := conn.Query(ctx, RelationSizesQuery, nil, 50)
	if err != nil {
		return fmt.Errorf("relation sizes: %w", err)
	}
	for rows.Next() {
		var s RelationSize
		if err := rows.Scan(
			&s.Schema, &s.Name,
			&s.TotalBytes, &s.TotalSize,
			&s.DataBytes, &s.IndexBytes, &s.ToastBytes,
		); err != nil {
			rows.Close()
			return fmt.Errorf("scan size: %w", err)
		}
		snap.Sizes = append(snap.Sizes, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Bloat estimates.
	rows, err = conn.Query(ctx, BloatEstimateQuery, nil, 50)
	if err != nil {
		nm.logger.Debug().Err(err).Msg("bloat estimation skipped")
	} else {
		for rows.Next() {
			var b BloatEstimate
			if err := rows.Scan(&b.Schema, &b.Name, &b.TotalBytes, &b.BloatBytes); err != nil {
				rows.Close()
				break
			}
			if b.TotalBytes > 0 {
				b.BloatRatio = float64(b.BloatBytes) / float64(b.TotalBytes)
			}
			snap.Bloat = append(snap.Bloat, b)
		}
		rows.Close()
	}

	// Top queries (pg_stat_statements may not be installed).
	rows, err = conn.Query(ctx, TopQueriesQuery, 20)
	if err != nil {
		nm.logger.Debug().Err(err).Msg("pg_stat_statements not available")
	} else {
		for rows.Next() {
			var q TopQuery
			if err := rows.Scan(
				&q.QueryID, &q.Query, &q.Calls,
				&q.TotalTimeMs, &q.MeanTimeMs, &q.Rows,
				&q.SharedBlksHit, &q.SharedBlksRead,
			); err != nil {
				rows.Close()
				break
			}
			total := q.SharedBlksHit + q.SharedBlksRead
			if total > 0 {
				q.HitRatio = float64(q.SharedBlksHit) / float64(total)
			}
			snap.TopQueries = append(snap.TopQueries, q)
		}
		rows.Close()
	}

	nm.mu.Lock()
	nm.tier3 = &snap
	nm.tier3Ring = appendRing(nm.tier3Ring, snap, tier3RingCap)
	nm.mu.Unlock()
	return nil
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

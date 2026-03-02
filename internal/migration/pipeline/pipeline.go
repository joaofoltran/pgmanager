package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmanager/internal/migration/bidi"
	"github.com/jfoltran/pgmanager/internal/config"
	"github.com/jfoltran/pgmanager/internal/metrics"
	"github.com/jfoltran/pgmanager/internal/migration/filter"
	"github.com/jfoltran/pgmanager/internal/migration/replay"
	"github.com/jfoltran/pgmanager/internal/migration/schema"
	"github.com/jfoltran/pgmanager/internal/migration/sentinel"
	"github.com/jfoltran/pgmanager/internal/migration/snapshot"
	"github.com/jfoltran/pgmanager/internal/migration/stream"
)

// Progress reports the current state of the pipeline.
type Progress struct {
	Phase        string
	LastLSN      pglogrepl.LSN
	TablesTotal  int
	TablesCopied int
	StartedAt    time.Time
}

// Pipeline orchestrates the full migration lifecycle: wires
// decoder → filter → applier, manages snapshot copies, and coordinates switchover.
type Pipeline struct {
	cfg    *config.Config
	logger zerolog.Logger

	// Connections
	replConn *pgconn.PgConn
	srcPool  *pgxpool.Pool
	dstPool  *pgxpool.Pool

	// Components
	decoder     *stream.Decoder
	applier     *replay.Applier
	copier      *snapshot.Copier
	schemaMgr   *schema.Manager
	coordinator *sentinel.Coordinator
	bidiFilter  *bidi.Filter

	// Metrics
	Metrics   *metrics.Collector
	persister *metrics.StatePersister

	// Channel that carries messages through the pipeline.
	messages chan stream.Message

	mu       sync.Mutex
	progress Progress

	cancel context.CancelFunc
}

// New creates a new Pipeline from the given configuration.
func New(cfg *config.Config, logger zerolog.Logger) *Pipeline {
	mc := metrics.NewCollector(logger)
	return &Pipeline{
		cfg:      cfg,
		logger:   logger.With().Str("component", "pipeline").Logger(),
		messages: make(chan stream.Message, 256),
		progress: Progress{Phase: "idle"},
		Metrics:  mc,
	}
}

// SetLogger replaces the pipeline logger. Use this to redirect log output
// (e.g. into the TUI metrics collector instead of stderr).
func (p *Pipeline) SetLogger(logger zerolog.Logger) {
	p.logger = logger.With().Str("component", "pipeline").Logger()
}

// connect establishes all required database connections.
func (p *Pipeline) connect(ctx context.Context) error {
	connTimeout := 30 * time.Second

	p.logger.Info().Str("host", p.cfg.Source.Host).Uint16("port", p.cfg.Source.Port).Str("db", p.cfg.Source.DBName).Msg("connecting to source (replication)")
	replCtx, replCancel := context.WithTimeout(ctx, connTimeout)
	replConn, err := pgconn.Connect(replCtx, p.cfg.Source.ReplicationDSN())
	replCancel()
	if err != nil {
		return fmt.Errorf("replication connection to %s:%d/%s: %w", p.cfg.Source.Host, p.cfg.Source.Port, p.cfg.Source.DBName, err)
	}
	p.replConn = replConn

	p.logger.Info().Str("host", p.cfg.Source.Host).Uint16("port", p.cfg.Source.Port).Str("db", p.cfg.Source.DBName).Msg("connecting to source (pool)")
	srcPool, err := pgxpool.New(ctx, p.cfg.Source.DSN())
	if err != nil {
		return fmt.Errorf("source pool: %w", err)
	}
	pingCtx, pingCancel := context.WithTimeout(ctx, connTimeout)
	if err := srcPool.Ping(pingCtx); err != nil {
		pingCancel()
		srcPool.Close()
		return fmt.Errorf("source pool ping %s:%d/%s: %w", p.cfg.Source.Host, p.cfg.Source.Port, p.cfg.Source.DBName, err)
	}
	pingCancel()
	p.srcPool = srcPool

	p.logger.Info().Str("host", p.cfg.Dest.Host).Uint16("port", p.cfg.Dest.Port).Str("db", p.cfg.Dest.DBName).Msg("connecting to destination (pool)")
	dstCfg, err := pgxpool.ParseConfig(p.cfg.Dest.DSN())
	if err != nil {
		return fmt.Errorf("parse dest pool config: %w", err)
	}
	dstCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET session_replication_role = 'replica'")
		return err
	}
	dstPool, err := pgxpool.NewWithConfig(ctx, dstCfg)
	if err != nil {
		return fmt.Errorf("dest pool: %w", err)
	}
	pingCtx2, pingCancel2 := context.WithTimeout(ctx, connTimeout)
	if err := dstPool.Ping(pingCtx2); err != nil {
		pingCancel2()
		dstPool.Close()
		return fmt.Errorf("dest pool ping %s:%d/%s: %w", p.cfg.Dest.Host, p.cfg.Dest.Port, p.cfg.Dest.DBName, err)
	}
	pingCancel2()
	p.dstPool = dstPool

	p.logger.Info().Msg("all connections established")
	return nil
}

// initComponents creates all pipeline components.
func (p *Pipeline) initComponents() {
	p.decoder = stream.NewDecoder(p.replConn, p.cfg.Replication.SlotName, p.cfg.Replication.Publication, p.logger)
	p.applier = replay.NewApplier(p.dstPool, p.logger)
	p.copier = snapshot.NewCopier(p.srcPool, p.dstPool, p.cfg.Snapshot.Workers, p.logger)
	lastReported := &sync.Map{}
	p.copier.SetProgressFunc(func(table snapshot.TableInfo, event string, rowsCopied int64) {
		key := table.Schema + "." + table.Name
		switch event {
		case "start":
			lastReported.Store(key, int64(0))
			p.Metrics.TableStarted(table.Schema, table.Name)
		case "retry":
			p.Metrics.RecordEvent("copy_retry", fmt.Sprintf("retrying COPY for %s (attempt %d)", key, rowsCopied), map[string]string{
				"table": key,
				"attempt": fmt.Sprintf("%d", rowsCopied),
			})
		case "progress":
			var delta int64
			if prev, ok := lastReported.Load(key); ok {
				delta = rowsCopied - prev.(int64)
			} else {
				delta = rowsCopied
			}
			lastReported.Store(key, rowsCopied)
			p.Metrics.UpdateTableProgress(table.Schema, table.Name, rowsCopied, 0)
			p.Metrics.RecordApplied(0, delta, 0)
		case "done":
			var delta int64
			if prev, ok := lastReported.Load(key); ok {
				delta = rowsCopied - prev.(int64)
			}
			if delta > 0 {
				p.Metrics.RecordApplied(0, delta, 0)
			}
			p.Metrics.TableDone(table.Schema, table.Name, rowsCopied)
			p.mu.Lock()
			p.progress.TablesCopied++
			p.mu.Unlock()
		}
	})
	p.schemaMgr = schema.NewManager(p.srcPool, p.dstPool, p.logger)
	if p.cfg.Schema.MaxErrors > 0 {
		p.schemaMgr.MaxErrors = p.cfg.Schema.MaxErrors
	}
	if len(p.cfg.Schema.ExcludeExtensions) > 0 {
		p.schemaMgr.ExcludeExtensions = p.cfg.Schema.ExcludeExtensions
	}
	p.coordinator = sentinel.NewCoordinator(p.messages, p.logger)

	filterCfg := filter.Config{
		IncludeSchemas: p.cfg.Filter.IncludeSchemas,
		ExcludeSchemas: p.cfg.Filter.ExcludeSchemas,
		IncludeTables:  p.cfg.Filter.IncludeTables,
		ExcludeTables:  p.cfg.Filter.ExcludeTables,
	}
	if !filterCfg.IsEmpty() {
		f := filter.New(filterCfg)
		p.applier.SetFilter(f.Allow)
		p.copier.SetFilter(f.Allow)
	}

	if p.cfg.Replication.OriginID != "" {
		p.bidiFilter = bidi.NewFilter(p.cfg.Replication.OriginID, p.logger)
	}
}

// startPersister initializes state file persistence.
func (p *Pipeline) startPersister() {
	persister, err := metrics.NewStatePersister(p.Metrics, p.logger)
	if err != nil {
		p.logger.Warn().Err(err).Msg("failed to start state persister")
		return
	}
	p.persister = persister
	p.persister.Start()
}

// RunClone performs schema copy + full data copy (no CDC follow).
func (p *Pipeline) RunClone(ctx context.Context) error {
	ctx, p.cancel = context.WithCancel(ctx)
	p.setPhase("connecting")
	p.startPersister()

	if err := p.connect(ctx); err != nil {
		return err
	}
	p.initComponents()

	if err := p.ensurePublication(ctx); err != nil {
		return err
	}

	// Dump and apply schema.
	p.setPhase("schema")
	p.logger.Info().Msg("dumping schema from source")
	ddl, err := p.schemaMgr.DumpSchema(ctx, p.cfg.Source.DSN())
	if err != nil {
		return fmt.Errorf("dump schema: %w", err)
	}
	p.logger.Info().Msg("applying schema to destination")
	schemaResult, err := p.schemaMgr.ApplySchema(ctx, ddl)
	p.Metrics.SetSchemaStats(schemaResult.Total, schemaResult.Applied, schemaResult.Skipped, schemaResult.ErrorsTolerated, convertSchemaDetails(schemaResult.SkippedDetails), convertSchemaDetails(schemaResult.ErroredDetails))
	if err != nil {
		p.Metrics.RecordErrorDetail(err, "schema", false)
		return fmt.Errorf("apply schema: %w", err)
	}
	p.Metrics.RecordEvent("schema_applied", fmt.Sprintf("applied %d/%d statements (%d skipped, %d errors tolerated)", schemaResult.Applied, schemaResult.Total, schemaResult.Skipped, schemaResult.ErrorsTolerated), nil)

	// Create replication slot to get consistent snapshot.
	// The snapshot stays valid until StartStreaming is called.
	p.logger.Info().Str("slot", p.cfg.Replication.SlotName).Msg("creating replication slot")
	snapshotName, err := p.decoder.CreateSlot(ctx, 0)
	if err != nil {
		return fmt.Errorf("create slot: %w", err)
	}
	p.logger.Info().Str("snapshot", snapshotName).Msg("replication slot created")

	// Parallel COPY using the snapshot (must complete before StartStreaming).
	p.setPhase("copy")
	tables, err := p.copier.ListTables(ctx)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}
	p.logger.Info().Int("tables", len(tables)).Int("workers", p.cfg.Snapshot.Workers).Msg("starting parallel COPY")

	p.mu.Lock()
	p.progress.TablesTotal = len(tables)
	p.mu.Unlock()

	p.initTableMetrics(tables)

	results := p.copier.CopyAll(ctx, tables, snapshotName)
	for _, r := range results {
		if r.Retries > 0 {
			p.Metrics.RecordEvent("copy_completed_with_retries", fmt.Sprintf("%s completed after %d retries", r.Table.QualifiedName(), r.Retries), map[string]string{
				"table": r.Table.QualifiedName(), "retries": fmt.Sprintf("%d", r.Retries),
			})
		}
		if r.Err != nil {
			p.Metrics.RecordErrorDetail(r.Err, "copy", false)
			return fmt.Errorf("copy %s: %w", r.Table.QualifiedName(), r.Err)
		}
		p.Metrics.RecordApplied(0, 0, r.Table.SizeBytes)
	}

	// Start and immediately drain the replication stream (clone-only, no CDC).
	msgCh, err := p.decoder.StartStreaming(ctx)
	if err != nil {
		return fmt.Errorf("start streaming: %w", err)
	}
	go func() {
		for range msgCh {
		}
	}()

	p.setPhase("done")
	p.logger.Info().Msg("clone completed")
	return nil
}

// RunCloneAndFollow performs clone then transitions to CDC streaming.
func (p *Pipeline) RunCloneAndFollow(ctx context.Context) error {
	ctx, p.cancel = context.WithCancel(ctx)
	p.setPhase("connecting")
	p.startPersister()

	if err := p.connect(ctx); err != nil {
		return err
	}
	p.initComponents()

	if err := p.ensurePublication(ctx); err != nil {
		return err
	}

	// Schema.
	p.setPhase("schema")
	p.logger.Info().Msg("dumping schema from source")
	ddl, err := p.schemaMgr.DumpSchema(ctx, p.cfg.Source.DSN())
	if err != nil {
		return fmt.Errorf("dump schema: %w", err)
	}
	p.logger.Info().Msg("applying schema to destination")
	schemaResult, err := p.schemaMgr.ApplySchema(ctx, ddl)
	p.Metrics.SetSchemaStats(schemaResult.Total, schemaResult.Applied, schemaResult.Skipped, schemaResult.ErrorsTolerated, convertSchemaDetails(schemaResult.SkippedDetails), convertSchemaDetails(schemaResult.ErroredDetails))
	if err != nil {
		p.Metrics.RecordErrorDetail(err, "schema", false)
		return fmt.Errorf("apply schema: %w", err)
	}
	p.Metrics.RecordEvent("schema_applied", fmt.Sprintf("applied %d/%d statements (%d skipped, %d errors tolerated)", schemaResult.Applied, schemaResult.Total, schemaResult.Skipped, schemaResult.ErrorsTolerated), nil)

	// Create replication slot to get consistent snapshot.
	p.logger.Info().Str("slot", p.cfg.Replication.SlotName).Msg("creating replication slot")
	snapshotName, err := p.decoder.CreateSlot(ctx, 0)
	if err != nil {
		return fmt.Errorf("create slot: %w", err)
	}
	p.logger.Info().Str("snapshot", snapshotName).Msg("replication slot created")

	// Parallel COPY using the snapshot (must complete before StartStreaming).
	p.setPhase("copy")
	tables, err := p.copier.ListTables(ctx)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}
	p.logger.Info().Int("tables", len(tables)).Int("workers", p.cfg.Snapshot.Workers).Msg("starting parallel COPY")

	p.mu.Lock()
	p.progress.TablesTotal = len(tables)
	p.mu.Unlock()

	p.initTableMetrics(tables)

	results := p.copier.CopyAll(ctx, tables, snapshotName)
	for _, r := range results {
		if r.Retries > 0 {
			p.Metrics.RecordEvent("copy_completed_with_retries", fmt.Sprintf("%s completed after %d retries", r.Table.QualifiedName(), r.Retries), map[string]string{
				"table": r.Table.QualifiedName(), "retries": fmt.Sprintf("%d", r.Retries),
			})
		}
		if r.Err != nil {
			p.Metrics.RecordErrorDetail(r.Err, "copy", false)
			return fmt.Errorf("copy %s: %w", r.Table.QualifiedName(), r.Err)
		}
		p.Metrics.RecordApplied(0, 0, r.Table.SizeBytes)
	}

	// COPY complete — now start streaming. This invalidates the snapshot
	// but we no longer need it. WAL accumulated since the slot was created
	// will be delivered through the channel.
	msgCh, err := p.decoder.StartStreaming(ctx)
	if err != nil {
		return fmt.Errorf("start streaming: %w", err)
	}

	// Transition to CDC.
	p.setPhase("streaming")
	p.logger.Info().Msg("COPY complete, switching to CDC streaming")

	for _, t := range tables {
		p.Metrics.TableStreaming(t.Schema, t.Name)
	}

	var applierCh <-chan stream.Message = msgCh
	if p.bidiFilter != nil {
		applierCh = p.bidiFilter.Run(ctx, msgCh)
	}

	return p.startApplier(ctx, applierCh)
}

// SlotInfo holds information about an existing replication slot.
type SlotInfo struct {
	SlotName      string
	ConfirmedLSN  pglogrepl.LSN
	RestartLSN    pglogrepl.LSN
	Active        bool
}

// checkSlot queries the source for the replication slot and returns its info.
func (p *Pipeline) checkSlot(ctx context.Context) (*SlotInfo, error) {
	var slotName string
	var confirmedFlush, restart *string
	var active bool

	err := p.srcPool.QueryRow(ctx, `
		SELECT slot_name, confirmed_flush_lsn::text, restart_lsn::text, active
		FROM pg_replication_slots
		WHERE slot_name = $1`, p.cfg.Replication.SlotName).Scan(&slotName, &confirmedFlush, &restart, &active)
	if err != nil {
		return nil, fmt.Errorf("slot %q not found: %w", p.cfg.Replication.SlotName, err)
	}

	info := &SlotInfo{SlotName: slotName, Active: active}
	if confirmedFlush != nil {
		lsn, err := pglogrepl.ParseLSN(*confirmedFlush)
		if err != nil {
			return nil, fmt.Errorf("parse confirmed_flush_lsn: %w", err)
		}
		info.ConfirmedLSN = lsn
	}
	if restart != nil {
		lsn, err := pglogrepl.ParseLSN(*restart)
		if err != nil {
			return nil, fmt.Errorf("parse restart_lsn: %w", err)
		}
		info.RestartLSN = lsn
	}
	return info, nil
}

// RunResumeCloneAndFollow resumes a previously interrupted clone:
// 1. Verifies the replication slot still exists (WAL is preserved)
// 2. Compares source vs dest row counts to find incomplete tables
// 3. Truncates and re-COPYs only incomplete tables (without snapshot)
// 4. Starts CDC streaming from the slot's LSN
func (p *Pipeline) RunResumeCloneAndFollow(ctx context.Context) error {
	ctx, p.cancel = context.WithCancel(ctx)
	p.setPhase("connecting")
	p.startPersister()

	if err := p.connect(ctx); err != nil {
		return err
	}
	p.initComponents()

	if err := p.ensurePublication(ctx); err != nil {
		return err
	}

	// Ensure schema exists on destination (idempotent).
	p.setPhase("schema")
	p.logger.Info().Msg("dumping schema from source")
	ddl, err := p.schemaMgr.DumpSchema(ctx, p.cfg.Source.DSN())
	if err != nil {
		return fmt.Errorf("dump schema: %w", err)
	}
	p.logger.Info().Msg("applying schema to destination")
	schemaResult, err := p.schemaMgr.ApplySchema(ctx, ddl)
	p.Metrics.SetSchemaStats(schemaResult.Total, schemaResult.Applied, schemaResult.Skipped, schemaResult.ErrorsTolerated, convertSchemaDetails(schemaResult.SkippedDetails), convertSchemaDetails(schemaResult.ErroredDetails))
	if err != nil {
		p.Metrics.RecordErrorDetail(err, "schema", false)
		return fmt.Errorf("apply schema: %w", err)
	}
	p.Metrics.RecordEvent("schema_applied", fmt.Sprintf("applied %d/%d statements (%d skipped, %d errors tolerated)", schemaResult.Applied, schemaResult.Total, schemaResult.Skipped, schemaResult.ErrorsTolerated), nil)

	// Check that the replication slot survived.
	p.setPhase("resuming")
	slotInfo, err := p.checkSlot(ctx)
	if err != nil {
		return fmt.Errorf("cannot resume: %w — run a full clone instead", err)
	}
	if slotInfo.Active {
		return fmt.Errorf("cannot resume: slot %q is active (another process is using it)", slotInfo.SlotName)
	}

	startLSN := slotInfo.RestartLSN
	if slotInfo.ConfirmedLSN > startLSN {
		startLSN = slotInfo.ConfirmedLSN
	}
	p.logger.Info().
		Stringer("restart_lsn", slotInfo.RestartLSN).
		Stringer("confirmed_lsn", slotInfo.ConfirmedLSN).
		Stringer("start_lsn", startLSN).
		Msg("replication slot found, WAL is preserved")

	// List source tables and check dest completeness.
	srcTables, err := p.copier.ListTables(ctx)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}

	var incompleteTables []snapshot.TableInfo
	var completeTables []snapshot.TableInfo
	for _, t := range srcTables {
		destCount, err := p.copier.DestRowCount(ctx, t.Schema, t.Name)
		if err != nil {
			return fmt.Errorf("check dest row count for %s: %w", t.QualifiedName(), err)
		}
		if destCount < t.RowCount {
			p.logger.Info().
				Str("table", t.QualifiedName()).
				Int64("source_rows", t.RowCount).
				Int64("dest_rows", destCount).
				Msg("incomplete table — will truncate and re-copy")
			incompleteTables = append(incompleteTables, t)
		} else {
			p.logger.Info().
				Str("table", t.QualifiedName()).
				Int64("rows", destCount).
				Msg("table complete — skipping")
			completeTables = append(completeTables, t)
		}
	}

	p.mu.Lock()
	p.progress.TablesTotal = len(srcTables)
	p.progress.TablesCopied = len(completeTables)
	p.mu.Unlock()

	p.initTableMetrics(srcTables)
	for _, t := range completeTables {
		p.Metrics.TableDone(t.Schema, t.Name, t.RowCount)
	}

	if len(incompleteTables) > 0 {
		p.setPhase("copy")
		p.logger.Info().Int("tables", len(incompleteTables)).Msg("truncating and re-copying incomplete tables")

		for _, t := range incompleteTables {
			p.logger.Info().Str("table", t.QualifiedName()).Msg("truncating")
			if err := p.copier.TruncateTable(ctx, t.Schema, t.Name); err != nil {
				return fmt.Errorf("truncate %s: %w", t.QualifiedName(), err)
			}
		}

		results := p.copier.CopyAll(ctx, incompleteTables, "")
		for _, r := range results {
			if r.Retries > 0 {
				p.Metrics.RecordEvent("copy_completed_with_retries", fmt.Sprintf("%s completed after %d retries", r.Table.QualifiedName(), r.Retries), map[string]string{
					"table": r.Table.QualifiedName(), "retries": fmt.Sprintf("%d", r.Retries),
				})
			}
			if r.Err != nil {
				p.Metrics.RecordErrorDetail(r.Err, "copy", false)
				return fmt.Errorf("copy %s: %w", r.Table.QualifiedName(), r.Err)
			}
			p.Metrics.RecordApplied(0, 0, r.Table.SizeBytes)
		}
	} else {
		p.logger.Info().Msg("all tables complete — skipping COPY phase")
	}

	// Start streaming from the slot's LSN. The decoder won't create a new slot.
	p.decoder = stream.NewDecoder(p.replConn, p.cfg.Replication.SlotName, p.cfg.Replication.Publication, p.logger)
	p.decoder.CreateSlot(ctx, startLSN) //nolint:errcheck
	msgCh, err := p.decoder.StartStreaming(ctx)
	if err != nil {
		return fmt.Errorf("start streaming: %w", err)
	}

	p.setPhase("streaming")
	p.logger.Info().Msg("resumed CDC streaming")

	for _, t := range srcTables {
		p.Metrics.TableStreaming(t.Schema, t.Name)
	}

	var applierCh <-chan stream.Message = msgCh
	if p.bidiFilter != nil {
		applierCh = p.bidiFilter.Run(ctx, msgCh)
	}

	return p.startApplier(ctx, applierCh)
}

// RunFollow starts CDC streaming from the given LSN (slot must already exist).
func (p *Pipeline) RunFollow(ctx context.Context, startLSN pglogrepl.LSN) error {
	ctx, p.cancel = context.WithCancel(ctx)
	p.setPhase("connecting")
	p.startPersister()

	if err := p.connect(ctx); err != nil {
		return err
	}
	p.initComponents()

	if err := p.ensurePublication(ctx); err != nil {
		return err
	}

	msgCh, _, err := p.decoder.Start(ctx, startLSN)
	if err != nil {
		return fmt.Errorf("start decoder: %w", err)
	}

	p.setPhase("streaming")

	var applierCh <-chan stream.Message = msgCh
	if p.bidiFilter != nil {
		applierCh = p.bidiFilter.Run(ctx, msgCh)
	}

	return p.startApplier(ctx, applierCh)
}

// RunSwitchover injects a sentinel message and waits for it to be confirmed,
// signaling that the destination is fully caught up.
func (p *Pipeline) RunSwitchover(ctx context.Context, timeout time.Duration) error {
	if p.coordinator == nil {
		return fmt.Errorf("pipeline not initialized")
	}

	p.setPhase("switchover")
	currentLSN := p.applier.LastLSN()

	id, err := p.coordinator.Initiate(ctx, currentLSN)
	if err != nil {
		return fmt.Errorf("initiate sentinel: %w", err)
	}

	if err := p.coordinator.WaitForConfirmation(id, timeout); err != nil {
		return fmt.Errorf("switchover: %w", err)
	}

	p.setPhase("switchover-complete")
	p.logger.Info().Msg("switchover confirmed — destination is caught up")
	return nil
}

// SetupReverseReplication prepares the destination to act as a new replication
// source after switchover. It creates a publication and logical replication slot
// on the destination, and drops the forward slot on the source.
// Returns the slot name and the LSN to start streaming from.
func (p *Pipeline) SetupReverseReplication(ctx context.Context) (slotName string, startLSN pglogrepl.LSN, err error) {
	reverseSlot := p.cfg.Replication.SlotName + "_reverse"
	reversePub := p.cfg.Replication.Publication + "_reverse"

	var walLevel string
	if err := p.dstPool.QueryRow(ctx, "SHOW wal_level").Scan(&walLevel); err != nil {
		return "", 0, fmt.Errorf("check destination wal_level: %w", err)
	}
	if walLevel != "logical" {
		return "", 0, fmt.Errorf("destination wal_level is %q, must be \"logical\" for reverse replication", walLevel)
	}

	// Create publication on destination (new source).
	var pubExists bool
	err = p.dstPool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_publication WHERE pubname = $1)", reversePub).Scan(&pubExists)
	if err != nil {
		return "", 0, fmt.Errorf("check reverse publication: %w", err)
	}
	if !pubExists {
		_, err = p.dstPool.Exec(ctx, fmt.Sprintf("CREATE PUBLICATION %q FOR ALL TABLES", reversePub))
		if err != nil {
			return "", 0, fmt.Errorf("create reverse publication: %w", err)
		}
		p.logger.Info().Str("publication", reversePub).Msg("created reverse publication on destination")
	}

	// Create replication slot on destination (new source).
	connTimeout := 30 * time.Second
	replCtx, replCancel := context.WithTimeout(ctx, connTimeout)
	destReplConn, err := pgconn.Connect(replCtx, p.cfg.Dest.ReplicationDSN())
	replCancel()
	if err != nil {
		return "", 0, fmt.Errorf("reverse replication connection: %w", err)
	}
	defer destReplConn.Close(ctx) //nolint:errcheck

	reverseDecoder := stream.NewDecoder(destReplConn, reverseSlot, reversePub, p.logger)
	_, err = reverseDecoder.CreateSlot(ctx, 0)
	if err != nil {
		return "", 0, fmt.Errorf("create reverse replication slot: %w", err)
	}
	reverseLSN := reverseDecoder.StartLSN()
	reverseDecoder.Close()

	p.logger.Info().
		Str("slot", reverseSlot).
		Str("publication", reversePub).
		Stringer("start_lsn", reverseLSN).
		Msg("reverse replication infrastructure ready")

	// Drop the forward slot on the source (cleanup).
	// NOTE: This may fail if the decoder is still connected. The caller
	// should close the pipeline first, then drop the slot via DropForwardSlot.
	p.logger.Info().Str("forward_slot", p.cfg.Replication.SlotName).Msg("forward slot should be dropped after pipeline close")

	return reverseSlot, reverseLSN, nil
}

// DropForwardSlot drops the forward replication slot on the source.
// Call this after Close() to ensure the slot is no longer active.
func (p *Pipeline) DropForwardSlot(ctx context.Context) error {
	dsn := p.cfg.Source.DSN()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect to source for slot drop: %w", err)
	}
	defer pool.Close()

	_, err = pool.Exec(ctx,
		"SELECT pg_drop_replication_slot(slot_name) FROM pg_replication_slots WHERE slot_name = $1 AND NOT active",
		p.cfg.Replication.SlotName)
	if err != nil {
		return fmt.Errorf("drop forward slot: %w", err)
	}
	return nil
}

// Status returns a snapshot of the current pipeline progress.
func (p *Pipeline) Status() Progress {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.progress
}

// Close shuts down all pipeline components and connections.
func (p *Pipeline) Close() {
	if p.cancel != nil {
		p.cancel()
	}
	if p.Metrics != nil {
		p.Metrics.Close()
	}
	if p.persister != nil {
		p.persister.Stop()
	}
	if p.decoder != nil {
		p.decoder.Close()
	}
	if p.applier != nil {
		p.applier.Close()
	}
	if p.replConn != nil {
		p.replConn.Close(context.Background()) //nolint:errcheck
	}
	if p.srcPool != nil {
		p.srcPool.Close()
	}
	if p.dstPool != nil {
		p.dstPool.Close()
	}
}

func (p *Pipeline) setPhase(phase string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.progress.Phase = phase
	if p.progress.StartedAt.IsZero() {
		p.progress.StartedAt = time.Now()
	}
	p.logger.Info().Str("phase", phase).Msg("phase transition")
	p.Metrics.SetPhase(phase)
}

func (p *Pipeline) initTableMetrics(tables []snapshot.TableInfo) {
	tps := make([]metrics.TableProgress, len(tables))
	for i, t := range tables {
		tps[i] = metrics.TableProgress{
			Schema:    t.Schema,
			Name:      t.Name,
			Status:    metrics.TablePending,
			RowsTotal: t.RowCount,
			SizeBytes: t.SizeBytes,
		}
	}
	p.Metrics.SetTables(tps)
}

// Config returns the pipeline configuration (for API exposure).
func (p *Pipeline) Config() *config.Config {
	return p.cfg
}

const (
	maxDecoderRetries  = 5
	initialRetryDelay  = 2 * time.Second
	maxRetryDelay      = 30 * time.Second
)

func (p *Pipeline) startApplier(ctx context.Context, ch <-chan stream.Message) error {
	merged := p.mergeMessages(ctx, ch)
	return p.runApplierWithRetry(ctx, merged)
}

func (p *Pipeline) mergeMessages(ctx context.Context, decoder <-chan stream.Message) <-chan stream.Message {
	out := make(chan stream.Message, cap(decoder))
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-decoder:
				if !ok {
					for {
						select {
						case msg := <-p.messages:
							select {
							case out <- msg:
							case <-ctx.Done():
								return
							}
						default:
							return
						}
					}
				}
				select {
				case out <- msg:
				case <-ctx.Done():
					return
				}
			case msg := <-p.messages:
				select {
				case out <- msg:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}

func (p *Pipeline) runApplierWithRetry(ctx context.Context, ch <-chan stream.Message) error {
	retries := 0
	delay := initialRetryDelay
	watermark := pglogrepl.LSN(0)

	for {
		err := p.applier.Start(ctx, ch, func(lsn pglogrepl.LSN) {
			p.decoder.ConfirmLSN(lsn)
			p.mu.Lock()
			p.progress.LastLSN = lsn
			p.mu.Unlock()
			p.Metrics.RecordApplied(lsn, 1, 0)
			p.Metrics.RecordConfirmedLSN(lsn)
		}, func(id string) {
			if p.coordinator != nil {
				p.coordinator.Confirm(id)
			}
		})
		if err != nil {
			return err
		}

		decErr := p.decoder.Err()
		if decErr == nil {
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		retries++
		if retries > maxDecoderRetries {
			return fmt.Errorf("decoder: %w (exhausted %d retries)", decErr, maxDecoderRetries)
		}

		p.mu.Lock()
		currentLSN := p.progress.LastLSN
		p.mu.Unlock()

		if currentLSN > watermark {
			watermark = currentLSN
			retries = 1
			delay = initialRetryDelay
		}

		p.logger.Warn().
			Err(decErr).
			Int("retry", retries).
			Int("max_retries", maxDecoderRetries).
			Stringer("resume_lsn", currentLSN).
			Dur("delay", delay).
			Msg("decoder failed, reconnecting")

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		delay = min(delay*2, maxRetryDelay)

		newCh, err := p.reconnectDecoder(ctx, currentLSN)
		if err != nil {
			return fmt.Errorf("reconnect decoder: %w (original: %v)", err, decErr)
		}
		ch = p.mergeMessages(ctx, newCh)
	}
}

func (p *Pipeline) reconnectDecoder(ctx context.Context, resumeLSN pglogrepl.LSN) (<-chan stream.Message, error) {
	p.decoder.Close()

	if p.replConn != nil {
		_ = p.replConn.Close(ctx)
	}

	connTimeout := 30 * time.Second
	replCtx, replCancel := context.WithTimeout(ctx, connTimeout)
	replConn, err := pgconn.Connect(replCtx, p.cfg.Source.ReplicationDSN())
	replCancel()
	if err != nil {
		return nil, fmt.Errorf("replication reconnect: %w", err)
	}
	p.replConn = replConn

	p.decoder = stream.NewDecoder(replConn, p.cfg.Replication.SlotName, p.cfg.Replication.Publication, p.logger)
	if _, err := p.decoder.CreateSlot(ctx, resumeLSN); err != nil {
		return nil, fmt.Errorf("create slot for resume: %w", err)
	}

	msgCh, err := p.decoder.StartStreaming(ctx)
	if err != nil {
		return nil, fmt.Errorf("start streaming after reconnect: %w", err)
	}

	if p.bidiFilter != nil {
		return p.bidiFilter.Run(ctx, msgCh), nil
	}
	return msgCh, nil
}

func (p *Pipeline) ensurePublication(ctx context.Context) error {
	pubName := p.cfg.Replication.Publication
	var exists bool
	err := p.srcPool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_publication WHERE pubname = $1)", pubName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check publication: %w", err)
	}
	if exists {
		p.logger.Info().Str("publication", pubName).Msg("publication already exists")
		return nil
	}
	_, err = p.srcPool.Exec(ctx,
		fmt.Sprintf("CREATE PUBLICATION %q FOR ALL TABLES", pubName))
	if err != nil {
		return fmt.Errorf("create publication: %w", err)
	}
	p.logger.Info().Str("publication", pubName).Msg("created publication")
	return nil
}

func convertSchemaDetails(in []schema.SchemaStatementDetail) []metrics.SchemaStatementDetail {
	if len(in) == 0 {
		return nil
	}
	out := make([]metrics.SchemaStatementDetail, len(in))
	for i, d := range in {
		out[i] = metrics.SchemaStatementDetail{Statement: d.Statement, Reason: d.Reason}
	}
	return out
}

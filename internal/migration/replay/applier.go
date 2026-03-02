package replay

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmanager/internal/migration/sentinel"
	"github.com/jfoltran/pgmanager/internal/migration/stream"
)

const (
	insertBatchSize = 1000
	copyThreshold   = 5
	coalesceTxLimit = 500
	coalesceMaxWait = 50 * time.Millisecond
	maxTxBytes      = 256 * 1024 * 1024
)

// Applier reads Messages from a channel and applies DML to the destination.
type Applier struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger

	mu      sync.Mutex
	lastLSN pglogrepl.LSN

	relations map[uint32]*stream.RelationMessage
	stmtCache map[string]string
	matviews  map[string]bool

	txCount   int64
	txBytes   int64
	lastLogAt time.Time

	filterFn func(namespace, table string) bool
}

// NewApplier creates an Applier that writes to the given connection pool.
func NewApplier(pool *pgxpool.Pool, logger zerolog.Logger) *Applier {
	return &Applier{
		pool:      pool,
		logger:    logger.With().Str("component", "applier").Logger(),
		relations: make(map[uint32]*stream.RelationMessage),
		stmtCache: make(map[string]string),
		matviews:  make(map[string]bool),
	}
}

// SetFilter sets a function that returns true if the given table should be applied.
// Messages for tables where filterFn returns false are silently skipped.
func (a *Applier) SetFilter(fn func(namespace, table string) bool) {
	a.filterFn = fn
}

// SetMatViews provides a set of materialized view names (schema.name) to skip during replay.
func (a *Applier) SetMatViews(views map[string]bool) {
	a.matviews = views
}

func (a *Applier) shouldSkip(namespace, table string) bool {
	key := namespace + "." + table
	if a.matviews[key] {
		return true
	}
	if a.filterFn != nil && !a.filterFn(namespace, table) {
		return true
	}
	return false
}

func estimateParamBytes(vals []any) int64 {
	var n int64
	for _, v := range vals {
		switch s := v.(type) {
		case string:
			n += int64(len(s))
		case []byte:
			n += int64(len(s))
		default:
			n += 8
		}
	}
	return n
}

// OnApplied is a callback invoked after a commit message has been applied.
type OnApplied func(lsn pglogrepl.LSN)

// OnSentinel is a callback invoked when the applier encounters a SentinelMessage.
type OnSentinel func(id string)

// insertBatch accumulates consecutive INSERT rows for the same table.
type insertBatch struct {
	namespace string
	table     string
	cols      []string
	rows      [][]any
}

func (b *insertBatch) add(m *stream.ChangeMessage) {
	if m.NewTuple == nil {
		return
	}
	if b.cols == nil {
		b.cols = make([]string, len(m.NewTuple.Columns))
		for i, c := range m.NewTuple.Columns {
			b.cols[i] = c.Name
		}
	}
	row := make([]any, len(m.NewTuple.Columns))
	for i, c := range m.NewTuple.Columns {
		row[i] = string(c.Value)
	}
	b.rows = append(b.rows, row)
}

func (b *insertBatch) matches(m *stream.ChangeMessage) bool {
	return b.namespace == m.Namespace && b.table == m.Table
}

func (b *insertBatch) len() int {
	return len(b.rows)
}

func (b *insertBatch) reset(namespace, table string) {
	b.namespace = namespace
	b.table = table
	b.cols = nil
	b.rows = b.rows[:0]
}

// Start consumes messages and applies them to the destination database.
// It coalesces multiple WAL transactions into larger destination transactions
// during catch-up for dramatically better throughput.
func (a *Applier) Start(ctx context.Context, messages <-chan stream.Message, onApplied OnApplied, onSentinel OnSentinel) error {
	var tx pgx.Tx
	var batch insertBatch
	var pendingCommits []pglogrepl.LSN
	var coalescedTx int
	var txStartTime time.Time

	commitCoalesced := func() error {
		if tx == nil {
			return nil
		}
		if err := a.flushBatch(ctx, tx, &batch); err != nil {
			_ = tx.Rollback(ctx)
			tx = nil
			pendingCommits = pendingCommits[:0]
			coalescedTx = 0
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			tx = nil
			pendingCommits = pendingCommits[:0]
			coalescedTx = 0
			return fmt.Errorf("commit tx: %w", err)
		}
		tx = nil

		a.mu.Lock()
		for _, lsn := range pendingCommits {
			a.lastLSN = lsn
			a.txCount++
		}
		totalTx := a.txCount
		a.mu.Unlock()

		if onApplied != nil {
			for _, lsn := range pendingCommits {
				onApplied(lsn)
			}
		}
		if time.Since(a.lastLogAt) >= 10*time.Second {
			a.lastLogAt = time.Now()
			lastLSN := pendingCommits[len(pendingCommits)-1]
			a.logger.Info().
				Stringer("lsn", lastLSN).
				Int64("tx_total", totalTx).
				Int("coalesced", len(pendingCommits)).
				Msg("applier progress")
		}
		pendingCommits = pendingCommits[:0]
		coalescedTx = 0
		a.txBytes = 0
		return nil
	}

	rollbackAndFail := func(err error) error {
		if tx != nil {
			_ = tx.Rollback(ctx)
			tx = nil
		}
		pendingCommits = pendingCommits[:0]
		coalescedTx = 0
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-messages:
			if !ok {
				if tx != nil {
					return commitCoalesced()
				}
				return nil
			}

			switch m := msg.(type) {
			case *stream.RelationMessage:
				if err := a.flushBatch(ctx, tx, &batch); err != nil {
					return rollbackAndFail(err)
				}
				a.relations[m.RelationID] = m

			case *stream.BeginMessage:
				if tx == nil {
					var err error
					tx, err = a.pool.Begin(ctx)
					if err != nil {
						return fmt.Errorf("begin tx: %w", err)
					}
					txStartTime = time.Now()
				}
				coalescedTx++

			case *stream.ChangeMessage:
				if tx == nil {
					a.logger.Warn().Msg("change outside transaction, skipping")
					continue
				}

				if a.shouldSkip(m.Namespace, m.Table) {
					continue
				}

				if m.Op == stream.OpInsert {
					if batch.len() > 0 && !batch.matches(m) {
						if err := a.flushBatch(ctx, tx, &batch); err != nil {
							return rollbackAndFail(err)
						}
					}
					if batch.len() == 0 {
						batch.reset(m.Namespace, m.Table)
					}
					batch.add(m)
					if batch.len() >= insertBatchSize {
						if err := a.flushBatch(ctx, tx, &batch); err != nil {
							return rollbackAndFail(err)
						}
					}
					continue
				}

				if err := a.flushBatch(ctx, tx, &batch); err != nil {
					return rollbackAndFail(err)
				}

				var applyErr error
				switch m.Op {
				case stream.OpUpdate:
					applyErr = a.applyUpdate(ctx, tx, m)
				case stream.OpDelete:
					applyErr = a.applyDelete(ctx, tx, m)
				}
				if applyErr != nil {
					return rollbackAndFail(fmt.Errorf("apply %s on %s.%s: %w", m.Op, m.Namespace, m.Table, applyErr))
				}

				if a.txBytes > maxTxBytes {
					if err := commitCoalesced(); err != nil {
						return err
					}
				}

			case *stream.CommitMessage:
				if err := a.flushBatch(ctx, tx, &batch); err != nil {
					return rollbackAndFail(err)
				}
				pendingCommits = append(pendingCommits, m.CommitLSN)

				shouldCommit := coalescedTx >= coalesceTxLimit ||
					time.Since(txStartTime) >= coalesceMaxWait ||
					len(messages) == 0

				if shouldCommit {
					if err := commitCoalesced(); err != nil {
						return err
					}
				}

			case *sentinel.SentinelMessage:
				if err := a.flushBatch(ctx, tx, &batch); err != nil {
					return rollbackAndFail(err)
				}
				if tx != nil {
					if err := commitCoalesced(); err != nil {
						return err
					}
				}
				if onSentinel != nil {
					onSentinel(m.ID)
				}
			}
		}
	}
}

func (a *Applier) flushBatch(ctx context.Context, tx pgx.Tx, batch *insertBatch) error {
	if batch.len() == 0 {
		return nil
	}
	n := batch.len()
	defer func() { batch.rows = batch.rows[:0]; batch.cols = nil }()

	if n <= copyThreshold {
		return a.flushBatchExec(ctx, tx, batch)
	}
	return a.flushBatchCopy(ctx, tx, batch)
}

func (a *Applier) flushBatchExec(ctx context.Context, tx pgx.Tx, batch *insertBatch) error {
	tbl := qualifiedName(batch.namespace, batch.table)
	ncols := len(batch.cols)

	quotedCols := make([]string, ncols)
	for i, c := range batch.cols {
		quotedCols[i] = quoteIdent(c)
	}
	colList := strings.Join(quotedCols, ", ")

	var sb strings.Builder
	sb.WriteString("INSERT INTO ")
	sb.WriteString(tbl)
	sb.WriteString(" (")
	sb.WriteString(colList)
	sb.WriteString(") VALUES ")

	vals := make([]any, 0, len(batch.rows)*ncols)
	for i, row := range batch.rows {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteByte('(')
		for j := range row {
			if j > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "$%d", len(vals)+1)
			vals = append(vals, row[j])
		}
		sb.WriteByte(')')
	}

	_, err := tx.Exec(ctx, sb.String(), vals...)
	if err != nil {
		return fmt.Errorf("insert into %s.%s (%d rows): %w", batch.namespace, batch.table, len(batch.rows), err)
	}
	a.txBytes += estimateParamBytes(vals)
	return nil
}

func (a *Applier) flushBatchCopy(ctx context.Context, tx pgx.Tx, batch *insertBatch) error {
	copyRows := make([][]any, len(batch.rows))
	copy(copyRows, batch.rows)

	_, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{batch.namespace, batch.table},
		batch.cols,
		pgx.CopyFromRows(copyRows),
	)
	if err != nil {
		return fmt.Errorf("copy into %s.%s (%d rows): %w", batch.namespace, batch.table, len(copyRows), err)
	}
	for _, row := range copyRows {
		a.txBytes += estimateParamBytes(row)
	}
	return nil
}

func (a *Applier) applyUpdate(ctx context.Context, tx pgx.Tx, m *stream.ChangeMessage) error {
	if m.NewTuple == nil {
		return nil
	}

	rel := a.relations[m.RelationID]
	setClauses, setVals := a.buildSetClauses(m.NewTuple)
	whereClauses, whereVals := a.buildWhereClauses(m, rel, len(setVals))

	query := a.cachedStmt("U", m.Namespace, m.Table, len(setVals), len(whereVals), func() string {
		return fmt.Sprintf("UPDATE %s SET %s WHERE %s",
			qualifiedName(m.Namespace, m.Table),
			strings.Join(setClauses, ", "),
			strings.Join(whereClauses, " AND "))
	})

	allVals := make([]any, 0, len(setVals)+len(whereVals))
	allVals = append(allVals, setVals...)
	allVals = append(allVals, whereVals...)
	_, err := tx.Exec(ctx, query, allVals...)
	return err
}

func (a *Applier) applyDelete(ctx context.Context, tx pgx.Tx, m *stream.ChangeMessage) error {
	rel := a.relations[m.RelationID]
	whereClauses, whereVals := a.buildWhereClauses(m, rel, 0)

	query := a.cachedStmt("D", m.Namespace, m.Table, 0, len(whereVals), func() string {
		return fmt.Sprintf("DELETE FROM %s WHERE %s",
			qualifiedName(m.Namespace, m.Table),
			strings.Join(whereClauses, " AND "))
	})

	_, err := tx.Exec(ctx, query, whereVals...)
	return err
}

func (a *Applier) cachedStmt(op, namespace, table string, nSet, nWhere int, build func() string) string {
	key := fmt.Sprintf("%s:%s.%s:%d:%d", op, namespace, table, nSet, nWhere)
	if q, ok := a.stmtCache[key]; ok {
		return q
	}
	q := build()
	a.stmtCache[key] = q
	return q
}

func (a *Applier) buildSetClauses(tuple *stream.TupleData) (clauses []string, vals []any) {
	for i, c := range tuple.Columns {
		clauses = append(clauses, fmt.Sprintf("%s = $%d", quoteIdent(c.Name), i+1))
		vals = append(vals, string(c.Value))
	}
	return
}

func (a *Applier) buildWhereClauses(m *stream.ChangeMessage, _ *stream.RelationMessage, offset int) (clauses []string, vals []any) {
	source := m.OldTuple
	if source == nil {
		source = m.NewTuple
	}
	if source == nil {
		return
	}
	for i, c := range source.Columns {
		clauses = append(clauses, fmt.Sprintf("%s = $%d", quoteIdent(c.Name), offset+i+1))
		vals = append(vals, string(c.Value))
	}
	return
}

// LastLSN returns the LSN of the most recently committed transaction.
func (a *Applier) LastLSN() pglogrepl.LSN {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastLSN
}

// Close releases resources held by the Applier.
func (a *Applier) Close() {
}

func qualifiedName(namespace, table string) string {
	if namespace == "" || namespace == "public" {
		return quoteIdent(table)
	}
	return quoteIdent(namespace) + "." + quoteIdent(table)
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

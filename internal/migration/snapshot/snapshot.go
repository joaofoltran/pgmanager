package snapshot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// TableInfo describes a table eligible for COPY.
type TableInfo struct {
	Schema    string
	Name      string
	RowCount  int64
	SizeBytes int64
}

// QualifiedName returns schema.table.
func (t TableInfo) QualifiedName() string {
	if t.Schema == "" || t.Schema == "public" {
		return t.Name
	}
	return t.Schema + "." + t.Name
}

const (
	copyRetryBudget = 30 * time.Minute
	copyRetryBase   = 2 * time.Second
	copyRetryCap    = 5 * time.Minute
)

// CopyResult holds the outcome of copying a single table.
type CopyResult struct {
	Table      TableInfo
	RowsCopied int64
	Retries    int
	Err        error
}

// ProgressFunc is called to report COPY progress for a table.
// event is "start", "progress", or "done".
type ProgressFunc func(table TableInfo, event string, rowsCopied int64)

// Copier performs parallel COPY of tables using a consistent snapshot.
type Copier struct {
	source   *pgxpool.Pool
	dest     *pgxpool.Pool
	logger   zerolog.Logger
	progress ProgressFunc
	filterFn func(namespace, table string) bool

	workers int
}

// NewCopier creates a Copier with the given source/dest pools and worker count.
func NewCopier(source, dest *pgxpool.Pool, workers int, logger zerolog.Logger) *Copier {
	return &Copier{
		source:  source,
		dest:    dest,
		logger:  logger.With().Str("component", "snapshot").Logger(),
		workers: workers,
	}
}

// SetProgressFunc sets a callback for COPY progress reporting.
func (c *Copier) SetProgressFunc(fn ProgressFunc) {
	c.progress = fn
}

// SetFilter sets a function that returns true if the given table should be copied.
// Tables where filterFn returns false are excluded from ListTables results.
func (c *Copier) SetFilter(fn func(namespace, table string) bool) {
	c.filterFn = fn
}

// ListTables returns all user tables from the source database.
func (c *Copier) ListTables(ctx context.Context) ([]TableInfo, error) {
	rows, err := c.source.Query(ctx, `
		SELECT s.schemaname, s.relname,
			GREATEST(COALESCE(s.n_live_tup, 0), COALESCE(c.reltuples::bigint, 0)),
			COALESCE(pg_table_size(quote_ident(s.schemaname) || '.' || quote_ident(s.relname)), 0)
		FROM pg_stat_user_tables s
		JOIN pg_class c ON c.relname = s.relname
			AND c.relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = s.schemaname)
		ORDER BY pg_table_size(quote_ident(s.schemaname) || '.' || quote_ident(s.relname)) DESC`)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.Schema, &t.Name, &t.RowCount, &t.SizeBytes); err != nil {
			return nil, fmt.Errorf("scan table info: %w", err)
		}
		if c.filterFn != nil && !c.filterFn(t.Schema, t.Name) {
			c.logger.Debug().Str("table", t.QualifiedName()).Msg("filtered out by table filter")
			continue
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

// DestRowCount returns the exact row count for a table on the destination.
func (c *Copier) DestRowCount(ctx context.Context, schema, name string) (int64, error) {
	qn := quoteQualifiedName(schema, name)
	var count int64
	err := c.dest.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", qn)).Scan(&count)
	return count, err
}

// TruncateTable truncates a table on the destination.
func (c *Copier) TruncateTable(ctx context.Context, schema, name string) error {
	qn := quoteQualifiedName(schema, name)
	_, err := c.dest.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s", qn))
	return err
}

// DestHasData returns true if any of the given tables have rows on the destination.
func (c *Copier) DestHasData(ctx context.Context, tables []TableInfo) (bool, error) {
	for _, t := range tables {
		qn := quoteQualifiedName(t.Schema, t.Name)
		var exists bool
		err := c.dest.QueryRow(ctx, fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s LIMIT 1)", qn)).Scan(&exists)
		if err != nil {
			return false, fmt.Errorf("check %s: %w", qn, err)
		}
		if exists {
			return true, nil
		}
	}
	return false, nil
}

// CopyAll copies all given tables in parallel using the provided snapshot name
// for read consistency. It returns results for each table.
func (c *Copier) CopyAll(ctx context.Context, tables []TableInfo, snapshotName string) []CopyResult {
	work := make(chan TableInfo, len(tables))
	for _, t := range tables {
		work <- t
	}
	close(work)

	var (
		mu      sync.Mutex
		results []CopyResult
		wg      sync.WaitGroup
	)

	for i := 0; i < c.workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for t := range work {
				result := c.copyTable(ctx, t, snapshotName, workerID)
				mu.Lock()
				results = append(results, result)
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	return results
}

func (c *Copier) reportProgress(table TableInfo, event string, rowsCopied int64) {
	if c.progress != nil {
		c.progress(table, event, rowsCopied)
	}
}

const progressReportInterval = 500 * time.Millisecond

func (c *Copier) copyTable(ctx context.Context, table TableInfo, snapshotName string, workerID int) CopyResult {
	log := c.logger.With().Str("table", table.QualifiedName()).Int("worker", workerID).Logger()
	log.Info().Msg("starting COPY")
	c.reportProgress(table, "start", 0)

	deadline := time.Now().Add(copyRetryBudget)
	delay := copyRetryBase
	retries := 0

	for {
		result := c.copyTableOnce(ctx, table, snapshotName, workerID)
		if result.Err == nil {
			result.Retries = retries
			log.Info().Int64("rows", result.RowsCopied).Int("retries", retries).Msg("COPY complete")
			c.reportProgress(table, "done", result.RowsCopied)
			return result
		}

		if !isConnectionError(result.Err) || time.Now().After(deadline) || ctx.Err() != nil {
			result.Retries = retries
			return result
		}

		retries++
		log.Warn().
			Err(result.Err).
			Int("retry", retries).
			Dur("delay", delay).
			Msg("COPY failed (connection error), retrying")
		c.reportProgress(table, "retry", int64(retries))

		select {
		case <-ctx.Done():
			return CopyResult{Table: table, Retries: retries, Err: ctx.Err()}
		case <-time.After(delay):
		}

		delay = min(delay*2, copyRetryCap)
	}
}

func (c *Copier) copyTableOnce(ctx context.Context, table TableInfo, snapshotName string, workerID int) CopyResult {
	srcConn, err := c.source.Acquire(ctx)
	if err != nil {
		return CopyResult{Table: table, Err: fmt.Errorf("acquire source conn: %w", err)}
	}
	defer srcConn.Release()

	srcTx, err := srcConn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return CopyResult{Table: table, Err: fmt.Errorf("begin source tx: %w", err)}
	}
	defer srcTx.Rollback(ctx) //nolint:errcheck

	if snapshotName != "" {
		if _, err := srcTx.Exec(ctx, fmt.Sprintf("SET TRANSACTION SNAPSHOT '%s'", snapshotName)); err != nil {
			return CopyResult{Table: table, Err: fmt.Errorf("set snapshot: %w", err)}
		}
	}

	qn := quoteQualifiedName(table.Schema, table.Name)
	rows, err := srcTx.Query(ctx, fmt.Sprintf("SELECT * FROM %s", qn))
	if err != nil {
		return CopyResult{Table: table, Err: fmt.Errorf("select from %s: %w", qn, err)}
	}

	fieldDescs := rows.FieldDescriptions()
	colNames := make([]string, len(fieldDescs))
	for i, fd := range fieldDescs {
		colNames[i] = fd.Name
	}

	src := &rowStreamer{
		rows:     rows,
		report:   c.reportProgress,
		table:    table,
		colCount: len(colNames),
	}

	n, err := c.dest.CopyFrom(ctx,
		pgx.Identifier{table.Schema, table.Name},
		colNames,
		src)
	rows.Close()
	if err != nil {
		return CopyResult{Table: table, Err: fmt.Errorf("copy to %s: %w", qn, err)}
	}
	if src.err != nil {
		return CopyResult{Table: table, Err: fmt.Errorf("read from %s: %w", qn, src.err)}
	}

	return CopyResult{Table: table, RowsCopied: n}
}

func isConnectionError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return strings.HasPrefix(pgErr.Code, "08")
	}
	var connErr *pgconn.ConnectError
	if errors.As(err, &connErr) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "EOF")
}

// rowStreamer implements pgx.CopyFromSource by streaming rows one at a time
// from a pgx.Rows result set. This avoids buffering entire tables in memory.
type rowStreamer struct {
	rows       pgx.Rows
	report     ProgressFunc
	table      TableInfo
	colCount   int
	count      int64
	vals       []any
	err        error
	lastReport time.Time
}

func (s *rowStreamer) Next() bool {
	if !s.rows.Next() {
		return false
	}
	vals, err := s.rows.Values()
	if err != nil {
		s.err = err
		return false
	}
	s.vals = vals
	s.count++
	if s.report != nil && time.Since(s.lastReport) >= progressReportInterval {
		s.report(s.table, "progress", s.count)
		s.lastReport = time.Now()
	}
	return true
}

func (s *rowStreamer) Values() ([]any, error) {
	return s.vals, nil
}

func (s *rowStreamer) Err() error {
	if s.err != nil {
		return s.err
	}
	return s.rows.Err()
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func quoteQualifiedName(schema, table string) string {
	if schema == "" || schema == "public" {
		return quoteIdent(table)
	}
	return quoteIdent(schema) + "." + quoteIdent(table)
}

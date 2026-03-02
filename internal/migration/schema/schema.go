package schema

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// SchemaStatementDetail records a skipped or errored DDL statement.
type SchemaStatementDetail struct {
	Statement string `json:"statement"`
	Reason    string `json:"reason"`
}

// SchemaResult holds the outcome of a DDL application.
type SchemaResult struct {
	Total           int
	Applied         int
	Skipped         int
	ErrorsTolerated int
	SkippedDetails  []SchemaStatementDetail
	ErroredDetails  []SchemaStatementDetail
}

// Manager handles DDL operations between source and destination databases.
type Manager struct {
	source            *pgxpool.Pool
	dest              *pgxpool.Pool
	logger            zerolog.Logger
	MaxErrors         int
	ExcludeExtensions []string
}

// NewMigrator creates a schema Manager.
func NewManager(source, dest *pgxpool.Pool, logger zerolog.Logger) *Manager {
	return &Manager{
		source: source,
		dest:   dest,
		logger: logger.With().Str("component", "schema").Logger(),
	}
}

// DumpSchema returns the DDL for the source database using pg_dump --schema-only.
func (m *Manager) DumpSchema(ctx context.Context, dsn string) (string, error) {
	cmd := exec.CommandContext(ctx, "pg_dump", "--schema-only", "--no-owner", "--no-privileges", dsn)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("pg_dump failed: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("pg_dump: %w", err)
	}
	return string(out), nil
}

// ApplySchema applies the given DDL to the destination database.
// It strips psql meta-commands (lines starting with \) and SQL comments
// from pg_dump output, then executes each statement individually.
// When MaxErrors > 0, up to that many non-duplicate errors are tolerated.
func (m *Manager) ApplySchema(ctx context.Context, ddl string) (SchemaResult, error) {
	stmts := splitStatements(ddl)
	var result SchemaResult
	result.Total = len(stmts)
	for i, stmt := range stmts {
		upper := strings.ToUpper(strings.TrimSpace(stmt))
		if strings.HasPrefix(upper, "SELECT ") {
			continue
		}
		m.logger.Debug().Int("index", i).Str("statement", truncate(stmt, 120)).Msg("applying schema statement")
		stmtCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err := m.dest.Exec(stmtCtx, stmt)
		cancel()
		if err != nil {
			if isDuplicateObjectErr(err) {
				m.logger.Debug().Str("statement", truncate(stmt, 120)).Msg("skipping (already exists)")
				result.Skipped++
				result.SkippedDetails = append(result.SkippedDetails, SchemaStatementDetail{
					Statement: truncate(stmt, 200),
					Reason:    "already exists",
				})
				continue
			}
			if m.MaxErrors > 0 && result.ErrorsTolerated < m.MaxErrors {
				result.ErrorsTolerated++
				m.logger.Warn().Str("statement", truncate(stmt, 200)).Err(err).Int("error_count", result.ErrorsTolerated).Msg("schema statement failed (tolerated)")
				result.ErroredDetails = append(result.ErroredDetails, SchemaStatementDetail{
					Statement: truncate(stmt, 200),
					Reason:    err.Error(),
				})
				continue
			}
			m.logger.Warn().Str("statement", truncate(stmt, 200)).Err(err).Msg("schema statement failed")
			return result, fmt.Errorf("apply schema: %w", err)
		}
		result.Applied++
	}
	m.logger.Info().Int("applied", result.Applied).Int("skipped", result.Skipped).Int("errors_tolerated", result.ErrorsTolerated).Int("total", result.Total).Msg("schema applied to destination")
	return result, nil
}

// splitStatements parses pg_dump output into individual SQL statements,
// stripping psql meta-commands and comments. It correctly handles
// dollar-quoted strings (e.g. $$ or $tag$) so that semicolons inside
// PL/pgSQL function bodies are not treated as statement terminators.
func splitStatements(dump string) []string {
	var stmts []string
	var current strings.Builder
	inDollarQuote := false
	dollarTag := ""

	for _, line := range strings.Split(dump, "\n") {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		if strings.HasPrefix(trimmed, "\\") {
			continue
		}

		current.WriteString(line)
		current.WriteByte('\n')

		inDollarQuote, dollarTag = trackDollarQuoting(line, inDollarQuote, dollarTag)

		if !inDollarQuote && strings.HasSuffix(trimmed, ";") {
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				stmts = append(stmts, stmt)
			}
			current.Reset()
		}
	}

	if trailing := strings.TrimSpace(current.String()); trailing != "" {
		stmts = append(stmts, trailing)
	}

	return stmts
}

// trackDollarQuoting scans a line for dollar-quote delimiters ($$ or $tag$)
// and toggles the quoting state. Returns the updated state.
func trackDollarQuoting(line string, inQuote bool, currentTag string) (bool, string) {
	i := 0
	for i < len(line) {
		if line[i] != '$' {
			i++
			continue
		}
		tag, end := parseDollarTag(line, i)
		if tag == "" {
			i++
			continue
		}
		if !inQuote {
			inQuote = true
			currentTag = tag
		} else if tag == currentTag {
			inQuote = false
			currentTag = ""
		}
		i = end
	}
	return inQuote, currentTag
}

// parseDollarTag tries to parse a dollar-quote tag starting at pos.
// Valid tags: $$ or $identifier$ where identifier is [A-Za-z0-9_].
// Returns the full tag (e.g. "$$" or "$body$") and the index past the
// closing $. Returns ("", pos) if no valid tag found.
func parseDollarTag(line string, pos int) (string, int) {
	if pos >= len(line) || line[pos] != '$' {
		return "", pos
	}
	j := pos + 1
	if j < len(line) && line[j] == '$' {
		return "$$", j + 1
	}
	for j < len(line) && isDollarTagChar(line[j]) {
		j++
	}
	if j > pos+1 && j < len(line) && line[j] == '$' {
		tag := line[pos : j+1]
		return tag, j + 1
	}
	return "", pos
}

func isDollarTagChar(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_'
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func isDuplicateObjectErr(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "42P07", "42P16", "42710":
			return true
		}
	}
	return false
}

// SchemaDiff represents differences between source and destination schemas.
type SchemaDiff struct {
	MissingTables []string
	ExtraTables   []string
	ColumnDiffs   []ColumnDiff
}

// ColumnDiff describes a column mismatch between source and destination.
type ColumnDiff struct {
	Table      string
	Column     string
	SourceType string
	DestType   string
}

// HasDifferences returns true if any schema differences were found.
func (d *SchemaDiff) HasDifferences() bool {
	return len(d.MissingTables) > 0 || len(d.ExtraTables) > 0 || len(d.ColumnDiffs) > 0
}

// CompareSchemas compares user table structures between source and destination.
func (m *Manager) CompareSchemas(ctx context.Context) (*SchemaDiff, error) {
	srcTables, err := m.listUserTables(ctx, m.source)
	if err != nil {
		return nil, fmt.Errorf("list source tables: %w", err)
	}
	destTables, err := m.listUserTables(ctx, m.dest)
	if err != nil {
		return nil, fmt.Errorf("list dest tables: %w", err)
	}

	diff := &SchemaDiff{}

	srcSet := make(map[string]bool)
	for _, t := range srcTables {
		srcSet[t] = true
	}
	destSet := make(map[string]bool)
	for _, t := range destTables {
		destSet[t] = true
	}

	for _, t := range srcTables {
		if !destSet[t] {
			diff.MissingTables = append(diff.MissingTables, t)
		}
	}
	for _, t := range destTables {
		if !srcSet[t] {
			diff.ExtraTables = append(diff.ExtraTables, t)
		}
	}

	// Compare columns for tables present in both.
	for _, t := range srcTables {
		if !destSet[t] {
			continue
		}
		srcCols, err := m.listColumns(ctx, m.source, t)
		if err != nil {
			return nil, fmt.Errorf("list source columns for %s: %w", t, err)
		}
		destCols, err := m.listColumns(ctx, m.dest, t)
		if err != nil {
			return nil, fmt.Errorf("list dest columns for %s: %w", t, err)
		}
		destColMap := make(map[string]string)
		for _, c := range destCols {
			destColMap[c.name] = c.dataType
		}
		for _, c := range srcCols {
			if dt, ok := destColMap[c.name]; ok {
				if dt != c.dataType {
					diff.ColumnDiffs = append(diff.ColumnDiffs, ColumnDiff{
						Table:      t,
						Column:     c.name,
						SourceType: c.dataType,
						DestType:   dt,
					})
				}
			} else {
				diff.ColumnDiffs = append(diff.ColumnDiffs, ColumnDiff{
					Table:      t,
					Column:     c.name,
					SourceType: c.dataType,
					DestType:   "(missing)",
				})
			}
		}
	}

	return diff, nil
}

type colInfo struct {
	name     string
	dataType string
}

func (m *Manager) listUserTables(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT schemaname || '.' || tablename
		FROM pg_tables
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
		ORDER BY schemaname, tablename`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

func (m *Manager) listColumns(ctx context.Context, pool *pgxpool.Pool, qualifiedTable string) ([]colInfo, error) {
	parts := strings.SplitN(qualifiedTable, ".", 2)
	schema, table := parts[0], parts[1]

	rows, err := pool.Query(ctx, `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []colInfo
	for rows.Next() {
		var c colInfo
		if err := rows.Scan(&c.name, &c.dataType); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

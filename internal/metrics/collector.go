package metrics

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmanager/pkg/lsn"
)

// TableStatus represents the current state of a table in the migration.
type TableStatus string

const (
	TablePending   TableStatus = "pending"
	TableCopying   TableStatus = "copying"
	TableCopied    TableStatus = "copied"
	TableStreaming  TableStatus = "streaming"
)

// TableProgress tracks per-table copy/stream progress.
type TableProgress struct {
	Schema      string      `json:"schema"`
	Name        string      `json:"name"`
	Status      TableStatus `json:"status"`
	RowsTotal   int64       `json:"rows_total"`
	RowsCopied  int64       `json:"rows_copied"`
	SizeBytes   int64       `json:"size_bytes"`
	BytesCopied int64       `json:"bytes_copied"`
	Percent     float64     `json:"percent"`
	ElapsedSec  float64     `json:"elapsed_sec"`
	StartedAt   time.Time   `json:"-"`
}

// Snapshot is the complete metrics state at a point in time.
type Snapshot struct {
	Timestamp    time.Time       `json:"timestamp"`
	Phase        string          `json:"phase"`
	ElapsedSec   float64         `json:"elapsed_sec"`

	// LSN tracking
	AppliedLSN   string          `json:"applied_lsn"`
	ConfirmedLSN string          `json:"confirmed_lsn"`
	LagBytes     uint64          `json:"lag_bytes"`
	LagFormatted string          `json:"lag_formatted"`

	// Copy progress
	TablesTotal  int             `json:"tables_total"`
	TablesCopied int             `json:"tables_copied"`
	Tables       []TableProgress `json:"tables"`

	// Throughput
	RowsPerSec   float64         `json:"rows_per_sec"`
	BytesPerSec  float64         `json:"bytes_per_sec"`
	TotalRows    int64           `json:"total_rows"`
	TotalBytes   int64           `json:"total_bytes"`

	// Errors
	ErrorCount   int             `json:"error_count"`
	LastError    string          `json:"last_error,omitempty"`

	// Observability
	Events       []MigrationEvent `json:"events,omitempty"`
	Phases       []PhaseEntry     `json:"phases,omitempty"`
	ErrorHistory []ErrorEntry     `json:"error_history,omitempty"`
	SchemaStats  *SchemaStats     `json:"schema_stats,omitempty"`
}

// MigrationEvent records a notable event during migration.
type MigrationEvent struct {
	Time    time.Time         `json:"time"`
	Type    string            `json:"type"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// PhaseEntry records how long a phase lasted.
type PhaseEntry struct {
	Phase     string    `json:"phase"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
	Duration  float64   `json:"duration_sec"`
}

// ErrorEntry records an error with context.
type ErrorEntry struct {
	Time      time.Time `json:"time"`
	Phase     string    `json:"phase"`
	Message   string    `json:"message"`
	Retryable bool      `json:"retryable"`
}

// SchemaStatementDetail records a skipped or errored DDL statement.
type SchemaStatementDetail struct {
	Statement string `json:"statement"`
	Reason    string `json:"reason"`
}

// SchemaStats tracks DDL application results.
type SchemaStats struct {
	StatementsTotal    int                     `json:"statements_total"`
	StatementsApplied  int                     `json:"statements_applied"`
	StatementsSkipped  int                     `json:"statements_skipped"`
	ErrorsTolerated    int                     `json:"errors_tolerated"`
	SkippedDetails     []SchemaStatementDetail `json:"skipped_details,omitempty"`
	ErroredDetails     []SchemaStatementDetail `json:"errored_details,omitempty"`
}

// LogEntry represents a log line captured for the UI.
type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// Collector aggregates pipeline metrics and provides snapshots for consumption
// by the HTTP API and TUI.
type Collector struct {
	logger zerolog.Logger

	mu          sync.RWMutex
	phase       string
	startedAt   time.Time
	tables      map[string]*TableProgress // key: schema.name
	tableOrder  []string                  // insertion-order keys

	appliedLSN   pglogrepl.LSN
	confirmedLSN pglogrepl.LSN
	latestLSN    pglogrepl.LSN // server-reported write position

	totalRows  atomic.Int64
	totalBytes atomic.Int64

	errorCount atomic.Int64
	lastError  atomic.Value // string

	// Throughput tracking (sliding window).
	rowWindow   *slidingWindow
	byteWindow  *slidingWindow

	// Subscribers for push-based updates.
	subMu       sync.Mutex
	subscribers map[chan Snapshot]struct{}

	// Log ring buffer.
	logMu   sync.Mutex
	logs    []LogEntry
	logCap  int

	// Event tracking for observability.
	eventMu      sync.Mutex
	events       []MigrationEvent
	phases       []PhaseEntry
	errorHistory []ErrorEntry
	schemaStats  *SchemaStats
	eventCap     int

	done chan struct{}
}

// NewCollector creates a new Collector.
func NewCollector(logger zerolog.Logger) *Collector {
	c := &Collector{
		logger:      logger.With().Str("component", "metrics").Logger(),
		tables:      make(map[string]*TableProgress),
		subscribers: make(map[chan Snapshot]struct{}),
		rowWindow:   newSlidingWindow(60 * time.Second),
		byteWindow:  newSlidingWindow(60 * time.Second),
		logs:        make([]LogEntry, 0, 500),
		logCap:      500,
		events:      make([]MigrationEvent, 0, 200),
		eventCap:    200,
		done:        make(chan struct{}),
	}
	go c.broadcastLoop()
	return c
}

// SetPhase updates the current pipeline phase.
func (c *Collector) SetPhase(phase string) {
	c.mu.Lock()
	now := time.Now()
	if c.startedAt.IsZero() {
		c.startedAt = now
	}
	oldPhase := c.phase
	c.phase = phase
	c.mu.Unlock()

	c.eventMu.Lock()
	if oldPhase != "" && len(c.phases) > 0 {
		last := &c.phases[len(c.phases)-1]
		if last.Phase == oldPhase && last.EndedAt.IsZero() {
			last.EndedAt = now
			last.Duration = now.Sub(last.StartedAt).Seconds()
		}
	}
	c.phases = append(c.phases, PhaseEntry{
		Phase:     phase,
		StartedAt: now,
	})
	c.eventMu.Unlock()

	c.RecordEvent("phase_change", "entered phase: "+phase, nil)
}

// SetTables initializes the table tracking list.
func (c *Collector) SetTables(tables []TableProgress) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tables = make(map[string]*TableProgress, len(tables))
	c.tableOrder = make([]string, 0, len(tables))
	for i := range tables {
		key := tables[i].Schema + "." + tables[i].Name
		tp := tables[i]
		c.tables[key] = &tp
		c.tableOrder = append(c.tableOrder, key)
	}
}

// TableStarted marks a table as actively being copied.
func (c *Collector) TableStarted(schema, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := schema + "." + name
	if tp, ok := c.tables[key]; ok {
		tp.Status = TableCopying
		tp.StartedAt = time.Now()
	}
}

// TableProgress updates copy progress for a table.
func (c *Collector) UpdateTableProgress(schema, name string, rowsCopied, bytesCopied int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := schema + "." + name
	if tp, ok := c.tables[key]; ok {
		tp.RowsCopied = rowsCopied
		tp.BytesCopied = bytesCopied
		if tp.RowsTotal > 0 {
			tp.Percent = float64(rowsCopied) / float64(tp.RowsTotal) * 100
			if tp.Percent > 99.9 {
				tp.Percent = 99.9
			}
		}
		if !tp.StartedAt.IsZero() {
			tp.ElapsedSec = time.Since(tp.StartedAt).Seconds()
		}
	}
}

// TableDone marks a table copy as complete.
func (c *Collector) TableDone(schema, name string, rowsCopied int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := schema + "." + name
	if tp, ok := c.tables[key]; ok {
		tp.Status = TableCopied
		tp.RowsCopied = rowsCopied
		if tp.RowsTotal == 0 {
			tp.RowsTotal = rowsCopied
		}
		tp.Percent = 100
		if !tp.StartedAt.IsZero() {
			tp.ElapsedSec = time.Since(tp.StartedAt).Seconds()
		}
	}
}

// TableStreaming marks a table as actively streaming CDC changes.
func (c *Collector) TableStreaming(schema, name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := schema + "." + name
	if tp, ok := c.tables[key]; ok {
		tp.Status = TableStreaming
	}
}

// RecordApplied records a successfully applied LSN and row/byte counts.
func (c *Collector) RecordApplied(appliedLSN pglogrepl.LSN, rows int64, bytes int64) {
	c.mu.Lock()
	c.appliedLSN = appliedLSN
	c.mu.Unlock()
	c.totalRows.Add(rows)
	c.totalBytes.Add(bytes)
	now := time.Now()
	c.rowWindow.Add(now, float64(rows))
	c.byteWindow.Add(now, float64(bytes))
}

// RecordConfirmedLSN updates the confirmed (flushed) LSN.
func (c *Collector) RecordConfirmedLSN(lsn pglogrepl.LSN) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.confirmedLSN = lsn
}

// RecordLatestLSN updates the server-reported latest LSN for lag calculation.
func (c *Collector) RecordLatestLSN(lsn pglogrepl.LSN) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.latestLSN = lsn
}

// RecordError increments the error count and stores the last error message.
func (c *Collector) RecordError(err error) {
	c.errorCount.Add(1)
	if err != nil {
		c.lastError.Store(err.Error())
	}
}

// RecordEvent adds a notable event to the event log.
func (c *Collector) RecordEvent(eventType, message string, fields map[string]string) {
	c.eventMu.Lock()
	defer c.eventMu.Unlock()
	if len(c.events) >= c.eventCap {
		n := c.eventCap / 4
		copy(c.events, c.events[n:])
		c.events = c.events[:len(c.events)-n]
	}
	c.events = append(c.events, MigrationEvent{
		Time:    time.Now(),
		Type:    eventType,
		Message: message,
		Fields:  fields,
	})
}

// RecordErrorDetail records an error with phase context and retryability.
func (c *Collector) RecordErrorDetail(err error, phase string, retryable bool) {
	c.RecordError(err)
	if err == nil {
		return
	}
	c.eventMu.Lock()
	defer c.eventMu.Unlock()
	c.errorHistory = append(c.errorHistory, ErrorEntry{
		Time:      time.Now(),
		Phase:     phase,
		Message:   err.Error(),
		Retryable: retryable,
	})
	if len(c.errorHistory) > c.eventCap {
		c.errorHistory = c.errorHistory[len(c.errorHistory)-c.eventCap:]
	}
}

// SetSchemaStats records the results of DDL application.
func (c *Collector) SetSchemaStats(total, applied, skipped, errTolerated int, skippedDetails, erroredDetails []SchemaStatementDetail) {
	c.eventMu.Lock()
	defer c.eventMu.Unlock()
	c.schemaStats = &SchemaStats{
		StatementsTotal:   total,
		StatementsApplied: applied,
		StatementsSkipped: skipped,
		ErrorsTolerated:   errTolerated,
		SkippedDetails:    skippedDetails,
		ErroredDetails:    erroredDetails,
	}
}

// AddLog appends a log entry to the ring buffer.
func (c *Collector) AddLog(entry LogEntry) {
	c.logMu.Lock()
	defer c.logMu.Unlock()
	if len(c.logs) >= c.logCap {
		// Shift buffer: drop oldest quarter.
		n := c.logCap / 4
		copy(c.logs, c.logs[n:])
		c.logs = c.logs[:len(c.logs)-n]
	}
	c.logs = append(c.logs, entry)
}

// Logs returns a copy of recent log entries.
func (c *Collector) Logs() []LogEntry {
	c.logMu.Lock()
	defer c.logMu.Unlock()
	out := make([]LogEntry, len(c.logs))
	copy(out, c.logs)
	return out
}

// Snapshot returns the current metrics state (thread-safe).
func (c *Collector) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now()
	var elapsed float64
	if !c.startedAt.IsZero() {
		elapsed = now.Sub(c.startedAt).Seconds()
	}

	lagBytes := lsn.Lag(c.appliedLSN, c.latestLSN)

	tables := make([]TableProgress, 0, len(c.tableOrder))
	tablesCopied := 0
	for _, key := range c.tableOrder {
		tp := *c.tables[key]
		tables = append(tables, tp)
		if tp.Status == TableCopied || tp.Status == TableStreaming {
			tablesCopied++
		}
	}

	var lastErr string
	if v := c.lastError.Load(); v != nil {
		lastErr = v.(string)
	}

	return Snapshot{
		Timestamp:    now,
		Phase:        c.phase,
		ElapsedSec:   elapsed,
		AppliedLSN:   c.appliedLSN.String(),
		ConfirmedLSN: c.confirmedLSN.String(),
		LagBytes:     lagBytes,
		LagFormatted: lsn.FormatLag(lagBytes, 0),
		TablesTotal:  len(c.tableOrder),
		TablesCopied: tablesCopied,
		Tables:       tables,
		RowsPerSec:   c.rowWindow.Rate(),
		BytesPerSec:  c.byteWindow.Rate(),
		TotalRows:    c.totalRows.Load(),
		TotalBytes:   c.totalBytes.Load(),
		ErrorCount:   int(c.errorCount.Load()),
		LastError:    lastErr,
		Events:       c.snapshotEvents(),
		Phases:       c.snapshotPhases(),
		ErrorHistory: c.snapshotErrorHistory(),
		SchemaStats:  c.snapshotSchemaStats(),
	}
}

func (c *Collector) snapshotEvents() []MigrationEvent {
	c.eventMu.Lock()
	defer c.eventMu.Unlock()
	if len(c.events) == 0 {
		return nil
	}
	out := make([]MigrationEvent, len(c.events))
	copy(out, c.events)
	return out
}

func (c *Collector) snapshotPhases() []PhaseEntry {
	c.eventMu.Lock()
	defer c.eventMu.Unlock()
	if len(c.phases) == 0 {
		return nil
	}
	now := time.Now()
	out := make([]PhaseEntry, len(c.phases))
	copy(out, c.phases)
	last := &out[len(out)-1]
	if last.EndedAt.IsZero() {
		last.Duration = now.Sub(last.StartedAt).Seconds()
	}
	return out
}

func (c *Collector) snapshotErrorHistory() []ErrorEntry {
	c.eventMu.Lock()
	defer c.eventMu.Unlock()
	if len(c.errorHistory) == 0 {
		return nil
	}
	out := make([]ErrorEntry, len(c.errorHistory))
	copy(out, c.errorHistory)
	return out
}

func (c *Collector) snapshotSchemaStats() *SchemaStats {
	c.eventMu.Lock()
	defer c.eventMu.Unlock()
	if c.schemaStats == nil {
		return nil
	}
	s := *c.schemaStats
	return &s
}

// Subscribe returns a channel that receives periodic Snapshot updates.
func (c *Collector) Subscribe() chan Snapshot {
	ch := make(chan Snapshot, 4)
	c.subMu.Lock()
	c.subscribers[ch] = struct{}{}
	c.subMu.Unlock()
	return ch
}

// Unsubscribe removes a subscription channel.
func (c *Collector) Unsubscribe(ch chan Snapshot) {
	c.subMu.Lock()
	delete(c.subscribers, ch)
	c.subMu.Unlock()
}

// Close stops the broadcast loop.
func (c *Collector) Close() {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
}

func (c *Collector) broadcastLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			snap := c.Snapshot()
			c.subMu.Lock()
			for ch := range c.subscribers {
				select {
				case ch <- snap:
				default:
					// Subscriber too slow, skip.
				}
			}
			c.subMu.Unlock()
		}
	}
}

// --- Sliding window for throughput calculation ---

type windowEntry struct {
	time  time.Time
	value float64
}

type slidingWindow struct {
	mu      sync.Mutex
	entries []windowEntry
	window  time.Duration
}

func newSlidingWindow(d time.Duration) *slidingWindow {
	return &slidingWindow{
		entries: make([]windowEntry, 0, 128),
		window:  d,
	}
}

func (w *slidingWindow) Add(t time.Time, val float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = append(w.entries, windowEntry{time: t, value: val})
	w.evict(t)
}

func (w *slidingWindow) Rate() float64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	w.evict(now)
	if len(w.entries) == 0 {
		return 0
	}
	var total float64
	for _, e := range w.entries {
		total += e.value
	}
	elapsed := now.Sub(w.entries[0].time).Seconds()
	if elapsed < 1 {
		elapsed = 1
	}
	return total / elapsed
}

func (w *slidingWindow) evict(now time.Time) {
	cutoff := now.Add(-w.window)
	i := 0
	for i < len(w.entries) && w.entries[i].time.Before(cutoff) {
		i++
	}
	if i > 0 {
		copy(w.entries, w.entries[i:])
		w.entries = w.entries[:len(w.entries)-i]
	}
}

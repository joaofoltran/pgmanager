# Migration Module

## Overview

Online logical migration for PostgreSQL: schema copy, parallel data COPY with consistent snapshots, WAL-based CDC streaming, zero-downtime switchover, and bidirectional replication support. All data flows through a single `chan stream.Message`.

## File Map

### Backend — `internal/migration/`

| File | Purpose |
|------|---------|
| `stream/message.go` | `Message` interface + all message types: Begin, Commit, Change, Relation, Sentinel |
| `stream/decoder.go` | `Decoder` — WAL consumer using pglogrepl/pgoutput, emits Messages on a channel (cap 4096) |
| `replay/applier.go` | `Applier` — reads chan Message, coalesces txns, applies DML to destination (INSERT batching, COPY for large batches) |
| `snapshot/snapshot.go` | `Copier` — parallel COPY engine with consistent snapshot (`SET TRANSACTION SNAPSHOT`) |
| `pipeline/pipeline.go` | `Pipeline` — top-level orchestrator wiring all components, implements Clone/Follow/Resume/Switchover modes |
| `sentinel/sentinel.go` | `Coordinator` + `SentinelMessage` — zero-footprint switchover coordination via in-channel sentinels |
| `bidi/bidi.go` | `Filter` — origin-based loop detection for bidirectional replication |
| `schema/schema.go` | `Manager` — DDL dump/apply/compare via pg_dump, tolerates duplicates |
| `pgwire/pgwire.go` | `Conn` — replication connection wrapper with origin setup and slot management |
| `filter/filter.go` | `Filter` + `Config` — include/exclude rules for schemas and tables |

### Server — `internal/server/`

| File | Lines | Purpose |
|------|-------|---------|
| `migrations.go` | full file | `migrationHandlers` — 8 HTTP handlers: list, get, create, remove, start, stop, switchover, logs |
| `server.go` | ~58, ~110-121 | `SetMigrationStore(store, runner)` + route registration |

### Frontend — `web/src/`

| File | Purpose |
|------|---------|
| `types/migration.ts` | TS interfaces: `Migration` (35 fields with live metrics), `CreateMigrationRequest`, status/mode enums |
| `api/client.ts` | 8 fetch functions: fetchMigrations, fetchMigration, createMigration, removeMigration, startMigration, stopMigration, switchoverMigration, fetchMigrationLogs |
| `hooks/useMetrics.ts` | WebSocket-based real-time metrics hook (shared, used by MigrationPage) |
| `pages/MigrationPage.tsx` | Real-time WebSocket dashboard: PhaseHeader, MetricCards, OverallProgress, LagChart, LogViewer, TableList, JobControls |
| `pages/MigrationsListPage.tsx` | List view polling every 5s: status badges, delete, navigate to detail |
| `pages/MigrationDetailPage.tsx` | Full detail: start/stop/switchover actions, copy progress, phase timeline, schema stats, error history, log viewer |
| `pages/CreateMigrationPage.tsx` | Migration creation form |
| `components/migration/PhaseHeader.tsx` | Phase badge with color, elapsed time, lag, connection status |
| `components/migration/MetricCards.tsx` | 4 cards: rows/sec, throughput, total rows, total data |
| `components/migration/OverallProgress.tsx` | Progress bar: tables copied/total with percentage |
| `components/migration/LagChart.tsx` | Recharts line chart of replication lag (last 60 points) |
| `components/migration/TableList.tsx` | Per-table status, row counts, size, progress bar |
| `components/migration/LogViewer.tsx` | Auto-scrolling log viewer polling every 2s with level coloring |
| `components/migration/JobControls.tsx` | Start dropdown (Clone/Follow), stop button, cluster/node selection modal |

## Architecture

```
Decoder (pglogrepl) → [BidiFilter] → Applier
                            ↑
                    Sentinel Coordinator (injected at exact LSN)
```

```
Pipeline
  ├── connect()         → repl conn + src/dst pools
  ├── initComponents()  → wires decoder, applier, copier, schemaMgr, sentinel, bidi, filter
  ├── RunClone()        → schema + parallel COPY
  ├── RunCloneAndFollow() → schema + COPY → CDC streaming
  ├── RunResumeCloneAndFollow() → verify slot, re-COPY incomplete, CDC
  ├── RunFollow()       → CDC-only from existing slot
  └── RunSwitchover()   → sentinel injection → confirmation → safe switch
```

## Key Components

### Message System (`stream/message.go`)

Unified `Message` interface with `Kind()`, `LSN()`, `OriginID()`, `Timestamp()`. Types:
- `BeginMessage` / `CommitMessage` — transaction boundaries
- `RelationMessage` — schema metadata for a table (sent before first change)
- `ChangeMessage` — DML row change (INSERT/UPDATE/DELETE) with old/new TupleData
- `SentinelMessage` — synthetic switchover marker (no SQL, injected into channel)

### WAL Decoder (`stream/decoder.go`)

- **pgoutput plugin** (PG10+) — binary protocol, zero library overhead, no wal2json dependency
- **Channel buffer:** 4096 messages
- **Backpressure:** when channel fills, sends standby status updates to prevent source timeout
- **LSN confirmation:** applier confirms post-write; decoder advances slot's flushed position
- **Origin tracking:** extracts OriginMessage for bidi loop detection
- **Empty txn skipping:** Begin+Commit with no changes are discarded

### Applier (`replay/applier.go`)

- **INSERT coalescing:** batches consecutive INSERTs for same table (up to threshold)
- **COPY mode:** switches to `COPY` protocol when batch exceeds 5 rows
- **Transaction coalescing:** merges up to 500 WAL txns or 50ms into one dest txn (max 256MB)
- **Statement cache:** prepared statements keyed by `op:schema.table:nSet:nWhere`
- **Origin tagging:** `pg_replication_origin_session_setup()` for bidi
- **Callbacks:** `OnApplied(lsn)` for LSN confirmation, `OnSentinel(id)` for switchover

### Parallel COPY (`snapshot/snapshot.go`)

- `CreateReplicationSlot` returns exported snapshot name
- All COPY workers use `SET TRANSACTION SNAPSHOT <name>` — gap-free, duplicate-free initial copy
- Work distribution via channel — workers pull tables FIFO, ordered by size DESC
- `rowStreamer` implements `pgx.CopyFromSource` — streams rows one-at-a-time (no full-table buffering)
- Retry budget: 30min with exponential backoff (2s base → 5min cap)
- Connection errors (class 08, reset, refused, EOF) trigger retries

### Schema Manager (`schema/schema.go`)

- `DumpSchema()` — runs `pg_dump --schema-only`
- `ApplySchema()` — applies DDL statement-by-statement, tolerates duplicates (PG error codes `42P07`, `42P16`, `42710`)
- `CompareSchemas()` — compares user table structures between source and dest
- `splitStatements()` — parses pg_dump output handling dollar-quoted strings (`$$`, `$tag$`)

### Sentinel Coordinator (`sentinel/sentinel.go`)

- **Zero database footprint** — no sentinel tables
- `Initiate(ctx, lsn)` — injects SentinelMessage into messages channel at exact LSN
- `WaitForConfirmation(id, timeout)` — blocks until applier calls `Confirm(id)`
- If confirmed, all prior WAL applied → safe to switch
- Auto-incrementing IDs (`sentinel-1`, `sentinel-2`, ...)

### Bidi Filter (`bidi/bidi.go`)

- Origin-based loop detection using PG's native `pg_replication_origin` infrastructure
- Filter drops messages with matching originID (echoed writes)
- `Run(ctx, in) <-chan Message` — returns filtered channel

### Table Filter (`filter/filter.go`)

- `Config` with include/exclude for schemas and tables
- Evaluation order: exclude tables → exclude schemas → include tables → include schemas → default allow
- Supports both qualified (`schema.table`) and unqualified (`table`) matching

## Pipeline Modes

1. **Clone** — schema + COPY, drain replication stream
2. **Clone+Follow** — schema + COPY → CDC streaming from slot LSN
3. **Resume** — detect incomplete clone (compare row counts), re-COPY missing tables, then CDC
4. **Follow** — CDC only from existing slot
5. **Switchover** — sentinel injection → confirmation → optional reverse replication

## REST API

| Method | Endpoint | Handler | Description |
|--------|----------|---------|-------------|
| GET | `/api/v1/migrations` | `list` | List all migrations |
| POST | `/api/v1/migrations` | `create` | Create migration (validates, defaults slot/publication/workers) |
| GET | `/api/v1/migrations/{id}` | `get` | Get migration + live metrics if running |
| DELETE | `/api/v1/migrations/{id}` | `remove` | Delete (requires `?force=true` if running) |
| POST | `/api/v1/migrations/{id}/start` | `start` | Start migration |
| POST | `/api/v1/migrations/{id}/stop` | `stop` | Stop migration |
| POST | `/api/v1/migrations/{id}/switchover` | `switchover` | Initiate switchover |
| GET | `/api/v1/migrations/{id}/logs` | `logs` | Get log entries |

## Key Types

### Go

```
stream.Message (interface)       — Kind, LSN, OriginID, Timestamp
stream.ChangeMessage             — DML change with Op, TupleData (old+new), origin
stream.RelationMessage           — table schema metadata
stream.BeginMessage/CommitMessage — txn boundaries
sentinel.SentinelMessage         — switchover marker
snapshot.TableInfo               — schema, name, row count, size
snapshot.CopyResult              — per-table copy outcome
pipeline.Pipeline                — top-level orchestrator
pipeline.Progress                — phase, LSN, tables total/copied, started_at
replay.Applier                   — DML writer with coalescing
filter.Config / filter.Filter    — include/exclude rules
schema.Manager                   — DDL operations
schema.SchemaResult              — DDL application outcome (applied/skipped/errored counts)
bidi.Filter                      — origin-based loop detection
pgwire.Conn                      — replication connection wrapper
```

### TypeScript

```
MigrationMode     — "clone_only" | "clone_and_follow" | "clone_follow_switchover"
MigrationStatus   — "created" | "running" | "streaming" | "switchover" | "completed" | "failed" | "stopped"
Migration          — 35 fields including live metrics (phase, lag, tables, rows/sec, events, error history, schema stats)
CreateMigrationRequest — source/dest cluster+node IDs, mode, fallback, slot_name, publication, copy_workers
```

## Key Design Decisions

- **Least privilege** — only requires logical replication slot + publication on source
- **Zero database footprint** — no tables, functions, or extensions installed on source/dest
- **Binary protocol everywhere** — pgoutput for decoding, binary COPY for snapshot, binary codec for replay
- **Unified Message channel** — both WAL changes and sentinels flow through same channel, no separate control channels
- **Origin tagging for bidi** — uses PG's native `pg_replication_origin` infrastructure
- **Snapshot consistency** — `SET TRANSACTION SNAPSHOT` ensures gap-free initial copy
- **Applier retry** — reconnects decoder up to 5 times with exponential backoff (2s→30s) via `runApplierWithRetry()`
- **Dest session_replication_role=replica** — disables triggers/constraints during replay

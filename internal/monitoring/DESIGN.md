# Monitoring Module

## Overview

Real-time PostgreSQL monitoring via a tiered polling architecture. Collects metrics from shared memory views at different frequencies based on query cost. Designed to never hurt the databases it monitors.

## File Map

### Backend — `internal/monitoring/`

| File | Purpose |
|------|---------|
| `types.go` | All data structures: tier configs, snapshots (Tier1/2/3), API response types, internal raw counter types |
| `collector.go` | `Collector` — top-level manager, maps clusterID→nodeMonitors, thread-safe, exposed to HTTP handlers |
| `node_monitor.go` | `nodeMonitor` — per-node goroutine with dedicated `*pgx.Conn`, runs tiered tickers, delta computation, ring buffers |
| `queries.go` | All SQL queries as `const` strings, organized by tier, with inline comments explaining cost/safety |

### Server — `internal/server/`

| File | Lines | Purpose |
|------|-------|---------|
| `monitoring.go` | full file | `monitoringHandlers` struct — 7 HTTP handlers for start/stop/status/overview/tables/sizes/refresh |
| `server.go` | ~68-71, ~134-143 | `SetMonitoringCollector()` wiring + route registration (guarded by `s.monitoring != nil`) |

### Frontend — `web/src/`

| File | Purpose |
|------|---------|
| `types/monitoring.ts` | TypeScript mirrors of all Go types — 1:1 match with `types.go` |
| `api/client.ts` | 7 fetch functions: `fetchMonitoringOverview`, `fetchNodeTableStats`, `fetchNodeSizes`, `refreshNodeSizes`, `startMonitoring`, `stopMonitoring`, `fetchMonitoringStatus` |
| `hooks/useMonitoring.ts` | `useMonitoring(clusterId)` — polls Tier 1 at 2s, Tier 2 at 30s per node |
| `pages/MonitoringPage.tsx` | Main dashboard — cluster selector, start/stop buttons, composes all sections |
| `components/monitoring/OverviewCards.tsx` | 4 cards: Connections, TPS, Cache Hit Ratio, Replication |
| `components/monitoring/ActivitySection.tsx` | Connection states breakdown, wait events, longest running query |
| `components/monitoring/PerformanceCharts.tsx` | 4 recharts `LineChart`s: TPS, Cache Hit %, Active Queries, Block Reads/s |
| `components/monitoring/TableStatsSection.tsx` | Expandable section with 3 tabs: Tables, Indexes (with "unused" badges), Locks |
| `components/monitoring/SizesSection.tsx` | On-demand Tier 3: Sizes, Bloat, Top Queries tabs with manual Refresh button |

## Architecture

```
MonitoringListPage (React) ──────► GET /api/v1/monitoring/clusters
  │
  └── click cluster ──► navigate to /monitoring/{clusterId}

MonitoringDetailPage (React)
  │
  ├── useMonitoring hook ──(HTTP 2s)──► GET /api/v1/monitoring/{clusterId}
  │                        (HTTP 30s)──► GET /api/v1/monitoring/{clusterId}/nodes/{nodeId}/tables
  │
  └── SizesSection ───(on-demand)────► POST .../refresh-sizes + GET .../sizes
                                              │
                                              ▼
                                    server.monitoringHandlers
                                              │
                                              ▼
                                    monitoring.Collector
                                         │
                                    ┌────┴────┐
                                    ▼         ▼
                              nodeMonitor   nodeMonitor
                              (goroutine)   (goroutine)
                                    │         │
                              *pgx.Conn    *pgx.Conn
                                    │         │
                                    ▼         ▼
                                 PG Node   PG Node
```

## Tiered Polling

### Tier 1 — Lightweight (default: every 2s)

**Queries:** `pg_stat_activity`, `pg_stat_database`, `pg_stat_bgwriter`/`pg_stat_checkpointer`, `pg_stat_replication`

**Why safe:** These views read from shared memory stats structures (PgBackendStatus, PgStat_StatDBEntry), NOT shared_buffers. Zero cache impact.

**Collects:**
- `ActivitySnapshot` — connection counts by state, wait events, longest query, blocked locks count
- `DatabaseStats` — cache hit ratio, TPS, tuple rates, temp files/bytes (rates + totals), deadlocks (all as **deltas/rates**)
- `CheckpointerStats` — checkpoint rates, buffer write rates (as deltas)
- `ReplicationInfo` — replica lag (bytes + seconds), standby list for primaries
- `DatabaseHealth` — txid age %, mxid age % (compared against autovacuum freeze thresholds)

**Ring buffer:** 600 entries (~20 min history)

### Tier 2 — Medium (default: every 30s)

**Queries:** `pg_stat_user_tables`, `pg_stat_user_indexes`, `pg_locks`

**Mitigations:** Schema filter (`$1::text[]`), `LIMIT $2` (default 100/50), ORDER BY activity.

**Collects:**
- `TableStat[]` — seq scans, index usage ratio, live/dead tuples, vacuum timestamps
- `IndexStat[]` — scan counts, size (sorted ascending to surface unused indexes first)
- `LockInfo[]` — blocked lock relationships only (WHERE NOT granted)

**Ring buffer:** 60 entries (~30 min)

### Tier 3 — Heavy (default: every 5 min or on-demand)

**Queries:** `pg_total_relation_size()`, bloat estimation via `pg_stats`, `pg_stat_statements`

**Why expensive:** `pg_total_relation_size()` calls `stat()` on files and reads catalog → touches shared_buffers.

**Mitigations:** ORDER BY `relpages` (cached estimate, no I/O), LIMIT, on-demand via UI "Refresh" button.

**Collects:**
- `RelationSize[]` — total/data/index/toast bytes per table
- `BloatEstimate[]` — statistics-based estimation (no pgstattuple extension needed)
- `TopQuery[]` — from pg_stat_statements (gracefully skipped if extension not installed)

**Ring buffer:** 12 entries (~1 hr)

## Key Design Decisions

### Delta-Based Rate Computation

All cumulative counters stored as raw values. Rates computed in Go: `rate = (current - previous) / elapsed_seconds`. Handles stats resets by comparing `stats_reset` epoch — discards one sample on reset.

Cache hit ratio is computed from deltas (current interval), not lifetime averages.

### Connection Strategy

- One dedicated `*pgx.Conn` per node (not pooled)
- `application_name=pgmanager_monitoring`, `statement_timeout=5s`, `idle_in_transaction_session_timeout=10s`
- Exponential backoff on failure: 1s → 2s → 4s → ... → max 30s
- PG version detected on connect for query selection (PG17+ `pg_stat_checkpointer`)

### Scale Strategy

- All Tier 2/3 queries accept schema filter + LIMIT
- Ring buffers have fixed capacity — memory bounded regardless of collection count
- Tier 1 is O(1) w.r.t. database objects
- No automatic full-catalog scans — Tier 3 is on-demand by default

### PG Version Compatibility

- **PG12+**: Full support
- **PG17+**: Uses `pg_stat_checkpointer` instead of checkpoint columns in `pg_stat_bgwriter`
- **pg_stat_statements**: Optional — gracefully handled if not installed

### Frontend Strategy

- Tier 1: HTTP polling every 2s (not WebSocket — separate from migration real-time stream)
- Tier 2: HTTP polling every 30s, only when nodes exist in overview
- Tier 3: Fetched on-demand when user clicks "Refresh Sizes"
- History for charts maintained from Tier 1 polling responses (server-side ring buffer)
- Charts use `recharts` library (LineChart, AreaChart)
- Monitoring list page polls `/api/v1/monitoring/clusters` every 5s for live summaries
- Route: `/monitoring` = cluster listing, `/monitoring/{clusterId}` = detail dashboard

## REST API

| Method | Endpoint | Handler | Description |
|--------|----------|---------|-------------|
| GET | `/api/v1/monitoring/status` | `status` | List monitored cluster IDs |
| POST | `/api/v1/monitoring/start` | `startMonitoring` | Start monitoring a cluster (body: `{cluster_id}`) |
| POST | `/api/v1/monitoring/stop` | `stopMonitoring` | Stop monitoring a cluster (body: `{cluster_id}`) |
| GET | `/api/v1/monitoring/{clusterId}` | `overview` | Full overview: all nodes' Tier1+2+3 + history |
| GET | `/api/v1/monitoring/{clusterId}/nodes/{nodeId}/tables` | `nodeTableStats` | Tier 2 data for a node |
| GET | `/api/v1/monitoring/{clusterId}/nodes/{nodeId}/sizes` | `nodeSizes` | Tier 3 data for a node |
| POST | `/api/v1/monitoring/{clusterId}/nodes/{nodeId}/refresh-sizes` | `refreshSizes` | Trigger immediate Tier 3 collection |

## Key Types

### Go (`types.go`)

```
TierConfig                     — polling intervals per tier
ActivitySnapshot               — pg_stat_activity aggregation (incl. blocked_locks count)
DatabaseStats                  — pg_stat_database rates (deltas) + temp_files/bytes totals
CheckpointerStats              — checkpoint/bgwriter rates (deltas)
WALStats                       — WAL generation/archiving rates
ReplicationInfo + StandbyInfo  — replication state
DatabaseHealth                 — txid age %, mxid age % (vs freeze thresholds)
Tier1Snapshot                  — composite of above, timestamped
TableStat                      — per-table pg_stat_user_tables
IndexStat                      — per-index stats + size
LockInfo                       — blocked lock pairs
Tier2Snapshot                  — tables + indexes + locks
RelationSize                   — total/data/index/toast breakdown
BloatEstimate                  — statistics-based bloat
TopQuery                       — pg_stat_statements top N
Tier3Snapshot                  — sizes + bloat + top queries
NodeMonitoringSnapshot         — per-node combined state (status + all tiers)
MonitoringOverview             — per-cluster API response (nodes[] + history[])
MonitoringClusterSummary       — per-cluster summary for listing (TPS, cache, lag, health)
rawDBStats, rawCheckpointerStats — internal, for delta computation
```

### TypeScript (`types/monitoring.ts`)

1:1 mirror of all Go types above. TypeScript uses `snake_case` matching JSON tags.

## SQL Queries (`queries.go`)

| Constant | Tier | Source View | Notes |
|----------|------|-------------|-------|
| `ActivityQuery` | 1 | `pg_stat_activity` | Single-row aggregate, filters `backend_type = 'client backend'` |
| `ActivityByStateQuery` | 1 | `pg_stat_activity` | GROUP BY state |
| `ActivityByWaitEventQuery` | 1 | `pg_stat_activity` | GROUP BY wait_event_type, active only |
| `LongestQueryQuery` | 1 | `pg_stat_activity` | PID + first 200 chars of query text |
| `DatabaseStatsQuery` | 1 | `pg_stat_database` | Raw cumulative counters for delta computation |
| `CheckpointerQueryPG17` | 1 | `pg_stat_checkpointer` | PG17+ only |
| `CheckpointerQueryLegacy` | 1 | `pg_stat_bgwriter` | PG < 17 |
| `ReplicationLagQuery` | 1 | `pg_is_in_recovery()`, WAL functions | Lag in bytes + seconds |
| `StandbyInfoQuery` | 1 | `pg_stat_replication` | Connected standbys on primary |
| `BlockedLocksCountQuery` | 1 | `pg_locks` | Count of sessions blocked by other sessions |
| `TxIDAgeQuery` | 1 | `pg_database` | max(age(datfrozenxid)) vs autovacuum_freeze_max_age |
| `MxIDAgeQuery` | 1 | `pg_database` | max(mxid_age(datminmxid)) vs autovacuum_multixact_freeze_max_age |
| `TableStatsQuery` | 2 | `pg_stat_user_tables` | Schema filter + LIMIT, ordered by scan activity |
| `IndexStatsQuery` | 2 | `pg_stat_user_indexes` | Includes `pg_relation_size`, ordered ASC (unused first) |
| `LockContentionQuery` | 2 | `pg_locks` | Self-join for blocked relationships, LIMIT 50 |
| `RelationSizesQuery` | 3 | `pg_class` + size functions | ORDER BY `relpages` (cached, no I/O) |
| `BloatEstimateQuery` | 3 | `pg_stat_user_tables` + `pg_stats` | Stats-based, no pgstattuple needed, min 1000 rows |
| `TopQueriesQuery` | 3 | `pg_stat_statements` | Top N by total_exec_time, gracefully fails |

## Collector Internals (`collector.go` + `node_monitor.go`)

### Collector (thread-safe, one per daemon)

- `nodes map[string]*nodeMonitor` — keyed by nodeID
- `StartCluster(ctx, cluster)` — spawns nodeMonitor goroutines
- `StopCluster(clusterID)` — cancels contexts, removes from map
- `GetOverview(clusterID)` — aggregates all node snapshots + history
- `GetClusterSummaries()` — returns summaries for all monitored clusters (listing page)
- `GetTier2(nodeID)` / `GetTier3(nodeID)` — per-node accessors
- `RefreshTier3(ctx, nodeID)` — triggers immediate collection
- `Close()` — stops everything

### nodeMonitor (one per PG node)

- **Lifecycle:** `run()` → `connect()` (with exponential backoff) → `runTickers()` → on disconnect, reconnects
- **Tickers:** 3 independent `time.Ticker`s for each tier, immediate collection on start
- **Tier 1 failure is fatal** (disconnects) — Tier 2/3 failures are non-fatal (logged, continue)
- **Ring buffers:** Fixed-capacity `appendRing[T]` generic function, oldest items dropped
- **Delta state:** `prevDB *rawDBStats`, `prevCkpt *rawCheckpointerStats` + timestamps for rate computation
- **Status:** `"connecting"` → `"ok"` → `"error"` / `"disconnected"` (exposed in API)

## Frontend Components

### MonitoringListPage (`pages/MonitoringListPage.tsx`)

Entry point. Lists all monitored clusters with summary metrics:
- TPS, Cache Hit Ratio, Connections, Replication Lag, Blocked Locks, TXID Age %
- Warning indicators when metrics exceed thresholds
- Click navigates to `/monitoring/{clusterId}`
- Polls every 5s for live updates

### MonitoringDetailPage (`pages/MonitoringDetailPage.tsx`)

Single-cluster dashboard. Manages:
- Back navigation to cluster listing
- API-down detection with retry
- Multi-node status pills (green/red/gray dots)
- Composes: `PerformanceCharts` → `ActivitySection` → `SlowQueryLog` → `TableStatsSection` → `SizesSection`

### ActivitySection (`components/monitoring/ActivitySection.tsx`)

- Connection states bar (active/idle/idle in tx) with color coding and percentages
- Wait events breakdown (active queries only)
- Longest running query text display with PID

### PerformanceCharts (`components/monitoring/PerformanceCharts.tsx`)

7-group tabbed chart system from Tier 1 history (last 300 data points):
- **Overview**: TPS, Cache Hit %, Connection States (stacked area), Longest Query
- **Sessions**: Connection States, Wait Events (stacked by type), Blocked Locks, Longest Query
- **Throughput**: Commits/Rollbacks, Tuple Operations, Temp Files/sec, Temp Bytes/sec
- **I/O & Cache**: Block Reads, Block Hits, Deadlocks, Replication Lag
- **Health**: TXID Age %, MXID Age %, Replication Lag, Blocked Locks
- **WAL**: WAL Generated, WAL Records, Full Page Images
- **Checkpointer**: Checkpoints, Buffers Written
Shared `ChartCard` and `StackedAreaCard` helper components.

### TableStatsSection (`components/monitoring/TableStatsSection.tsx`)

Expandable section with 3 tabs from Tier 2 data:
- **Tables** — seq scans, index usage %, live/dead rows, last vacuum (with dead tuple alerts)
- **Indexes** — scan count, size, "unused" badge for zero-scan indexes
- **Locks** — waiting/blocking PID pairs with mode and relation

### SizesSection (`components/monitoring/SizesSection.tsx`)

On-demand Tier 3 section with Refresh button:
- **Sizes** — total/data/index/toast per relation
- **Bloat** — estimated bloat ratio (red >30%, amber >15%)
- **Top Queries** — from pg_stat_statements: calls, total time, mean time, hit ratio

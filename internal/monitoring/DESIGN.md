# Monitoring Module — Design Decisions

## Core Constraint

**Our monitoring must not hurt the databases it monitors.** This means:
1. No polluting shared_buffers (destroying cache hit ratio)
2. No expensive queries at high frequency
3. Must scale to customers with thousands of tables/schemas/databases

## Tiered Polling Architecture

We split metrics into three tiers based on their cost to collect:

### Tier 1 — Lightweight (default: every 2s)

**Queries:** `pg_stat_activity`, `pg_stat_database`, `pg_stat_bgwriter`/`pg_stat_checkpointer`, `pg_stat_replication`

**Why it's safe:** These PostgreSQL views read from **shared memory stats structures** maintained by the stats collector process. They do NOT access shared_buffers or read heap/index pages. Specifically:
- `pg_stat_activity` iterates the in-memory `PgBackendStatus` array
- `pg_stat_database` reads `PgStat_StatDBEntry` structs
- `pg_stat_bgwriter` reads global shared memory counters

Running these every second has **zero impact** on buffer cache.

### Tier 2 — Medium (default: every 30s)

**Queries:** `pg_stat_user_tables`, `pg_stat_user_indexes`, `pg_locks`

**Why it needs care:** While these are also shared memory views, they can return thousands of rows for large schemas. We mitigate with:
- `WHERE schemaname = ANY($1)` — optional schema filter
- `LIMIT $2` — always bounded (default 100)
- `ORDER BY` activity metrics (scans, dead tuples) so the most interesting tables come first

### Tier 3 — Heavy (default: every 5min or on-demand)

**Queries:** `pg_total_relation_size()`, bloat estimation, `pg_stat_statements`

**Why it's expensive:** `pg_total_relation_size()` calls `stat()` on relation files and reads `pg_class` catalog entries — this DOES touch shared_buffers. Bloat estimation joins `pg_stats`. We mitigate with:
- Infrequent polling (5min default) or purely on-demand (user clicks "Refresh")
- `ORDER BY relpages DESC` — `relpages` is a cached estimate in `pg_class` (no I/O), so we sort cheaply and only call the expensive size functions on the top N
- `LIMIT $2` — always bounded (default 50)

## Delta-Based Rate Computation

All cumulative counters (transactions, blocks read/hit, tuples inserted/updated/deleted, etc.) are stored as raw values. We compute rates in Go:

```
rate = (current_value - previous_value) / elapsed_seconds
```

This is critical because:
- We never ask PostgreSQL to compute rates (no `pg_stat_get_*` rate functions, no window functions over stats)
- Delta computation handles counter wraps and stats resets gracefully
- We detect `stats_reset` timestamp changes and discard one sample on reset

## Cache Hit Ratio Calculation

```
cache_hit_ratio = delta(blks_hit) / (delta(blks_hit) + delta(blks_read))
```

Computed from deltas, not absolute values. This gives the **current** cache hit ratio for the polling interval, not the lifetime average (which hides recent degradation).

## Connection Strategy

- **One dedicated `*pgx.Conn` per monitored node** (not from a pool)
- Connection settings: `statement_timeout=5s`, `idle_in_transaction_session_timeout=10s`, `application_name=pgmanager_monitoring`
- On connection failure: exponential backoff (1s, 2s, 4s... max 30s)
- PG version detected on connect (`SHOW server_version_num`) for query selection (e.g., PG17+ has `pg_stat_checkpointer` instead of checkpoint columns in `pg_stat_bgwriter`)

## Scale Strategy

For environments with many databases/schemas/relations:

1. **All Tier 2/3 queries accept schema filter + LIMIT** — the API exposes `?schemas=public,app&limit=100` parameters
2. **Ring buffers have fixed capacity** — 600 entries for T1 (~20min at 2s), 60 for T2 (~30min at 30s), 12 for T3 (~1hr at 5min). Memory is bounded regardless of collection count.
3. **Tier 1 is O(1) w.r.t. database objects** — `pg_stat_database` returns one row per database, `pg_stat_activity` is bounded by `max_connections`
4. **No automatic full-catalog scans** — Tier 3 is on-demand by default; automatic polling requires explicit opt-in

## PG Version Compatibility

- **PG12+**: Full support (pgoutput, pg_stat_activity with backend_type)
- **PG14+**: `query_id` in pg_stat_activity
- **PG17+**: `pg_stat_checkpointer` replaces checkpoint columns in `pg_stat_bgwriter`
- **pg_stat_statements**: Optional — gracefully handled if extension is not installed

## Frontend Polling Strategy

- Tier 1: HTTP polling every 2s (not WebSocket — monitoring is separate from migration real-time stream)
- Tier 2: HTTP polling every 30s, only when the tables section is expanded
- Tier 3: Fetched on-demand when user clicks "Refresh Sizes"
- History for charts maintained client-side from T1 polling responses

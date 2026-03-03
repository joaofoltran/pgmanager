# Migration Module — Design Decisions

## Core Architecture

**Message-centric pipeline**: All data flows through a single `chan stream.Message` (cap 4096). Both WAL changes and sentinel markers implement the same `Message` interface. No separate control channels.

```
Decoder (pglogrepl) → [BidiFilter] → Applier
                            ↑
                    Sentinel Coordinator (injected at exact LSN)
```

## WAL Decoding (stream/decoder.go)

- **pgoutput plugin** (PG10+) — binary protocol, zero library overhead, no wal2json dependency
- **OriginMessage tracking** — decoder extracts replication origin on each change for bidi support
- **Backpressure** — when channel fills, sends standby status updates to prevent source timeout
- **LSN confirmation** — applier confirms post-write; decoder advances slot's flushed position

## Snapshot Consistency (snapshot/snapshot.go)

- `CreateReplicationSlot` returns exported snapshot name
- All COPY workers use `SET TRANSACTION SNAPSHOT <name>` for gap-free, duplicate-free initial copy
- Work distribution via channel — workers pull tables FIFO
- Retry with exponential backoff (30min budget, base 2s, cap 5min)

## Replay/Applier (replay/applier.go)

- **INSERT coalescing** — batches consecutive INSERTs (up to 1000 rows) into multi-row INSERT
- **Transaction coalescing** — merges up to 500 WAL txns into larger dest transactions
- **Binary protocol** — pgx native binary codec (no text serialization)
- **Origin tagging** — `pg_replication_origin_session_setup()` before writes for bidi
- **Statement cache** — prepared statements cached per table+operation

## Zero-Downtime Switchover (sentinel/sentinel.go)

- **Zero database footprint** — no sentinel tables
- SentinelMessage injected into messages channel at exact LSN
- Round-trip confirmation: applier calls `Confirm(id)` → unblocks `WaitForConfirmation(id, timeout)`
- If sentinel is confirmed, all prior WAL has been applied → safe to switch

## Bidirectional Replication (bidi/bidi.go)

- Origin-based loop detection using PG's native `pg_replication_origin` infrastructure
- Filter drops messages with matching originID (echoed writes)
- No sentinel tables needed — uses PG's built-in origin tagging

## Pipeline Modes

1. **Clone** — schema + COPY, drain replication stream
2. **Clone+Follow** — schema + COPY → CDC streaming from slot LSN
3. **Resume** — detect incomplete clone, re-COPY missing tables, then CDC
4. **Follow** — CDC only from existing slot
5. **Switchover** — sentinel injection → confirmation → optional reverse replication

## Key Design Constraints

- **Least privilege** — only requires logical replication slot + publication on source
- **Zero database footprint** — no tables, functions, or extensions installed on source/dest
- **Binary protocol everywhere** — pgoutput for decoding, binary COPY for snapshot, binary codec for replay

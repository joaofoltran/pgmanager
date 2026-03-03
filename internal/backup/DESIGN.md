# Backup Module

## Overview

PostgreSQL backup management via pgBackRest orchestration. Central daemon tracks backup metadata in its internal DB, while actual backups can be created by agents, cron, or manual invocation. Supports full, differential, and incremental backup types.

## File Map

### Backend — `internal/backup/`

| File | Purpose |
|------|---------|
| `types.go` | All data structures: `Backup`, `BackupType`/`BackupStatus` enums, `StanzaConfig`, `BackupRequest`, `InfoResponse`, `BackupInfo`, `RestoreOptions` |
| `store.go` | `Store` — database-backed CRUD for backup records (pgxpool), includes `Sync()` for upserting external backups |
| `executor.go` | `Executor` — wraps pgbackrest CLI: Backup, Restore, Info, Check, StanzaCreate |
| `pgbackrest.go` | `GenerateConfig()` — produces pgbackrest.conf INI from StanzaConfig structs; `StanzaNameForCluster()` — normalizes cluster ID to safe stanza name |

### Server — `internal/server/`

| File | Lines | Purpose |
|------|-------|---------|
| `backups.go` | full file | `backupHandlers` — 6 HTTP handlers: list, get, latest, sync, remove, generateConfig |
| `server.go` | ~33, ~64, ~123-132 | `backups *backup.Store` field, `SetBackupStore()`, route registration |

### Frontend — `web/src/`

| File | Purpose |
|------|---------|
| `types/backup.ts` | TS interfaces: `Backup`, `BackupType`, `BackupStatus` |
| `api/client.ts` | 4 fetch functions: `fetchBackups`, `fetchBackup`, `fetchLatestBackup`, `removeBackup` |
| `pages/BackupPage.tsx` | Main page: cluster selector, expandable backup cards with status/type/size/WAL info, auto-refresh every 10s |

## Architecture

```
pgbackrest CLI (on PG host)
    ↓
Executor (internal/backup/executor.go)
    ↓  wraps: backup, restore, info, check, stanza-create
Store (internal/backup/store.go)      ← DB persistence (pgxpool)
    ↓  CRUD + Sync
backupHandlers (internal/server/backups.go) ← HTTP handlers
    ↓
6 REST endpoints: /api/v1/backups/*
    ↓
web/src/api/client.ts                ← 4 fetch functions
    ↓
web/src/pages/BackupPage.tsx         ← expandable card UI, auto-refresh 10s
```

### Sync Pattern (Agent Discovery)

```
Agent on PG host
  └─ pgbackrest info --output=json
     └─ POST /api/v1/backups/sync
        └─ Store.Sync(clusterID, stanza, infos)
           └─ UPSERT records + track synced_at timestamp
```

Decouples backup execution from metadata tracking — backups can be created by any means (agent, cron, manual) and the daemon discovers them.

## REST API

| Method | Endpoint | Handler | Description |
|--------|----------|---------|-------------|
| GET | `/api/v1/backups?cluster_id=` | `list` | List backups for a cluster |
| GET | `/api/v1/backups/latest?cluster_id=` | `latest` | Get latest completed backup |
| GET | `/api/v1/backups/{id}` | `get` | Get single backup by ID |
| DELETE | `/api/v1/backups/{id}` | `remove` | Delete a backup record (204) |
| POST | `/api/v1/backups/sync` | `sync` | Upsert backup metadata from agent/pgBackRest info |
| POST | `/api/v1/backups/generate-config` | `generateConfig` | Generate pgbackrest.conf from stanza configs (validates first) |

## Key Types

### Go (`types.go`)

```
BackupType       — "full", "diff", "incr"
BackupStatus     — "pending", "running", "complete", "failed"
Backup           — full record: ID, clusterID, nodeID, stanza, type, status, error, label,
                   WAL start/stop, size/delta/repo bytes, database list, duration, timestamps
StanzaConfig     — pgBackRest stanza definition: name, PG path/port/user, repo path, retention, compression
BackupRequest    — API request to initiate a backup
InfoResponse     — parsed pgBackRest info output per stanza
BackupInfo       — individual backup entry from pgBackRest info
RestoreOptions   — PITR target, backup set, delta flag
```

### TypeScript (`types/backup.ts`)

```
BackupType    — "full" | "diff" | "incr"
BackupStatus  — "pending" | "running" | "complete" | "failed"
Backup        — mirrors Go struct: id, cluster_id, node_id, stanza, type, status, error, label,
                WAL, sizes, database_list, duration, timestamps
```

## Store Operations (`store.go`)

| Method | Description |
|--------|-------------|
| `List(ctx, clusterID)` | All backups for cluster, ordered by started_at DESC |
| `Get(ctx, id)` | Single backup by ID |
| `Create(ctx, backup)` | Insert new record |
| `UpdateStatus(ctx, id, status, errMsg)` | Update status + error message |
| `Sync(ctx, clusterID, stanza, infos)` | Transactional upsert from pgBackRest info — matches by backup_label |
| `Remove(ctx, id)` | Delete record |
| `LatestByCluster(ctx, clusterID)` | Most recent completed backup |

## Executor Operations (`executor.go`)

| Method | pgBackRest Command | Description |
|--------|-------------------|-------------|
| `CheckInstalled()` | `which pgbackrest` | Verify binary in PATH |
| `StanzaCreate(ctx, stanza)` | `pgbackrest stanza-create` | Initialize stanza |
| `Backup(ctx, stanza, type)` | `pgbackrest backup --type=...` | Run backup |
| `Info(ctx, stanza)` | `pgbackrest info --output=json` | Parse backup info |
| `Check(ctx, stanza)` | `pgbackrest check` | Verify config/repo |
| `Restore(ctx, stanza, opts)` | `pgbackrest restore` | Restore with optional PITR target, set, delta |

## Config Generation (`pgbackrest.go`)

`GenerateConfig(stanzas)` produces INI format:
- `[global]` section: repo path, retention-full, retention-diff, compress-type, log-level
- `[stanza]` section: pg-path, pg-port, pg-user

`StanzaNameForCluster(clusterID)` normalizes: lowercase, replace non-alphanum with hyphens.

## Key Design Decisions

- **pgBackRest as external tool** — orchestrates rather than reimplements backup logic
- **Agentless first** — metadata sync works without agent (manual pgBackRest + API sync)
- **pgBackRest native** — no custom backup format, standard pgBackRest repos
- **Central metadata** — daemon DB is single source of truth for backup state
- **Sync model** — agent or cron creates backups; daemon discovers them via `POST /sync`
- **One stanza per cluster** — named from cluster ID (normalized)
- **Database-backed store** — all records in pgmanager's internal DB, no local state files

## Backup Types

- **Full** — complete base backup
- **Differential** — changes since last full
- **Incremental** — changes since last backup of any type

## Frontend

`BackupPage.tsx` manages:
- Cluster list fetching + selection dropdown
- API-down detection with retry
- Backup list with expandable cards showing: status icon, type label, backup label, date, duration, size, status badge
- Expanded view: stanza, type, original/repo size, WAL start/stop, started/finished dates, database list tags, error message
- Auto-refresh every 10s via `setInterval`
- Delete with confirmation

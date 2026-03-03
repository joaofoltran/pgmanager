# Backup Module — Design Decisions

## Integration Approach

**pgBackRest as external tool** — we orchestrate pgBackRest rather than reimplementing backup logic. pgBackRest is the industry standard for PostgreSQL backups with proven reliability.

- Config generation: `GenerateConfig()` builds pgbackrest.conf from `StanzaConfig` structs
- One stanza per cluster (named from cluster ID)
- Supports repo path, retention policy, compression, log levels

## Metadata Store (store.go)

**Database-backed metadata** — all backup records in a `backups` table in pgmanager's internal DB.

- Full CRUD: List, Get, Create, UpdateStatus, Remove
- Filter by cluster, get latest complete backup
- No local state files — everything in DB

## Sync Pattern

The daemon discovers backups created externally (via cron or agent) through a sync operation:

```
Agent on PG host
  └─ pgbackrest info --output=json
     └─ POST /api/v1/backups/sync
        └─ Store.Sync(clusterID, stanza, infos)
           └─ UPSERT records + track synced_at timestamp
```

This decouples backup execution from metadata tracking — backups can be created by any means (agent, cron, manual) and the daemon discovers them.

## Backup Types

- **Full** — complete base backup
- **Differential** — changes since last full
- **Incremental** — changes since last backup of any type

## Agent Model (planned)

- Stateless agent deployed on PG hosts
- Receives backup requests from central daemon
- Runs `pgbackrest backup` locally (needs filesystem access)
- Reports status back via API
- Agent holds no local state — all coordination through daemon

## Key Design Constraints

- **Agentless first** — metadata sync works without agent (manual pgBackRest + API sync)
- **pgBackRest native** — no custom backup format, standard pgBackRest repos
- **Central metadata** — daemon is the single source of truth for backup state

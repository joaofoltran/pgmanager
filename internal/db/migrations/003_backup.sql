ALTER TABLE clusters ADD COLUMN backup_path TEXT NOT NULL DEFAULT '';

CREATE TABLE backups (
    id             TEXT PRIMARY KEY,
    cluster_id     TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    node_id        TEXT NOT NULL,
    stanza         TEXT NOT NULL,
    backup_type    TEXT NOT NULL DEFAULT 'full',
    status         TEXT NOT NULL DEFAULT 'pending',
    error_message  TEXT NOT NULL DEFAULT '',

    backup_label   TEXT NOT NULL DEFAULT '',
    wal_start      TEXT NOT NULL DEFAULT '',
    wal_stop       TEXT NOT NULL DEFAULT '',
    size_bytes     BIGINT NOT NULL DEFAULT 0,
    delta_bytes    BIGINT NOT NULL DEFAULT 0,
    repo_size_bytes BIGINT NOT NULL DEFAULT 0,
    database_list  TEXT[] NOT NULL DEFAULT '{}',

    duration_ms    BIGINT NOT NULL DEFAULT 0,
    started_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,
    synced_at      TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_backups_cluster ON backups(cluster_id);
CREATE INDEX idx_backups_status ON backups(status);
CREATE INDEX idx_backups_started ON backups(started_at DESC);

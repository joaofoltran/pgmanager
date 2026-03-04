CREATE TABLE monitoring_snapshots (
    id          BIGSERIAL PRIMARY KEY,
    node_id     TEXT        NOT NULL,
    cluster_id  TEXT        NOT NULL,
    ts          TIMESTAMPTZ NOT NULL,
    snapshot    JSONB       NOT NULL
);

CREATE INDEX idx_monitoring_snapshots_node_ts
    ON monitoring_snapshots (node_id, ts DESC);

CREATE INDEX idx_monitoring_snapshots_cluster_ts
    ON monitoring_snapshots (cluster_id, ts DESC);

-- Drop the old unpartitioned monitoring_snapshots table (no production data yet).
DROP TABLE IF EXISTS monitoring_snapshots;

-- Recreate as a partitioned table: LIST by cluster_id, sub-partitioned by RANGE on ts.
-- Each cluster gets its own list partition; each list partition is range-partitioned
-- by 15-day buckets for cheap DROP PARTITION pruning.
CREATE TABLE monitoring_snapshots (
    id          BIGSERIAL    NOT NULL,
    node_id     TEXT         NOT NULL,
    cluster_id  TEXT         NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    ts          TIMESTAMPTZ  NOT NULL,
    snapshot    JSONB        NOT NULL,
    PRIMARY KEY (cluster_id, ts, id)
) PARTITION BY LIST (cluster_id);

CREATE INDEX idx_mon_snap_node_ts ON monitoring_snapshots (node_id, ts DESC);
CREATE INDEX idx_mon_snap_cluster_ts ON monitoring_snapshots (cluster_id, ts DESC);

-- Default partition catches inserts for clusters whose list partition
-- hasn't been created yet (e.g. race at startup). The partition manager
-- will create the proper partition on the next maintenance cycle and
-- rows can be moved if needed, but this prevents data loss.
CREATE TABLE mon_snap_default PARTITION OF monitoring_snapshots DEFAULT;

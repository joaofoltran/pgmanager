package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

type Store struct {
	pool    *pgxpool.Pool
	logger  zerolog.Logger
	partMgr *PartitionManager
}

func NewStore(pool *pgxpool.Pool, logger zerolog.Logger) *Store {
	return &Store{
		pool:   pool,
		logger: logger.With().Str("component", "monitoring-store").Logger(),
	}
}

func (s *Store) SetPartitionManager(pm *PartitionManager) {
	s.partMgr = pm
}

func (s *Store) Insert(ctx context.Context, snap Tier1Snapshot, clusterID string) {
	data, err := json.Marshal(snap)
	if err != nil {
		s.logger.Warn().Err(err).Msg("marshal snapshot")
		return
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO monitoring_snapshots (node_id, cluster_id, ts, snapshot)
		 VALUES ($1, $2, $3, $4)`,
		snap.NodeID, clusterID, snap.Timestamp, data,
	)
	if err != nil {
		if strings.Contains(err.Error(), "no partition") && s.partMgr != nil {
			if pErr := s.partMgr.EnsureClusterPartition(ctx, clusterID); pErr != nil {
				s.logger.Warn().Err(pErr).Str("cluster", clusterID).Msg("auto-create partition failed")
				return
			}
			_, err = s.pool.Exec(ctx,
				`INSERT INTO monitoring_snapshots (node_id, cluster_id, ts, snapshot)
				 VALUES ($1, $2, $3, $4)`,
				snap.NodeID, clusterID, snap.Timestamp, data,
			)
			if err != nil {
				s.logger.Warn().Err(err).Str("node", snap.NodeID).Msg("insert snapshot after partition create")
			}
			return
		}
		s.logger.Warn().Err(err).Str("node", snap.NodeID).Msg("insert snapshot")
	}
}

func (s *Store) Query(ctx context.Context, clusterID string, from, to time.Time, maxPoints int) ([]Tier1Snapshot, error) {
	if maxPoints <= 0 {
		maxPoints = 1000
	}

	duration := to.Sub(from)
	rows, err := s.pool.Query(ctx, buildDownsampleQuery(duration), clusterID, from, to, maxPoints)
	if err != nil {
		return nil, fmt.Errorf("query snapshots: %w", err)
	}
	defer rows.Close()

	var result []Tier1Snapshot
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		var snap Tier1Snapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			continue
		}
		result = append(result, snap)
	}
	return result, rows.Err()
}

func buildDownsampleQuery(duration time.Duration) string {
	switch {
	case duration <= 2*time.Hour:
		return `SELECT snapshot FROM monitoring_snapshots
			WHERE cluster_id = $1 AND ts >= $2 AND ts <= $3
			ORDER BY ts ASC LIMIT $4`
	case duration <= 24*time.Hour:
		return `SELECT snapshot FROM (
			SELECT snapshot, ts,
				ROW_NUMBER() OVER (
					PARTITION BY date_trunc('minute', ts) ORDER BY ts
				) AS rn
			FROM monitoring_snapshots
			WHERE cluster_id = $1 AND ts >= $2 AND ts <= $3
		) sub WHERE rn = 1 ORDER BY ts ASC LIMIT $4`
	default:
		return `SELECT snapshot FROM (
			SELECT snapshot, ts,
				ROW_NUMBER() OVER (
					PARTITION BY floor(extract(epoch FROM ts) / 14400) ORDER BY ts
				) AS rn
			FROM monitoring_snapshots
			WHERE cluster_id = $1 AND ts >= $2 AND ts <= $3
		) sub WHERE rn = 1 ORDER BY ts ASC LIMIT $4`
	}
}

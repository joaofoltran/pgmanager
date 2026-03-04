package monitoring

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

const partitionDays = 15

type PartitionManager struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
}

func NewPartitionManager(pool *pgxpool.Pool, logger zerolog.Logger) *PartitionManager {
	return &PartitionManager{
		pool:   pool,
		logger: logger.With().Str("component", "partition-mgr").Logger(),
	}
}

func sanitize(id string) string {
	return strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(id))
}

func (pm *PartitionManager) EnsureClusterPartition(ctx context.Context, clusterID string) error {
	safe := sanitize(clusterID)
	listPart := fmt.Sprintf("mon_snap_%s", safe)

	_, err := pm.pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF monitoring_snapshots
		 FOR VALUES IN (%s) PARTITION BY RANGE (ts)`,
		listPart, quoteLiteral(clusterID),
	))
	if err != nil {
		return fmt.Errorf("create list partition for %s: %w", clusterID, err)
	}

	if err := pm.ensureRangePartitions(ctx, safe); err != nil {
		return err
	}

	pm.logger.Debug().Str("cluster", clusterID).Msg("cluster partition ready")
	return nil
}

func (pm *PartitionManager) ensureRangePartitions(ctx context.Context, safeID string) error {
	now := time.Now().UTC()
	start := now.Truncate(24 * time.Hour)
	for i := 0; i < 2; i++ {
		from := start.AddDate(0, 0, i*partitionDays)
		to := from.AddDate(0, 0, partitionDays)
		partName := fmt.Sprintf("mon_snap_%s_%s", safeID, from.Format("20060102"))
		_, err := pm.pool.Exec(ctx, fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF %s
			 FOR VALUES FROM (%s) TO (%s)`,
			partName, fmt.Sprintf("mon_snap_%s", safeID),
			quoteLiteral(from.Format(time.RFC3339)),
			quoteLiteral(to.Format(time.RFC3339)),
		))
		if err != nil {
			return fmt.Errorf("create range partition %s: %w", partName, err)
		}
	}
	return nil
}

func (pm *PartitionManager) PruneOldPartitions(ctx context.Context) error {
	cutoff := time.Now().UTC().AddDate(0, 0, -partitionDays).Truncate(24 * time.Hour)

	rows, err := pm.pool.Query(ctx,
		`SELECT inhrelid::regclass::text
		 FROM pg_inherits
		 WHERE inhparent = 'monitoring_snapshots'::regclass`)
	if err != nil {
		return fmt.Errorf("list partitions: %w", err)
	}
	defer rows.Close()

	var listParts []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		listParts = append(listParts, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, lp := range listParts {
		subRows, err := pm.pool.Query(ctx,
			`SELECT c.relname,
			        pg_get_expr(c.relpartbound, c.oid)
			 FROM pg_inherits i
			 JOIN pg_class c ON c.oid = i.inhrelid
			 WHERE i.inhparent = $1::regclass`, lp)
		if err != nil {
			continue
		}
		for subRows.Next() {
			var name, bound string
			if err := subRows.Scan(&name, &bound); err != nil {
				continue
			}
			to := extractToDate(bound)
			if !to.IsZero() && to.Before(cutoff) {
				if _, err := pm.pool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", name)); err != nil {
					pm.logger.Warn().Err(err).Str("partition", name).Msg("drop old partition failed")
				} else {
					pm.logger.Info().Str("partition", name).Msg("dropped expired partition")
				}
			}
		}
		subRows.Close()
	}
	return nil
}

func (pm *PartitionManager) MaintainAll(ctx context.Context) error {
	rows, err := pm.pool.Query(ctx, `SELECT id FROM clusters`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, id := range ids {
		if err := pm.EnsureClusterPartition(ctx, id); err != nil {
			pm.logger.Warn().Err(err).Str("cluster", id).Msg("maintain partition failed")
		}
	}
	return pm.PruneOldPartitions(ctx)
}

func (pm *PartitionManager) StartMaintainer(ctx context.Context) {
	if err := pm.MaintainAll(ctx); err != nil {
		pm.logger.Warn().Err(err).Msg("initial partition maintenance failed")
	}

	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := pm.MaintainAll(ctx); err != nil {
					pm.logger.Warn().Err(err).Msg("partition maintenance failed")
				}
			}
		}
	}()
}

func extractToDate(bound string) time.Time {
	idx := strings.Index(bound, "TO ('")
	if idx < 0 {
		return time.Time{}
	}
	sub := bound[idx+5:]
	end := strings.Index(sub, "'")
	if end < 0 {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, sub[:end])
	if err != nil {
		return time.Time{}
	}
	return t
}

func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

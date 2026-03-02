package backup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) List(ctx context.Context, clusterID string) ([]Backup, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, cluster_id, node_id, stanza, backup_type, status, error_message,
		        backup_label, wal_start, wal_stop, size_bytes, delta_bytes, repo_size_bytes,
		        database_list, duration_ms, started_at, finished_at, synced_at, created_at
		 FROM backups
		 WHERE cluster_id = $1
		 ORDER BY started_at DESC NULLS LAST`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var backups []Backup
	for rows.Next() {
		b, err := scanBackup(rows)
		if err != nil {
			return nil, err
		}
		backups = append(backups, b)
	}
	if backups == nil {
		backups = []Backup{}
	}
	return backups, nil
}

func (s *Store) Get(ctx context.Context, id string) (Backup, bool, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, cluster_id, node_id, stanza, backup_type, status, error_message,
		        backup_label, wal_start, wal_stop, size_bytes, delta_bytes, repo_size_bytes,
		        database_list, duration_ms, started_at, finished_at, synced_at, created_at
		 FROM backups
		 WHERE id = $1`, id)
	if err != nil {
		return Backup{}, false, err
	}
	defer rows.Close()

	if !rows.Next() {
		return Backup{}, false, nil
	}
	b, err := scanBackup(rows)
	if err != nil {
		return Backup{}, false, err
	}
	return b, true, nil
}

func (s *Store) Create(ctx context.Context, b Backup) error {
	dbList := b.DatabaseList
	if dbList == nil {
		dbList = []string{}
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO backups (id, cluster_id, node_id, stanza, backup_type, status, error_message,
		        backup_label, wal_start, wal_stop, size_bytes, delta_bytes, repo_size_bytes,
		        database_list, duration_ms, started_at, finished_at, synced_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)`,
		b.ID, b.ClusterID, b.NodeID, b.Stanza, string(b.Type), string(b.Status), b.ErrorMessage,
		b.BackupLabel, b.WALStart, b.WALStop, b.SizeBytes, b.DeltaBytes, b.RepoSizeBytes,
		dbList, b.DurationMs, b.StartedAt, b.FinishedAt, b.SyncedAt, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("insert backup: %w", err)
	}
	return nil
}

func (s *Store) UpdateStatus(ctx context.Context, id string, status BackupStatus, errMsg string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE backups SET status = $2, error_message = $3 WHERE id = $1`,
		id, string(status), errMsg)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("backup %q not found", id)
	}
	return nil
}

func (s *Store) Sync(ctx context.Context, clusterID, stanza string, infos []BackupInfo) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()

	for _, info := range infos {
		var exists bool
		err := tx.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM backups WHERE backup_label = $1 AND cluster_id = $2)",
			info.Label, clusterID,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check backup %s: %w", info.Label, err)
		}

		if exists {
			_, err = tx.Exec(ctx,
				`UPDATE backups SET
					wal_start = $3, wal_stop = $4, size_bytes = $5, delta_bytes = $6,
					repo_size_bytes = $7, database_list = $8, synced_at = $9
				 WHERE backup_label = $1 AND cluster_id = $2`,
				info.Label, clusterID,
				info.StartWAL, info.StopWAL, info.SizeBytes, info.DeltaBytes,
				info.RepoBytes, info.DatabaseList, now)
			if err != nil {
				return fmt.Errorf("update backup %s: %w", info.Label, err)
			}
		} else {
			started := info.StartTime
			finished := info.StopTime
			duration := finished.Sub(started).Milliseconds()
			dbList := info.DatabaseList
			if dbList == nil {
				dbList = []string{}
			}
			_, err = tx.Exec(ctx,
				`INSERT INTO backups (id, cluster_id, node_id, stanza, backup_type, status,
					error_message, backup_label, wal_start, wal_stop, size_bytes, delta_bytes,
					repo_size_bytes, database_list, duration_ms, started_at, finished_at, synced_at, created_at)
				 VALUES ($1, $2, '', $3, $4, $5, '', $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
				fmt.Sprintf("%s-%s", clusterID, info.Label), clusterID, stanza,
				info.Type, string(StatusComplete),
				info.Label, info.StartWAL, info.StopWAL,
				info.SizeBytes, info.DeltaBytes, info.RepoBytes,
				dbList, duration, started, finished, now, now)
			if err != nil {
				return fmt.Errorf("insert synced backup %s: %w", info.Label, err)
			}
		}
	}

	return tx.Commit(ctx)
}

func (s *Store) Remove(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, "DELETE FROM backups WHERE id = $1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("backup %q not found", id)
	}
	return nil
}

func (s *Store) LatestByCluster(ctx context.Context, clusterID string) (Backup, bool, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, cluster_id, node_id, stanza, backup_type, status, error_message,
		        backup_label, wal_start, wal_stop, size_bytes, delta_bytes, repo_size_bytes,
		        database_list, duration_ms, started_at, finished_at, synced_at, created_at
		 FROM backups
		 WHERE cluster_id = $1 AND status = 'complete'
		 ORDER BY started_at DESC NULLS LAST
		 LIMIT 1`, clusterID)
	if err != nil {
		return Backup{}, false, err
	}
	defer rows.Close()

	if !rows.Next() {
		return Backup{}, false, nil
	}
	b, err := scanBackup(rows)
	if err != nil {
		return Backup{}, false, err
	}
	return b, true, nil
}

func scanBackup(rows pgx.Rows) (Backup, error) {
	var b Backup
	var btype, status string
	var dbList []string
	var startedAt, finishedAt, syncedAt *time.Time

	err := rows.Scan(
		&b.ID, &b.ClusterID, &b.NodeID, &b.Stanza, &btype, &status, &b.ErrorMessage,
		&b.BackupLabel, &b.WALStart, &b.WALStop, &b.SizeBytes, &b.DeltaBytes, &b.RepoSizeBytes,
		&dbList, &b.DurationMs, &startedAt, &finishedAt, &syncedAt, &b.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Backup{}, nil
		}
		return Backup{}, fmt.Errorf("scan backup: %w", err)
	}

	b.Type = BackupType(btype)
	b.Status = BackupStatus(status)
	b.DatabaseList = dbList
	b.StartedAt = startedAt
	b.FinishedAt = finishedAt
	b.SyncedAt = syncedAt

	return b, nil
}

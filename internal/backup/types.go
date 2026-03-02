package backup

import (
	"fmt"
	"strings"
	"time"
)

type BackupType string

const (
	BackupFull         BackupType = "full"
	BackupDifferential BackupType = "diff"
	BackupIncremental  BackupType = "incr"
)

type BackupStatus string

const (
	StatusPending  BackupStatus = "pending"
	StatusRunning  BackupStatus = "running"
	StatusComplete BackupStatus = "complete"
	StatusFailed   BackupStatus = "failed"
)

type Backup struct {
	ID            string       `json:"id"`
	ClusterID     string       `json:"cluster_id"`
	NodeID        string       `json:"node_id"`
	Stanza        string       `json:"stanza"`
	Type          BackupType   `json:"backup_type"`
	Status        BackupStatus `json:"status"`
	ErrorMessage  string       `json:"error_message,omitempty"`
	BackupLabel   string       `json:"backup_label,omitempty"`
	WALStart      string       `json:"wal_start,omitempty"`
	WALStop       string       `json:"wal_stop,omitempty"`
	SizeBytes     int64        `json:"size_bytes"`
	DeltaBytes    int64        `json:"delta_bytes"`
	RepoSizeBytes int64        `json:"repo_size_bytes"`
	DatabaseList  []string     `json:"database_list,omitempty"`
	DurationMs    int64        `json:"duration_ms"`
	StartedAt     *time.Time   `json:"started_at,omitempty"`
	FinishedAt    *time.Time   `json:"finished_at,omitempty"`
	SyncedAt      *time.Time   `json:"synced_at,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
}

type StanzaConfig struct {
	Name       string `json:"name"`
	PGPath     string `json:"pg_path"`
	PGPort     uint16 `json:"pg_port"`
	PGUser     string `json:"pg_user,omitempty"`
	RepoPath   string `json:"repo_path"`
	RetainFull int    `json:"retain_full,omitempty"`
	RetainDiff int    `json:"retain_diff,omitempty"`
	Compress   string `json:"compress,omitempty"`
}

func (sc StanzaConfig) Validate() error {
	var errs []string
	if sc.Name == "" {
		errs = append(errs, "stanza name is required")
	}
	if sc.PGPath == "" {
		errs = append(errs, "pg data path (pg_path) is required")
	}
	if sc.PGPort == 0 {
		errs = append(errs, "pg port is required")
	}
	if sc.RepoPath == "" {
		errs = append(errs, "repo path is required")
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid stanza config: %s", strings.Join(errs, "; "))
	}
	return nil
}

type BackupRequest struct {
	ClusterID string     `json:"cluster_id"`
	NodeID    string     `json:"node_id"`
	Stanza    string     `json:"stanza"`
	Type      BackupType `json:"backup_type"`
}

type InfoResponse struct {
	Stanza  string       `json:"stanza"`
	Status  string       `json:"status"`
	Backups []BackupInfo `json:"backups"`
}

type BackupInfo struct {
	Label        string    `json:"label"`
	Type         string    `json:"type"`
	StartWAL     string    `json:"start_wal"`
	StopWAL      string    `json:"stop_wal"`
	SizeBytes    int64     `json:"size_bytes"`
	DeltaBytes   int64     `json:"delta_bytes"`
	RepoBytes    int64     `json:"repo_bytes"`
	DatabaseList []string  `json:"database_list"`
	StartTime    time.Time `json:"start_time"`
	StopTime     time.Time `json:"stop_time"`
}

package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

type Executor struct {
	binary string
}

func NewExecutor() *Executor {
	return &Executor{binary: "pgbackrest"}
}

func (e *Executor) CheckInstalled() error {
	_, err := exec.LookPath(e.binary)
	if err != nil {
		return fmt.Errorf("pgbackrest not found in PATH: %w", err)
	}
	return nil
}

func (e *Executor) StanzaCreate(ctx context.Context, stanza string) (string, error) {
	out, err := e.run(ctx, "stanza-create", "--stanza", stanza)
	if err != nil {
		return string(out), fmt.Errorf("stanza-create failed: %w", err)
	}
	return string(out), nil
}

func (e *Executor) Backup(ctx context.Context, stanza string, backupType BackupType) (string, error) {
	args := []string{"backup", "--stanza", stanza, "--type", string(backupType)}
	out, err := e.run(ctx, args...)
	if err != nil {
		return string(out), fmt.Errorf("backup failed: %w", err)
	}
	return string(out), nil
}

func (e *Executor) Info(ctx context.Context, stanza string) ([]InfoResponse, error) {
	out, err := e.run(ctx, "info", "--stanza", stanza, "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("info failed: %w", err)
	}

	var raw []pgBackRestInfoJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse info output: %w", err)
	}

	var results []InfoResponse
	for _, r := range raw {
		info := InfoResponse{
			Stanza: r.Name,
			Status: r.Status.Message,
		}
		for _, b := range r.Backup {
			bi := BackupInfo{
				Label:     b.Label,
				Type:      b.Type,
				SizeBytes: b.Info.Size,
				DeltaBytes: b.Info.Delta,
				RepoBytes: b.Info.Repository.Size,
			}
			if len(b.Database.List) > 0 {
				for _, db := range b.Database.List {
					bi.DatabaseList = append(bi.DatabaseList, db.Name)
				}
			}
			if b.Archive.Start != "" {
				bi.StartWAL = b.Archive.Start
			}
			if b.Archive.Stop != "" {
				bi.StopWAL = b.Archive.Stop
			}
			if b.Timestamp.Start > 0 {
				t := time.Unix(b.Timestamp.Start, 0).UTC()
				bi.StartTime = t
			}
			if b.Timestamp.Stop > 0 {
				t := time.Unix(b.Timestamp.Stop, 0).UTC()
				bi.StopTime = t
			}
			info.Backups = append(info.Backups, bi)
		}
		results = append(results, info)
	}

	return results, nil
}

func (e *Executor) Check(ctx context.Context, stanza string) (string, error) {
	out, err := e.run(ctx, "check", "--stanza", stanza)
	if err != nil {
		return string(out), fmt.Errorf("check failed: %w", err)
	}
	return string(out), nil
}

func (e *Executor) Restore(ctx context.Context, stanza string, opts RestoreOptions) (string, error) {
	args := []string{"restore", "--stanza", stanza}
	if opts.Target != "" {
		args = append(args, "--target", opts.Target)
		args = append(args, "--type", "time")
	}
	if opts.Set != "" {
		args = append(args, "--set", opts.Set)
	}
	if opts.Delta {
		args = append(args, "--delta")
	}
	out, err := e.run(ctx, args...)
	if err != nil {
		return string(out), fmt.Errorf("restore failed: %w", err)
	}
	return string(out), nil
}

func (e *Executor) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, e.binary, args...)
	return cmd.CombinedOutput()
}

type RestoreOptions struct {
	Target string `json:"target,omitempty"`
	Set    string `json:"set,omitempty"`
	Delta  bool   `json:"delta,omitempty"`
}

type pgBackRestInfoJSON struct {
	Name   string `json:"name"`
	Status struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"status"`
	Backup []pgBackRestBackupJSON `json:"backup"`
}

type pgBackRestBackupJSON struct {
	Label string `json:"label"`
	Type  string `json:"type"`
	Info  struct {
		Size       int64 `json:"size"`
		Delta      int64 `json:"delta"`
		Repository struct {
			Size  int64 `json:"size"`
			Delta int64 `json:"delta"`
		} `json:"repository"`
	} `json:"info"`
	Archive struct {
		Start string `json:"start"`
		Stop  string `json:"stop"`
	} `json:"archive"`
	Database struct {
		List []struct {
			Name string `json:"name"`
			OID  int    `json:"oid"`
		} `json:"list"`
	} `json:"database"`
	Timestamp struct {
		Start int64 `json:"start"`
		Stop  int64 `json:"stop"`
	} `json:"timestamp"`
}

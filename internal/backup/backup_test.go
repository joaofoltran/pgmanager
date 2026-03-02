package backup

import (
	"testing"
)

func TestStanzaConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  StanzaConfig
		wantErr bool
	}{
		{
			name: "valid",
			config: StanzaConfig{
				Name:     "prod",
				PGPath:   "/var/lib/postgresql/16/main",
				PGPort:   5432,
				RepoPath: "/var/lib/pgbackrest",
			},
			wantErr: false,
		},
		{
			name: "missing name",
			config: StanzaConfig{
				PGPath:   "/var/lib/postgresql/16/main",
				PGPort:   5432,
				RepoPath: "/var/lib/pgbackrest",
			},
			wantErr: true,
		},
		{
			name: "missing pg_path",
			config: StanzaConfig{
				Name:     "prod",
				PGPort:   5432,
				RepoPath: "/var/lib/pgbackrest",
			},
			wantErr: true,
		},
		{
			name: "missing port",
			config: StanzaConfig{
				Name:     "prod",
				PGPath:   "/var/lib/postgresql/16/main",
				RepoPath: "/var/lib/pgbackrest",
			},
			wantErr: true,
		},
		{
			name: "missing repo_path",
			config: StanzaConfig{
				Name:   "prod",
				PGPath: "/var/lib/postgresql/16/main",
				PGPort: 5432,
			},
			wantErr: true,
		},
		{
			name:    "all empty",
			config:  StanzaConfig{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestStanzaNameForCluster(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"prod-cluster", "prod-cluster"},
		{"My Cluster", "my-cluster"},
		{"test_cluster", "test-cluster"},
		{"UPPERCASE", "uppercase"},
		{"mixed_Case Name", "mixed-case-name"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := StanzaNameForCluster(tt.input)
			if got != tt.want {
				t.Errorf("StanzaNameForCluster(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGenerateConfig(t *testing.T) {
	stanzas := []StanzaConfig{
		{
			Name:     "prod",
			PGPath:   "/var/lib/postgresql/16/main",
			PGPort:   5432,
			PGUser:   "postgres",
			RepoPath: "/var/lib/pgbackrest",
		},
	}

	cfg := GenerateConfig(stanzas)

	mustContain := []string{
		"[global]",
		"repo1-retention-full=2",
		"compress-type=zst",
		"start-fast=y",
		"[prod]",
		"pg1-path=/var/lib/postgresql/16/main",
		"pg1-port=5432",
		"pg1-user=postgres",
		"repo1-path=/var/lib/pgbackrest",
	}

	for _, s := range mustContain {
		if !containsStr(cfg, s) {
			t.Errorf("GenerateConfig() missing %q in output:\n%s", s, cfg)
		}
	}
}

func TestGenerateConfigMultipleStanzas(t *testing.T) {
	stanzas := []StanzaConfig{
		{
			Name:       "prod",
			PGPath:     "/pgdata/prod",
			PGPort:     5432,
			RepoPath:   "/backup/prod",
			RetainFull: 3,
		},
		{
			Name:     "staging",
			PGPath:   "/pgdata/staging",
			PGPort:   5433,
			RepoPath: "/backup/staging",
			Compress: "lz4",
		},
	}

	cfg := GenerateConfig(stanzas)

	mustContain := []string{
		"[prod]",
		"pg1-path=/pgdata/prod",
		"repo1-retention-full=3",
		"[staging]",
		"pg1-port=5433",
		"compress-type=lz4",
	}

	for _, s := range mustContain {
		if !containsStr(cfg, s) {
			t.Errorf("GenerateConfig() missing %q", s, )
		}
	}
}

func TestGenerateConfigNoUser(t *testing.T) {
	stanzas := []StanzaConfig{
		{
			Name:     "test",
			PGPath:   "/pgdata",
			PGPort:   5432,
			RepoPath: "/backup",
		},
	}

	cfg := GenerateConfig(stanzas)

	if containsStr(cfg, "pg1-user=") {
		t.Errorf("GenerateConfig() should not include pg1-user when empty, got:\n%s", cfg)
	}
}

func TestBackupTypes(t *testing.T) {
	if BackupFull != "full" {
		t.Errorf("BackupFull = %q, want full", BackupFull)
	}
	if BackupDifferential != "diff" {
		t.Errorf("BackupDifferential = %q, want diff", BackupDifferential)
	}
	if BackupIncremental != "incr" {
		t.Errorf("BackupIncremental = %q, want incr", BackupIncremental)
	}
}

func TestBackupStatuses(t *testing.T) {
	if StatusPending != "pending" {
		t.Errorf("StatusPending = %q, want pending", StatusPending)
	}
	if StatusRunning != "running" {
		t.Errorf("StatusRunning = %q, want running", StatusRunning)
	}
	if StatusComplete != "complete" {
		t.Errorf("StatusComplete = %q, want complete", StatusComplete)
	}
	if StatusFailed != "failed" {
		t.Errorf("StatusFailed = %q, want failed", StatusFailed)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

package cluster

import (
	"testing"
)

func TestNodeDSN(t *testing.T) {
	tests := []struct {
		name string
		node Node
		want string
	}{
		{
			name: "full node",
			node: Node{Host: "10.0.0.1", Port: 5432, User: "admin", Password: "secret", DBName: "mydb"},
			want: "postgres://admin:secret@10.0.0.1:5432/mydb",
		},
		{
			name: "defaults applied",
			node: Node{Host: "10.0.0.1"},
			want: "postgres://postgres@10.0.0.1:5432/postgres",
		},
		{
			name: "custom port no password",
			node: Node{Host: "db.example.com", Port: 5433, User: "repl", DBName: "prod"},
			want: "postgres://repl@db.example.com:5433/prod",
		},
		{
			name: "with password",
			node: Node{Host: "db.local", Port: 5432, User: "postgres", Password: "p@ss", DBName: "test"},
			want: "postgres://postgres:p@ss@db.local:5432/test",
		},
		{
			name: "zero port defaults to 5432",
			node: Node{Host: "h", Port: 0, User: "u", DBName: "d"},
			want: "postgres://u@h:5432/d",
		},
		{
			name: "empty user defaults to postgres",
			node: Node{Host: "h", Port: 5432, User: "", DBName: "d"},
			want: "postgres://postgres@h:5432/d",
		},
		{
			name: "empty dbname defaults to postgres",
			node: Node{Host: "h", Port: 5432, User: "u", DBName: ""},
			want: "postgres://u@h:5432/postgres",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.node.DSN()
			if got != tt.want {
				t.Errorf("DSN() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateCluster(t *testing.T) {
	tests := []struct {
		name    string
		cluster Cluster
		wantErr bool
	}{
		{
			name: "valid minimal",
			cluster: Cluster{
				ID:    "prod",
				Name:  "Production",
				Nodes: []Node{{ID: "n1", Host: "h1", Port: 5432}},
			},
			wantErr: false,
		},
		{
			name: "valid multi-node",
			cluster: Cluster{
				ID:   "prod",
				Name: "Production",
				Nodes: []Node{
					{ID: "primary", Host: "h1", Port: 5432, Role: RolePrimary},
					{ID: "replica", Host: "h2", Port: 5432, Role: RoleReplica},
				},
			},
			wantErr: false,
		},
		{
			name:    "missing id is ok (auto-generated)",
			cluster: Cluster{Name: "x", Nodes: []Node{{ID: "n", Host: "h", Port: 5432}}},
			wantErr: false,
		},
		{
			name:    "missing name",
			cluster: Cluster{ID: "x", Nodes: []Node{{ID: "n", Host: "h", Port: 5432}}},
			wantErr: true,
		},
		{
			name:    "no nodes",
			cluster: Cluster{ID: "x", Name: "x"},
			wantErr: true,
		},
		{
			name:    "empty nodes slice",
			cluster: Cluster{ID: "x", Name: "x", Nodes: []Node{}},
			wantErr: true,
		},
		{
			name:    "node missing id is ok (auto-generated)",
			cluster: Cluster{ID: "x", Name: "x", Nodes: []Node{{Host: "h", Port: 5432}}},
			wantErr: false,
		},
		{
			name:    "node missing host",
			cluster: Cluster{ID: "x", Name: "x", Nodes: []Node{{ID: "n", Port: 5432}}},
			wantErr: true,
		},
		{
			name:    "node missing port",
			cluster: Cluster{ID: "x", Name: "x", Nodes: []Node{{ID: "n", Host: "h"}}},
			wantErr: true,
		},
		{
			name:    "all empty",
			cluster: Cluster{},
			wantErr: true,
		},
		{
			name: "multiple validation errors",
			cluster: Cluster{
				Nodes: []Node{{Port: 5432}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCluster(tt.cluster)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCluster() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 kB"},
		{1536, "1.5 kB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1.0 TB"},
		{5368709120, "5.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatBytes(tt.input)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestReplaceDSNDatabase(t *testing.T) {
	tests := []struct {
		name   string
		dsn    string
		dbname string
		want   string
	}{
		{
			name:   "standard dsn",
			dsn:    "postgres://user:pass@host:5432/olddb",
			dbname: "newdb",
			want:   "postgres://user:pass@host:5432/newdb",
		},
		{
			name:   "no existing db",
			dsn:    "postgres://user:pass@host:5432",
			dbname: "mydb",
			want:   "postgres://user:pass@host:5432/mydb",
		},
		{
			name:   "invalid dsn returns original",
			dsn:    "://broken",
			dbname: "mydb",
			want:   "://broken",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := replaceDSNDatabase(tt.dsn, tt.dbname)
			if got != tt.want {
				t.Errorf("replaceDSNDatabase(%q, %q) = %q, want %q", tt.dsn, tt.dbname, got, tt.want)
			}
		})
	}
}

func TestNodeRoles(t *testing.T) {
	if RolePrimary != "primary" {
		t.Errorf("RolePrimary = %q, want primary", RolePrimary)
	}
	if RoleReplica != "replica" {
		t.Errorf("RoleReplica = %q, want replica", RoleReplica)
	}
	if RoleStandby != "standby" {
		t.Errorf("RoleStandby = %q, want standby", RoleStandby)
	}
}

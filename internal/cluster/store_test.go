//go:build integration

package cluster

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func setupTestStore(t *testing.T) *Store {
	t.Helper()
	dbURL := os.Getenv("PGMANAGER_TEST_DB_URL")
	if dbURL == "" {
		t.Skip("PGMANAGER_TEST_DB_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	pool.Exec(ctx, "DROP TABLE IF EXISTS nodes")
	pool.Exec(ctx, "DROP TABLE IF EXISTS clusters")
	pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS clusters (
		id TEXT PRIMARY KEY, name TEXT NOT NULL,
		tags TEXT[] NOT NULL DEFAULT '{}',
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS nodes (
		id TEXT NOT NULL, cluster_id TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
		name TEXT NOT NULL DEFAULT '', host TEXT NOT NULL, port INTEGER NOT NULL DEFAULT 5432,
		role TEXT NOT NULL DEFAULT 'primary', username TEXT NOT NULL DEFAULT 'postgres',
		password TEXT NOT NULL DEFAULT '', dbname TEXT NOT NULL DEFAULT 'postgres',
		agent_url TEXT NOT NULL DEFAULT '', PRIMARY KEY (cluster_id, id)
	)`)

	return NewStore(pool)
}

func TestStoreAddAndGet(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	c := Cluster{
		ID:   "prod",
		Name: "Production",
		Nodes: []Node{
			{ID: "primary", Name: "pg-primary", Host: "10.0.0.1", Port: 5432, Role: RolePrimary},
		},
	}

	created, err := s.Add(ctx, c)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, ok, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("Get: cluster not found")
	}
	if got.Name != "Production" {
		t.Errorf("Name = %q, want %q", got.Name, "Production")
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("Nodes count = %d, want 1", len(got.Nodes))
	}
	if got.Nodes[0].Host != "10.0.0.1" {
		t.Errorf("Node host = %q, want %q", got.Nodes[0].Host, "10.0.0.1")
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestStoreDuplicateAdd(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	c := Cluster{
		ID:    "prod",
		Name:  "Production",
		Nodes: []Node{{ID: "n1", Host: "h1", Port: 5432}},
	}
	if _, err := s.Add(ctx, c); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := s.Add(ctx, c); err == nil {
		t.Fatal("expected error on duplicate add")
	}
}

func TestStoreList(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List: expected empty, got %d", len(list))
	}

	for _, id := range []string{"a", "b", "c"} {
		_, _ = s.Add(ctx, Cluster{
			ID:    id,
			Name:  id,
			Nodes: []Node{{ID: "n", Host: "h", Port: 5432}},
		})
	}

	list, err = s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("List: expected 3, got %d", len(list))
	}
}

func TestStoreUpdate(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	c := Cluster{
		ID:    "prod",
		Name:  "Production",
		Nodes: []Node{{ID: "n1", Host: "h1", Port: 5432}},
	}
	_, _ = s.Add(ctx, c)

	c.Name = "Production (updated)"
	if err := s.Update(ctx, c); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _, _ := s.Get(ctx, "prod")
	if got.Name != "Production (updated)" {
		t.Errorf("Name = %q, want %q", got.Name, "Production (updated)")
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be preserved after update")
	}
}

func TestStoreUpdateNotFound(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()
	err := s.Update(ctx, Cluster{ID: "nope"})
	if err == nil {
		t.Fatal("expected error on update of nonexistent cluster")
	}
}

func TestStoreRemove(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	prodC, _ := s.Add(ctx, Cluster{
		ID:    "prod",
		Name:  "Production",
		Nodes: []Node{{ID: "n1", Host: "h1", Port: 5432}},
	})

	if err := s.Remove(ctx, prodC.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, ok, _ := s.Get(ctx, prodC.ID); ok {
		t.Fatal("cluster should be removed")
	}
}

func TestStoreRemoveNotFound(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()
	if err := s.Remove(ctx, "nope"); err == nil {
		t.Fatal("expected error on remove of nonexistent cluster")
	}
}

func TestStoreAddNode(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	_, _ = s.Add(ctx, Cluster{
		ID:    "prod",
		Name:  "Production",
		Nodes: []Node{{ID: "primary", Host: "h1", Port: 5432, Role: RolePrimary}},
	})

	n := Node{ID: "replica1", Name: "pg-replica1", Host: "10.0.0.2", Port: 5432, Role: RoleReplica}
	if err := s.AddNode(ctx, "prod", n); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	c, _, _ := s.Get(ctx, "prod")
	if len(c.Nodes) != 2 {
		t.Fatalf("Nodes count = %d, want 2", len(c.Nodes))
	}
}

func TestStoreRemoveNode(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	_, _ = s.Add(ctx, Cluster{
		ID:   "prod",
		Name: "Production",
		Nodes: []Node{
			{ID: "primary", Host: "h1", Port: 5432},
			{ID: "replica", Host: "h2", Port: 5432},
		},
	})

	if err := s.RemoveNode(ctx, "prod", "replica"); err != nil {
		t.Fatalf("RemoveNode: %v", err)
	}

	c, _, _ := s.Get(ctx, "prod")
	if len(c.Nodes) != 1 {
		t.Fatalf("Nodes count = %d, want 1", len(c.Nodes))
	}
}

func TestValidateCluster(t *testing.T) {
	tests := []struct {
		name    string
		cluster Cluster
		wantErr bool
	}{
		{
			name: "valid",
			cluster: Cluster{
				ID:    "prod",
				Name:  "Production",
				Nodes: []Node{{ID: "n1", Host: "h1", Port: 5432}},
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
			name:    "node missing host",
			cluster: Cluster{ID: "x", Name: "x", Nodes: []Node{{ID: "n", Port: 5432}}},
			wantErr: true,
		},
		{
			name:    "node missing port",
			cluster: Cluster{ID: "x", Name: "x", Nodes: []Node{{ID: "n", Host: "h"}}},
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

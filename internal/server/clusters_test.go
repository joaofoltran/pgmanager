//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jfoltran/pgmanager/internal/cluster"
)

func setupClusterTest(t *testing.T) *clusterHandlers {
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

	store := cluster.NewStore(pool, nil)
	return &clusterHandlers{store: store}
}

func TestClusterHandlersCRUD(t *testing.T) {
	ch := setupClusterTest(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/clusters", ch.list)
	mux.HandleFunc("POST /api/v1/clusters", ch.add)
	mux.HandleFunc("GET /api/v1/clusters/{id}", ch.get)
	mux.HandleFunc("PUT /api/v1/clusters/{id}", ch.update)
	mux.HandleFunc("DELETE /api/v1/clusters/{id}", ch.remove)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, _ := http.Get(srv.URL + "/api/v1/clusters")
	var listed []cluster.Cluster
	json.NewDecoder(resp.Body).Decode(&listed)
	resp.Body.Close()
	if len(listed) != 0 {
		t.Fatalf("expected 0 clusters, got %d", len(listed))
	}

	body := `{"id":"prod","name":"Production","nodes":[{"id":"primary","host":"10.0.0.1","port":5432,"role":"primary"}]}`
	resp, _ = http.Post(srv.URL+"/api/v1/clusters", "application/json", bytes.NewBufferString(body))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add: expected 201, got %d", resp.StatusCode)
	}
	var added cluster.Cluster
	json.NewDecoder(resp.Body).Decode(&added)
	resp.Body.Close()
	if added.ID != "prod" {
		t.Errorf("add: ID = %q, want %q", added.ID, "prod")
	}
	if added.CreatedAt.IsZero() {
		t.Error("add: CreatedAt should not be zero")
	}

	resp, _ = http.Get(srv.URL + "/api/v1/clusters/prod")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", resp.StatusCode)
	}
	var got cluster.Cluster
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got.Name != "Production" {
		t.Errorf("get: Name = %q, want %q", got.Name, "Production")
	}

	updateBody := `{"name":"Production (v2)","nodes":[{"id":"primary","host":"10.0.0.2","port":5432,"role":"primary"}]}`
	req, _ := http.NewRequest("PUT", srv.URL+"/api/v1/clusters/prod", bytes.NewBufferString(updateBody))
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: expected 200, got %d", resp.StatusCode)
	}
	var updated cluster.Cluster
	json.NewDecoder(resp.Body).Decode(&updated)
	resp.Body.Close()
	if updated.Name != "Production (v2)" {
		t.Errorf("update: Name = %q, want %q", updated.Name, "Production (v2)")
	}

	req, _ = http.NewRequest("DELETE", srv.URL+"/api/v1/clusters/prod", nil)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", resp.StatusCode)
	}

	resp, _ = http.Get(srv.URL + "/api/v1/clusters/prod")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get deleted: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestClusterHandlersValidation(t *testing.T) {
	ch := setupClusterTest(t)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/clusters", ch.add)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := `{"id":"","name":"","nodes":[]}`
	resp, _ := http.Post(srv.URL+"/api/v1/clusters", "application/json", bytes.NewBufferString(body))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	body = `{"id":"x","name":"X","nodes":[{"id":"n","host":"h","port":5432}]}`
	http.Post(srv.URL+"/api/v1/clusters", "application/json", bytes.NewBufferString(body))
	resp, _ = http.Post(srv.URL+"/api/v1/clusters", "application/json", bytes.NewBufferString(body))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate: expected 409, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

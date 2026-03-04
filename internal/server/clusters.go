package server

import (
	"encoding/json"
	"net/http"

	"github.com/jfoltran/pgmanager/internal/cluster"
)

type clusterHandlers struct {
	store *cluster.Store
}

func (ch *clusterHandlers) list(w http.ResponseWriter, r *http.Request) {
	clusters, err := ch.store.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redacted := make([]cluster.Cluster, len(clusters))
	for i, c := range clusters {
		redacted[i] = c.Redacted()
	}
	writeJSON(w, redacted)
}

func (ch *clusterHandlers) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "cluster id required", http.StatusBadRequest)
		return
	}

	c, ok, err := ch.store.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "cluster not found", http.StatusNotFound)
		return
	}
	writeJSON(w, c.Redacted())
}

type addClusterRequest struct {
	Name       string         `json:"name"`
	Nodes      []cluster.Node `json:"nodes"`
	Tags       []string       `json:"tags,omitempty"`
	BackupPath string         `json:"backup_path,omitempty"`
}

func (ch *clusterHandlers) add(w http.ResponseWriter, r *http.Request) {
	var req addClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	c := cluster.Cluster{
		Name:       req.Name,
		Nodes:      req.Nodes,
		Tags:       req.Tags,
		BackupPath: req.BackupPath,
	}

	if err := cluster.ValidateCluster(c); err != nil {
		http.Error(w, "validation: "+err.Error(), http.StatusBadRequest)
		return
	}

	created, err := ch.store.Add(r.Context(), c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, created.Redacted())
}

func (ch *clusterHandlers) update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "cluster id required", http.StatusBadRequest)
		return
	}

	var req addClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	c := cluster.Cluster{
		ID:         id,
		Name:       req.Name,
		Nodes:      req.Nodes,
		Tags:       req.Tags,
		BackupPath: req.BackupPath,
	}

	if err := cluster.ValidateCluster(c); err != nil {
		http.Error(w, "validation: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := ch.store.Update(r.Context(), c); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	got, _, _ := ch.store.Get(r.Context(), id)
	writeJSON(w, got.Redacted())
}

func (ch *clusterHandlers) remove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "cluster id required", http.StatusBadRequest)
		return
	}

	if err := ch.store.Remove(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (ch *clusterHandlers) testConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DSN string `json:"dsn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.DSN == "" {
		http.Error(w, "dsn is required", http.StatusBadRequest)
		return
	}

	result := cluster.TestConnection(r.Context(), req.DSN)
	writeJSON(w, result)
}

func (ch *clusterHandlers) introspect(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	nodeID := r.URL.Query().Get("node")

	c, ok, err := ch.store.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "cluster not found", http.StatusNotFound)
		return
	}

	var node cluster.Node
	if nodeID != "" {
		found := false
		for _, n := range c.Nodes {
			if n.ID == nodeID {
				node = n
				found = true
				break
			}
		}
		if !found {
			http.Error(w, "node not found", http.StatusNotFound)
			return
		}
	} else {
		for _, n := range c.Nodes {
			if n.Role == cluster.RolePrimary {
				node = n
				break
			}
		}
		if node.Host == "" && len(c.Nodes) > 0 {
			node = c.Nodes[0]
		}
	}

	if node.Host == "" {
		http.Error(w, "no node available", http.StatusBadRequest)
		return
	}

	info, err := cluster.Introspect(r.Context(), node.DSN())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, info)
}

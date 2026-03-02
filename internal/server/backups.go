package server

import (
	"encoding/json"
	"net/http"

	"github.com/jfoltran/pgmanager/internal/backup"
)

type backupHandlers struct {
	store    *backup.Store
	clusters clusterGetter
}

type clusterGetter interface {
	GetBackupPath(clusterID string) (string, error)
}

func (bh *backupHandlers) list(w http.ResponseWriter, r *http.Request) {
	clusterID := r.URL.Query().Get("cluster_id")
	if clusterID == "" {
		http.Error(w, "cluster_id query parameter required", http.StatusBadRequest)
		return
	}

	backups, err := bh.store.List(r.Context(), clusterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, backups)
}

func (bh *backupHandlers) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "backup id required", http.StatusBadRequest)
		return
	}

	b, ok, err := bh.store.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "backup not found", http.StatusNotFound)
		return
	}
	writeJSON(w, b)
}

func (bh *backupHandlers) latest(w http.ResponseWriter, r *http.Request) {
	clusterID := r.URL.Query().Get("cluster_id")
	if clusterID == "" {
		http.Error(w, "cluster_id query parameter required", http.StatusBadRequest)
		return
	}

	b, ok, err := bh.store.LatestByCluster(r.Context(), clusterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "no backups found for cluster", http.StatusNotFound)
		return
	}
	writeJSON(w, b)
}

type syncBackupRequest struct {
	ClusterID string              `json:"cluster_id"`
	Stanza    string              `json:"stanza"`
	Backups   []backup.BackupInfo `json:"backups"`
}

func (bh *backupHandlers) sync(w http.ResponseWriter, r *http.Request) {
	var req syncBackupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ClusterID == "" || req.Stanza == "" {
		http.Error(w, "cluster_id and stanza are required", http.StatusBadRequest)
		return
	}

	if err := bh.store.Sync(r.Context(), req.ClusterID, req.Stanza, req.Backups); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"ok":      true,
		"synced":  len(req.Backups),
		"message": "backup metadata synced",
	})
}

func (bh *backupHandlers) remove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "backup id required", http.StatusBadRequest)
		return
	}

	if err := bh.store.Remove(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type configRequest struct {
	ClusterID string               `json:"cluster_id"`
	Stanzas   []backup.StanzaConfig `json:"stanzas"`
}

type configResponse struct {
	Config string `json:"config"`
}

func (bh *backupHandlers) generateConfig(w http.ResponseWriter, r *http.Request) {
	var req configRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Stanzas) == 0 {
		http.Error(w, "at least one stanza config required", http.StatusBadRequest)
		return
	}

	for _, sc := range req.Stanzas {
		if err := sc.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	cfg := backup.GenerateConfig(req.Stanzas)
	writeJSON(w, configResponse{Config: cfg})
}

package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jfoltran/pgmanager/internal/cluster"
	"github.com/jfoltran/pgmanager/internal/monitoring"
)

type monitoringHandlers struct {
	collector *monitoring.Collector
	clusters  *cluster.Store
}

// GET /api/v1/monitoring/{clusterId}
func (mh *monitoringHandlers) overview(w http.ResponseWriter, r *http.Request) {
	clusterID := r.PathValue("clusterId")
	if clusterID == "" {
		http.Error(w, "cluster id required", http.StatusBadRequest)
		return
	}
	overview := mh.collector.GetOverview(clusterID)
	writeJSON(w, overview)
}

// GET /api/v1/monitoring/{clusterId}/nodes/{nodeId}/tables
func (mh *monitoringHandlers) nodeTableStats(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("nodeId")
	if nodeID == "" {
		http.Error(w, "node id required", http.StatusBadRequest)
		return
	}
	data := mh.collector.GetTier2(nodeID)
	if data == nil {
		http.Error(w, "node not monitored or data not yet collected", http.StatusNotFound)
		return
	}
	writeJSON(w, data)
}

// GET /api/v1/monitoring/{clusterId}/nodes/{nodeId}/sizes
func (mh *monitoringHandlers) nodeSizes(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("nodeId")
	if nodeID == "" {
		http.Error(w, "node id required", http.StatusBadRequest)
		return
	}
	data := mh.collector.GetTier3(nodeID)
	if data == nil {
		http.Error(w, "node not monitored or data not yet collected", http.StatusNotFound)
		return
	}
	writeJSON(w, data)
}

// POST /api/v1/monitoring/{clusterId}/nodes/{nodeId}/refresh-sizes
func (mh *monitoringHandlers) refreshSizes(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("nodeId")
	if nodeID == "" {
		http.Error(w, "node id required", http.StatusBadRequest)
		return
	}
	if err := mh.collector.RefreshTier3(r.Context(), nodeID); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

type monitoringActionRequest struct {
	ClusterID string `json:"cluster_id"`
}

// POST /api/v1/monitoring/start
func (mh *monitoringHandlers) startMonitoring(w http.ResponseWriter, r *http.Request) {
	var req monitoringActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ClusterID == "" {
		http.Error(w, "cluster_id required", http.StatusBadRequest)
		return
	}

	cl, ok, err := mh.clusters.Get(r.Context(), req.ClusterID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "cluster not found", http.StatusNotFound)
		return
	}

	if err := mh.collector.StartCluster(context.Background(), cl); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{"ok": true, "cluster_id": req.ClusterID})
}

// POST /api/v1/monitoring/stop
func (mh *monitoringHandlers) stopMonitoring(w http.ResponseWriter, r *http.Request) {
	var req monitoringActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ClusterID == "" {
		http.Error(w, "cluster_id required", http.StatusBadRequest)
		return
	}

	mh.collector.StopCluster(req.ClusterID)
	writeJSON(w, map[string]any{"ok": true, "cluster_id": req.ClusterID})
}

// GET /api/v1/monitoring/status
func (mh *monitoringHandlers) status(w http.ResponseWriter, r *http.Request) {
	ids := mh.collector.MonitoredClusterIDs()
	writeJSON(w, map[string]any{"monitored_clusters": ids})
}

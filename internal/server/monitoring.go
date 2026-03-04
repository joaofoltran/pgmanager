package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmanager/internal/cluster"
	"github.com/jfoltran/pgmanager/internal/monitoring"
)

type monitoringHandlers struct {
	collector *monitoring.Collector
	clusters  *cluster.Store
	logger    zerolog.Logger
}

// GET /api/v1/monitoring/{clusterId}
func (mh *monitoringHandlers) overview(w http.ResponseWriter, r *http.Request) {
	clusterID := r.PathValue("clusterId")
	if clusterID == "" {
		http.Error(w, "cluster id required", http.StatusBadRequest)
		return
	}

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	if fromStr != "" && toStr != "" {
		from, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			http.Error(w, "invalid 'from' param: "+err.Error(), http.StatusBadRequest)
			return
		}
		to, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			http.Error(w, "invalid 'to' param: "+err.Error(), http.StatusBadRequest)
			return
		}
		overview, err := mh.collector.GetOverviewWithRange(r.Context(), clusterID, from, to)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, overview)
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

// GET /api/v1/monitoring/{clusterId}/nodes/{nodeId}/slow-queries
func (mh *monitoringHandlers) slowQueries(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("nodeId")
	if nodeID == "" {
		http.Error(w, "node id required", http.StatusBadRequest)
		return
	}
	data := mh.collector.GetSlowQueries(nodeID)
	if data == nil {
		data = []monitoring.SlowQueryEntry{}
	}
	writeJSON(w, data)
}

type toggleMonitoringRequest struct {
	ClusterID string `json:"cluster_id"`
	NodeID    string `json:"node_id"`
	Enabled   bool   `json:"enabled"`
}

// POST /api/v1/monitoring/toggle
func (mh *monitoringHandlers) toggleMonitoring(w http.ResponseWriter, r *http.Request) {
	var req toggleMonitoringRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ClusterID == "" || req.NodeID == "" {
		http.Error(w, "cluster_id and node_id required", http.StatusBadRequest)
		return
	}

	mh.logger.Debug().
		Str("cluster_id", req.ClusterID).
		Str("node_id", req.NodeID).
		Bool("enabled", req.Enabled).
		Msg("toggle monitoring request")

	if err := mh.clusters.SetNodeMonitoring(r.Context(), req.ClusterID, req.NodeID, req.Enabled); err != nil {
		mh.logger.Error().Err(err).Str("cluster_id", req.ClusterID).Str("node_id", req.NodeID).Msg("SetNodeMonitoring failed")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if req.Enabled {
		cl, ok, err := mh.clusters.Get(r.Context(), req.ClusterID)
		if err != nil {
			mh.logger.Error().Err(err).Str("cluster_id", req.ClusterID).Msg("clusters.Get failed after toggle")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			mh.logger.Warn().Str("cluster_id", req.ClusterID).Msg("cluster not found after toggle")
			http.Error(w, "cluster not found", http.StatusNotFound)
			return
		}
		mh.logger.Debug().
			Str("cluster_id", cl.ID).
			Str("cluster_name", cl.Name).
			Int("node_count", len(cl.Nodes)).
			Msg("starting node monitor")
		if err := mh.collector.StartNode(context.Background(), cl, req.NodeID); err != nil {
			mh.logger.Error().Err(err).Str("node_id", req.NodeID).Msg("StartNode failed")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		mh.collector.StopNode(req.NodeID)
	}

	mh.logger.Debug().
		Str("node_id", req.NodeID).
		Bool("enabled", req.Enabled).
		Msg("toggle monitoring complete")
	writeJSON(w, map[string]any{"ok": true, "node_id": req.NodeID, "enabled": req.Enabled})
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

// GET /api/v1/monitoring/clusters
func (mh *monitoringHandlers) listMonitoredClusters(w http.ResponseWriter, r *http.Request) {
	summaries := mh.collector.GetClusterSummaries()
	if summaries == nil {
		summaries = []monitoring.MonitoringClusterSummary{}
	}
	writeJSON(w, summaries)
}

package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jfoltran/pgmanager/internal/metrics"
	ms "github.com/jfoltran/pgmanager/internal/migrationstore"
)

type migrationHandlers struct {
	store  *ms.Store
	runner *ms.Runner
}

func (mh *migrationHandlers) list(w http.ResponseWriter, r *http.Request) {
	migrations, err := mh.store.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, migrations)
}

func (mh *migrationHandlers) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "migration id required", http.StatusBadRequest)
		return
	}

	m, ok, err := mh.store.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "migration not found", http.StatusNotFound)
		return
	}

	if mh.runner != nil {
		if snap := mh.runner.MetricsSnapshot(id); snap != nil {
			resp := struct {
				ms.Migration
				LivePhase        string                 `json:"live_phase,omitempty"`
				LiveLSN          string                 `json:"live_lsn,omitempty"`
				LiveTables       int                    `json:"live_tables_total,omitempty"`
				LiveCopied       int                    `json:"live_tables_copied,omitempty"`
				LiveTablesList   []metrics.TableProgress `json:"live_tables,omitempty"`
				LiveRowsPerSec   float64                `json:"live_rows_per_sec,omitempty"`
				LiveBytesPerSec  float64                `json:"live_bytes_per_sec,omitempty"`
				LiveTotalRows    int64                  `json:"live_total_rows,omitempty"`
				LiveTotalBytes   int64                  `json:"live_total_bytes,omitempty"`
				LiveLagBytes     uint64                 `json:"live_lag_bytes,omitempty"`
				LiveLagFormatted string                 `json:"live_lag_formatted,omitempty"`
				LiveEvents       []metrics.MigrationEvent `json:"live_events,omitempty"`
				LivePhases       []metrics.PhaseEntry   `json:"live_phases,omitempty"`
				LiveErrorHistory []metrics.ErrorEntry   `json:"live_error_history,omitempty"`
				LiveSchemaStats  *metrics.SchemaStats   `json:"live_schema_stats,omitempty"`
				LiveErrorCount   int                    `json:"live_error_count,omitempty"`
				LiveElapsedSec   float64                `json:"live_elapsed_sec,omitempty"`
			}{
				Migration:        m,
				LivePhase:        snap.Phase,
				LiveLSN:          snap.AppliedLSN,
				LiveTables:       snap.TablesTotal,
				LiveCopied:       snap.TablesCopied,
				LiveTablesList:   snap.Tables,
				LiveRowsPerSec:   snap.RowsPerSec,
				LiveBytesPerSec:  snap.BytesPerSec,
				LiveTotalRows:    snap.TotalRows,
				LiveTotalBytes:   snap.TotalBytes,
				LiveLagBytes:     snap.LagBytes,
				LiveLagFormatted: snap.LagFormatted,
				LiveEvents:       snap.Events,
				LivePhases:       snap.Phases,
				LiveErrorHistory: snap.ErrorHistory,
				LiveSchemaStats:  snap.SchemaStats,
				LiveErrorCount:   snap.ErrorCount,
				LiveElapsedSec:   snap.ElapsedSec,
			}
			writeJSON(w, resp)
			return
		}
	}

	writeJSON(w, m)
}

type createMigrationRequest struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	SourceClusterID string  `json:"source_cluster_id"`
	DestClusterID   string  `json:"dest_cluster_id"`
	SourceNodeID    string  `json:"source_node_id"`
	DestNodeID      string  `json:"dest_node_id"`
	Mode            ms.Mode `json:"mode"`
	Fallback        bool    `json:"fallback"`
	SlotName        string  `json:"slot_name,omitempty"`
	Publication     string  `json:"publication,omitempty"`
	CopyWorkers     int     `json:"copy_workers,omitempty"`
}

func (mh *migrationHandlers) create(w http.ResponseWriter, r *http.Request) {
	var req createMigrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	m := ms.Migration{
		ID:              req.ID,
		Name:            req.Name,
		SourceClusterID: req.SourceClusterID,
		DestClusterID:   req.DestClusterID,
		SourceNodeID:    req.SourceNodeID,
		DestNodeID:      req.DestNodeID,
		Mode:            req.Mode,
		Fallback:        req.Fallback,
		SlotName:        req.SlotName,
		Publication:     req.Publication,
		CopyWorkers:     req.CopyWorkers,
	}

	if m.SlotName == "" {
		m.SlotName = "pgmanager_" + strings.ReplaceAll(m.ID, "-", "_")
	}
	if m.Publication == "" {
		m.Publication = "pgmanager_pub_" + strings.ReplaceAll(m.ID, "-", "_")
	}
	if m.CopyWorkers <= 0 {
		m.CopyWorkers = 4
	}

	if err := ms.ValidateMigration(m); err != nil {
		http.Error(w, "validation: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := mh.store.Create(r.Context(), m); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	got, _, _ := mh.store.Get(r.Context(), m.ID)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, got)
}

func (mh *migrationHandlers) remove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "migration id required", http.StatusBadRequest)
		return
	}

	force := r.URL.Query().Get("force") == "true"

	if mh.runner != nil && mh.runner.IsRunning(id) {
		if !force {
			http.Error(w, "cannot delete a running migration (use ?force=true)", http.StatusConflict)
			return
		}
		mh.runner.Stop(r.Context(), id)
	}

	if err := mh.store.Delete(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (mh *migrationHandlers) start(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if mh.runner == nil {
		http.Error(w, "migration runner not configured", http.StatusServiceUnavailable)
		return
	}

	if err := mh.runner.Start(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, map[string]any{"ok": true, "message": "migration started"})
}

func (mh *migrationHandlers) stop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if mh.runner == nil {
		http.Error(w, "migration runner not configured", http.StatusServiceUnavailable)
		return
	}

	if err := mh.runner.Stop(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, map[string]any{"ok": true, "message": "stop requested"})
}

func (mh *migrationHandlers) switchover(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if mh.runner == nil {
		http.Error(w, "migration runner not configured", http.StatusServiceUnavailable)
		return
	}

	if err := mh.runner.Switchover(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, map[string]any{"ok": true, "message": "switchover started"})
}

func (mh *migrationHandlers) logs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if mh.runner == nil {
		http.Error(w, "migration runner not configured", http.StatusServiceUnavailable)
		return
	}

	logs := mh.runner.Logs(id)
	if logs == nil {
		logs = []metrics.LogEntry{}
	}
	writeJSON(w, logs)
}

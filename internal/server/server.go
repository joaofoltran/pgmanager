package server

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"

	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmanager/internal/backup"
	"github.com/jfoltran/pgmanager/internal/cluster"
	"github.com/jfoltran/pgmanager/internal/config"
	"github.com/jfoltran/pgmanager/internal/daemon"
	"github.com/jfoltran/pgmanager/internal/metrics"
	ms "github.com/jfoltran/pgmanager/internal/migrationstore"
	"github.com/jfoltran/pgmanager/internal/monitoring"
)

// Server is the HTTP server that serves the REST API, WebSocket endpoint,
// and embedded frontend static files.
type Server struct {
	collector *metrics.Collector
	cfg       *config.Config
	logger    zerolog.Logger
	hub       *Hub
	jobs       *daemon.JobManager
	clusters   *cluster.Store
	migStore   *ms.Store
	migRunner  *ms.Runner
	backups    *backup.Store
	monitoring *monitoring.Collector
	srv        *http.Server
}

// New creates a new Server. Collector and cfg may be nil for lightweight (serve) mode.
func New(collector *metrics.Collector, cfg *config.Config, logger zerolog.Logger) *Server {
	return &Server{
		collector: collector,
		cfg:       cfg,
		logger:    logger.With().Str("component", "http-server").Logger(),
	}
}

// SetJobManager attaches a job manager for daemon mode.
func (s *Server) SetJobManager(jm *daemon.JobManager) {
	s.jobs = jm
}

// SetClusterStore attaches a cluster store for multi-cluster management.
func (s *Server) SetClusterStore(cs *cluster.Store) {
	s.clusters = cs
}

// SetMigrationStore attaches the migration store and runner.
func (s *Server) SetMigrationStore(store *ms.Store, runner *ms.Runner) {
	s.migStore = store
	s.migRunner = runner
}

// SetBackupStore attaches a backup store for backup management.
func (s *Server) SetBackupStore(bs *backup.Store) {
	s.backups = bs
}

// SetMonitoringCollector attaches a monitoring collector.
func (s *Server) SetMonitoringCollector(mc *monitoring.Collector) {
	s.monitoring = mc
}

// Start begins serving on the given address and port. It blocks until the context is cancelled.
func (s *Server) Start(ctx context.Context, listen string, port int) error {
	mux := http.NewServeMux()

	// Metrics & WebSocket routes (requires collector).
	if s.collector != nil {
		h := &handlers{collector: s.collector, cfg: s.cfg}
		mux.HandleFunc("GET /api/v1/status", h.status)
		mux.HandleFunc("GET /api/v1/tables", h.tables)
		mux.HandleFunc("GET /api/v1/config", h.configHandler)
		mux.HandleFunc("GET /api/v1/logs", h.logs)
		s.hub = newHub(s.collector, s.logger)
		mux.HandleFunc("/api/v1/ws", s.hub.handleWS)
	}

	// Job control routes (requires job manager).
	if s.jobs != nil {
		jh := &jobHandlers{jobs: s.jobs}
		mux.HandleFunc("POST /api/v1/jobs/clone", jh.submitClone)
		mux.HandleFunc("POST /api/v1/jobs/follow", jh.submitFollow)
		mux.HandleFunc("POST /api/v1/jobs/switchover", jh.submitSwitchover)
		mux.HandleFunc("POST /api/v1/jobs/stop", jh.stopJob)
		mux.HandleFunc("GET /api/v1/jobs/status", jh.jobStatus)
	}

	// Cluster management routes (always available).
	if s.clusters != nil {
		ch := &clusterHandlers{store: s.clusters}
		mux.HandleFunc("GET /api/v1/clusters", ch.list)
		mux.HandleFunc("POST /api/v1/clusters", ch.add)
		mux.HandleFunc("GET /api/v1/clusters/{id}", ch.get)
		mux.HandleFunc("PUT /api/v1/clusters/{id}", ch.update)
		mux.HandleFunc("DELETE /api/v1/clusters/{id}", ch.remove)
		mux.HandleFunc("POST /api/v1/clusters/test-connection", ch.testConnection)
		mux.HandleFunc("GET /api/v1/clusters/{id}/introspect", ch.introspect)
	}

	// Migration routes.
	if s.migStore != nil {
		mh := &migrationHandlers{store: s.migStore, runner: s.migRunner}
		mux.HandleFunc("GET /api/v1/migrations", mh.list)
		mux.HandleFunc("POST /api/v1/migrations", mh.create)
		mux.HandleFunc("GET /api/v1/migrations/{id}", mh.get)
		mux.HandleFunc("DELETE /api/v1/migrations/{id}", mh.remove)
		mux.HandleFunc("POST /api/v1/migrations/{id}/start", mh.start)
		mux.HandleFunc("POST /api/v1/migrations/{id}/stop", mh.stop)
		mux.HandleFunc("POST /api/v1/migrations/{id}/switchover", mh.switchover)
		mux.HandleFunc("GET /api/v1/migrations/{id}/logs", mh.logs)
	}

	// Backup routes.
	if s.backups != nil {
		bh := &backupHandlers{store: s.backups}
		mux.HandleFunc("GET /api/v1/backups", bh.list)
		mux.HandleFunc("GET /api/v1/backups/latest", bh.latest)
		mux.HandleFunc("GET /api/v1/backups/{id}", bh.get)
		mux.HandleFunc("DELETE /api/v1/backups/{id}", bh.remove)
		mux.HandleFunc("POST /api/v1/backups/sync", bh.sync)
		mux.HandleFunc("POST /api/v1/backups/generate-config", bh.generateConfig)
	}

	// Monitoring routes.
	if s.monitoring != nil && s.clusters != nil {
		moh := &monitoringHandlers{collector: s.monitoring, clusters: s.clusters}
		mux.HandleFunc("GET /api/v1/monitoring/status", moh.status)
		mux.HandleFunc("POST /api/v1/monitoring/start", moh.startMonitoring)
		mux.HandleFunc("POST /api/v1/monitoring/stop", moh.stopMonitoring)
		mux.HandleFunc("GET /api/v1/monitoring/{clusterId}", moh.overview)
		mux.HandleFunc("GET /api/v1/monitoring/{clusterId}/nodes/{nodeId}/tables", moh.nodeTableStats)
		mux.HandleFunc("GET /api/v1/monitoring/{clusterId}/nodes/{nodeId}/sizes", moh.nodeSizes)
		mux.HandleFunc("POST /api/v1/monitoring/{clusterId}/nodes/{nodeId}/refresh-sizes", moh.refreshSizes)
	}

	// Serve embedded frontend with SPA fallback.
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return fmt.Errorf("embed fs: %w", err)
	}
	mux.Handle("/", spaHandler(http.FS(sub)))

	addr := fmt.Sprintf("%s:%d", listen, port)
	s.srv = &http.Server{
		Addr:    addr,
		Handler: mux,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
	}

	// Start WebSocket hub only if collector is available.
	if s.hub != nil {
		go s.hub.start(ctx)
	}

	s.logger.Info().Str("addr", addr).Msg("starting HTTP server")

	errCh := make(chan error, 1)
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		return s.srv.Close()
	case err := <-errCh:
		return err
	}
}

// StartBackground starts the server in a goroutine (non-blocking).
func (s *Server) StartBackground(ctx context.Context, listen string, port int) {
	go func() {
		if err := s.Start(ctx, listen, port); err != nil {
			s.logger.Err(err).Msg("http server error")
		}
	}()
}

func spaHandler(fsys http.FileSystem) http.Handler {
	fileServer := http.FileServer(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path != "/" && !strings.HasPrefix(path, "/api/") {
			f, err := fsys.Open(path)
			if err != nil {
				r.URL.Path = "/"
				fileServer.ServeHTTP(w, r)
				return
			}
			f.Close()
		}
		fileServer.ServeHTTP(w, r)
	})
}

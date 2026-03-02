package migrationstore

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmanager/internal/cluster"
	"github.com/jfoltran/pgmanager/internal/config"
	"github.com/jfoltran/pgmanager/internal/metrics"
	"github.com/jfoltran/pgmanager/internal/migration/pipeline"
)

type Runner struct {
	store    *Store
	clusters *cluster.Store
	logger   zerolog.Logger
	ctx      context.Context

	mu       sync.Mutex
	running  map[string]*runningJob
}

type runningJob struct {
	pipeline     *pipeline.Pipeline
	cancel       context.CancelFunc
	switchedOver bool
	done         chan struct{}
}

func NewRunner(ctx context.Context, store *Store, clusters *cluster.Store, logger zerolog.Logger) *Runner {
	return &Runner{
		store:    store,
		clusters: clusters,
		logger:   logger.With().Str("component", "migration-runner").Logger(),
		ctx:      ctx,
		running:  make(map[string]*runningJob),
	}
}

func (r *Runner) RecoverStale(ctx context.Context) error {
	migrations, err := r.store.List(ctx)
	if err != nil {
		return fmt.Errorf("list migrations for recovery: %w", err)
	}
	for _, m := range migrations {
		if m.Status == StatusRunning || m.Status == StatusStreaming {
			r.mu.Lock()
			_, live := r.running[m.ID]
			r.mu.Unlock()
			if !live {
				r.logger.Warn().Str("migration", m.ID).Str("status", string(m.Status)).
					Msg("recovering stale migration — marking as failed")
				r.store.UpdateStatus(ctx, m.ID, StatusFailed, "stale", "process was interrupted")
			}
		}
	}
	return nil
}

func (r *Runner) Start(ctx context.Context, migrationID string) error {
	m, ok, err := r.store.Get(ctx, migrationID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("migration %q not found", migrationID)
	}
	if m.Status == StatusRunning || m.Status == StatusStreaming {
		return fmt.Errorf("migration %q is already running", migrationID)
	}

	srcCluster, ok, err := r.clusters.Get(ctx, m.SourceClusterID)
	if err != nil {
		return fmt.Errorf("get source cluster: %w", err)
	}
	if !ok {
		return fmt.Errorf("source cluster %q not found", m.SourceClusterID)
	}

	dstCluster, ok, err := r.clusters.Get(ctx, m.DestClusterID)
	if err != nil {
		return fmt.Errorf("get dest cluster: %w", err)
	}
	if !ok {
		return fmt.Errorf("destination cluster %q not found", m.DestClusterID)
	}

	srcNode := findNode(srcCluster.Nodes, m.SourceNodeID)
	if srcNode == nil {
		return fmt.Errorf("source node %q not found in cluster %q", m.SourceNodeID, m.SourceClusterID)
	}
	dstNode := findNode(dstCluster.Nodes, m.DestNodeID)
	if dstNode == nil {
		return fmt.Errorf("dest node %q not found in cluster %q", m.DestNodeID, m.DestClusterID)
	}

	cfg := &config.Config{}
	cfg.Source.ParseURI(srcNode.DSN())
	cfg.Dest.ParseURI(dstNode.DSN())
	cfg.Replication.SlotName = m.SlotName
	cfg.Replication.Publication = m.Publication
	cfg.Replication.OutputPlugin = "pgoutput"
	cfg.Snapshot.Workers = m.CopyWorkers

	r.mu.Lock()
	if _, exists := r.running[migrationID]; exists {
		r.mu.Unlock()
		return fmt.Errorf("migration %q is already running", migrationID)
	}

	logWriter := metrics.NewLogWriter(nil)
	pipelineLogger := zerolog.New(zerolog.MultiLevelWriter(r.logger, logWriter)).
		With().Timestamp().Str("migration", migrationID).Logger()
	p := pipeline.New(cfg, pipelineLogger)
	logWriter.SetCollector(p.Metrics)

	jobCtx, cancel := context.WithCancel(r.ctx)
	r.running[migrationID] = &runningJob{pipeline: p, cancel: cancel, done: make(chan struct{})}
	r.mu.Unlock()

	if err := r.store.UpdateStatus(ctx, migrationID, StatusRunning, "initializing", ""); err != nil {
		cancel()
		r.mu.Lock()
		delete(r.running, migrationID)
		r.mu.Unlock()
		return err
	}

	r.logger.Info().
		Str("migration", migrationID).
		Str("mode", string(m.Mode)).
		Str("source", m.SourceClusterID+"/"+m.SourceNodeID).
		Str("dest", m.DestClusterID+"/"+m.DestNodeID).
		Msg("starting migration")

	go r.run(jobCtx, migrationID, m.Mode, p)

	return nil
}

func (r *Runner) Stop(ctx context.Context, migrationID string) error {
	r.mu.Lock()
	job, ok := r.running[migrationID]
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("migration %q is not running", migrationID)
	}

	job.cancel()
	return nil
}

func (r *Runner) Switchover(ctx context.Context, migrationID string) error {
	r.mu.Lock()
	job, ok := r.running[migrationID]
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("migration %q is not running", migrationID)
	}

	m, ok, err := r.store.Get(ctx, migrationID)
	if err != nil || !ok {
		return fmt.Errorf("migration %q not found", migrationID)
	}

	r.store.UpdateStatus(ctx, migrationID, StatusSwitchover, "switchover", "")

	go func() {
		bgCtx := context.Background()

		if err := job.pipeline.RunSwitchover(bgCtx, 30*time.Second); err != nil {
			r.logger.Err(err).Str("migration", migrationID).Msg("switchover failed")
			r.store.UpdateStatus(bgCtx, migrationID, StatusFailed, "switchover_failed", err.Error())
			return
		}

		if m.Fallback {
			r.logger.Info().Str("migration", migrationID).Msg("setting up reverse replication for fallback")
			reverseSlot, reverseLSN, err := job.pipeline.SetupReverseReplication(bgCtx)
			if err != nil {
				r.logger.Err(err).Str("migration", migrationID).Msg("reverse replication setup failed")
				r.store.UpdateStatus(bgCtx, migrationID, StatusCompleted, "switchover_complete", "reverse replication setup failed: "+err.Error())
				r.cleanup(migrationID)
				return
			}

			r.mu.Lock()
			job.switchedOver = true
			r.mu.Unlock()

			job.cancel()
			<-job.done

			if err := job.pipeline.DropForwardSlot(bgCtx); err != nil {
				r.logger.Warn().Err(err).Str("migration", migrationID).Msg("failed to drop forward slot (non-fatal)")
			} else {
				r.logger.Info().Str("migration", migrationID).Msg("dropped forward replication slot")
			}

			r.store.UpdateStatus(bgCtx, migrationID, StatusCompleted, "switchover_complete", "")
			r.cleanup(migrationID)

			reverseID := migrationID + "-reverse"
			reverseMigration := Migration{
				ID:              reverseID,
				Name:            m.Name + " (reverse)",
				SourceClusterID: m.DestClusterID,
				DestClusterID:   m.SourceClusterID,
				SourceNodeID:    m.DestNodeID,
				DestNodeID:      m.SourceNodeID,
				Mode:            ModeCloneAndFollow,
				SlotName:        reverseSlot,
				Publication:     m.Publication + "_reverse",
				CopyWorkers:     m.CopyWorkers,
			}
			if err := r.store.Create(bgCtx, reverseMigration); err != nil {
				r.logger.Err(err).Str("migration", reverseID).Msg("failed to create reverse migration record")
				return
			}

			r.startReverse(reverseID, reverseMigration, reverseLSN)
			return
		}

		r.mu.Lock()
		job.switchedOver = true
		r.mu.Unlock()

		job.cancel()
		<-job.done

		r.store.UpdateStatus(bgCtx, migrationID, StatusCompleted, "switchover_complete", "")
		r.cleanup(migrationID)
	}()

	return nil
}

func (r *Runner) startReverse(id string, m Migration, startLSN pglogrepl.LSN) {
	srcCluster, ok, err := r.clusters.Get(context.Background(), m.SourceClusterID)
	if err != nil || !ok {
		r.logger.Err(err).Str("migration", id).Msg("reverse migration: source cluster not found")
		r.store.UpdateStatus(context.Background(), id, StatusFailed, "error", "source cluster not found")
		return
	}
	dstCluster, ok, err := r.clusters.Get(context.Background(), m.DestClusterID)
	if err != nil || !ok {
		r.logger.Err(err).Str("migration", id).Msg("reverse migration: dest cluster not found")
		r.store.UpdateStatus(context.Background(), id, StatusFailed, "error", "dest cluster not found")
		return
	}

	srcNode := findNode(srcCluster.Nodes, m.SourceNodeID)
	dstNode := findNode(dstCluster.Nodes, m.DestNodeID)
	if srcNode == nil || dstNode == nil {
		r.logger.Error().Str("migration", id).Msg("reverse migration: node not found")
		r.store.UpdateStatus(context.Background(), id, StatusFailed, "error", "node not found")
		return
	}

	cfg := &config.Config{}
	cfg.Source.ParseURI(srcNode.DSN())
	cfg.Dest.ParseURI(dstNode.DSN())
	cfg.Replication.SlotName = m.SlotName
	cfg.Replication.Publication = m.Publication
	cfg.Replication.OutputPlugin = "pgoutput"
	cfg.Snapshot.Workers = m.CopyWorkers

	logWriter := metrics.NewLogWriter(nil)
	pipelineLogger := zerolog.New(zerolog.MultiLevelWriter(r.logger, logWriter)).
		With().Timestamp().Str("migration", id).Logger()
	p := pipeline.New(cfg, pipelineLogger)
	logWriter.SetCollector(p.Metrics)

	jobCtx, cancel := context.WithCancel(r.ctx)

	r.mu.Lock()
	r.running[id] = &runningJob{pipeline: p, cancel: cancel, done: make(chan struct{})}
	r.mu.Unlock()

	r.store.UpdateStatus(context.Background(), id, StatusRunning, "initializing", "")

	r.logger.Info().
		Str("migration", id).
		Str("source", m.SourceClusterID+"/"+m.SourceNodeID).
		Str("dest", m.DestClusterID+"/"+m.DestNodeID).
		Stringer("start_lsn", startLSN).
		Msg("starting reverse replication")

	go func() {
		defer func() {
			p.Close()
			r.cleanup(id)
		}()

		go r.pollProgress(jobCtx, id, p)

		err := p.RunFollow(jobCtx, startLSN)
		bgCtx := context.Background()
		if err != nil && jobCtx.Err() != nil {
			r.logger.Info().Str("migration", id).Msg("reverse migration stopped by cancellation")
			r.store.UpdateStatus(bgCtx, id, StatusStopped, "stopped", "")
		} else if err != nil {
			r.logger.Err(err).Str("migration", id).Msg("reverse migration failed")
			r.store.UpdateStatus(bgCtx, id, StatusFailed, "error", err.Error())
		}
	}()
}

func (r *Runner) IsRunning(migrationID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.running[migrationID]
	return ok
}

func (r *Runner) Status(migrationID string) *pipeline.Progress {
	r.mu.Lock()
	job, ok := r.running[migrationID]
	r.mu.Unlock()
	if !ok {
		return nil
	}
	p := job.pipeline.Status()
	return &p
}

func (r *Runner) MetricsSnapshot(migrationID string) *metrics.Snapshot {
	r.mu.Lock()
	job, ok := r.running[migrationID]
	r.mu.Unlock()
	if !ok {
		return nil
	}
	snap := job.pipeline.Metrics.Snapshot()
	return &snap
}

// Logs returns the log entries from a running migration's collector.
func (r *Runner) Logs(migrationID string) []metrics.LogEntry {
	r.mu.Lock()
	job, ok := r.running[migrationID]
	r.mu.Unlock()
	if !ok {
		return nil
	}
	return job.pipeline.Metrics.Logs()
}

func (r *Runner) run(ctx context.Context, id string, mode Mode, p *pipeline.Pipeline) {
	var err error

	defer func() {
		p.Close()

		r.mu.Lock()
		job, exists := r.running[id]
		wasSwitchover := exists && job.switchedOver
		if exists {
			close(job.done)
		}
		r.mu.Unlock()

		if wasSwitchover {
			return
		}

		bgCtx := context.Background()
		if err != nil && ctx.Err() != nil {
			r.logger.Info().Str("migration", id).Msg("migration stopped by cancellation")
			r.store.UpdateStatus(bgCtx, id, StatusStopped, "stopped", "")
		} else if err != nil {
			r.logger.Err(err).Str("migration", id).Msg("migration failed")
			r.store.UpdateStatus(bgCtx, id, StatusFailed, "error", err.Error())
		} else {
			r.logger.Info().Str("migration", id).Msg("migration completed")
			r.store.UpdateStatus(bgCtx, id, StatusCompleted, "done", "")
		}
		r.cleanup(id)
	}()

	go r.pollProgress(ctx, id, p)

	switch mode {
	case ModeCloneOnly:
		err = p.RunClone(ctx)
	case ModeCloneAndFollow:
		err = p.RunCloneAndFollow(ctx)
	case ModeCloneFollowSwitch:
		err = p.RunCloneAndFollow(ctx)
	default:
		err = fmt.Errorf("unknown mode %q", mode)
	}
}

func (r *Runner) pollProgress(ctx context.Context, id string, p *pipeline.Pipeline) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			prog := p.Status()
			lsn := prog.LastLSN.String()
			r.store.UpdateProgress(ctx, id, prog.Phase, lsn, prog.TablesTotal, prog.TablesCopied)

			if prog.Phase == "streaming" || prog.Phase == "following" {
				r.store.UpdateStatus(ctx, id, StatusStreaming, prog.Phase, "")
			}
		}
	}
}

func (r *Runner) cleanup(id string) {
	r.mu.Lock()
	delete(r.running, id)
	r.mu.Unlock()
}

func findNode(nodes []cluster.Node, id string) *cluster.Node {
	for i := range nodes {
		if nodes[i].ID == id {
			return &nodes[i]
		}
	}
	return nil
}

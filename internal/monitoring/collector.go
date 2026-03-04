package monitoring

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmanager/internal/cluster"
)

// Collector manages monitoring for all registered clusters.
// Thread-safe for concurrent access from HTTP handlers.
type Collector struct {
	logger   zerolog.Logger
	clusters *cluster.Store
	config   TierConfig
	store    *Store
	partMgr  *PartitionManager

	mu    sync.RWMutex
	nodes map[string]*nodeMonitor // key: clusterID:nodeID
}

func monKey(clusterID, nodeID string) string {
	return clusterID + ":" + nodeID
}

// NewCollector creates a monitoring collector.
func NewCollector(clusters *cluster.Store, store *Store, logger zerolog.Logger, cfg TierConfig) *Collector {
	return &Collector{
		logger:   logger.With().Str("component", "monitoring").Logger(),
		clusters: clusters,
		store:    store,
		config:   cfg,
		nodes:    make(map[string]*nodeMonitor),
	}
}

// SetPartitionManager sets the partition manager for automatic partition creation.
func (c *Collector) SetPartitionManager(pm *PartitionManager) {
	c.partMgr = pm
}

// AutoStart starts monitoring for all clusters that have monitoring_enabled nodes.
// Called once at daemon boot.
func (c *Collector) AutoStart(ctx context.Context) error {
	monitored, err := c.clusters.ListMonitoredClusters(ctx)
	if err != nil {
		return fmt.Errorf("list monitored clusters: %w", err)
	}

	c.logger.Debug().Int("clusters_found", len(monitored)).Msg("auto-start: queried monitored clusters")

	for _, cl := range monitored {
		c.logger.Debug().Str("cluster", cl.ID).Str("name", cl.Name).Int("nodes", len(cl.Nodes)).Msg("auto-start: starting cluster")
		if err := c.StartCluster(ctx, cl); err != nil {
			c.logger.Warn().Err(err).Str("cluster", cl.ID).Msg("auto-start monitoring failed")
		}
	}

	if len(monitored) > 0 {
		c.logger.Info().Int("clusters", len(monitored)).Msg("auto-started monitoring")
	}
	return nil
}

// StartCluster begins monitoring all enabled nodes in a cluster.
func (c *Collector) StartCluster(ctx context.Context, cl cluster.Cluster) error {
	if c.partMgr != nil {
		if err := c.partMgr.EnsureClusterPartition(ctx, cl.ID); err != nil {
			c.logger.Warn().Err(err).Str("cluster", cl.ID).Msg("ensure partition failed")
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	started := 0
	for _, node := range cl.Nodes {
		if !node.MonitoringEnabled {
			c.logger.Debug().Str("node", node.ID).Str("cluster", cl.ID).Msg("skipping node: monitoring not enabled")
			continue
		}
		key := monKey(cl.ID, node.ID)
		if _, exists := c.nodes[key]; exists {
			c.logger.Debug().Str("key", key).Msg("skipping node: already monitored")
			continue
		}
		c.logger.Debug().Str("key", key).Str("host", node.Host).Msg("starting node monitor")
		nm := newNodeMonitor(cl.ID, cl.Name, node, c.config, c.store, c.logger)
		c.nodes[key] = nm
		go nm.run(ctx)
		started++
	}

	c.logger.Info().
		Str("cluster", cl.ID).
		Str("cluster_name", cl.Name).
		Int("nodes_started", started).
		Msg("monitoring started")
	return nil
}

// StartNode begins monitoring a single node.
func (c *Collector) StartNode(ctx context.Context, cl cluster.Cluster, nodeID string) error {
	if c.partMgr != nil {
		if err := c.partMgr.EnsureClusterPartition(ctx, cl.ID); err != nil {
			c.logger.Warn().Err(err).Str("cluster", cl.ID).Msg("ensure partition failed")
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	key := monKey(cl.ID, nodeID)
	if _, exists := c.nodes[key]; exists {
		c.logger.Debug().Str("node", nodeID).Str("cluster", cl.ID).Msg("node already monitored, skipping")
		return nil
	}

	for _, node := range cl.Nodes {
		if node.ID == nodeID {
			nm := newNodeMonitor(cl.ID, cl.Name, node, c.config, c.store, c.logger)
			c.nodes[key] = nm
			go nm.run(ctx)
			c.logger.Info().Str("node", nodeID).Str("cluster", cl.ID).Msg("node monitoring started")
			return nil
		}
	}
	return fmt.Errorf("node %q not found in cluster %q", nodeID, cl.ID)
}

// StopNode stops monitoring a single node across all clusters.
func (c *Collector) StopNode(nodeID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key, nm := range c.nodes {
		if nm.node.ID == nodeID {
			nm.stop()
			delete(c.nodes, key)
			c.logger.Info().Str("node", nodeID).Str("cluster", nm.clusterID).Msg("node monitoring stopped")
			return
		}
	}
}

// StopCluster stops monitoring all nodes in a cluster.
func (c *Collector) StopCluster(clusterID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	stopped := 0
	for key, nm := range c.nodes {
		if nm.clusterID == clusterID {
			nm.stop()
			delete(c.nodes, key)
			stopped++
		}
	}
	c.logger.Info().Str("cluster", clusterID).Int("nodes_stopped", stopped).Msg("monitoring stopped")
}

// IsMonitoring returns true if any node in the cluster is being monitored.
func (c *Collector) IsMonitoring(clusterID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, nm := range c.nodes {
		if nm.clusterID == clusterID {
			return true
		}
	}
	return false
}

// IsNodeMonitoring returns true if a specific node is being monitored.
func (c *Collector) IsNodeMonitoring(nodeID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, nm := range c.nodes {
		if nm.node.ID == nodeID {
			return true
		}
	}
	return false
}

// MonitoredClusterIDs returns the IDs of all clusters being monitored.
func (c *Collector) MonitoredClusterIDs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	seen := make(map[string]struct{})
	for _, nm := range c.nodes {
		seen[nm.clusterID] = struct{}{}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids
}

// GetOverview returns the current monitoring state for a cluster.
func (c *Collector) GetOverview(clusterID string) MonitoringOverview {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var nodes []NodeMonitoringSnapshot
	var history []Tier1Snapshot
	var clusterName string

	for _, nm := range c.nodes {
		if nm.clusterID != clusterID {
			continue
		}
		clusterName = nm.clusterName
		nodes = append(nodes, nm.snapshot())
		history = append(history, nm.tier1History()...)
	}

	return MonitoringOverview{
		ClusterID:   clusterID,
		ClusterName: clusterName,
		Nodes:       nodes,
		History:     history,
	}
}

// GetOverviewWithRange returns overview with history from the persistent store for a time range.
func (c *Collector) GetOverviewWithRange(ctx context.Context, clusterID string, from, to time.Time) (MonitoringOverview, error) {
	c.mu.RLock()
	var nodes []NodeMonitoringSnapshot
	var clusterName string
	for _, nm := range c.nodes {
		if nm.clusterID != clusterID {
			continue
		}
		clusterName = nm.clusterName
		nodes = append(nodes, nm.snapshot())
	}
	c.mu.RUnlock()

	var history []Tier1Snapshot
	if c.store != nil {
		var err error
		history, err = c.store.Query(ctx, clusterID, from, to, 2000)
		if err != nil {
			return MonitoringOverview{}, fmt.Errorf("query history: %w", err)
		}
	}

	return MonitoringOverview{
		ClusterID:   clusterID,
		ClusterName: clusterName,
		Nodes:       nodes,
		History:     history,
	}, nil
}

// GetClusterSummaries returns summaries for all monitored clusters (for listing page).
func (c *Collector) GetClusterSummaries() []MonitoringClusterSummary {
	c.mu.RLock()
	defer c.mu.RUnlock()

	type clusterAcc struct {
		name             string
		nodesTotal       int
		nodesOK          int
		tps              float64
		activeQueries    int
		cacheHitRatio    float64
		cacheHitCount    int
		replicationLag   float64
		txidAgePct       float64
		blockedLocks     int
		totalConnections int
		maxConnections   int
	}

	acc := make(map[string]*clusterAcc)
	for _, nm := range c.nodes {
		a, ok := acc[nm.clusterID]
		if !ok {
			a = &clusterAcc{name: nm.clusterName}
			acc[nm.clusterID] = a
		}
		a.nodesTotal++

		snap := nm.snapshot()
		if snap.Status == "ok" {
			a.nodesOK++
		}
		if snap.Tier1 != nil {
			a.tps += snap.Tier1.Database.TxnCommitRate + snap.Tier1.Database.TxnRollbackRate
			a.activeQueries += snap.Tier1.Activity.ActiveQueries
			a.blockedLocks += snap.Tier1.Activity.BlockedLocks
			a.totalConnections += snap.Tier1.Activity.TotalConnections
			a.maxConnections += snap.Tier1.Activity.MaxConnections
			if snap.Tier1.Database.CacheHitRatio > 0 {
				a.cacheHitRatio += snap.Tier1.Database.CacheHitRatio
				a.cacheHitCount++
			}
			if snap.Tier1.Replication.ReplayLagSec > a.replicationLag {
				a.replicationLag = snap.Tier1.Replication.ReplayLagSec
			}
			if snap.Tier1.Health.TxIDAgePct > a.txidAgePct {
				a.txidAgePct = snap.Tier1.Health.TxIDAgePct
			}
		}
	}

	summaries := make([]MonitoringClusterSummary, 0, len(acc))
	for id, a := range acc {
		hitRatio := 0.0
		if a.cacheHitCount > 0 {
			hitRatio = a.cacheHitRatio / float64(a.cacheHitCount)
		}
		summaries = append(summaries, MonitoringClusterSummary{
			ClusterID:        id,
			ClusterName:      a.name,
			NodesTotal:       a.nodesTotal,
			NodesOK:          a.nodesOK,
			TPS:              a.tps,
			ActiveQueries:    a.activeQueries,
			CacheHitRatio:    hitRatio,
			ReplicationLag:   a.replicationLag,
			TxIDAgePct:       a.txidAgePct,
			BlockedLocks:     a.blockedLocks,
			TotalConnections: a.totalConnections,
			MaxConnections:   a.maxConnections,
		})
	}
	return summaries
}

// GetTier2 returns the latest Tier 2 data for a specific node.
func (c *Collector) GetTier2(nodeID string) *Tier2Snapshot {
	c.mu.RLock()
	var nm *nodeMonitor
	for _, n := range c.nodes {
		if n.node.ID == nodeID {
			nm = n
			break
		}
	}
	c.mu.RUnlock()
	if nm == nil {
		return nil
	}
	return nm.latestTier2()
}

// GetTier3 returns the latest Tier 3 data for a specific node.
func (c *Collector) GetTier3(nodeID string) *Tier3Snapshot {
	c.mu.RLock()
	var nm *nodeMonitor
	for _, n := range c.nodes {
		if n.node.ID == nodeID {
			nm = n
			break
		}
	}
	c.mu.RUnlock()
	if nm == nil {
		return nil
	}
	return nm.latestTier3()
}

// GetSlowQueries returns the slow query log for a specific node.
func (c *Collector) GetSlowQueries(nodeID string) []SlowQueryEntry {
	c.mu.RLock()
	var nm *nodeMonitor
	for _, n := range c.nodes {
		if n.node.ID == nodeID {
			nm = n
			break
		}
	}
	c.mu.RUnlock()
	if nm == nil {
		return nil
	}
	return nm.getSlowQueries()
}

// RefreshTier3 triggers an immediate Tier 3 collection for a node.
func (c *Collector) RefreshTier3(ctx context.Context, nodeID string) error {
	c.mu.RLock()
	var nm *nodeMonitor
	for _, n := range c.nodes {
		if n.node.ID == nodeID {
			nm = n
			break
		}
	}
	c.mu.RUnlock()
	if nm == nil {
		return fmt.Errorf("node %q not monitored", nodeID)
	}
	return nm.collectTier3(ctx)
}

// Close stops all monitoring.
func (c *Collector) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, nm := range c.nodes {
		nm.stop()
		delete(c.nodes, id)
	}
	c.logger.Info().Msg("monitoring collector closed")
}

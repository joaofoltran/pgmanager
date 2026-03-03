package monitoring

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog"

	"github.com/jfoltran/pgmanager/internal/cluster"
)

// Collector manages monitoring for all registered clusters.
// Thread-safe for concurrent access from HTTP handlers.
type Collector struct {
	logger   zerolog.Logger
	clusters *cluster.Store
	config   TierConfig

	mu    sync.RWMutex
	nodes map[string]*nodeMonitor // key: nodeID
}

// NewCollector creates a monitoring collector.
func NewCollector(clusters *cluster.Store, logger zerolog.Logger, cfg TierConfig) *Collector {
	return &Collector{
		logger:   logger.With().Str("component", "monitoring").Logger(),
		clusters: clusters,
		config:   cfg,
		nodes:    make(map[string]*nodeMonitor),
	}
}

// AutoStart starts monitoring for all clusters that have monitoring_enabled nodes.
// Called once at daemon boot.
func (c *Collector) AutoStart(ctx context.Context) error {
	monitored, err := c.clusters.ListMonitoredClusters(ctx)
	if err != nil {
		return fmt.Errorf("list monitored clusters: %w", err)
	}

	for _, cl := range monitored {
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
	c.mu.Lock()
	defer c.mu.Unlock()

	started := 0
	for _, node := range cl.Nodes {
		if !node.MonitoringEnabled {
			continue
		}
		if _, exists := c.nodes[node.ID]; exists {
			continue
		}
		nm := newNodeMonitor(cl.ID, cl.Name, node, c.config, c.logger)
		c.nodes[node.ID] = nm
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
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.nodes[nodeID]; exists {
		return nil
	}

	for _, node := range cl.Nodes {
		if node.ID == nodeID {
			nm := newNodeMonitor(cl.ID, cl.Name, node, c.config, c.logger)
			c.nodes[node.ID] = nm
			go nm.run(ctx)
			c.logger.Info().Str("node", nodeID).Str("cluster", cl.ID).Msg("node monitoring started")
			return nil
		}
	}
	return fmt.Errorf("node %q not found in cluster %q", nodeID, cl.ID)
}

// StopNode stops monitoring a single node.
func (c *Collector) StopNode(nodeID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if nm, ok := c.nodes[nodeID]; ok {
		nm.stop()
		delete(c.nodes, nodeID)
		c.logger.Info().Str("node", nodeID).Msg("node monitoring stopped")
	}
}

// StopCluster stops monitoring all nodes in a cluster.
func (c *Collector) StopCluster(clusterID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	stopped := 0
	for id, nm := range c.nodes {
		if nm.clusterID == clusterID {
			nm.stop()
			delete(c.nodes, id)
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
	_, ok := c.nodes[nodeID]
	return ok
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

// GetTier2 returns the latest Tier 2 data for a specific node.
func (c *Collector) GetTier2(nodeID string) *Tier2Snapshot {
	c.mu.RLock()
	nm, ok := c.nodes[nodeID]
	c.mu.RUnlock()
	if !ok {
		return nil
	}
	return nm.latestTier2()
}

// GetTier3 returns the latest Tier 3 data for a specific node.
func (c *Collector) GetTier3(nodeID string) *Tier3Snapshot {
	c.mu.RLock()
	nm, ok := c.nodes[nodeID]
	c.mu.RUnlock()
	if !ok {
		return nil
	}
	return nm.latestTier3()
}

// GetSlowQueries returns the slow query log for a specific node.
func (c *Collector) GetSlowQueries(nodeID string) []SlowQueryEntry {
	c.mu.RLock()
	nm, ok := c.nodes[nodeID]
	c.mu.RUnlock()
	if !ok {
		return nil
	}
	return nm.getSlowQueries()
}

// RefreshTier3 triggers an immediate Tier 3 collection for a node.
func (c *Collector) RefreshTier3(ctx context.Context, nodeID string) error {
	c.mu.RLock()
	nm, ok := c.nodes[nodeID]
	c.mu.RUnlock()
	if !ok {
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

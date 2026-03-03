import { useEffect, useState, useCallback } from "react";
import type {
  MonitoringOverview,
  Tier2Snapshot,
} from "../types/monitoring";
import {
  fetchMonitoringOverview,
  fetchNodeTableStats,
} from "../api/client";

export function useMonitoring(clusterId: string | null) {
  const [overview, setOverview] = useState<MonitoringOverview | null>(null);
  const [tier2, setTier2] = useState<Record<string, Tier2Snapshot>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Tier 1 polling at 2s.
  const pollOverview = useCallback(async () => {
    if (!clusterId) return;
    try {
      const data = await fetchMonitoringOverview(clusterId);
      setOverview(data);
      setError(null);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  }, [clusterId]);

  useEffect(() => {
    if (!clusterId) {
      setOverview(null);
      setLoading(false);
      return;
    }
    setLoading(true);
    pollOverview();
    const id = setInterval(pollOverview, 2000);
    return () => clearInterval(id);
  }, [clusterId, pollOverview]);

  // Tier 2 polling at 30s for each node.
  useEffect(() => {
    if (!overview?.nodes?.length || !clusterId) return;
    const poll = () => {
      for (const n of overview.nodes) {
        fetchNodeTableStats(clusterId, n.node_id)
          .then((data) =>
            setTier2((prev) => ({ ...prev, [n.node_id]: data }))
          )
          .catch(() => {});
      }
    };
    poll();
    const id = setInterval(poll, 30000);
    return () => clearInterval(id);
  }, [clusterId, overview?.nodes?.length]);

  return { overview, tier2, loading, error };
}

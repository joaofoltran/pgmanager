import { useEffect, useState, useCallback, useRef } from "react";
import type {
  MonitoringOverview,
  Tier2Snapshot,
} from "../types/monitoring";
import type { TimeRange } from "../components/monitoring/TimeRangePicker";
import {
  fetchMonitoringOverview,
  fetchNodeTableStats,
} from "../api/client";

export function useMonitoring(
  clusterId: string | null,
  timeRange?: TimeRange | null,
) {
  const [overview, setOverview] = useState<MonitoringOverview | null>(null);
  const [tier2, setTier2] = useState<Record<string, Tier2Snapshot>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const prevRangeRef = useRef<string | null>(null);

  const isLive = !timeRange;

  const pollOverview = useCallback(async () => {
    if (!clusterId) return;
    try {
      const data = await fetchMonitoringOverview(
        clusterId,
        timeRange?.from,
        timeRange?.to,
      );
      setOverview(data);
      setError(null);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  }, [clusterId, timeRange?.from, timeRange?.to]);

  useEffect(() => {
    if (!clusterId) {
      setOverview(null);
      setLoading(false);
      return;
    }

    const rangeKey = timeRange ? `${timeRange.from}|${timeRange.to}` : null;
    if (rangeKey !== prevRangeRef.current) {
      setLoading(true);
      prevRangeRef.current = rangeKey;
    }

    pollOverview();

    if (isLive) {
      const id = setInterval(pollOverview, 2000);
      return () => clearInterval(id);
    }
  }, [clusterId, pollOverview, isLive, timeRange?.from, timeRange?.to]);

  useEffect(() => {
    if (!overview?.nodes?.length || !clusterId || !isLive) return;
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
  }, [clusterId, overview?.nodes?.length, isLive]);

  return { overview, tier2, loading, error, isLive };
}

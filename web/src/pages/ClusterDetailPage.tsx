import { useEffect, useState, useRef } from "react";
import { useParams, Link, useNavigate } from "react-router-dom";
import {
  ArrowLeft,
  Database,
  Server,
  Settings,
  Table2,
  Loader2,
  AlertCircle,
  HardDrive,
  ChevronRight,
  Search,
  Clock,
  Layers,
  Pencil,
  Plus,
  Trash2,
  Save,
  XCircle,
} from "lucide-react";
import type { Cluster, ClusterNode, ClusterInfo, DBInfo, ParameterInfo } from "../types/cluster";
import { fetchCluster, introspectCluster, updateCluster, removeCluster, toggleNodeMonitoring } from "../api/client";

type Tab = "overview" | "parameters" | "databases" | "settings";

export function ClusterDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [cluster, setCluster] = useState<Cluster | null>(null);
  const [info, setInfo] = useState<ClusterInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>("overview");
  const [selectedNode, setSelectedNode] = useState<string | undefined>();

  useEffect(() => {
    if (!id) return;
    setLoading(true);
    setError(null);

    fetchCluster(id)
      .then((c) => {
        setCluster(c);
        const primary = c.nodes.find((n) => n.role === "primary");
        const target = primary || c.nodes[0];
        if (target) setSelectedNode(target.id);
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, [id]);

  useEffect(() => {
    if (!id || !selectedNode) return;
    setInfo(null);
    setError(null);
    introspectCluster(id, selectedNode)
      .then(setInfo)
      .catch((e) => setError(e.message));
  }, [id, selectedNode]);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <Loader2 className="w-8 h-8 animate-spin" style={{ color: "var(--color-accent)" }} />
      </div>
    );
  }

  if (error && !cluster) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="text-center space-y-3">
          <AlertCircle className="w-8 h-8 mx-auto text-red-400" />
          <p style={{ color: "var(--color-text)" }}>{error}</p>
          <Link to="/clusters" className="text-sm" style={{ color: "var(--color-accent)" }}>
            Back to clusters
          </Link>
        </div>
      </div>
    );
  }

  if (!cluster) return null;

  const tabs: { id: Tab; label: string; icon: React.ComponentType<{ className?: string; style?: React.CSSProperties }> }[] = [
    { id: "overview", label: "Overview", icon: Layers },
    { id: "databases", label: "Databases", icon: Database },
    { id: "parameters", label: "Parameters", icon: Settings },
    { id: "settings", label: "Settings", icon: Pencil },
  ];

  return (
    <div className="space-y-6 max-w-6xl">
      <div className="flex items-center gap-4">
        <Link
          to="/clusters"
          className="p-2 rounded-lg transition-colors hover:bg-white/5"
          style={{ color: "var(--color-text-muted)" }}
        >
          <ArrowLeft className="w-4 h-4" />
        </Link>
        <div className="w-10 h-10 rounded-xl flex items-center justify-center"
          style={{ backgroundColor: "var(--color-accent)" }}>
          <Database className="w-5 h-5 text-white" />
        </div>
        <div className="flex-1">
          <h2 className="text-lg font-semibold" style={{ color: "var(--color-text)" }}>
            {cluster.name}
          </h2>
          <p className="text-sm" style={{ color: "var(--color-text-muted)" }}>
            {cluster.id} &middot; {cluster.nodes.length} node{cluster.nodes.length !== 1 ? "s" : ""}
          </p>
        </div>
        {cluster.nodes.length > 1 && (
          <select
            value={selectedNode}
            onChange={(e) => setSelectedNode(e.target.value)}
            className="rounded-lg border px-3 py-2 text-sm"
            style={{
              backgroundColor: "var(--color-surface)",
              borderColor: "var(--color-border)",
              color: "var(--color-text)",
            }}
          >
            {cluster.nodes.map((n) => (
              <option key={n.id} value={n.id}>
                {n.role}: {n.host}:{n.port}
              </option>
            ))}
          </select>
        )}
      </div>

      {error && (
        <div className="rounded-lg border p-3 flex items-center gap-2 text-sm"
          style={{ backgroundColor: "#dc262610", borderColor: "#dc262640", color: "#f87171" }}>
          <AlertCircle className="w-4 h-4 shrink-0" />
          {error}
        </div>
      )}

      <div className="flex gap-1 rounded-lg p-1" style={{ backgroundColor: "var(--color-surface)" }}>
        {tabs.map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className="flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium transition-colors"
            style={{
              backgroundColor: tab === t.id ? "var(--color-accent)" : "transparent",
              color: tab === t.id ? "#fff" : "var(--color-text-secondary)",
            }}
          >
            <t.icon className="w-4 h-4" />
            {t.label}
          </button>
        ))}
      </div>

      {tab === "settings" ? (
        <SettingsTab cluster={cluster} onUpdated={setCluster} />
      ) : !info ? (
        <div className="flex items-center justify-center py-16">
          <div className="text-center space-y-3">
            <Loader2 className="w-6 h-6 animate-spin mx-auto" style={{ color: "var(--color-accent)" }} />
            <p className="text-sm" style={{ color: "var(--color-text-muted)" }}>
              Connecting to cluster...
            </p>
          </div>
        </div>
      ) : (
        <>
          {tab === "overview" && <OverviewTab cluster={cluster} info={info} />}
          {tab === "databases" && <DatabasesTab databases={info.databases} />}
          {tab === "parameters" && <ParametersTab parameters={info.parameters} />}
        </>
      )}
    </div>
  );
}

function OverviewTab({ cluster, info }: { cluster: Cluster; info: ClusterInfo }) {
  const [uptime, setUptime] = useState(info.uptime);
  const baseRef = useRef(info.uptime);

  useEffect(() => {
    baseRef.current = info.uptime;
    setUptime(info.uptime);
  }, [info.uptime]);

  useEffect(() => {
    const interval = setInterval(() => {
      setUptime(incrementUptime(baseRef.current));
      baseRef.current = incrementUptime(baseRef.current);
    }, 1000);
    return () => clearInterval(interval);
  }, []);

  const dbCount = info.databases.length;
  const totalTables = info.databases.reduce((sum, db) => {
    return sum + (db.schemas || []).reduce((s, schema) => s + schema.table_count, 0);
  }, 0);

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        <StatCard label="Cluster Size" value={info.cluster_size} icon={HardDrive} />
        <StatCard label="Databases" value={String(dbCount)} icon={Database} />
        <StatCard label="Total Tables" value={String(totalTables)} icon={Table2} />
        <StatCard
          label="Role"
          value={info.is_replica ? "Replica" : "Primary"}
          icon={Server}
          valueColor={info.is_replica ? "#3b82f6" : "#22c55e"}
        />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
        <div className="rounded-lg border p-4" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
          <div className="flex items-center gap-2 mb-3">
            <Clock className="w-4 h-4" style={{ color: "var(--color-text-muted)" }} />
            <h3 className="text-sm font-medium" style={{ color: "var(--color-text)" }}>Uptime</h3>
          </div>
          <p className="text-lg font-semibold font-mono" style={{ color: "var(--color-text)" }}>
            {uptime}
          </p>
          <p className="text-xs mt-1" style={{ color: "var(--color-text-muted)" }}>
            Started {info.started_at}
          </p>
        </div>

        <div className="rounded-lg border p-4" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
          <div className="flex items-center gap-2 mb-3">
            <Settings className="w-4 h-4" style={{ color: "var(--color-text-muted)" }} />
            <h3 className="text-sm font-medium" style={{ color: "var(--color-text)" }}>Configuration</h3>
          </div>
          <p className="text-xs" style={{ color: "var(--color-text-muted)" }}>
            Max connections: <span className="font-mono" style={{ color: "var(--color-text)" }}>{info.max_connections}</span>
          </p>
          <p className="text-xs mt-1" style={{ color: "var(--color-text-muted)" }}>
            Non-default params: <span className="font-mono" style={{ color: "var(--color-text)" }}>{info.parameters.length}</span>
          </p>
        </div>
      </div>

      <div className="rounded-lg border p-4" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
        <h3 className="text-sm font-medium mb-3" style={{ color: "var(--color-text)" }}>Version</h3>
        <p className="text-sm font-mono" style={{ color: "var(--color-text-secondary)" }}>
          {info.version}
        </p>
      </div>

      <div className="rounded-lg border p-4" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
        <h3 className="text-sm font-medium mb-3" style={{ color: "var(--color-text)" }}>Nodes</h3>
        <div className="space-y-2">
          {cluster.nodes.map((n) => (
            <NodeRow key={n.id} node={n} clusterId={cluster.id} />
          ))}
        </div>
      </div>

      {info.databases.length > 0 && (
        <div className="rounded-lg border p-4" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
          <h3 className="text-sm font-medium mb-3" style={{ color: "var(--color-text)" }}>Databases</h3>
          <div className="space-y-2">
            {info.databases.map((db) => (
              <div key={db.name} className="flex items-center justify-between py-1.5">
                <div className="flex items-center gap-2">
                  <Database className="w-3.5 h-3.5" style={{ color: "var(--color-text-muted)" }} />
                  <span className="text-sm font-mono" style={{ color: "var(--color-text)" }}>{db.name}</span>
                  <span className="text-xs" style={{ color: "var(--color-text-muted)" }}>({db.owner})</span>
                </div>
                <div className="flex items-center gap-4 text-xs" style={{ color: "var(--color-text-secondary)" }}>
                  {db.schemas && <span>{db.schemas.reduce((s, sc) => s + sc.table_count, 0)} tables</span>}
                  <span className="font-mono">{db.size}</span>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function StatCard({
  label,
  value,
  icon: Icon,
  valueColor,
}: {
  label: string;
  value: string;
  icon: React.ComponentType<{ className?: string; style?: React.CSSProperties }>;
  valueColor?: string;
}) {
  return (
    <div className="rounded-lg border p-4" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
      <div className="flex items-center gap-2 mb-2">
        <Icon className="w-3.5 h-3.5" style={{ color: "var(--color-text-muted)" }} />
        <span className="text-xs" style={{ color: "var(--color-text-muted)" }}>{label}</span>
      </div>
      <span className="text-lg font-semibold font-mono" style={{ color: valueColor || "var(--color-text)" }}>
        {value}
      </span>
    </div>
  );
}

function DatabasesTab({ databases }: { databases: DBInfo[] }) {
  const [expandedDB, setExpandedDB] = useState<string | null>(databases.length === 1 ? databases[0].name : null);
  const [expandedSchema, setExpandedSchema] = useState<string | null>(null);
  const [filter, setFilter] = useState("");

  return (
    <div className="space-y-3">
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4" style={{ color: "var(--color-text-muted)" }} />
        <input
          type="text"
          placeholder="Filter databases or tables..."
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="w-full rounded-lg border pl-10 pr-4 py-2.5 text-sm"
          style={{
            backgroundColor: "var(--color-surface)",
            borderColor: "var(--color-border)",
            color: "var(--color-text)",
          }}
        />
      </div>

      {databases.map((db) => {
        const matchesDB = db.name.toLowerCase().includes(filter.toLowerCase());
        const filteredSchemas = (db.schemas || []).map((s) => ({
          ...s,
          tables: filter && !matchesDB
            ? s.tables.filter((t) => t.name.toLowerCase().includes(filter.toLowerCase()))
            : s.tables,
        })).filter((s) => matchesDB || s.tables.length > 0);

        if (filter && !matchesDB && filteredSchemas.length === 0) return null;

        const isDBExpanded = expandedDB === db.name || !!filter;

        return (
          <div key={db.name} className="rounded-lg border" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
            <button
              onClick={() => setExpandedDB(isDBExpanded && !filter ? null : db.name)}
              className="w-full flex items-center gap-3 p-4 text-left"
            >
              <ChevronRight
                className={`w-4 h-4 transition-transform ${isDBExpanded ? "rotate-90" : ""}`}
                style={{ color: "var(--color-text-muted)" }}
              />
              <Database className="w-4 h-4" style={{ color: "var(--color-accent)" }} />
              <span className="text-sm font-medium font-mono" style={{ color: "var(--color-text)" }}>
                {db.name}
              </span>
              <span className="text-xs" style={{ color: "var(--color-text-muted)" }}>
                {db.owner}
              </span>
              <span className="text-xs ml-auto font-mono" style={{ color: "var(--color-text-secondary)" }}>
                {db.size}
              </span>
            </button>

            {isDBExpanded && (
              <div className="border-t px-4 pb-3" style={{ borderColor: "var(--color-border)" }}>
                {filteredSchemas.length === 0 ? (
                  <p className="text-sm py-4 text-center" style={{ color: "var(--color-text-muted)" }}>
                    No user tables
                  </p>
                ) : (
                  filteredSchemas.map((schema) => {
                    const schemaKey = `${db.name}.${schema.name}`;
                    const isSchemaExpanded = expandedSchema === schemaKey || !!filter;

                    return (
                      <div key={schema.name} className="mt-2">
                        <button
                          onClick={() => setExpandedSchema(isSchemaExpanded && !filter ? null : schemaKey)}
                          className="w-full flex items-center gap-2 py-2 text-left"
                        >
                          <ChevronRight
                            className={`w-3.5 h-3.5 transition-transform ${isSchemaExpanded ? "rotate-90" : ""}`}
                            style={{ color: "var(--color-text-muted)" }}
                          />
                          <Layers className="w-3.5 h-3.5" style={{ color: "var(--color-text-muted)" }} />
                          <span className="text-xs font-mono font-medium" style={{ color: "var(--color-text)" }}>
                            {schema.name}
                          </span>
                          <span className="text-xs ml-auto" style={{ color: "var(--color-text-muted)" }}>
                            {schema.table_count} table{schema.table_count !== 1 ? "s" : ""} &middot; {schema.total_size}
                          </span>
                        </button>

                        {isSchemaExpanded && (
                          <div className="ml-5 mt-1 rounded-md border overflow-hidden" style={{ borderColor: "var(--color-border)" }}>
                            <table className="w-full">
                              <thead>
                                <tr style={{ backgroundColor: "var(--color-bg)" }}>
                                  <th className="text-left px-3 py-1.5 text-xs font-medium" style={{ color: "var(--color-text-muted)" }}>Table</th>
                                  <th className="text-right px-3 py-1.5 text-xs font-medium" style={{ color: "var(--color-text-muted)" }}>Rows</th>
                                  <th className="text-right px-3 py-1.5 text-xs font-medium" style={{ color: "var(--color-text-muted)" }}>Data</th>
                                  <th className="text-right px-3 py-1.5 text-xs font-medium" style={{ color: "var(--color-text-muted)" }}>Indexes</th>
                                  <th className="text-right px-3 py-1.5 text-xs font-medium" style={{ color: "var(--color-text-muted)" }}>Total</th>
                                </tr>
                              </thead>
                              <tbody>
                                {schema.tables.map((t) => (
                                  <tr key={t.name} className="border-t" style={{ borderColor: "var(--color-border)" }}>
                                    <td className="px-3 py-1.5 text-xs font-mono" style={{ color: "var(--color-text)" }}>{t.name}</td>
                                    <td className="px-3 py-1.5 text-xs font-mono text-right" style={{ color: "var(--color-text-secondary)" }}>{t.row_count.toLocaleString()}</td>
                                    <td className="px-3 py-1.5 text-xs font-mono text-right" style={{ color: "var(--color-text-secondary)" }}>{t.data_size}</td>
                                    <td className="px-3 py-1.5 text-xs font-mono text-right" style={{ color: "var(--color-text-secondary)" }}>{t.index_size}</td>
                                    <td className="px-3 py-1.5 text-xs font-mono text-right font-medium" style={{ color: "var(--color-text)" }}>{t.total_size}</td>
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                          </div>
                        )}
                      </div>
                    );
                  })
                )}
              </div>
            )}
          </div>
        );
      })}

      {databases.length === 0 && (
        <div className="rounded-lg border p-8 text-center" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
          <Database className="w-8 h-8 mx-auto mb-3" style={{ color: "var(--color-text-muted)" }} />
          <p className="text-sm" style={{ color: "var(--color-text-muted)" }}>No databases found</p>
        </div>
      )}
    </div>
  );
}

function ParametersTab({ parameters }: { parameters: ParameterInfo[] }) {
  const [filter, setFilter] = useState("");
  const filtered = parameters.filter(
    (p) =>
      p.name.toLowerCase().includes(filter.toLowerCase()) ||
      p.setting.toLowerCase().includes(filter.toLowerCase())
  );

  return (
    <div className="space-y-3">
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4" style={{ color: "var(--color-text-muted)" }} />
        <input
          type="text"
          placeholder="Filter parameters..."
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="w-full rounded-lg border pl-10 pr-4 py-2.5 text-sm"
          style={{
            backgroundColor: "var(--color-surface)",
            borderColor: "var(--color-border)",
            color: "var(--color-text)",
          }}
        />
      </div>

      <div className="rounded-lg border overflow-hidden" style={{ borderColor: "var(--color-border)" }}>
        <table className="w-full">
          <thead>
            <tr style={{ backgroundColor: "var(--color-surface)" }}>
              <th className="text-left px-4 py-2.5 text-xs font-medium" style={{ color: "var(--color-text-muted)" }}>Parameter</th>
              <th className="text-left px-4 py-2.5 text-xs font-medium" style={{ color: "var(--color-text-muted)" }}>Value</th>
              <th className="text-left px-4 py-2.5 text-xs font-medium" style={{ color: "var(--color-text-muted)" }}>Source</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((p) => (
              <tr key={p.name} className="border-t" style={{ borderColor: "var(--color-border)" }}>
                <td className="px-4 py-2 text-sm font-mono" style={{ color: "var(--color-text)" }}>
                  {p.name}
                </td>
                <td className="px-4 py-2 text-sm font-mono" style={{ color: "var(--color-accent)" }}>
                  {p.setting}{p.unit ? ` ${p.unit}` : ""}
                </td>
                <td className="px-4 py-2 text-xs" style={{ color: "var(--color-text-muted)" }}>
                  {p.source}
                </td>
              </tr>
            ))}
            {filtered.length === 0 && (
              <tr>
                <td colSpan={3} className="px-4 py-8 text-center text-sm" style={{ color: "var(--color-text-muted)" }}>
                  No parameters match your filter
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <p className="text-xs" style={{ color: "var(--color-text-muted)" }}>
        Showing {filtered.length} of {parameters.length} non-default parameters
      </p>
    </div>
  );
}

function NodeRow({ node, clusterId, onToggleMonitoring }: { node: ClusterNode; clusterId: string; onToggleMonitoring?: (nodeId: string, enabled: boolean) => void }) {
  const [toggling, setToggling] = useState(false);
  const [enabled, setEnabled] = useState(node.monitoring_enabled);
  const roleColors: Record<string, string> = {
    primary: "#22c55e",
    replica: "#3b82f6",
    standby: "#eab308",
  };
  const color = roleColors[node.role] || "var(--color-text-muted)";

  async function handleToggle() {
    if (!clusterId) return;
    setToggling(true);
    try {
      await toggleNodeMonitoring(clusterId, node.id, !enabled);
      setEnabled(!enabled);
      onToggleMonitoring?.(node.id, !enabled);
    } catch {
    } finally {
      setToggling(false);
    }
  }

  return (
    <div className="flex items-center gap-3 px-3 py-2 rounded-md" style={{ backgroundColor: "var(--color-bg)" }}>
      <Server className="w-4 h-4" style={{ color }} />
      <span className="text-xs font-mono px-1.5 py-0.5 rounded" style={{ backgroundColor: color + "20", color }}>
        {node.role}
      </span>
      <span className="text-sm font-mono flex-1" style={{ color: "var(--color-text)" }}>
        {node.host}:{node.port}
      </span>
      {node.dbname && (
        <span className="text-xs font-mono" style={{ color: "var(--color-text-muted)" }}>
          {node.dbname}
        </span>
      )}
      <button
        onClick={handleToggle}
        disabled={toggling}
        className="relative inline-flex h-5 w-9 items-center rounded-full transition-colors disabled:opacity-50"
        style={{ backgroundColor: enabled ? "var(--color-accent)" : "var(--color-border)" }}
        title={enabled ? "Monitoring enabled" : "Monitoring disabled"}
      >
        <span
          className="inline-block h-3.5 w-3.5 rounded-full bg-white transition-transform"
          style={{ transform: enabled ? "translateX(18px)" : "translateX(3px)" }}
        />
      </button>
      <span className="text-[10px]" style={{ color: enabled ? "var(--color-accent)" : "var(--color-text-muted)" }}>
        {enabled ? "Monitoring" : "Off"}
      </span>
    </div>
  );
}

function SettingsTab({ cluster, onUpdated }: { cluster: Cluster; onUpdated: (c: Cluster) => void }) {
  const navigate = useNavigate();
  const [name, setName] = useState(cluster.name);
  const [tags, setTags] = useState(cluster.tags?.join(", ") || "");
  const [nodes, setNodes] = useState<ClusterNode[]>(JSON.parse(JSON.stringify(cluster.nodes)));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const inputStyle = {
    backgroundColor: "var(--color-bg)",
    borderColor: "var(--color-border)",
    color: "var(--color-text)",
  };

  function updateNode(index: number, field: keyof ClusterNode, value: string | number | boolean) {
    setNodes((prev) => {
      const next = [...prev];
      next[index] = { ...next[index], [field]: value };
      return next;
    });
  }

  function addNode() {
    setNodes((prev) => [
      ...prev,
      { id: `node-${prev.length + 1}`, name: "", host: "", port: 5432, role: "replica" as const, monitoring_enabled: false },
    ]);
  }

  function removeNode(index: number) {
    setNodes((prev) => prev.filter((_, i) => i !== index));
  }

  async function handleSave(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    setError(null);
    setSuccess(false);
    try {
      const tagList = tags.split(",").map((t) => t.trim()).filter(Boolean);
      const updated = await updateCluster(cluster.id, {
        name,
        nodes: nodes.map((n) => ({
          ...n,
          port: typeof n.port === "string" ? parseInt(n.port as string) || 5432 : n.port,
        })),
        tags: tagList.length > 0 ? tagList : undefined,
      });
      onUpdated(updated);
      setSuccess(true);
      setTimeout(() => setSuccess(false), 3000);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to save");
    } finally {
      setSaving(false);
    }
  }

  async function handleDelete() {
    if (!confirm(`Delete cluster "${cluster.name}"? This cannot be undone.`)) return;
    setDeleting(true);
    try {
      await removeCluster(cluster.id);
      navigate("/clusters");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to delete");
      setDeleting(false);
    }
  }

  return (
    <form onSubmit={handleSave} className="space-y-5">
      {error && (
        <div className="flex items-center gap-2 text-sm rounded-lg border p-3"
          style={{ backgroundColor: "#dc262610", borderColor: "#dc262640", color: "#f87171" }}>
          <XCircle className="w-4 h-4 shrink-0" />
          {error}
        </div>
      )}
      {success && (
        <div className="flex items-center gap-2 text-sm rounded-lg border p-3"
          style={{ backgroundColor: "#16a34a10", borderColor: "#16a34a40", color: "#4ade80" }}>
          <Save className="w-4 h-4 shrink-0" />
          Cluster updated successfully
        </div>
      )}

      <div className="rounded-lg border p-5 space-y-4" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
        <h3 className="text-sm font-medium" style={{ color: "var(--color-text)" }}>General</h3>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="block text-xs mb-1" style={{ color: "var(--color-text-secondary)" }}>Cluster ID</label>
            <input className="w-full rounded-md border px-3 py-2 text-sm opacity-60 cursor-not-allowed" style={inputStyle} value={cluster.id} disabled />
            <p className="text-[10px] mt-1" style={{ color: "var(--color-text-muted)" }}>Cannot be changed after creation</p>
          </div>
          <div>
            <label className="block text-xs mb-1" style={{ color: "var(--color-text-secondary)" }}>Cluster Name</label>
            <input className="w-full rounded-md border px-3 py-2 text-sm" style={inputStyle} value={name} onChange={(e) => setName(e.target.value)} required />
          </div>
        </div>
        <div>
          <label className="block text-xs mb-1" style={{ color: "var(--color-text-secondary)" }}>Tags</label>
          <input className="w-full rounded-md border px-3 py-2 text-sm" style={inputStyle} value={tags} onChange={(e) => setTags(e.target.value)} placeholder="prod, us-east-1, critical" />
          <p className="text-[10px] mt-1" style={{ color: "var(--color-text-muted)" }}>Comma-separated</p>
        </div>
      </div>

      <div className="rounded-lg border p-5 space-y-4" style={{ backgroundColor: "var(--color-surface)", borderColor: "var(--color-border)" }}>
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-medium" style={{ color: "var(--color-text)" }}>Nodes</h3>
          <button type="button" onClick={addNode} className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-md transition-colors hover:bg-white/5" style={{ color: "var(--color-accent)" }}>
            <Plus className="w-3.5 h-3.5" /> Add node
          </button>
        </div>

        {nodes.map((node, i) => (
          <div key={i} className="rounded-md border p-4 space-y-3" style={{ backgroundColor: "var(--color-bg)", borderColor: "var(--color-border)" }}>
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium" style={{ color: "var(--color-text-muted)" }}>Node {i + 1}</span>
              {nodes.length > 1 && (
                <button type="button" onClick={() => removeNode(i)} className="p-1 rounded hover:bg-red-500/10 transition-colors">
                  <Trash2 className="w-3.5 h-3.5 text-red-400" />
                </button>
              )}
            </div>
            <div className="grid grid-cols-4 gap-2">
              <div>
                <label className="block text-[10px] mb-0.5" style={{ color: "var(--color-text-muted)" }}>Host</label>
                <input className="w-full rounded-md border px-2.5 py-1.5 text-sm" style={inputStyle} value={node.host} onChange={(e) => updateNode(i, "host", e.target.value)} required />
              </div>
              <div>
                <label className="block text-[10px] mb-0.5" style={{ color: "var(--color-text-muted)" }}>Port</label>
                <input type="number" className="w-full rounded-md border px-2.5 py-1.5 text-sm" style={inputStyle} value={node.port} onChange={(e) => updateNode(i, "port", parseInt(e.target.value) || 5432)} required />
              </div>
              <div>
                <label className="block text-[10px] mb-0.5" style={{ color: "var(--color-text-muted)" }}>Role</label>
                <select className="w-full rounded-md border px-2.5 py-1.5 text-sm" style={inputStyle} value={node.role} onChange={(e) => updateNode(i, "role", e.target.value)}>
                  <option value="primary">Primary</option>
                  <option value="replica">Replica</option>
                  <option value="standby">Standby</option>
                </select>
              </div>
              <div>
                <label className="block text-[10px] mb-0.5" style={{ color: "var(--color-text-muted)" }}>Node ID</label>
                <input className="w-full rounded-md border px-2.5 py-1.5 text-sm" style={inputStyle} value={node.id} onChange={(e) => updateNode(i, "id", e.target.value)} required />
              </div>
            </div>
            <div className="grid grid-cols-3 gap-2">
              <div>
                <label className="block text-[10px] mb-0.5" style={{ color: "var(--color-text-muted)" }}>User</label>
                <input className="w-full rounded-md border px-2.5 py-1.5 text-sm" style={inputStyle} value={node.user || ""} onChange={(e) => updateNode(i, "user", e.target.value)} placeholder="postgres" />
              </div>
              <div>
                <label className="block text-[10px] mb-0.5" style={{ color: "var(--color-text-muted)" }}>Password</label>
                <input type="password" className="w-full rounded-md border px-2.5 py-1.5 text-sm" style={inputStyle} value={node.password || ""} onChange={(e) => updateNode(i, "password", e.target.value)} placeholder="Optional" />
              </div>
              <div>
                <label className="block text-[10px] mb-0.5" style={{ color: "var(--color-text-muted)" }}>Database</label>
                <input className="w-full rounded-md border px-2.5 py-1.5 text-sm" style={inputStyle} value={node.dbname || ""} onChange={(e) => updateNode(i, "dbname", e.target.value)} placeholder="postgres" />
              </div>
            </div>
            <div className="flex items-center gap-3 pt-1">
              <button
                type="button"
                onClick={() => updateNode(i, "monitoring_enabled", !node.monitoring_enabled)}
                className="relative inline-flex h-5 w-9 items-center rounded-full transition-colors"
                style={{ backgroundColor: node.monitoring_enabled ? "var(--color-accent)" : "var(--color-border)" }}
              >
                <span
                  className="inline-block h-3.5 w-3.5 rounded-full bg-white transition-transform"
                  style={{ transform: node.monitoring_enabled ? "translateX(18px)" : "translateX(3px)" }}
                />
              </button>
              <span className="text-xs" style={{ color: node.monitoring_enabled ? "var(--color-accent)" : "var(--color-text-muted)" }}>
                {node.monitoring_enabled ? "Monitoring enabled" : "Monitoring disabled"}
              </span>
            </div>
          </div>
        ))}
      </div>

      <div className="flex items-center justify-between">
        <button
          type="button"
          onClick={handleDelete}
          disabled={deleting}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors hover:bg-red-500/10 disabled:opacity-40"
          style={{ color: "#f87171" }}
        >
          {deleting ? <Loader2 className="w-4 h-4 animate-spin" /> : <Trash2 className="w-4 h-4" />}
          Delete Cluster
        </button>
        <button
          type="submit"
          disabled={saving}
          className="flex items-center gap-2 px-5 py-2 rounded-lg text-sm font-medium transition-colors disabled:opacity-40"
          style={{ backgroundColor: "var(--color-accent)", color: "#fff" }}
        >
          {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
          Save Changes
        </button>
      </div>
    </form>
  );
}

function incrementUptime(uptime: string): string {
  const match = uptime.match(/^(\d+):(\d+):(\d+)$/);
  if (!match) return uptime;

  let [, hStr, mStr, sStr] = match;
  let h = parseInt(hStr, 10);
  let m = parseInt(mStr, 10);
  let s = parseInt(sStr, 10);

  s++;
  if (s >= 60) { s = 0; m++; }
  if (m >= 60) { m = 0; h++; }

  return `${String(h).padStart(2, "0")}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
}

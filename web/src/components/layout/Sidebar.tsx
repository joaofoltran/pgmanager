import { NavLink } from "react-router-dom";
import {
  Database,
  Layers,
  ArrowLeftRight,
  HardDrive,
  Server,
  Settings,
  Activity,
} from "lucide-react";

const modules = [
  {
    name: "Migration",
    path: "/migration",
    icon: ArrowLeftRight,
    description: "Online migration & CDC",
  },
  {
    name: "Backup",
    path: "/backup",
    icon: HardDrive,
    description: "Backup & restore",
  },
  {
    name: "Monitoring",
    path: "/monitoring",
    icon: Activity,
    description: "Real-time metrics",
  },
  {
    name: "Standby",
    path: "/standby",
    icon: Server,
    description: "Replica management",
    soon: true,
  },
];

function NavItem({
  to,
  icon: Icon,
  label,
  badge,
  disabled,
}: {
  to: string;
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  badge?: string;
  disabled?: boolean;
}) {
  return (
    <NavLink
      to={to}
      className={({ isActive }) =>
        `group flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm transition-colors ${
          isActive ? "text-white" : "hover:text-white"
        } ${disabled ? "opacity-50 pointer-events-none" : ""}`
      }
      style={({ isActive }) => ({
        backgroundColor: isActive ? "var(--color-accent)" : "transparent",
        color: isActive ? "#fff" : "var(--color-text-secondary)",
      })}
    >
      <Icon className="w-4 h-4 shrink-0" />
      <div className="flex-1 min-w-0">
        <span className="block truncate">{label}</span>
      </div>
      {badge && (
        <span
          className="text-[9px] px-1.5 py-0.5 rounded font-medium"
          style={{
            backgroundColor: "var(--color-border)",
            color: "var(--color-text-muted)",
          }}
        >
          {badge}
        </span>
      )}
    </NavLink>
  );
}

export function Sidebar() {
  return (
    <aside
      className="w-64 shrink-0 border-r flex flex-col"
      style={{
        borderColor: "var(--color-border)",
        backgroundColor: "var(--color-surface)",
      }}
    >
      <div
        className="px-5 py-5 border-b"
        style={{ borderColor: "var(--color-border)" }}
      >
        <div className="flex items-center gap-2.5">
          <div
            className="w-8 h-8 rounded-lg flex items-center justify-center"
            style={{ backgroundColor: "var(--color-accent)" }}
          >
            <Database className="w-4 h-4 text-white" />
          </div>
          <div>
            <h1
              className="text-sm font-semibold tracking-tight"
              style={{ color: "var(--color-text)" }}
            >
              pgmanager
            </h1>
            <p
              className="text-[10px]"
              style={{ color: "var(--color-text-muted)" }}
            >
              PostgreSQL Admin Suite
            </p>
          </div>
        </div>
      </div>

      <nav className="flex-1 px-3 py-4 space-y-4">
        <div className="space-y-1">
          <p
            className="px-2 mb-2 text-[10px] font-medium uppercase tracking-widest"
            style={{ color: "var(--color-text-muted)" }}
          >
            Infrastructure
          </p>
          <NavItem to="/clusters" icon={Layers} label="Clusters" />
        </div>

        <div className="space-y-1">
          <p
            className="px-2 mb-2 text-[10px] font-medium uppercase tracking-widest"
            style={{ color: "var(--color-text-muted)" }}
          >
            Modules
          </p>
          {modules.map((m) => (
            <NavItem
              key={m.path}
              to={m.path}
              icon={m.icon}
              label={m.name}
              badge={m.soon ? "Soon" : undefined}
              disabled={m.soon}
            />
          ))}
        </div>
      </nav>

      <div
        className="px-3 py-3 border-t"
        style={{ borderColor: "var(--color-border)" }}
      >
        <NavItem to="/settings" icon={Settings} label="Settings" />
      </div>

      <div
        className="px-5 py-3 border-t"
        style={{ borderColor: "var(--color-border)" }}
      >
        <div className="flex items-center gap-2">
          <Activity
            className="w-3 h-3"
            style={{ color: "var(--color-text-muted)" }}
          />
          <span
            className="text-[10px]"
            style={{ color: "var(--color-text-muted)" }}
          >
            pgmanager
          </span>
          <span className="ml-auto w-1.5 h-1.5 rounded-full bg-emerald-500" />
        </div>
      </div>
    </aside>
  );
}

import { useState, useRef, useEffect } from "react";
import { Clock, ChevronDown, Radio } from "lucide-react";

export interface TimeRange {
  from: string;
  to: string;
  label?: string;
}

interface Props {
  value: TimeRange | null;
  onChange: (range: TimeRange | null) => void;
}

interface Preset {
  label: string;
  duration: number;
}

const presets: Preset[] = [
  { label: "Last 5m", duration: 5 * 60 * 1000 },
  { label: "Last 15m", duration: 15 * 60 * 1000 },
  { label: "Last 1h", duration: 60 * 60 * 1000 },
  { label: "Last 6h", duration: 6 * 60 * 60 * 1000 },
  { label: "Last 12h", duration: 12 * 60 * 60 * 1000 },
  { label: "Last 24h", duration: 24 * 60 * 60 * 1000 },
  { label: "Last 2d", duration: 2 * 24 * 60 * 60 * 1000 },
  { label: "Last 7d", duration: 7 * 24 * 60 * 60 * 1000 },
  { label: "Last 15d", duration: 15 * 24 * 60 * 60 * 1000 },
];

function formatUTCDatetime(d: Date): string {
  const pad = (n: number) => n.toString().padStart(2, "0");
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}T${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`;
}

function parseLocalToUTC(val: string): Date {
  const [datePart, timePart] = val.split("T");
  const [y, m, d] = datePart.split("-").map(Number);
  const [h, min] = timePart.split(":").map(Number);
  return new Date(Date.UTC(y, m - 1, d, h, min));
}

function displayLabel(range: TimeRange | null): string {
  if (!range) return "Live";
  if (range.label) return range.label;
  const f = new Date(range.from);
  const t = new Date(range.to);
  const fmt = (d: Date) =>
    `${d.getUTCMonth() + 1}/${d.getUTCDate()} ${d.getUTCHours().toString().padStart(2, "0")}:${d.getUTCMinutes().toString().padStart(2, "0")}`;
  return `${fmt(f)} — ${fmt(t)} UTC`;
}

export function TimeRangePicker({ value, onChange }: Props) {
  const [open, setOpen] = useState(false);
  const [tab, setTab] = useState<"presets" | "custom">("presets");
  const [customFrom, setCustomFrom] = useState(() =>
    formatUTCDatetime(new Date(Date.now() - 60 * 60 * 1000))
  );
  const [customTo, setCustomTo] = useState(() =>
    formatUTCDatetime(new Date())
  );
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", handleClick);
    return () => document.removeEventListener("mousedown", handleClick);
  }, []);

  function selectPreset(preset: Preset) {
    const now = new Date();
    const from = new Date(now.getTime() - preset.duration);
    onChange({
      from: from.toISOString(),
      to: now.toISOString(),
      label: preset.label,
    });
    setOpen(false);
  }

  function selectLive() {
    onChange(null);
    setOpen(false);
  }

  function applyCustom() {
    const from = parseLocalToUTC(customFrom);
    const to = parseLocalToUTC(customTo);
    if (from >= to) return;
    onChange({ from: from.toISOString(), to: to.toISOString() });
    setOpen(false);
  }

  return (
    <div className="relative" ref={ref}>
      <button
        onClick={() => setOpen(!open)}
        className="flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors hover:bg-white/5"
        style={{
          backgroundColor: "var(--color-surface)",
          borderColor: value ? "var(--color-accent)" : "var(--color-border)",
          color: value ? "var(--color-accent)" : "var(--color-text)",
        }}
      >
        {value ? (
          <Clock className="w-3.5 h-3.5" />
        ) : (
          <Radio className="w-3.5 h-3.5" style={{ color: "#10b981" }} />
        )}
        {displayLabel(value)}
        <ChevronDown className="w-3 h-3" style={{ color: "var(--color-text-muted)" }} />
      </button>

      {open && (
        <div
          className="absolute right-0 top-full mt-1 z-50 rounded-lg border shadow-lg overflow-hidden"
          style={{
            backgroundColor: "var(--color-surface)",
            borderColor: "var(--color-border)",
            minWidth: 280,
          }}
        >
          <div className="flex border-b" style={{ borderColor: "var(--color-border)" }}>
            <button
              className="flex-1 px-3 py-2 text-xs font-medium transition-colors"
              style={{
                backgroundColor: tab === "presets" ? "var(--color-accent)" : "transparent",
                color: tab === "presets" ? "#fff" : "var(--color-text-secondary)",
              }}
              onClick={() => setTab("presets")}
            >
              Quick ranges
            </button>
            <button
              className="flex-1 px-3 py-2 text-xs font-medium transition-colors"
              style={{
                backgroundColor: tab === "custom" ? "var(--color-accent)" : "transparent",
                color: tab === "custom" ? "#fff" : "var(--color-text-secondary)",
              }}
              onClick={() => setTab("custom")}
            >
              Custom
            </button>
          </div>

          {tab === "presets" && (
            <div className="p-2 space-y-0.5">
              <button
                onClick={selectLive}
                className="w-full text-left px-3 py-1.5 rounded text-xs transition-colors hover:bg-white/5 flex items-center gap-2"
                style={{
                  color: !value ? "var(--color-accent)" : "var(--color-text)",
                  fontWeight: !value ? 600 : 400,
                }}
              >
                <Radio className="w-3 h-3" style={{ color: "#10b981" }} />
                Live (real-time)
              </button>
              {presets.map((p) => (
                <button
                  key={p.label}
                  onClick={() => selectPreset(p)}
                  className="w-full text-left px-3 py-1.5 rounded text-xs transition-colors hover:bg-white/5"
                  style={{
                    color: value?.label === p.label ? "var(--color-accent)" : "var(--color-text)",
                    fontWeight: value?.label === p.label ? 600 : 400,
                  }}
                >
                  {p.label}
                </button>
              ))}
            </div>
          )}

          {tab === "custom" && (
            <div className="p-3 space-y-3">
              <div>
                <label
                  className="block text-[10px] font-medium uppercase tracking-wide mb-1"
                  style={{ color: "var(--color-text-muted)" }}
                >
                  From (UTC)
                </label>
                <input
                  type="datetime-local"
                  value={customFrom}
                  onChange={(e) => setCustomFrom(e.target.value)}
                  className="w-full px-2 py-1.5 rounded text-xs border"
                  style={{
                    backgroundColor: "var(--color-bg)",
                    borderColor: "var(--color-border)",
                    color: "var(--color-text)",
                  }}
                />
              </div>
              <div>
                <label
                  className="block text-[10px] font-medium uppercase tracking-wide mb-1"
                  style={{ color: "var(--color-text-muted)" }}
                >
                  To (UTC)
                </label>
                <input
                  type="datetime-local"
                  value={customTo}
                  onChange={(e) => setCustomTo(e.target.value)}
                  className="w-full px-2 py-1.5 rounded text-xs border"
                  style={{
                    backgroundColor: "var(--color-bg)",
                    borderColor: "var(--color-border)",
                    color: "var(--color-text)",
                  }}
                />
              </div>
              <button
                onClick={applyCustom}
                className="w-full py-1.5 rounded text-xs font-medium transition-colors"
                style={{
                  backgroundColor: "var(--color-accent)",
                  color: "#fff",
                }}
              >
                Apply range
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

import { useStore } from "../state";

const quickActions = [
  {
    id: "screenshot",
    label: "Screenshot",
    icon: (
      <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
        <rect x="2" y="3" width="12" height="10" rx="2" stroke="currentColor" strokeWidth="1.5" />
        <circle cx="8" cy="8" r="2" stroke="currentColor" strokeWidth="1.5" />
      </svg>
    ),
  },
  {
    id: "speak-clipboard",
    label: "Speak Clipboard",
    icon: (
      <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
        <path
          d="M3 6v4l4 3V3L3 6z"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinejoin="round"
        />
        <path
          d="M10 5.5a3 3 0 010 5"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
        />
      </svg>
    ),
  },
  {
    id: "system-info",
    label: "System Info",
    icon: (
      <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
        <circle cx="8" cy="8" r="6" stroke="currentColor" strokeWidth="1.5" />
        <path
          d="M8 5v3h2"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    ),
  },
  {
    id: "list-windows",
    label: "List Windows",
    icon: (
      <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
        <rect x="1.5" y="2.5" width="13" height="11" rx="2" stroke="currentColor" strokeWidth="1.5" />
        <path d="M1.5 5.5h13" stroke="currentColor" strokeWidth="1.5" />
      </svg>
    ),
  },
];

interface TopbarProps {
  onQuickAction: (action: string) => void;
}

export function Topbar({ onQuickAction }: TopbarProps) {
  const connected = useStore((s) => s.connected);
  const clips = useStore((s) => s.clips);
  const providers = useStore((s) => s.providers);

  const port = window.location.port || "80";

  return (
    <>
      {/* Compact header */}
      <header className="flex items-center justify-between gap-4 mb-4 py-2">
        <div className="flex items-center gap-2">
          <span
            className="inline-flex items-center justify-center w-7 h-7 rounded-[6px] font-bold text-sm tracking-tight shrink-0"
            style={{
              background: "linear-gradient(135deg, var(--c-accent), var(--c-accent-dark))",
              color: "var(--c-text-inv)",
            }}
          >
            P
          </span>
          <span className="font-bold text-[15px] tracking-tight">Pinix Portal</span>
        </div>
        <div className="flex items-center gap-3">
          <span className="text-[var(--c-text-2)] text-xs font-medium">hub :{port}</span>
          <span className="w-px h-3.5" style={{ background: "var(--c-border)" }} />
          <span className="text-[var(--c-text-2)] text-xs font-medium">
            {providers.length} provider{providers.length === 1 ? "" : "s"}
          </span>
          <span className="w-px h-3.5" style={{ background: "var(--c-border)" }} />
          <span className="text-[var(--c-text-2)] text-xs font-medium">
            {clips.length} clip{clips.length === 1 ? "" : "s"}
          </span>
          <span
            className={`inline-flex items-center gap-1 px-2.5 py-[3px] rounded-full text-[11px] font-semibold uppercase tracking-wide ${
              connected
                ? "text-[var(--c-ok)] bg-[var(--c-ok-bg)]"
                : "text-[var(--c-err)] bg-[var(--c-err-bg)]"
            }`}
          >
            <span
              className={`w-1.5 h-1.5 rounded-full ${
                connected ? "bg-[var(--c-ok)]" : "bg-[var(--c-err)]"
              }`}
            />
            {connected ? "Connected" : "Disconnected"}
          </span>
        </div>
      </header>

      {/* Quick Actions bar */}
      <div className="flex gap-2 mb-4 flex-wrap">
        {quickActions.map((qa) => (
          <button
            key={qa.id}
            type="button"
            onClick={() => onQuickAction(qa.id)}
            className="inline-flex items-center gap-1.5 px-3.5 py-1.5 rounded-full text-xs font-medium cursor-pointer transition-all duration-150 backdrop-blur-[8px]"
            style={{
              border: "1px solid var(--c-border)",
              background: "var(--c-card)",
            }}
            onMouseEnter={(e) => {
              const el = e.currentTarget;
              el.style.background = "var(--c-card-hover)";
              el.style.borderColor = "var(--c-accent)";
              el.style.color = "var(--c-accent)";
              el.style.transform = "translateY(-1px)";
              el.style.boxShadow = "var(--shadow-sm)";
            }}
            onMouseLeave={(e) => {
              const el = e.currentTarget;
              el.style.background = "var(--c-card)";
              el.style.borderColor = "var(--c-border)";
              el.style.color = "";
              el.style.transform = "";
              el.style.boxShadow = "";
            }}
          >
            <span className="inline-flex items-center justify-center shrink-0">{qa.icon}</span>
            {qa.label}
          </button>
        ))}
      </div>
    </>
  );
}

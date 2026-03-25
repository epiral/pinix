import { useStore, getClipCommands, relativeTime } from "../state";
import type { ClipInfo } from "../types";

const RefreshIcon = () => (
  <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
    <path
      d="M2 8a6 6 0 0111.3-2.8M14 8a6 6 0 01-11.3 2.8"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
    />
    <path
      d="M13 2v3.2h-3.2M3 14v-3.2h3.2"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
);

const SearchIcon = () => (
  <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
    <circle cx="7" cy="7" r="4.5" stroke="currentColor" strokeWidth="1.5" />
    <path d="M10.5 10.5L14 14" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
  </svg>
);

const WebIcon = () => (
  <svg
    className="shrink-0 ml-1"
    style={{ color: "var(--c-accent)" }}
    width="12"
    height="12"
    viewBox="0 0 16 16"
    fill="none"
  >
    <circle cx="8" cy="8" r="6" stroke="currentColor" strokeWidth="1.5" />
    <path
      d="M2 8h12M8 2c-2 2-2 10 0 12M8 2c2 2 2 10 0 12"
      stroke="currentColor"
      strokeWidth="1.2"
    />
  </svg>
);

export function Sidebar() {
  const clips = useStore((s) => s.clips);
  const providers = useStore((s) => s.providers);
  const selectedClipName = useStore((s) => s.selectedClipName);
  const searchQuery = useStore((s) => s.searchQuery);
  const collapsedGroups = useStore((s) => s.collapsedGroups);
  const refreshClips = useStore((s) => s.refreshClips);
  const refreshProviders = useStore((s) => s.refreshProviders);
  const setSearchQuery = useStore((s) => s.setSearchQuery);
  const toggleGroup = useStore((s) => s.toggleGroup);
  const selectClip = useStore((s) => s.selectClip);

  // Filter clips by search
  let filteredClips = clips;
  if (searchQuery) {
    filteredClips = clips.filter(
      (c) =>
        c.name.toLowerCase().includes(searchQuery) ||
        (c.provider && c.provider.toLowerCase().includes(searchQuery)) ||
        (c.package && c.package.toLowerCase().includes(searchQuery)) ||
        (c.domain && c.domain.toLowerCase().includes(searchQuery)),
    );
  }

  // Group by provider
  const groups = new Map<string, ClipInfo[]>();
  for (const clip of filteredClips) {
    const provider = clip.provider || "unknown";
    if (!groups.has(provider)) groups.set(provider, []);
    groups.get(provider)!.push(clip);
  }

  return (
    <aside
      className="flex flex-col gap-3 sticky top-5 max-[900px]:static"
      style={{ maxHeight: "calc(100vh - 40px)" }}
    >
      {/* Provider panel */}
      <section
        className="overflow-hidden"
        style={{
          border: "1px solid var(--c-border)",
          borderRadius: "14px",
          background: "var(--c-panel)",
          boxShadow: "var(--shadow-md)",
          backdropFilter: "blur(12px)",
          WebkitBackdropFilter: "blur(12px)",
          maxHeight: "200px",
        }}
      >
        <div
          className="flex items-center justify-between px-4 py-3"
          style={{ borderBottom: "1px solid var(--c-border)" }}
        >
          <h3 className="m-0 text-xs font-semibold uppercase tracking-widest text-[var(--c-text-2)]">
            Providers
          </h3>
          <button
            type="button"
            onClick={() => void refreshProviders()}
            className="inline-flex items-center justify-center w-[26px] h-[26px] border-none rounded-[6px] bg-transparent cursor-pointer transition-all duration-150"
            style={{ color: "var(--c-text-3)" }}
            title="Refresh providers"
          >
            <RefreshIcon />
          </button>
        </div>
        <div
          className="custom-scrollbar px-3 py-2 overflow-y-auto"
          style={{ maxHeight: "140px" }}
        >
          {providers.length === 0 ? (
            <div className="px-3 py-2 text-xs text-center" style={{ color: "var(--c-text-3)" }}>
              No providers connected
            </div>
          ) : (
            providers.map((p) => {
              const clipCount = Array.isArray(p.clips) ? p.clips.length : 0;
              const name = p.name || "unknown";
              const connectedAt = p.connectedAt || p.connected_at || "";
              const timeAgo = relativeTime(connectedAt);
              return (
                <div
                  key={name}
                  className="flex items-center justify-between px-2 py-1 rounded-[6px] text-xs"
                >
                  <div className="flex items-center overflow-hidden">
                    <span
                      className="w-1.5 h-1.5 rounded-full shrink-0 mr-2"
                      style={{ background: "var(--c-ok)" }}
                    />
                    <span
                      className="font-medium overflow-hidden text-ellipsis whitespace-nowrap"
                      style={{ maxWidth: "180px" }}
                      title={name}
                    >
                      {name}
                    </span>
                    {timeAgo && (
                      <span
                        className="shrink-0 ml-1 text-[10px]"
                        style={{ color: "var(--c-text-3)" }}
                        title={connectedAt}
                      >
                        {timeAgo}
                      </span>
                    )}
                  </div>
                  <span className="shrink-0 text-[11px]" style={{ color: "var(--c-text-3)" }}>
                    {clipCount} clip{clipCount === 1 ? "" : "s"}
                  </span>
                </div>
              );
            })
          )}
        </div>
      </section>

      {/* Clips panel */}
      <section
        className="overflow-hidden"
        style={{
          border: "1px solid var(--c-border)",
          borderRadius: "14px",
          background: "var(--c-panel)",
          boxShadow: "var(--shadow-md)",
          backdropFilter: "blur(12px)",
          WebkitBackdropFilter: "blur(12px)",
        }}
      >
        <div
          className="flex items-center justify-between px-4 py-3"
          style={{ borderBottom: "1px solid var(--c-border)" }}
        >
          <h3 className="m-0 text-xs font-semibold uppercase tracking-widest text-[var(--c-text-2)]">
            Clips
          </h3>
          <button
            type="button"
            onClick={() => void refreshClips()}
            className="inline-flex items-center justify-center w-[26px] h-[26px] border-none rounded-[6px] bg-transparent cursor-pointer transition-all duration-150"
            style={{ color: "var(--c-text-3)" }}
            title="Refresh clips"
          >
            <RefreshIcon />
          </button>
        </div>

        {/* Search */}
        <div className="relative px-3 py-2" style={{ borderBottom: "1px solid var(--c-border)" }}>
          <span
            className="absolute top-1/2 -translate-y-1/2 pointer-events-none"
            style={{ left: "calc(12px + 8px)", color: "var(--c-text-3)" }}
          >
            <SearchIcon />
          </span>
          <input
            id="clipSearch"
            type="text"
            className="w-full py-1.5 pr-2 text-xs outline-none transition-[border-color] duration-150"
            style={{
              paddingLeft: "28px",
              border: "1px solid var(--c-border-card)",
              borderRadius: "6px",
              background: "var(--c-input-bg)",
            }}
            placeholder="Search clips... (Cmd+K)"
            autoComplete="off"
            spellCheck={false}
            value={searchQuery ? searchQuery : ""}
            onChange={(e) => setSearchQuery(e.target.value)}
            onFocus={(e) => (e.currentTarget.style.borderColor = "var(--c-accent)")}
            onBlur={(e) => (e.currentTarget.style.borderColor = "var(--c-border-card)")}
          />
        </div>

        {/* Clip list */}
        <div
          className="flex flex-col p-2 gap-px overflow-y-auto custom-scrollbar max-[900px]:max-h-[300px]"
          style={{ maxHeight: "calc(100vh - 380px)" }}
        >
          {clips.length === 0 ? (
            <div className="px-3 py-2 text-xs text-center" style={{ color: "var(--c-text-3)" }}>
              No clips registered.
            </div>
          ) : filteredClips.length === 0 ? (
            <div className="px-3 py-2 text-xs text-center" style={{ color: "var(--c-text-3)" }}>
              No clips match your search.
            </div>
          ) : (
            Array.from(groups).map(([provider, groupClips]) => (
              <div key={provider} className="mb-1">
                {/* Group header */}
                <button
                  type="button"
                  className="flex items-center justify-between gap-2 px-2 py-1 border-none rounded-[6px] bg-transparent w-full text-left cursor-pointer transition-[background] duration-100 select-none"
                  onClick={() => toggleGroup(provider)}
                  onMouseEnter={(e) =>
                    (e.currentTarget.style.background = "var(--c-card)")
                  }
                  onMouseLeave={(e) =>
                    (e.currentTarget.style.background = "transparent")
                  }
                >
                  <span
                    className="text-[11px] font-semibold uppercase tracking-wide overflow-hidden text-ellipsis whitespace-nowrap"
                    style={{ color: "var(--c-text-2)" }}
                    title={provider}
                  >
                    {provider}
                  </span>
                  <span className="flex items-center gap-1">
                    <span
                      className="text-[10px] shrink-0"
                      style={{ color: "var(--c-text-3)" }}
                    >
                      {groupClips.length}
                    </span>
                    <span
                      className="text-[10px] shrink-0 transition-transform duration-150"
                      style={{
                        color: "var(--c-text-3)",
                        transform: collapsedGroups[provider] ? "rotate(-90deg)" : "rotate(0deg)",
                      }}
                    >
                      &#9660;
                    </span>
                  </span>
                </button>

                {/* Group items */}
                {!collapsedGroups[provider] && (
                  <div className="flex flex-col gap-px pt-0.5">
                    {groupClips.map((clip) => {
                      const commandCount = getClipCommands(clip).length;
                      const pkgInfo =
                        clip.package && clip.package !== clip.name ? clip.package : "";
                      const versionInfo = clip.version ? `v${clip.version}` : "";
                      const metaParts = [pkgInfo, versionInfo].filter(Boolean).join(" ");
                      const isActive = clip.name === selectedClipName;

                      return (
                        <button
                          key={clip.name}
                          type="button"
                          className="w-full px-3 py-2 text-left cursor-pointer transition-all duration-100"
                          style={{
                            border: isActive
                              ? "1px solid var(--c-border-active)"
                              : "1px solid transparent",
                            borderRadius: "10px",
                            background: isActive ? "var(--c-card-active)" : "transparent",
                          }}
                          onClick={() => void selectClip(clip.name)}
                          onMouseEnter={(e) => {
                            if (!isActive) e.currentTarget.style.background = "var(--c-card)";
                          }}
                          onMouseLeave={(e) => {
                            if (!isActive) e.currentTarget.style.background = "transparent";
                          }}
                        >
                          <div className="flex items-center justify-between gap-2">
                            <span className="text-[13px] font-semibold overflow-hidden text-ellipsis whitespace-nowrap flex items-center">
                              {clip.name}
                              {clip.hasWeb && <WebIcon />}
                            </span>
                            <span
                              className="text-[11px] shrink-0"
                              style={{ color: "var(--c-text-3)" }}
                            >
                              {commandCount} cmd{commandCount === 1 ? "" : "s"}
                            </span>
                          </div>
                          {metaParts && (
                            <div className="flex items-center gap-2 mt-0.5">
                              <span
                                className="text-[11px] overflow-hidden text-ellipsis whitespace-nowrap"
                                style={{ color: "var(--c-text-3)" }}
                              >
                                {metaParts}
                              </span>
                            </div>
                          )}
                        </button>
                      );
                    })}
                  </div>
                )}
              </div>
            ))
          )}
        </div>
      </section>
    </aside>
  );
}

import { useEffect, useCallback } from "react";
import { useStore, getClipCommands } from "./state";
import { Topbar } from "./components/Topbar";
import { Sidebar } from "./components/Sidebar";
import { ClipDetail } from "./components/ClipDetail";

export default function App() {
  const {
    clips,
    searchQuery,
    selectedClipName,
    expandedCommand,
    refreshClips,
    refreshProviders,
    selectClip,
    setExpandedCommand,
    runQuickAction,
    parseHash,
  } = useStore();

  // ---- Init & auto-refresh ----
  useEffect(() => {
    let mounted = true;

    async function init() {
      await Promise.all([refreshClips(), refreshProviders()]);

      if (!mounted) return;
      const parsed = parseHash();
      if (parsed?.clipName) {
        const state = useStore.getState();
        const clip = state.clips.find((c) => c.name === parsed.clipName);
        if (clip) {
          if (parsed.command) {
            useStore.setState({ expandedCommand: parsed.command });
          }
          await selectClip(parsed.clipName);
        }
      }
    }

    void init();

    const interval = setInterval(() => {
      void refreshClips(true);
      void refreshProviders();
    }, 30000);

    return () => {
      mounted = false;
      clearInterval(interval);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ---- Hash change listener ----
  useEffect(() => {
    function onHashChange() {
      const parsed = parseHash();
      const state = useStore.getState();
      if (parsed) {
        if (state.selectedClipName !== parsed.clipName) {
          useStore.setState({ expandedCommand: parsed.command });
          void selectClip(parsed.clipName);
        } else if (state.expandedCommand !== parsed.command) {
          setExpandedCommand(parsed.command);
        }
      } else {
        useStore.setState({
          selectedClipName: null,
          selectedManifest: null,
          expandedCommand: null,
          lastResult: null,
        });
      }
    }

    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ---- Keyboard shortcuts ----
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      // Cmd+K to focus search
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        const input = document.getElementById("clipSearch") as HTMLInputElement | null;
        if (input) {
          input.focus();
          input.select();
        }
        return;
      }

      // Skip if in input/textarea/select
      const tag = (e.target as HTMLElement).tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;

      const state = useStore.getState();
      let visibleClips = state.clips;
      if (state.searchQuery) {
        visibleClips = state.clips.filter(
          (c) =>
            c.name.toLowerCase().includes(state.searchQuery) ||
            (c.provider && c.provider.toLowerCase().includes(state.searchQuery)) ||
            (c.package && c.package.toLowerCase().includes(state.searchQuery)) ||
            (c.domain && c.domain.toLowerCase().includes(state.searchQuery)),
        );
      }
      if (visibleClips.length === 0) return;

      const clipNames = visibleClips.map((c) => c.name);
      const idx = clipNames.indexOf(state.selectedClipName || "");

      if (e.key === "ArrowDown") {
        e.preventDefault();
        const next = idx < clipNames.length - 1 ? idx + 1 : 0;
        void selectClip(clipNames[next]);
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        const prev = idx > 0 ? idx - 1 : clipNames.length - 1;
        void selectClip(clipNames[prev]);
      } else if (e.key === "Enter" && state.selectedClipName && !state.expandedCommand) {
        e.preventDefault();
        const clip = state.clips.find((c) => c.name === state.selectedClipName);
        if (clip) {
          const commands = getClipCommands(clip);
          if (commands.length > 0) {
            setExpandedCommand(commands[0].name);
          }
        }
      }
    },
    [selectClip, setExpandedCommand],
  );

  useEffect(() => {
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [handleKeyDown]);

  return (
    <div className="max-w-[1440px] mx-auto p-5">
      <Topbar onQuickAction={(action) => void runQuickAction(action)} />
      <main className="grid grid-cols-[300px_minmax(0,1fr)] gap-4 items-start max-[900px]:grid-cols-1">
        <Sidebar />
        <ClipDetail />
      </main>
    </div>
  );
}

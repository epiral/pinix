import { useStore, getClipCommands, clipWebURL, clipStatusInfo } from "../state";
import type { ClipManifest } from "../types";
import { CommandCard } from "./CommandCard";
import { DependenciesPanel } from "./DependenciesPanel";

function normalizeManifestFallback(clip: { name: string; commands: unknown[] }): ClipManifest {
  return {
    name: clip.name,
    package: "",
    version: "",
    domain: "",
    description: "",
    commands: [],
    dependencies: [],
    dependencySlots: {},
    hasWeb: false,
    patterns: [],
  };
}

export function ClipDetail() {
  const selectedClipName = useStore((s) => s.selectedClipName);
  const clips = useStore((s) => s.clips);
  const providers = useStore((s) => s.providers);
  const connected = useStore((s) => s.connected);
  const selectedManifest = useStore((s) => s.selectedManifest);
  const detailError = useStore((s) => s.detailError);

  const clip = clips.find((c) => c.name === selectedClipName);

  if (!clip) {
    return (
      <section
        className="min-h-[70vh]"
        style={{
          border: "1px solid var(--c-border)",
          borderRadius: "14px",
          background: "var(--c-panel)",
          boxShadow: "var(--shadow-md)",
          backdropFilter: "blur(12px)",
          WebkitBackdropFilter: "blur(12px)",
        }}
      >
        <div className="flex items-center justify-center min-h-[50vh] p-6">
          <div className="text-center max-w-[300px]">
            <div className="mb-4" style={{ color: "var(--c-text-3)" }}>
              <svg width="48" height="48" viewBox="0 0 48 48" fill="none">
                <rect x="8" y="10" width="32" height="28" rx="4" stroke="currentColor" strokeWidth="2" opacity="0.3" />
                <path d="M16 22h16M16 28h10" stroke="currentColor" strokeWidth="2" strokeLinecap="round" opacity="0.3" />
              </svg>
            </div>
            <h2 className="m-0 mb-2 text-base font-semibold">Select a clip</h2>
            <p className="m-0 text-[13px]" style={{ color: "var(--c-text-2)" }}>
              Choose a clip from the list to inspect its manifest and run commands.
            </p>
          </div>
        </div>
      </section>
    );
  }

  const manifest = selectedManifest || clip.manifest || normalizeManifestFallback(clip);
  const commands = manifest.commands || [];
  const hasWeb = Boolean(manifest.hasWeb ?? clip.hasWeb);
  const description = manifest.description || "";
  const status = clipStatusInfo(clip, providers, connected);

  return (
    <section
      className="min-h-[70vh]"
      style={{
        border: "1px solid var(--c-border)",
        borderRadius: "14px",
        background: "var(--c-panel)",
        boxShadow: "var(--shadow-md)",
        backdropFilter: "blur(12px)",
        WebkitBackdropFilter: "blur(12px)",
      }}
    >
      <div className="p-5 flex flex-col gap-5">
        {/* Header */}
        <div className="flex items-start justify-between gap-4 max-[600px]:flex-col">
          <div>
            <h2 className="m-0 text-lg font-bold tracking-tight">{clip.name}</h2>
            {description && (
              <p className="mt-1 mb-0 text-[13px]" style={{ color: "var(--c-text-2)" }}>
                {description}
              </p>
            )}
          </div>
          <div className="flex items-center gap-2 shrink-0">
            {hasWeb && (
              <a
                href={clipWebURL(clip.name)}
                className="inline-flex items-center gap-1.5 px-4.5 py-[7px] border-none rounded-full text-xs font-semibold no-underline cursor-pointer transition-all duration-150"
                style={{
                  background: "linear-gradient(135deg, var(--c-accent), var(--c-accent-dark))",
                  color: "var(--c-text-inv)",
                  boxShadow: "var(--shadow-sm)",
                }}
                onMouseEnter={(e) => {
                  e.currentTarget.style.transform = "translateY(-1px)";
                  e.currentTarget.style.boxShadow = "var(--shadow-md)";
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.transform = "";
                  e.currentTarget.style.boxShadow = "var(--shadow-sm)";
                }}
              >
                Open Web UI
              </a>
            )}
            <span
              className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-semibold uppercase tracking-wide ${
                status.cls === "running"
                  ? "text-[var(--c-ok)] bg-[var(--c-ok-bg)]"
                  : "text-[var(--c-warn)] bg-[var(--c-warn-bg)]"
              }`}
            >
              {status.label}
            </span>
          </div>
        </div>

        {/* Meta grid */}
        <div className="grid gap-2 max-[600px]:grid-cols-2" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(140px, 1fr))" }}>
          {[
            { label: "Domain", value: manifest.domain || "-" },
            { label: "Provider", value: clip.provider || "-" },
            { label: "Package", value: manifest.package || clip.package || "-" },
            { label: "Commands", value: String(commands.length) },
          ].map((item) => (
            <div
              key={item.label}
              className="p-3"
              style={{
                border: "1px solid var(--c-border-card)",
                borderRadius: "10px",
                background: "var(--c-card)",
              }}
            >
              <span
                className="block mb-0.5 text-[10px] font-semibold uppercase tracking-widest"
                style={{ color: "var(--c-text-3)" }}
              >
                {item.label}
              </span>
              <div className="text-[13px] font-medium break-words">{item.value}</div>
            </div>
          ))}
        </div>

        {/* Error notice */}
        {detailError && (
          <div
            className="px-3 py-2 rounded-[6px] text-xs font-medium"
            style={{ background: "var(--c-err-bg)", color: "var(--c-err)" }}
          >
            {detailError}
          </div>
        )}

        {/* Dependencies */}
        <DependenciesPanel clip={clip} />

        {/* Commands */}
        <section>
          <h3
            className="m-0 text-[13px] font-semibold uppercase tracking-wide"
            style={{ color: "var(--c-text-2)" }}
          >
            Commands
          </h3>
          <div className="flex flex-col gap-2 mt-3">
            {commands.length === 0 ? (
              <div className="px-3 py-2 text-xs text-center" style={{ color: "var(--c-text-3)" }}>
                No commands found in the manifest.
              </div>
            ) : (
              commands.map((cmd) => <CommandCard key={cmd.name} clip={clip} command={cmd} />)
            )}
          </div>
        </section>
      </div>
    </section>
  );
}

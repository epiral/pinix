import { useStore, getBindingCandidates } from "../state";
import type { ClipInfo, DependencySlot } from "../types";

interface DependenciesPanelProps {
  clip: ClipInfo;
}

export function DependenciesPanel({ clip }: DependenciesPanelProps) {
  const selectedManifest = useStore((s) => s.selectedManifest);
  const bindings = useStore((s) => s.bindings);
  const bindingsLoading = useStore((s) => s.bindingsLoading);
  const clips = useStore((s) => s.clips);
  const setBinding = useStore((s) => s.setBinding);
  const removeBinding = useStore((s) => s.removeBinding);

  const manifest = selectedManifest || clip.manifest;

  // Merge sources: GetBindings RPC response, manifest dependencySlots, or fallback from dependencies string array
  let slots: Record<string, DependencySlot> =
    bindings?.dependencySlots || manifest?.dependencySlots || {};
  const boundBindings = bindings?.bindings || {};

  // Fallback: if no slots but there are string dependencies, create slots from them
  if (Object.keys(slots).length === 0) {
    const deps = manifest?.dependencies || clip.dependencies || [];
    if (Array.isArray(deps) && deps.length > 0) {
      slots = {};
      for (const d of deps) slots[d] = { package: d, version: "" };
    }
  }

  const slotNames = Object.keys(slots);
  if (slotNames.length === 0 && !bindingsLoading) return null;

  if (bindingsLoading) {
    return (
      <section className="mb-4">
        <h3
          className="m-0 text-[13px] font-semibold uppercase tracking-wide"
          style={{ color: "var(--c-text-2)" }}
        >
          Dependencies
        </h3>
        <div className="p-3 text-xs text-center" style={{ color: "var(--c-text-3)" }}>
          Loading bindings...
        </div>
      </section>
    );
  }

  return (
    <section className="mb-4">
      <h3
        className="m-0 text-[13px] font-semibold uppercase tracking-wide"
        style={{ color: "var(--c-text-2)" }}
      >
        Dependencies
      </h3>
      <div className="flex flex-col gap-2 mt-3">
        {slotNames.map((slot) => {
          const dep = slots[slot] || { package: "", version: "" };
          const binding = boundBindings[slot];
          const candidates = getBindingCandidates(dep, slot, clips);
          const boundAlias = binding?.alias || "";
          const isBound = Boolean(boundAlias);

          return (
            <div
              key={slot}
              className="flex items-center justify-between gap-3 px-3.5 py-2.5"
              style={{
                border: "1px solid var(--c-border-card)",
                borderRadius: "10px",
                background: "var(--c-card)",
              }}
            >
              {/* Slot info */}
              <div className="flex flex-col gap-0.5 min-w-0">
                <span className="text-[13px] font-semibold">{slot}</span>
                {((dep.package && dep.package !== slot) || dep.version) && (
                  <span
                    className="text-[11px]"
                    style={{ color: "var(--c-text-3)", fontFamily: "var(--f-mono)" }}
                  >
                    {dep.package || slot}
                    {dep.version ? `@${dep.version}` : ""}
                  </span>
                )}
              </div>

              {/* Binding controls */}
              <div className="flex items-center gap-2 shrink-0">
                <select
                  className="text-xs py-1 px-2 cursor-pointer"
                  style={{
                    fontFamily: "var(--f-mono)",
                    border: "1px solid var(--c-border)",
                    borderRadius: "6px",
                    background: "var(--c-input-bg)",
                    color: "var(--c-text)",
                    minWidth: "140px",
                  }}
                  value={boundAlias}
                  onChange={(e) => {
                    const alias = e.target.value;
                    if (alias) {
                      void setBinding(clip.name, slot, alias);
                    } else {
                      void removeBinding(clip.name, slot);
                    }
                  }}
                >
                  <option value="">-- unbound --</option>
                  {candidates.map((c) => (
                    <option key={c.name} value={c.name}>
                      {c.name}
                      {c.version ? ` (${c.version})` : ""}
                    </option>
                  ))}
                </select>
                <span
                  className="text-[11px] font-medium px-2 py-0.5 rounded-full whitespace-nowrap"
                  style={{
                    color: isBound ? "var(--c-ok)" : "var(--c-text-3)",
                    background: isBound ? "var(--c-ok-bg)" : "var(--c-border)",
                  }}
                >
                  {isBound ? "bound" : "unbound"}
                </span>
              </div>
            </div>
          );
        })}
      </div>
    </section>
  );
}

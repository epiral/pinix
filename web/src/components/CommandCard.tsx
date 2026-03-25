import { useStore, getCommandSchemaState, truncate } from "../state";
import type { ClipInfo, CommandInfo } from "../types";
import { SchemaForm } from "./SchemaForm";
import { ResultSection } from "./ResultSection";

interface CommandCardProps {
  clip: ClipInfo;
  command: CommandInfo;
}

export function CommandCard({ clip, command }: CommandCardProps) {
  const expandedCommand = useStore((s) => s.expandedCommand);
  const runningCommand = useStore((s) => s.runningCommand);
  const lastResult = useStore((s) => s.lastResult);
  const setExpandedCommand = useStore((s) => s.setExpandedCommand);
  const invokeCommand = useStore((s) => s.invokeCommand);
  const setCommandInput = useStore((s) => s.setCommandInput);
  const getCommandInput = useStore((s) => s.getCommandInput);

  const commandName = command.name;
  const expanded = expandedCommand === commandName;
  const running = runningCommand === commandName;
  const commandDescription = command.description || "";

  const schemaState = getCommandSchemaState(command);

  const toggleExpand = () => {
    setExpandedCommand(expanded ? null : commandName);
  };

  const hasInlineResult =
    lastResult?.command === commandName && lastResult?.clip === clip.name;

  return (
    <article
      style={{
        border: expanded ? "1px solid var(--c-border-active)" : "1px solid var(--c-border-card)",
        borderRadius: "10px",
        background: expanded ? "var(--c-card-active)" : "var(--c-card)",
        overflow: "hidden",
        transition: "all 0.15s ease",
      }}
    >
      {/* Toggle header */}
      <button
        type="button"
        className="w-full flex items-center justify-between gap-3 px-4 py-3 border-none bg-transparent text-left cursor-pointer transition-[background] duration-100"
        onClick={toggleExpand}
        onMouseEnter={(e) => (e.currentTarget.style.background = "var(--c-card-hover)")}
        onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}
      >
        <span>
          <span className="font-semibold text-[13px]">{commandName}</span>
          {commandDescription && (
            <span className="ml-2 text-xs font-normal" style={{ color: "var(--c-text-3)" }}>
              {truncate(commandDescription, 60)}
            </span>
          )}
        </span>
        <span
          className="text-[11px] shrink-0 transition-transform duration-150"
          style={{
            color: "var(--c-text-3)",
            transform: expanded ? "rotate(90deg)" : "rotate(0deg)",
          }}
        >
          {expanded ? "\u25BE" : "\u25B6"}
        </span>
      </button>

      {/* Expanded body */}
      {expanded && (
        <div className="px-4 pb-4 flex flex-col gap-3">
          {commandDescription && (
            <p className="m-0 text-xs" style={{ color: "var(--c-text-2)" }}>
              {commandDescription}
            </p>
          )}

          {/* Form / input area */}
          {schemaState.kind === "schema" && schemaState.schema ? (
            <SchemaForm
              clipName={clip.name}
              commandName={commandName}
              schema={schemaState.schema}
              onSubmit={() => void invokeCommand(command)}
            />
          ) : schemaState.kind === "empty" ? (
            <p className="m-0 text-xs" style={{ color: "var(--c-text-2)" }}>
              This command requires no input.
            </p>
          ) : (
            <div className="flex flex-col gap-1">
              <label
                className="text-[11px] font-semibold uppercase tracking-wide"
                style={{ color: "var(--c-text-2)" }}
              >
                Input JSON
              </label>
              <textarea
                className="min-h-[100px] p-3 text-xs leading-6 resize-y outline-none transition-[border-color] duration-150"
                style={{
                  border: "1px solid var(--c-border-card)",
                  borderRadius: "6px",
                  background: "var(--c-input-bg)",
                  fontFamily: "var(--f-mono)",
                }}
                spellCheck={false}
                value={getCommandInput(clip.name, commandName)}
                onChange={(e) => setCommandInput(clip.name, commandName, e.target.value)}
                onFocus={(e) => (e.currentTarget.style.borderColor = "var(--c-accent)")}
                onBlur={(e) => (e.currentTarget.style.borderColor = "var(--c-border-card)")}
              />
            </div>
          )}

          {/* Run button */}
          <div className="flex items-center justify-end gap-3">
            <button
              type="button"
              disabled={running}
              onClick={() => void invokeCommand(command)}
              className="inline-flex items-center gap-1.5 px-4.5 py-[7px] border-none rounded-full text-xs font-semibold cursor-pointer transition-all duration-150"
              style={{
                background: "linear-gradient(135deg, var(--c-accent), var(--c-accent-dark))",
                color: "var(--c-text-inv)",
                boxShadow: "var(--shadow-sm)",
                opacity: running ? 0.5 : 1,
                cursor: running ? "not-allowed" : "pointer",
              }}
              onMouseEnter={(e) => {
                if (!running) {
                  e.currentTarget.style.transform = "translateY(-1px)";
                  e.currentTarget.style.boxShadow = "var(--shadow-md)";
                }
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.transform = "";
                e.currentTarget.style.boxShadow = "var(--shadow-sm)";
              }}
            >
              {running ? (
                <>
                  <span className="spinner" />
                  Running...
                </>
              ) : (
                "Run"
              )}
            </button>
          </div>

          {/* Inline result */}
          {hasInlineResult && <ResultSection />}
        </div>
      )}
    </article>
  );
}

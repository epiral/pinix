import { useStore, syntaxHighlightJSON, escapeHTML, extractBase64Image } from "../state";

export function ResultSection() {
  const lastResult = useStore((s) => s.lastResult);
  const resultExpanded = useStore((s) => s.resultExpanded);
  const setResultExpanded = useStore((s) => s.setResultExpanded);

  if (!lastResult) return null;

  // Build result text
  let bodyText: string;
  if (typeof lastResult.payload === "string") {
    bodyText = lastResult.payload;
  } else {
    bodyText = JSON.stringify(lastResult.payload, null, 2);
  }

  // Build highlighted HTML
  let bodyHTML: string;
  try {
    const parsed = JSON.parse(bodyText);
    bodyHTML = syntaxHighlightJSON(parsed);
  } catch {
    bodyHTML = escapeHTML(bodyText);
  }

  const isLarge = bodyText.length > 1500;
  const imagePreview = extractBase64Image(lastResult.payload);
  const meta = `${lastResult.clip}/${lastResult.command} - ${lastResult.meta}`;

  return (
    <section
      style={{
        border: "1px solid var(--c-border-card)",
        borderRadius: "10px",
        background: "var(--c-card)",
        overflow: "hidden",
      }}
    >
      {/* Header */}
      <div
        className="flex items-center justify-between px-4 py-3 gap-3"
        style={{ borderBottom: "1px solid var(--c-border)" }}
      >
        <span
          className="text-xs font-semibold uppercase tracking-wide"
          style={{ color: "var(--c-text-2)" }}
        >
          Result
        </span>
        <span
          className="text-[11px] text-right"
          style={{
            color: lastResult.ok ? "var(--c-ok)" : lastResult.ok === false ? "var(--c-err)" : "var(--c-text-3)",
          }}
        >
          {meta}
        </span>
      </div>

      {/* Image preview */}
      {imagePreview && (
        <div className="p-4 text-center" style={{ background: "var(--c-code-bg)" }}>
          <img
            src={imagePreview}
            alt="Result image"
            className="max-w-full rounded-[6px]"
            style={{ maxHeight: "400px", boxShadow: "var(--shadow-md)" }}
          />
        </div>
      )}

      {/* Code output */}
      <pre
        className="m-0 p-4 overflow-auto text-xs leading-[1.6] relative"
        style={{
          background: "var(--c-code-bg)",
          fontFamily: "var(--f-mono)",
          color: "var(--c-code-text)",
          maxHeight: isLarge && !resultExpanded ? "200px" : "500px",
        }}
        dangerouslySetInnerHTML={{ __html: bodyHTML }}
      />

      {/* Gradient overlay for collapsed large output */}
      {isLarge && !resultExpanded && (
        <div
          className="pointer-events-none absolute bottom-0 left-0 right-0 h-[60px]"
          style={{
            background: "linear-gradient(to bottom, transparent, var(--c-code-bg))",
          }}
        />
      )}

      {/* Toggle button for large output */}
      {isLarge && (
        <button
          type="button"
          className="block w-full py-2 border-none text-[11px] font-semibold cursor-pointer text-center transition-opacity duration-100"
          style={{
            background: "var(--c-code-bg)",
            color: "var(--c-accent)",
            borderTop: "1px solid var(--c-border)",
          }}
          onClick={() => setResultExpanded(!resultExpanded)}
          onMouseEnter={(e) => (e.currentTarget.style.opacity = "0.8")}
          onMouseLeave={(e) => (e.currentTarget.style.opacity = "1")}
        >
          {resultExpanded ? "Collapse" : "Show all"}
        </button>
      )}
    </section>
  );
}

import { useStore, getSchemaProperties, getSchemaRequiredSet, getSchemaFieldKind, humanizeFieldName } from "../state";
import type { JsonSchema } from "../state";

interface SchemaFormProps {
  clipName: string;
  commandName: string;
  schema: JsonSchema;
  onSubmit: () => void;
}

export function SchemaForm({ clipName, commandName, schema, onSubmit }: SchemaFormProps) {
  const setSmartInput = useStore((s) => s.setSmartInput);
  const getSmartInput = useStore((s) => s.getSmartInput);

  const required = getSchemaRequiredSet(schema);
  const properties = getSchemaProperties(schema);

  if (properties.length === 0) return null;

  return (
    <div className="flex gap-2 items-end max-[600px]:flex-col">
      {properties.map(([fieldName, fieldSchema]) => {
        const normalized: JsonSchema =
          fieldSchema && typeof fieldSchema === "object" && !Array.isArray(fieldSchema)
            ? fieldSchema
            : {};
        const kind = getSchemaFieldKind(normalized);
        const value = getSmartInput(clipName, commandName, fieldName);
        const label = String(normalized.title || humanizeFieldName(fieldName)).trim() || fieldName;
        const labelText = `${label}${required.has(fieldName) ? " *" : ""}`;
        const placeholder = String(normalized.description || "").trim();

        const handleChange = (newValue: string | boolean) => {
          setSmartInput(clipName, commandName, fieldName, newValue);
        };

        if (kind === "enum") {
          const currentValue = value === "" ? "" : String(value);
          return (
            <div key={fieldName} className="flex flex-col gap-1 flex-1">
              <label
                className="text-[11px] font-semibold uppercase tracking-wide"
                style={{ color: "var(--c-text-2)" }}
              >
                {labelText}
              </label>
              <select
                className="py-[7px] px-2.5 text-[13px] outline-none transition-[border-color] duration-150"
                style={{
                  border: "1px solid var(--c-border-card)",
                  borderRadius: "6px",
                  background: "var(--c-input-bg)",
                }}
                value={currentValue}
                onChange={(e) => handleChange(e.target.value)}
                onFocus={(e) => (e.currentTarget.style.borderColor = "var(--c-accent)")}
                onBlur={(e) => (e.currentTarget.style.borderColor = "var(--c-border-card)")}
              >
                <option value="">{placeholder || "Select an option"}</option>
                {(normalized.enum || []).map((opt) => (
                  <option key={opt} value={opt}>
                    {opt}
                  </option>
                ))}
              </select>
            </div>
          );
        }

        if (kind === "boolean") {
          return (
            <div key={fieldName} className="flex flex-col gap-1 flex-1">
              <label
                className="text-[11px] font-semibold uppercase tracking-wide"
                style={{ color: "var(--c-text-2)" }}
              >
                {labelText}
              </label>
              <input
                type="checkbox"
                className="w-4 h-4 p-0"
                style={{ accentColor: "var(--c-accent)" }}
                checked={Boolean(value)}
                onChange={(e) => handleChange(e.target.checked)}
                title={placeholder}
              />
            </div>
          );
        }

        if (kind === "json") {
          const textValue = value === "" ? "" : String(value);
          return (
            <div key={fieldName} className="flex flex-col gap-1 flex-1 w-full">
              <label
                className="text-[11px] font-semibold uppercase tracking-wide"
                style={{ color: "var(--c-text-2)" }}
              >
                {labelText}
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
                placeholder={placeholder}
                value={textValue}
                onChange={(e) => handleChange(e.target.value)}
                onFocus={(e) => (e.currentTarget.style.borderColor = "var(--c-accent)")}
                onBlur={(e) => (e.currentTarget.style.borderColor = "var(--c-border-card)")}
              />
            </div>
          );
        }

        // string, number, integer, date
        const inputType =
          kind === "date"
            ? "date"
            : kind === "number" || kind === "integer"
              ? "number"
              : "text";
        const textValue = value === "" ? "" : String(value);
        const stepAttr =
          kind === "integer" ? "1" : kind === "number" ? "any" : undefined;

        return (
          <div
            key={fieldName}
            className="flex flex-col gap-1 flex-1"
            style={{
              maxWidth:
                kind === "number" || kind === "integer"
                  ? "120px"
                  : kind === "date"
                    ? "180px"
                    : undefined,
            }}
          >
            <label
              className="text-[11px] font-semibold uppercase tracking-wide"
              style={{ color: "var(--c-text-2)" }}
            >
              {labelText}
            </label>
            <input
              type={inputType}
              className="py-[7px] px-2.5 text-[13px] outline-none transition-[border-color] duration-150"
              style={{
                border: "1px solid var(--c-border-card)",
                borderRadius: "6px",
                background: "var(--c-input-bg)",
              }}
              value={textValue}
              placeholder={placeholder}
              step={stepAttr}
              onChange={(e) => handleChange(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") onSubmit();
              }}
              onFocus={(e) => (e.currentTarget.style.borderColor = "var(--c-accent)")}
              onBlur={(e) => (e.currentTarget.style.borderColor = "var(--c-border-card)")}
            />
          </div>
        );
      })}
    </div>
  );
}

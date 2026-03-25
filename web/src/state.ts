import { create } from "zustand";
import type {
  ClipInfo,
  ClipManifest,
  CommandInfo,
  ProviderInfo,
  BindingsData,
  CommandResult,
  DependencySlot,
} from "./types";
import { hubUnary, hubInvoke, PROCEDURES } from "./api";

// ---- Normalization helpers ----

function normalizeStringArray(values: unknown): string[] {
  if (!Array.isArray(values)) return [];
  const result: string[] = [];
  const seen = new Set<string>();
  for (const value of values) {
    const text = String(value || "").trim();
    if (!text || seen.has(text)) continue;
    seen.add(text);
    result.push(text);
  }
  return result.sort();
}

function normalizeCommandInfo(command: unknown): CommandInfo | null {
  if (typeof command === "string") {
    const name = command.trim();
    return name ? { name, description: "", input: "", output: "" } : null;
  }
  if (!command) return null;
  const c = command as Record<string, unknown>;
  const name = String(c.name || "").trim();
  if (!name) return null;
  return {
    name,
    description: String(c.description || "").trim(),
    input: String(c.input || "").trim(),
    output: String(c.output || "").trim(),
  };
}

function normalizeManifest(
  manifest: Record<string, unknown> | null | undefined,
  fallback: Record<string, unknown> | null = null,
): ClipManifest | null {
  if (!manifest && !fallback) return null;
  const m = manifest || {};
  const f = fallback || {};

  const commandSource = Array.isArray(m.commands)
    ? m.commands
    : Array.isArray(f.commands)
      ? f.commands
      : [];

  return {
    name: String(m.name || f.name || "").trim(),
    package: String(m.package || f.package || "").trim(),
    version: String(m.version || f.version || "").trim(),
    domain: String(m.domain || f.domain || "").trim(),
    description: String(m.description || "").trim(),
    commands: commandSource.map(normalizeCommandInfo).filter(Boolean) as CommandInfo[],
    dependencies: normalizeStringArray(m.dependencies || f.dependencies),
    dependencySlots:
      (m.dependencySlots as Record<string, DependencySlot>) ||
      (m.dependency_slots as Record<string, DependencySlot>) ||
      {},
    hasWeb: Boolean(m.hasWeb ?? m.has_web ?? f.hasWeb ?? f.has_web),
    patterns: normalizeStringArray(m.patterns),
  };
}

function normalizeClipInfo(clip: Record<string, unknown>): ClipInfo {
  const commands = Array.isArray(clip.commands)
    ? (clip.commands.map(normalizeCommandInfo).filter(Boolean) as CommandInfo[])
    : [];
  const manifestRaw = clip.manifest as Record<string, unknown> | null;
  return {
    name: String(clip.name || "").trim(),
    package: String(clip.package || "").trim(),
    version: String(clip.version || "").trim(),
    provider: String(clip.provider || "").trim(),
    domain: String(clip.domain || "").trim(),
    commands,
    hasWeb: Boolean(clip.hasWeb ?? clip.has_web),
    tokenProtected: Boolean(clip.tokenProtected ?? clip.token_protected),
    dependencies: normalizeStringArray(clip.dependencies),
    manifest: manifestRaw ? normalizeManifest(manifestRaw, clip) : null,
  };
}

// ---- Store ----

export interface AppState {
  clips: ClipInfo[];
  providers: ProviderInfo[];
  selectedClipName: string | null;
  selectedManifest: ClipManifest | null;
  detailError: string | null;
  expandedCommand: string | null;
  inputs: Record<string, string>;
  smartInputs: Record<string, string | boolean>;
  runningCommand: string | null;
  lastResult: CommandResult | null;
  searchQuery: string;
  collapsedGroups: Record<string, boolean>;
  resultExpanded: boolean;
  connected: boolean;
  bindings: BindingsData | null;
  bindingsLoading: boolean;

  // Actions
  setSearchQuery: (query: string) => void;
  toggleGroup: (provider: string) => void;
  setExpandedCommand: (cmd: string | null) => void;
  setResultExpanded: (expanded: boolean) => void;
  setSmartInput: (clipName: string, commandName: string, fieldName: string, value: string | boolean) => void;
  getSmartInput: (clipName: string, commandName: string, fieldName: string) => string | boolean;
  hasSmartInput: (clipName: string, commandName: string, fieldName: string) => boolean;
  setCommandInput: (clipName: string, commandName: string, value: string) => void;
  getCommandInput: (clipName: string, commandName: string) => string;

  refreshClips: (silent?: boolean) => Promise<void>;
  refreshProviders: () => Promise<void>;
  selectClip: (name: string) => Promise<void>;
  loadManifest: (name: string, preserveResult?: boolean) => Promise<void>;
  loadBindings: (clipName: string) => Promise<void>;
  setBinding: (clipName: string, slot: string, alias: string) => Promise<void>;
  removeBinding: (clipName: string, slot: string) => Promise<void>;
  invokeCommand: (commandObj: CommandInfo) => Promise<void>;
  runQuickAction: (action: string) => Promise<void>;
  updateHash: () => void;
  parseHash: () => { clipName: string; command: string | null } | null;
}

export const useStore = create<AppState>((set, get) => ({
  clips: [],
  providers: [],
  selectedClipName: null,
  selectedManifest: null,
  detailError: null,
  expandedCommand: null,
  inputs: {},
  smartInputs: {},
  runningCommand: null,
  lastResult: null,
  searchQuery: "",
  collapsedGroups: {},
  resultExpanded: false,
  connected: true,
  bindings: null,
  bindingsLoading: false,

  setSearchQuery: (query) => set({ searchQuery: query.trim().toLowerCase() }),
  toggleGroup: (provider) =>
    set((s) => ({
      collapsedGroups: {
        ...s.collapsedGroups,
        [provider]: !s.collapsedGroups[provider],
      },
    })),
  setExpandedCommand: (cmd) => {
    set({ expandedCommand: cmd });
    get().updateHash();
  },
  setResultExpanded: (expanded) => set({ resultExpanded: expanded }),

  setSmartInput: (clipName, commandName, fieldName, value) =>
    set((s) => ({
      smartInputs: {
        ...s.smartInputs,
        [`${clipName}:${commandName}:${fieldName}`]: value,
      },
    })),
  getSmartInput: (clipName, commandName, fieldName) => {
    const key = `${clipName}:${commandName}:${fieldName}`;
    const val = get().smartInputs[key];
    return val !== undefined ? val : "";
  },
  hasSmartInput: (clipName, commandName, fieldName) => {
    const key = `${clipName}:${commandName}:${fieldName}`;
    return key in get().smartInputs;
  },

  setCommandInput: (clipName, commandName, value) =>
    set((s) => ({
      inputs: { ...s.inputs, [`${clipName}:${commandName}`]: value },
    })),
  getCommandInput: (clipName, commandName) => {
    const key = `${clipName}:${commandName}`;
    return get().inputs[key] ?? "{}";
  },

  refreshClips: async (silent = false) => {
    try {
      const payload = await hubUnary(PROCEDURES.listClips, {});
      const clips = Array.isArray(payload?.clips)
        ? (payload.clips as Record<string, unknown>[]).map(normalizeClipInfo)
        : [];
      const state = get();
      let selectedClipName = state.selectedClipName;
      let selectedManifest = state.selectedManifest;
      let expandedCommand = state.expandedCommand;
      let detailError = state.detailError;

      if (selectedClipName) {
        const current = clips.find((c) => c.name === selectedClipName);
        if (!current) {
          selectedClipName = null;
          selectedManifest = null;
          expandedCommand = null;
          detailError = null;
        }
      }

      set({
        clips,
        connected: true,
        selectedClipName,
        selectedManifest,
        expandedCommand,
        detailError,
      });

      if (selectedClipName) {
        await get().loadManifest(selectedClipName, true);
      }
    } catch (error) {
      set({ connected: false });
      if (!silent) {
        set({ detailError: String((error as Error).message || error) });
      }
    }
  },

  refreshProviders: async () => {
    try {
      const payload = await hubUnary(PROCEDURES.listProviders, {});
      const providers = Array.isArray(payload?.providers) ? (payload.providers as ProviderInfo[]) : [];
      set({ providers });
    } catch {
      set({ providers: [] });
    }
  },

  selectClip: async (name) => {
    const state = get();
    if (state.selectedClipName !== name) {
      set({ lastResult: null, resultExpanded: false, bindings: null });
    }

    const clip = get().clips.find((c) => c.name === name);
    set({
      selectedClipName: name,
      expandedCommand: null,
      detailError: null,
      selectedManifest: clip?.manifest || null,
    });
    get().updateHash();

    await Promise.all([get().loadManifest(name, true), get().loadBindings(name)]);
  },

  loadManifest: async (name, preserveResult = false) => {
    const state = get();
    const selected = state.clips.find((c) => c.name === name);
    if (!selected || state.selectedClipName !== name) return;

    if (!preserveResult) set({ lastResult: null });
    set({ detailError: null });

    try {
      const payload = await hubUnary(PROCEDURES.getManifest, { clipName: name });
      const manifest = normalizeManifest(payload?.manifest as Record<string, unknown>, selected as unknown as Record<string, unknown>);
      // Update the clip's cached manifest
      set((s) => ({
        selectedManifest: manifest,
        clips: s.clips.map((c) =>
          c.name === name ? { ...c, manifest: manifest } : c,
        ),
      }));
    } catch (error) {
      const fallbackManifest =
        selected.manifest ||
        normalizeManifest(null, selected as unknown as Record<string, unknown>);
      set({
        selectedManifest: fallbackManifest,
        detailError: String((error as Error).message || error),
      });
    }
  },

  loadBindings: async (clipName) => {
    set({ bindingsLoading: true });
    try {
      const payload = await hubUnary(PROCEDURES.getBindings, { clipName });
      set({
        bindings: {
          bindings: (payload.bindings as Record<string, { alias: string }>) || {},
          dependencySlots:
            (payload.dependencySlots as Record<string, DependencySlot>) ||
            (payload.dependency_slots as Record<string, DependencySlot>) ||
            {},
        },
        bindingsLoading: false,
      });
    } catch {
      set({ bindings: null, bindingsLoading: false });
    }
  },

  setBinding: async (clipName, slot, alias) => {
    try {
      await hubUnary(PROCEDURES.setBinding, {
        clipName,
        slot,
        binding: { alias },
      });
      await get().loadBindings(clipName);
      showToast(`Bound ${slot} -> ${alias}`);
    } catch (error) {
      showToast(`Bind failed: ${(error as Error).message || error}`);
    }
  },

  removeBinding: async (clipName, slot) => {
    try {
      await hubUnary(PROCEDURES.removeBinding, { clipName, slot });
      await get().loadBindings(clipName);
      showToast(`Unbound ${slot}`);
    } catch (error) {
      showToast(`Unbind failed: ${(error as Error).message || error}`);
    }
  },

  invokeCommand: async (commandObj) => {
    const state = get();
    const clip = state.clips.find((c) => c.name === state.selectedClipName);
    if (!clip) return;

    const commandName = commandObj.name;
    const schemaState = getCommandSchemaState(commandObj);

    let input: unknown;

    if (schemaState.kind === "schema" && schemaState.schema) {
      try {
        input = buildSchemaInput(clip.name, commandName, schemaState.schema, get);
      } catch (error) {
        set({
          lastResult: {
            ok: false,
            clip: clip.name,
            command: commandName,
            meta: "Invalid form input",
            payload: { error: String((error as Error).message || error) },
          },
        });
        return;
      }
    } else if (schemaState.kind === "empty") {
      input = {};
    } else {
      const inputText = state.getCommandInput(clip.name, commandName).trim();
      try {
        input = inputText ? JSON.parse(inputText) : {};
      } catch (error) {
        set({
          lastResult: {
            ok: false,
            clip: clip.name,
            command: commandName,
            meta: "Invalid JSON input",
            payload: { error: String((error as Error).message || error) },
          },
        });
        return;
      }
    }

    set({ runningCommand: commandName, resultExpanded: false });

    try {
      const payload = await hubInvoke(clip.name, commandName, input);
      set({
        lastResult: {
          ok: true,
          clip: clip.name,
          command: commandName,
          meta: "Invocation succeeded",
          payload,
        },
        runningCommand: null,
      });
    } catch (error) {
      set({
        lastResult: {
          ok: false,
          clip: clip.name,
          command: commandName,
          meta: String((error as Error).message || error),
          payload: { error: String((error as Error).message || error) },
        },
        runningCommand: null,
      });
    }
  },

  runQuickAction: async (action) => {
    const actionMap: Record<string, { command: string; input?: unknown }> = {
      screenshot: { command: "screenshot" },
      "speak-clipboard": { command: "speak", input: { text: "__CLIPBOARD__" } },
      "system-info": { command: "info" },
      "list-windows": { command: "listWindows" },
    };

    const spec = actionMap[action];
    if (!spec) return;

    const state = get();
    const clip = state.clips.find((c) =>
      getClipCommands(c).some((cmd) => cmd.name === spec.command),
    );

    if (!clip) {
      showToast(`No clip found with command "${spec.command}"`);
      return;
    }

    let input = spec.input || {};

    if (action === "speak-clipboard") {
      try {
        const clipboardText = await navigator.clipboard.readText();
        input = { text: clipboardText || "Clipboard is empty" };
      } catch {
        input = { text: "Cannot read clipboard" };
      }
    }

    try {
      const payload = await hubInvoke(clip.name, spec.command, input);
      set({
        lastResult: {
          ok: true,
          clip: clip.name,
          command: spec.command,
          meta: "Quick action succeeded",
          payload,
        },
        selectedClipName: clip.name,
        selectedManifest: clip.manifest || null,
        expandedCommand: spec.command,
      });
      get().updateHash();
    } catch (error) {
      set({
        lastResult: {
          ok: false,
          clip: clip.name,
          command: spec.command,
          meta: String((error as Error).message || error),
          payload: { error: String((error as Error).message || error) },
        },
        selectedClipName: clip.name,
        selectedManifest: clip.manifest || null,
        expandedCommand: spec.command,
      });
      get().updateHash();
    }
  },

  updateHash: () => {
    const state = get();
    let hash = "";
    if (state.selectedClipName) {
      hash = `clip/${encodeURIComponent(state.selectedClipName)}`;
      if (state.expandedCommand) hash += `/${encodeURIComponent(state.expandedCommand)}`;
    }
    history.replaceState(null, "", hash ? `#${hash}` : window.location.pathname);
  },

  parseHash: () => {
    const hash = window.location.hash.slice(1);
    if (!hash) return null;
    const match = hash.match(/^clip\/([^/]+)\/?(.*)$/);
    if (!match) return null;
    return {
      clipName: decodeURIComponent(match[1]),
      command: match[2] ? decodeURIComponent(match[2]) : null,
    };
  },
}));

// ---- Utility functions (exported for use by components) ----

export function getClipCommands(clip: ClipInfo): CommandInfo[] {
  if (Array.isArray(clip?.manifest?.commands)) return clip.manifest.commands;
  if (Array.isArray(clip?.commands)) return clip.commands;
  return [];
}

export function clipWebURL(name: string): string {
  return `/clips/${encodeURIComponent(String(name || "").trim())}/`;
}

export function clipStatusInfo(
  clip: ClipInfo,
  providers: ProviderInfo[],
  connected: boolean,
): { cls: string; label: string } {
  if (!connected) return { cls: "stopped", label: "hub offline" };
  const provider = providers.find((p) => p.name === clip.provider);
  if (!provider) return { cls: "stopped", label: "no provider" };
  return { cls: "running", label: "available" };
}

export function getBindingCandidates(
  depSlot: DependencySlot | undefined,
  slotName: string,
  clips: ClipInfo[],
): ClipInfo[] {
  const pkg = depSlot?.package || "";
  return clips.filter((c) => {
    if (pkg && (c.package || "") === pkg) return true;
    if ((c.package || "") === slotName || (c.name || "") === slotName) return true;
    return false;
  });
}

export function relativeTime(dateStr: string): string {
  if (!dateStr) return "";
  const then = new Date(dateStr);
  if (isNaN(then.getTime())) return "";
  const seconds = Math.floor((Date.now() - then.getTime()) / 1000);
  if (seconds < 60) return "just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

export function truncate(text: string, max: number): string {
  if (text.length <= max) return text;
  return text.slice(0, max - 1) + "\u2026";
}

// ---- Schema helpers (exported for SchemaForm) ----

export interface JsonSchema {
  type?: string | string[];
  properties?: Record<string, JsonSchema>;
  required?: string[];
  enum?: string[];
  title?: string;
  description?: string;
  format?: string;
}

export type SchemaFieldKind = "string" | "number" | "integer" | "boolean" | "enum" | "date" | "json";

export function parseCommandSchema(schemaStr: string): JsonSchema | null {
  if (typeof schemaStr !== "string") return null;
  const trimmed = schemaStr.trim();
  if (!trimmed) return null;
  try {
    const parsed = JSON.parse(trimmed);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
    return parsed;
  } catch {
    return null;
  }
}

export function isSchemaEmpty(schema: JsonSchema | null): boolean {
  if (!schema || typeof schema !== "object" || Array.isArray(schema)) return true;
  if (!schema.properties || typeof schema.properties !== "object" || Array.isArray(schema.properties))
    return true;
  return Object.keys(schema.properties).length === 0;
}

export function getSchemaProperties(schema: JsonSchema): [string, JsonSchema][] {
  if (!schema?.properties || typeof schema.properties !== "object" || Array.isArray(schema.properties))
    return [];
  return Object.entries(schema.properties);
}

export function getSchemaRequiredSet(schema: JsonSchema): Set<string> {
  return new Set(Array.isArray(schema?.required) ? schema.required.map(String) : []);
}

export function humanizeFieldName(value: string): string {
  return String(value || "")
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/\b\w/g, (char) => char.toUpperCase());
}

export function getSchemaFieldKind(fieldSchema: JsonSchema): SchemaFieldKind {
  const schema = fieldSchema && typeof fieldSchema === "object" && !Array.isArray(fieldSchema) ? fieldSchema : {};
  const types = Array.isArray(schema.type)
    ? schema.type.filter((value: string) => typeof value === "string" && value !== "null")
    : typeof schema.type === "string" && schema.type !== "null"
      ? [schema.type]
      : [];

  if (
    (types.length === 0 || types.includes("string")) &&
    Array.isArray(schema.enum) &&
    schema.enum.every((value: unknown) => typeof value === "string")
  ) {
    return "enum";
  }
  if (types.includes("string")) {
    return schema.format === "date" ? "date" : "string";
  }
  if (types.includes("number")) return "number";
  if (types.includes("integer")) return "integer";
  if (types.includes("boolean")) return "boolean";
  return "json";
}

export function getCommandSchemaState(commandObj: CommandInfo): {
  kind: "fallback" | "empty" | "schema";
  schema: JsonSchema | null;
  schemaStr: string;
} {
  const schemaStr = String(commandObj?.input || "").trim();
  const schema = parseCommandSchema(schemaStr);

  if (!schema) return { kind: "fallback", schema: null, schemaStr };
  if (isSchemaEmpty(schema)) return { kind: "empty", schema, schemaStr };
  return { kind: "schema", schema, schemaStr };
}

export function buildSchemaInput(
  clipName: string,
  commandName: string,
  schema: JsonSchema,
  getState: () => AppState,
): Record<string, unknown> {
  if (!schema || isSchemaEmpty(schema)) return {};

  const input: Record<string, unknown> = {};
  const required = getSchemaRequiredSet(schema);
  const state = getState();

  for (const [fieldName, fieldSchema] of getSchemaProperties(schema)) {
    const normalized: JsonSchema =
      fieldSchema && typeof fieldSchema === "object" && !Array.isArray(fieldSchema) ? fieldSchema : {};
    const kind = getSchemaFieldKind(normalized);
    const hasValue = state.hasSmartInput(clipName, commandName, fieldName);
    const rawValue = state.getSmartInput(clipName, commandName, fieldName);

    if (kind === "boolean") {
      if (hasValue || required.has(fieldName)) {
        input[fieldName] = Boolean(rawValue);
      }
      continue;
    }

    const stringValue = rawValue === "" ? "" : String(rawValue);
    if (kind === "string" || kind === "enum" || kind === "date") {
      if (stringValue === "" && !required.has(fieldName)) continue;
      input[fieldName] = stringValue;
      continue;
    }

    const trimmedValue = stringValue.trim();
    if (!trimmedValue && !required.has(fieldName)) continue;

    if (kind === "integer") {
      const parsed = parseInt(trimmedValue, 10);
      if (Number.isNaN(parsed)) {
        throw new Error(`Invalid integer for "${fieldName}"`);
      }
      input[fieldName] = parsed;
      continue;
    }

    if (kind === "number") {
      const parsed = parseFloat(trimmedValue);
      if (Number.isNaN(parsed)) {
        throw new Error(`Invalid number for "${fieldName}"`);
      }
      input[fieldName] = parsed;
      continue;
    }

    if (!trimmedValue) {
      throw new Error(`Invalid JSON for "${fieldName}"`);
    }
    try {
      input[fieldName] = JSON.parse(trimmedValue);
    } catch (error) {
      throw new Error(`Invalid JSON for "${fieldName}": ${(error as Error).message || error}`);
    }
  }

  return input;
}

// ---- Toast ----

export function showToast(message: string): void {
  const toast = document.createElement("div");
  toast.style.cssText =
    "position:fixed;bottom:20px;left:50%;transform:translateX(-50%);padding:8px 16px;border-radius:8px;background:var(--c-text);color:var(--c-bg);font-size:12px;z-index:9999;pointer-events:none;opacity:0;transition:opacity 0.2s";
  toast.textContent = message;
  document.body.appendChild(toast);
  requestAnimationFrame(() => {
    toast.style.opacity = "1";
  });
  setTimeout(() => {
    toast.style.opacity = "0";
    setTimeout(() => toast.remove(), 200);
  }, 2000);
}

// ---- JSON syntax highlighting ----

export function syntaxHighlightJSON(value: unknown, indent = 0): string {
  const pad = "  ".repeat(indent);
  const pad1 = "  ".repeat(indent + 1);

  if (value === null) {
    return '<span class="json-null">null</span>';
  }
  if (typeof value === "boolean") {
    return `<span class="json-bool">${value}</span>`;
  }
  if (typeof value === "number") {
    return `<span class="json-number">${value}</span>`;
  }
  if (typeof value === "string") {
    return `<span class="json-string">"${escapeHTML(value)}"</span>`;
  }

  if (Array.isArray(value)) {
    if (value.length === 0) return '<span class="json-bracket">[]</span>';
    const items = value.map((item) => `${pad1}${syntaxHighlightJSON(item, indent + 1)}`);
    return `<span class="json-bracket">[</span>\n${items.join(",\n")}\n${pad}<span class="json-bracket">]</span>`;
  }

  if (typeof value === "object") {
    const keys = Object.keys(value as Record<string, unknown>);
    if (keys.length === 0) return '<span class="json-bracket">{}</span>';
    const entries = keys.map((key) => {
      return `${pad1}<span class="json-key">"${escapeHTML(key)}"</span>: ${syntaxHighlightJSON((value as Record<string, unknown>)[key], indent + 1)}`;
    });
    return `<span class="json-bracket">{</span>\n${entries.join(",\n")}\n${pad}<span class="json-bracket">}</span>`;
  }

  return escapeHTML(String(value));
}

export function escapeHTML(value: string): string {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

export function extractBase64Image(payload: unknown): string | null {
  if (!payload || typeof payload !== "object") return null;

  const p = payload as Record<string, unknown>;
  const r = (p.result || {}) as Record<string, unknown>;
  const candidates = [
    p.image, p.data, p.screenshot,
    p.imageData, p.image_data, p.base64,
    r.image, r.data, r.screenshot,
  ];

  for (const val of candidates) {
    if (typeof val === "string" && val.length > 100) {
      if (val.startsWith("data:image/")) return val;
      if (val.startsWith("iVBOR") || val.startsWith("/9j/") || val.startsWith("R0lGOD")) {
        const prefix = val.startsWith("iVBOR")
          ? "data:image/png;base64,"
          : val.startsWith("/9j/")
            ? "data:image/jpeg;base64,"
            : "data:image/gif;base64,";
        return prefix + val;
      }
    }
  }

  return null;
}

// ---- ClipInfo ----

export interface CommandInfo {
  name: string;
  description: string;
  input: string;
  output: string;
}

export interface ClipManifest {
  name: string;
  package: string;
  version: string;
  domain: string;
  description: string;
  commands: CommandInfo[];
  dependencies: string[];
  dependencySlots: Record<string, DependencySlot>;
  hasWeb: boolean;
  patterns: string[];
}

export interface ClipInfo {
  name: string;
  package: string;
  version: string;
  provider: string;
  domain: string;
  commands: CommandInfo[];
  hasWeb: boolean;
  tokenProtected: boolean;
  dependencies: string[];
  manifest: ClipManifest | null;
}

// ---- Provider ----

export interface ProviderInfo {
  name: string;
  clips?: string[];
  connectedAt?: string;
  connected_at?: string;
}

// ---- Bindings ----

export interface DependencySlot {
  package: string;
  version: string;
}

export interface BindingEntry {
  alias: string;
  hub?: string;
}

export interface BindingsData {
  bindings: Record<string, BindingEntry>;
  dependencySlots: Record<string, DependencySlot>;
}

// ---- Command result ----

export interface CommandResult {
  ok: boolean;
  clip: string;
  command: string;
  meta: string;
  payload: unknown;
}

// ---- Schema form types ----

export type SchemaFieldKind = "string" | "number" | "integer" | "boolean" | "enum" | "date" | "json";

export interface SchemaState {
  kind: "fallback" | "empty" | "schema";
  schema: JsonSchema | null;
  schemaStr: string;
}

export interface JsonSchema {
  type?: string | string[];
  properties?: Record<string, JsonSchema>;
  required?: string[];
  enum?: string[];
  title?: string;
  description?: string;
  format?: string;
  items?: JsonSchema;
}

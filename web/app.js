const HUB_PROCEDURES = {
  listClips: "/pinix.v2.HubService/ListClips",
  getManifest: "/pinix.v2.HubService/GetManifest",
  invoke: "/pinix.v2.HubService/Invoke",
};

const CONNECT_PROTOCOL_VERSION = "1";
const JSON_HEADERS = {
  Accept: "application/json",
  "Content-Type": "application/json",
  "Connect-Protocol-Version": CONNECT_PROTOCOL_VERSION,
};
const STREAM_HEADERS = {
  Accept: "application/connect+json",
  "Content-Type": "application/connect+json",
  "Connect-Protocol-Version": CONNECT_PROTOCOL_VERSION,
};

const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder();

const state = {
  clips: [],
  selectedClipName: null,
  selectedManifest: null,
  detailError: null,
  expandedCommand: null,
  inputs: {},
  runningCommand: null,
  lastResult: null,
};

const refreshButton = document.getElementById("refreshButton");
const clipCount = document.getElementById("clipCount");
const clipList = document.getElementById("clipList");
const detailView = document.getElementById("detailView");

refreshButton.addEventListener("click", () => {
  void refreshClips();
});

void refreshClips();

async function refreshClips() {
  refreshButton.disabled = true;
  clipList.innerHTML = '<div class="loading">Loading clips...</div>';

  try {
    const payload = await hubUnary(HUB_PROCEDURES.listClips, {});
    state.clips = Array.isArray(payload?.clips) ? payload.clips.map(normalizeClipInfo) : [];

    if (state.selectedClipName) {
      const current = getSelectedClip();
      if (!current) {
        state.selectedClipName = null;
        state.selectedManifest = null;
        state.expandedCommand = null;
        state.detailError = null;
      }
    }

    renderClipList();

    if (state.selectedClipName) {
      await loadManifest(state.selectedClipName, { preserveResult: true });
    } else {
      renderDetail();
    }
  } catch (error) {
    renderClipList(String(error.message || error));
    detailView.innerHTML = renderNotice(
      "Unable to load clips",
      String(error.message || error),
    );
  } finally {
    refreshButton.disabled = false;
  }
}

async function selectClip(name) {
  if (state.selectedClipName !== name) {
    state.lastResult = null;
  }

  state.selectedClipName = name;
  state.expandedCommand = null;
  state.detailError = null;
  state.selectedManifest = getSelectedClip()?.manifest || null;

  renderClipList();
  renderDetail(true);

  await loadManifest(name, { preserveResult: true });
}

async function loadManifest(name, options = {}) {
  const selected = getSelectedClip();
  if (!selected || selected.name !== name) {
    return;
  }

  state.detailError = null;
  if (!options.preserveResult) {
    state.lastResult = null;
  }

  try {
    const payload = await hubUnary(HUB_PROCEDURES.getManifest, { clipName: name });
    state.selectedManifest = normalizeManifest(payload?.manifest, selected);
    selected.manifest = state.selectedManifest;
    renderClipList();
  } catch (error) {
    state.selectedManifest = selected.manifest || normalizeManifest(null, selected);
    state.detailError = String(error.message || error);
  }

  renderDetail();
}

async function invokeCommand(commandName) {
  const clip = getSelectedClip();
  if (!clip) {
    return;
  }

  const inputText = getCommandInput(clip.name, commandName).trim();
  let input;

  try {
    input = inputText ? JSON.parse(inputText) : {};
  } catch (error) {
    state.lastResult = {
      ok: false,
      clip: clip.name,
      command: commandName,
      meta: "Invalid JSON input",
      payload: { error: String(error.message || error) },
    };
    renderDetail();
    return;
  }

  state.runningCommand = commandName;
  renderDetail();

  try {
    const payload = await hubInvoke(clip.name, commandName, input);

    state.lastResult = {
      ok: true,
      clip: clip.name,
      command: commandName,
      meta: "Invocation succeeded",
      payload,
    };
  } catch (error) {
    state.lastResult = {
      ok: false,
      clip: clip.name,
      command: commandName,
      meta: String(error.message || error),
      payload: { error: String(error.message || error) },
    };
  } finally {
    state.runningCommand = null;
    renderDetail();
  }
}

function renderClipList(errorMessage = "") {
  clipCount.textContent = `${state.clips.length} clip${state.clips.length === 1 ? "" : "s"}`;

  if (errorMessage) {
    clipList.innerHTML = `<div class="notice">${escapeHTML(errorMessage)}</div>`;
    return;
  }

  if (state.clips.length === 0) {
    clipList.innerHTML = '<div class="empty-list">No clips registered.</div>';
    return;
  }

  clipList.innerHTML = "";

  for (const clip of state.clips) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `clip-item${clip.name === state.selectedClipName ? " active" : ""}`;
    button.addEventListener("click", () => {
      void selectClip(clip.name);
    });

    const commandCount = getClipCommands(clip).length;

    button.innerHTML = `
      <div class="clip-item-head">
        <span class="clip-name">${escapeHTML(clip.name)}</span>
        <span class="status-pill ${clipStateClass(clip)}">${clipStateLabel(clip)}</span>
      </div>
      <p class="clip-source">${escapeHTML(displayClipSource(clip))}</p>
      <div class="clip-stats">
        <span class="badge">${commandCount} command${commandCount === 1 ? "" : "s"}</span>
        ${clip.tokenProtected ? '<span class="badge">token</span>' : ""}
      </div>
    `;

    clipList.appendChild(button);
  }
}

function renderDetail(isLoading = false) {
  const clip = getSelectedClip();
  if (!clip) {
    detailView.innerHTML = `
      <div class="empty-state">
        <h2>Select a clip</h2>
        <p>Choose a clip from the list to inspect its manifest and run commands.</p>
      </div>
    `;
    return;
  }

  const manifest = state.selectedManifest || clip.manifest || normalizeManifest(null, clip) || { name: clip.name, commands: [] };
  const commands = Array.isArray(manifest.commands) ? manifest.commands : [];

  detailView.innerHTML = `
    <div class="detail-wrap">
      <div class="detail-header">
        <div class="detail-title">
          <h2>${escapeHTML(clip.name)}</h2>
          <p class="detail-meta">Inspect manifest metadata and invoke clip commands through HubService.</p>
        </div>
        <span class="status-pill ${clipStateClass(clip)}">${clipStateLabel(clip)}</span>
      </div>

      <div class="detail-grid">
        <div class="meta-card">
          <span class="meta-label">Domain</span>
          <div class="meta-value">${escapeHTML(manifest.domain || "-")}</div>
        </div>
        <div class="meta-card">
          <span class="meta-label">Provider</span>
          <div class="meta-value">${escapeHTML(clip.provider || "-")}</div>
        </div>
        <div class="meta-card">
          <span class="meta-label">Package</span>
          <div class="meta-value">${escapeHTML(manifest.package || clip.package || "-")}</div>
        </div>
        <div class="meta-card">
          <span class="meta-label">Commands</span>
          <div class="meta-value">${commands.length}</div>
        </div>
      </div>

      ${isLoading ? '<div class="loading">Loading manifest...</div>' : ""}
      ${state.detailError ? renderInlineNotice(state.detailError) : ""}

      <section>
        <div class="command-header">
          <div>
            <h3>Commands</h3>
            <p class="command-help">Click a command to open a JSON input form and run it.</p>
          </div>
        </div>
        <div class="commands">
          ${commands.length === 0 ? '<div class="empty-list">No commands found in the manifest.</div>' : commands.map((command) => renderCommandCard(clip, command)).join("")}
        </div>
      </section>

      <section class="result-card">
        <div class="result-header">
          <span class="result-title">Execution Result</span>
          <span class="result-meta">${renderResultMeta()}</span>
        </div>
        <pre class="result-output">${escapeHTML(renderResultBody())}</pre>
      </section>
    </div>
  `;

  for (const commandObj of commands) {
    const commandName = typeof commandObj === "string" ? commandObj : commandObj.name;

    const toggle = document.querySelector(`[data-command-toggle="${cssEscape(commandName)}"]`);
    if (toggle) {
      toggle.addEventListener("click", () => {
        state.expandedCommand = state.expandedCommand === commandName ? null : commandName;
        renderDetail();
      });
    }

    const runButton = document.querySelector(`[data-command-run="${cssEscape(commandName)}"]`);
    if (runButton) {
      runButton.addEventListener("click", () => {
        void invokeCommand(commandName);
      });
    }
  }
}

function renderCommandCard(clip, commandObj) {
  const commandName = typeof commandObj === "string" ? commandObj : commandObj.name;
  const expanded = state.expandedCommand === commandName;
  const running = state.runningCommand === commandName;
  const commandDescription = typeof commandObj === "object" ? String(commandObj.description || "").trim() : "";

  let inputValue = getCommandInput(clip.name, commandName);
  if (!inputValue) {
    inputValue = "{}";
    setCommandInput(clip.name, commandName, inputValue);
  }

  return `
    <article class="command-card${expanded ? " expanded" : ""}">
      <button type="button" class="command-toggle" data-command-toggle="${escapeHTML(commandName)}">
        <span class="command-name">${escapeHTML(commandName)}</span>
        <span class="command-arrow">${expanded ? "Hide" : "Open"}</span>
      </button>
      ${expanded ? `
        <div class="command-body">
          ${commandDescription ? `<p class="command-help">${escapeHTML(commandDescription)}</p>` : ""}
          <label class="command-label" for="command-input-${escapeHTML(commandName)}">Input JSON</label>
          <textarea
            id="command-input-${escapeHTML(commandName)}"
            class="command-input"
            data-command-input="${escapeHTML(commandName)}"
            spellcheck="false"
            oninput="setCommandInput('${escapeHTML(clip.name)}', '${escapeHTML(commandName)}', this.value)"
          >${escapeHTML(inputValue)}</textarea>
          <div class="command-footer">
            <p class="status-note">Requests are sent to <code>${escapeHTML(HUB_PROCEDURES.invoke)}</code>.</p>
            <button type="button" class="run-button" data-command-run="${escapeHTML(commandName)}" ${running ? "disabled" : ""}>
              ${running ? "Running..." : "Run command"}
            </button>
          </div>
        </div>
      ` : ""}
    </article>
  `;
}

function renderResultMeta() {
  if (!state.lastResult) {
    return "No command executed yet";
  }
  return `${state.lastResult.clip}/${state.lastResult.command} - ${state.lastResult.meta}`;
}

function renderResultBody() {
  if (!state.lastResult) {
    return JSON.stringify({ status: "idle" }, null, 2);
  }
  if (typeof state.lastResult.payload === "string") {
    return state.lastResult.payload;
  }
  return JSON.stringify(state.lastResult.payload, null, 2);
}

function renderInlineNotice(message) {
  return `<div class="notice">${escapeHTML(message)}</div>`;
}

function renderNotice(title, message) {
  return `
    <div class="empty-state">
      <h2>${escapeHTML(title)}</h2>
      <div class="notice">${escapeHTML(message)}</div>
    </div>
  `;
}

function getSelectedClip() {
  return state.clips.find((clip) => clip.name === state.selectedClipName) || null;
}

function getClipCommands(clip) {
  if (Array.isArray(clip?.manifest?.commands)) {
    return clip.manifest.commands;
  }
  if (Array.isArray(clip?.commands)) {
    return clip.commands;
  }
  return [];
}

function clipStateClass() {
  return "running";
}

function clipStateLabel() {
  return "available";
}

function displayClipSource(clip) {
  const parts = [];
  if (clip.provider) {
    parts.push(clip.provider);
  }
  if (clip.package && clip.package !== clip.name) {
    parts.push(clip.package);
  }
  if (clip.version) {
    parts.push(`v${clip.version}`);
  }
  return parts.join(" / ") || "-";
}

function getCommandInput(clipName, commandName) {
  const key = `${clipName}:${commandName}`;
  if (!(key in state.inputs)) {
    state.inputs[key] = "{}";
  }
  return state.inputs[key];
}

function setCommandInput(clipName, commandName, value) {
  state.inputs[`${clipName}:${commandName}`] = value;
}

async function hubUnary(path, message) {
  const response = await fetch(path, {
    method: "POST",
    headers: JSON_HEADERS,
    body: JSON.stringify(message || {}),
  });

  const payload = await parseResponsePayload(response);
  if (!response.ok) {
    throw new Error(extractErrorMessage(payload, response));
  }
  return payload || {};
}

async function hubInvoke(clipName, commandName, input) {
  const response = await fetch(HUB_PROCEDURES.invoke, {
    method: "POST",
    headers: STREAM_HEADERS,
    body: encodeConnectEnvelope({
      clipName,
      command: commandName,
      input: encodeProtoBytes(input),
    }),
  });

  if (!response.ok) {
    const payload = await parseResponsePayload(response);
    throw new Error(extractErrorMessage(payload, response));
  }
  if (!response.body) {
    throw new Error("Streaming response body is unavailable");
  }

  const outputs = [];
  for await (const message of readConnectStream(response.body)) {
    if (message?.error) {
      throw new Error(message.error.message || "Invocation failed");
    }
    if (typeof message?.output === "string" && message.output !== "") {
      outputs.push(decodeBase64Bytes(message.output));
    }
  }
  return decodeInvocationOutput(outputs);
}

async function parseResponsePayload(response) {
  const text = await response.text();
  if (!text) {
    return null;
  }
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function extractErrorMessage(payload, response) {
  if (payload && typeof payload === "object") {
    if (typeof payload.message === "string" && payload.message.trim()) {
      return payload.message;
    }
    if (typeof payload.error?.message === "string" && payload.error.message.trim()) {
      return payload.error.message;
    }
  }
  if (typeof payload === "string" && payload.trim()) {
    return payload.trim();
  }
  return response.statusText || "Request failed";
}

async function* readConnectStream(body) {
  const reader = body.getReader();
  let buffer = new Uint8Array(0);
  let streamEnded = false;

  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) {
        break;
      }
      if (value && value.length > 0) {
        buffer = concatByteArrays(buffer, value);
      }

      for (;;) {
        const frame = readConnectFrame(buffer);
        if (!frame) {
          break;
        }

        buffer = frame.rest;
        if (frame.compressed) {
          throw new Error("Compressed Connect frames are not supported");
        }

        const payload = parseConnectJSON(frame.payload);
        if (frame.endStream) {
          if (buffer.length > 0) {
            throw new Error(`Corrupt response: ${buffer.length} extra bytes after end of stream`);
          }
          if (payload?.error) {
            throw new Error(payload.error.message || "Request failed");
          }
          streamEnded = true;
          return;
        }

        yield payload;
      }
    }
  } finally {
    reader.releaseLock();
  }

  if (buffer.length > 0) {
    throw new Error("Incomplete Connect frame");
  }
  if (!streamEnded) {
    throw new Error("Unexpected EOF while reading Connect stream");
  }
}

function readConnectFrame(buffer) {
  if (!(buffer instanceof Uint8Array) || buffer.length < 5) {
    return null;
  }

  const view = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);
  const flags = view.getUint8(0);
  const length = view.getUint32(1);
  const totalLength = 5 + length;

  if (buffer.length < totalLength) {
    return null;
  }

  return {
    compressed: Boolean(flags & 0b00000001),
    endStream: Boolean(flags & 0b00000010),
    payload: buffer.slice(5, totalLength),
    rest: buffer.slice(totalLength),
  };
}

function parseConnectJSON(bytes) {
  const text = textDecoder.decode(bytes);
  if (!text) {
    return {};
  }
  try {
    return JSON.parse(text);
  } catch (error) {
    throw new Error(`Invalid Connect JSON frame: ${error.message || error}`);
  }
}

function encodeConnectEnvelope(message) {
  const payload = textEncoder.encode(JSON.stringify(message || {}));
  const frame = new Uint8Array(5 + payload.length);
  const view = new DataView(frame.buffer);
  view.setUint8(0, 0);
  view.setUint32(1, payload.length);
  frame.set(payload, 5);
  return frame;
}

function encodeProtoBytes(value) {
  return encodeBase64Bytes(textEncoder.encode(JSON.stringify(value || {})));
}

function decodeInvocationOutput(outputs) {
  if (!outputs.length) {
    return {};
  }

  const bytes = outputs.length === 1 ? outputs[0] : concatManyByteArrays(outputs);
  const text = textDecoder.decode(bytes);
  if (!text.trim()) {
    return {};
  }

  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function concatByteArrays(left, right) {
  if (!left.length) {
    return right.slice();
  }
  if (!right.length) {
    return left.slice();
  }
  const combined = new Uint8Array(left.length + right.length);
  combined.set(left, 0);
  combined.set(right, left.length);
  return combined;
}

function concatManyByteArrays(chunks) {
  let length = 0;
  for (const chunk of chunks) {
    length += chunk.length;
  }
  const combined = new Uint8Array(length);
  let offset = 0;
  for (const chunk of chunks) {
    combined.set(chunk, offset);
    offset += chunk.length;
  }
  return combined;
}

function encodeBase64Bytes(bytes) {
  let binary = "";
  const chunkSize = 0x8000;
  for (let i = 0; i < bytes.length; i += chunkSize) {
    const chunk = bytes.subarray(i, i + chunkSize);
    binary += String.fromCharCode(...chunk);
  }
  return btoa(binary);
}

function decodeBase64Bytes(value) {
  const binary = atob(value || "");
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

function normalizeClipInfo(clip) {
  const commands = Array.isArray(clip?.commands) ? clip.commands.map(normalizeCommandInfo).filter(Boolean) : [];
  return {
    name: String(clip?.name || "").trim(),
    package: String(clip?.package || "").trim(),
    version: String(clip?.version || "").trim(),
    provider: String(clip?.provider || "").trim(),
    domain: String(clip?.domain || "").trim(),
    commands,
    hasWeb: Boolean(clip?.hasWeb ?? clip?.has_web),
    tokenProtected: Boolean(clip?.tokenProtected ?? clip?.token_protected),
    dependencies: normalizeStringArray(clip?.dependencies),
    manifest: clip?.manifest ? normalizeManifest(clip.manifest, clip) : null,
  };
}

function normalizeManifest(manifest, fallback = null) {
  if (!manifest && !fallback) {
    return null;
  }

  const commandSource = Array.isArray(manifest?.commands)
    ? manifest.commands
    : Array.isArray(fallback?.commands)
      ? fallback.commands
      : [];

  return {
    name: String(manifest?.name || fallback?.name || "").trim(),
    package: String(manifest?.package || fallback?.package || "").trim(),
    version: String(manifest?.version || fallback?.version || "").trim(),
    domain: String(manifest?.domain || fallback?.domain || "").trim(),
    description: String(manifest?.description || "").trim(),
    commands: commandSource.map(normalizeCommandInfo).filter(Boolean),
    dependencies: normalizeStringArray(manifest?.dependencies || fallback?.dependencies),
    hasWeb: Boolean(manifest?.hasWeb ?? manifest?.has_web ?? fallback?.hasWeb ?? fallback?.has_web),
    patterns: normalizeStringArray(manifest?.patterns),
  };
}

function normalizeCommandInfo(command) {
  if (typeof command === "string") {
    const name = command.trim();
    return name ? { name, description: "", input: "", output: "" } : null;
  }
  if (!command) {
    return null;
  }

  const name = String(command.name || "").trim();
  if (!name) {
    return null;
  }

  return {
    name,
    description: String(command.description || "").trim(),
    input: String(command.input || "").trim(),
    output: String(command.output || "").trim(),
  };
}

function normalizeStringArray(values) {
  if (!Array.isArray(values)) {
    return [];
  }
  const result = [];
  const seen = new Set();
  for (const value of values) {
    const text = String(value || "").trim();
    if (!text || seen.has(text)) {
      continue;
    }
    seen.add(text);
    result.push(text);
  }
  return result.sort();
}

function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function cssEscape(value) {
  if (typeof CSS !== "undefined" && typeof CSS.escape === "function") {
    return CSS.escape(value);
  }
  return String(value).replace(/[^a-zA-Z0-9_-]/g, "\\$&");
}

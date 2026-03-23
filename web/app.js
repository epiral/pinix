// ─── Connect-RPC protocol constants ───

const HUB_PROCEDURES = {
  listClips: "/pinix.v2.HubService/ListClips",
  listProviders: "/pinix.v2.HubService/ListProviders",
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

// ─── Smart form definitions ───
// Maps command names to form types for non-JSON input
const SMART_FORMS = {
  // ── Audio ──
  speak: {
    fields: [
      { name: "text", label: "Text", inputType: "textarea", placeholder: "Enter text to speak..." },
      { name: "voice", label: "Voice", inputType: "select", options: ["Tingting", "Meijia", "Sinji", "Samantha", "Alex", "Daniel", "Karen", "Victoria"] },
    ],
    buildInput: (v) => { const r = { text: v.text || "" }; if (v.voice) r.voice = v.voice; return r; },
  },
  setVolume: {
    fields: [{ name: "level", label: "Volume (0-100)", inputType: "number", placeholder: "50" }],
    buildInput: (v) => ({ level: parseInt(v.level) || 50 }),
  },
  // ── Search / Query ──
  search: {
    fields: [{ name: "query", label: "Query", inputType: "text", placeholder: "Search query..." }],
    buildInput: (v) => ({ query: v.query || "" }),
  },
  searchContacts: {
    fields: [{ name: "query", label: "Name", inputType: "text", placeholder: "Contact name..." }],
    buildInput: (v) => ({ query: v.query || "" }),
  },
  note: {
    fields: [{ name: "id", label: "Note ID", inputType: "text", placeholder: "Note ID or URL..." }],
    buildInput: (v) => ({ id: v.id || "" }),
  },
  // ── Shell ──
  exec: {
    fields: [
      { name: "command", label: "Command", inputType: "text", placeholder: "e.g. ls -la /tmp" },
      { name: "cwd", label: "Working Directory", inputType: "text", placeholder: "(optional) /path/to/dir" },
    ],
    buildInput: (v) => { const r = { command: v.command || "" }; if (v.cwd) r.cwd = v.cwd; return r; },
  },
  execScript: {
    fields: [
      { name: "language", label: "Language", inputType: "select", options: ["python3", "bash", "node", "swift", "ruby"] },
      { name: "code", label: "Code", inputType: "textarea", placeholder: "print('hello world')" },
    ],
    buildInput: (v) => ({ language: v.language || "python3", code: v.code || "" }),
  },
  // ── Apps ──
  launch: {
    fields: [{ name: "app", label: "App Name", inputType: "text", placeholder: "e.g. Safari, Finder, iTerm" }],
    buildInput: (v) => ({ app: v.app || "" }),
  },
  quit: {
    fields: [{ name: "app", label: "App Name", inputType: "text", placeholder: "App to quit" }],
    buildInput: (v) => ({ app: v.app || "" }),
  },
  focus: {
    fields: [{ name: "app", label: "App Name", inputType: "text", placeholder: "App to focus" }],
    buildInput: (v) => ({ app: v.app || "" }),
  },
  // ── Notifications ──
  notify: {
    fields: [
      { name: "title", label: "Title", inputType: "text", placeholder: "Notification title" },
      { name: "body", label: "Body", inputType: "text", placeholder: "Notification body" },
    ],
    buildInput: (v) => ({ title: v.title || "", body: v.body || "" }),
  },
  alert: {
    fields: [
      { name: "title", label: "Title", inputType: "text", placeholder: "Alert title" },
      { name: "message", label: "Message", inputType: "text", placeholder: "Alert message" },
    ],
    buildInput: (v) => ({ title: v.title || "", message: v.message || "" }),
  },
  prompt: {
    fields: [
      { name: "title", label: "Title", inputType: "text", placeholder: "Prompt title" },
      { name: "message", label: "Message", inputType: "text", placeholder: "Prompt message" },
      { name: "defaultAnswer", label: "Default Answer", inputType: "text", placeholder: "(optional)" },
    ],
    buildInput: (v) => { const r = { title: v.title || "", message: v.message || "" }; if (v.defaultAnswer) r.defaultAnswer = v.defaultAnswer; return r; },
  },
  // ── Clipboard ──
  write: {
    fields: [{ name: "content", label: "Content", inputType: "textarea", placeholder: "Text to copy to clipboard" }],
    buildInput: (v) => ({ content: v.content || "" }),
  },
  // ── Filesystem ──
  getMetadata: {
    fields: [{ name: "path", label: "File Path", inputType: "text", placeholder: "/path/to/file" }],
    buildInput: (v) => ({ path: v.path || "" }),
  },
  preview: {
    fields: [{ name: "path", label: "File Path", inputType: "text", placeholder: "/path/to/file" }],
    buildInput: (v) => ({ path: v.path || "" }),
  },
  convertDoc: {
    fields: [
      { name: "path", label: "File Path", inputType: "text", placeholder: "/path/to/document" },
      { name: "format", label: "Format", inputType: "select", options: ["html", "txt", "rtf", "docx"] },
    ],
    buildInput: (v) => ({ path: v.path || "", format: v.format || "html" }),
  },
  // ── Input ──
  click: {
    fields: [
      { name: "x", label: "X", inputType: "number", placeholder: "500" },
      { name: "y", label: "Y", inputType: "number", placeholder: "300" },
    ],
    buildInput: (v) => ({ x: parseFloat(v.x) || 0, y: parseFloat(v.y) || 0 }),
  },
  typeText: {
    fields: [{ name: "text", label: "Text", inputType: "text", placeholder: "Text to type..." }],
    buildInput: (v) => ({ text: v.text || "" }),
  },
  pressKey: {
    fields: [
      { name: "key", label: "Key", inputType: "text", placeholder: "e.g. c" },
      { name: "modifiers", label: "Modifiers", inputType: "text", placeholder: "e.g. command,shift (comma separated)" },
    ],
    buildInput: (v) => { const r = { key: v.key || "" }; if (v.modifiers) r.modifiers = v.modifiers.split(",").map(m => m.trim()); return r; },
  },
  // ── Windows ──
  move: {
    fields: [
      { name: "app", label: "App Name", inputType: "text", placeholder: "App name" },
      { name: "x", label: "X", inputType: "number", placeholder: "0" },
      { name: "y", label: "Y", inputType: "number", placeholder: "0" },
    ],
    buildInput: (v) => ({ app: v.app || "", x: parseInt(v.x) || 0, y: parseInt(v.y) || 0 }),
  },
  resize: {
    fields: [
      { name: "app", label: "App Name", inputType: "text", placeholder: "App name" },
      { name: "width", label: "Width", inputType: "number", placeholder: "800" },
      { name: "height", label: "Height", inputType: "number", placeholder: "600" },
    ],
    buildInput: (v) => ({ app: v.app || "", width: parseInt(v.width) || 800, height: parseInt(v.height) || 600 }),
  },
  // ── ML ──
  detectLanguage: {
    fields: [{ name: "text", label: "Text", inputType: "textarea", placeholder: "Enter text to detect language..." }],
    buildInput: (v) => ({ text: v.text || "" }),
  },
  sentiment: {
    fields: [{ name: "text", label: "Text", inputType: "textarea", placeholder: "Enter text for sentiment analysis..." }],
    buildInput: (v) => ({ text: v.text || "" }),
  },
  // ── Shortcuts ──
  run: {
    fields: [{ name: "name", label: "Shortcut Name", inputType: "text", placeholder: "e.g. 车门上锁" }],
    buildInput: (v) => ({ name: v.name || "" }),
  },
  // ── PIM ──
  getEvents: {
    fields: [
      { name: "from", label: "From Date", inputType: "date", placeholder: "" },
      { name: "to", label: "To Date", inputType: "date", placeholder: "" },
    ],
    buildInput: (v) => ({ from: v.from || "", to: v.to || "" }),
  },
  // ── Todo ──
  add: {
    fields: [{ name: "title", label: "Title", inputType: "text", placeholder: "Todo item title..." }],
    buildInput: (v) => ({ title: v.title || "" }),
  },
  delete: {
    fields: [{ name: "id", label: "ID", inputType: "text", placeholder: "Item ID to delete" }],
    buildInput: (v) => ({ id: v.id || "" }),
  },
  // ── Twitter ──
  getProfile: {
    fields: [{ name: "username", label: "Username", inputType: "text", placeholder: "@username" }],
    buildInput: (v) => ({ username: v.username || "" }),
  },
  getTweet: {
    fields: [{ name: "id", label: "Tweet ID", inputType: "text", placeholder: "Tweet ID or URL" }],
    buildInput: (v) => ({ id: v.id || "" }),
  },
  // ── Agent ──
  send: {
    fields: [
      { name: "topic", label: "Topic", inputType: "text", placeholder: "Topic name or ID" },
      { name: "message", label: "Message", inputType: "textarea", placeholder: "Your message..." },
    ],
    buildInput: (v) => ({ topic: v.topic || "", message: v.message || "" }),
  },
  "create-topic": {
    fields: [{ name: "name", label: "Topic Name", inputType: "text", placeholder: "New topic name" }],
    buildInput: (v) => ({ name: v.name || "" }),
  },
  "get-topic": {
    fields: [{ name: "topic", label: "Topic", inputType: "text", placeholder: "Topic name or ID" }],
    buildInput: (v) => ({ topic: v.topic || "" }),
  },
  "get-run": {
    fields: [{ name: "runId", label: "Run ID", inputType: "text", placeholder: "Run ID" }],
    buildInput: (v) => ({ runId: v.runId || "" }),
  },
  "cancel-run": {
    fields: [{ name: "runId", label: "Run ID", inputType: "text", placeholder: "Run ID to cancel" }],
    buildInput: (v) => ({ runId: v.runId || "" }),
  },
};

// Commands that require no input — just a Run button
const NO_INPUT_COMMANDS = new Set([
  "screenshot", "list", "listCalendars", "battery", "info",
  "listWindows", "listApps", "getClipboard", "getBrightness",
  "getVolume", "listDisplays", "listTabs", "frontmostApp",
  "getFrontmost", "activeWindow", "listReminders", "listNotes",
  "screenshotAll", "listContacts", "currentTrack", "systemInfo",
  "health", "ping", "status", "read", "listTypes",
  "getCursorPosition", "uptime", "diskUsage", "processes",
  "interfaces", "wifiInfo", "speedTest", "bluetoothDevices",
  "list-topics", "event-check", "ocr",
]);

// ─── Application state ───

const state = {
  clips: [],
  providers: [],
  selectedClipName: null,
  selectedManifest: null,
  detailError: null,
  expandedCommand: null,
  inputs: {},          // JSON textarea fallback inputs
  smartInputs: {},     // smart form field values
  runningCommand: null,
  lastResult: null,
  searchQuery: "",
  collapsedGroups: {}, // provider group collapse state
  resultExpanded: false,
  connected: true,
};

// ─── DOM references ───

const refreshButton = document.getElementById("refreshButton");
const refreshProviders = document.getElementById("refreshProviders");
const clipCountEl = document.getElementById("clipCount");
const providerCountEl = document.getElementById("providerCount");
const connectionBadge = document.getElementById("connectionBadge");
const hubAddressEl = document.getElementById("hubAddress");
const clipList = document.getElementById("clipList");
const providerList = document.getElementById("providerList");
const detailView = document.getElementById("detailView");
const clipSearch = document.getElementById("clipSearch");

// Set hub address from current page location
hubAddressEl.textContent = `hub :${window.location.port || "80"}`;

// ─── Event listeners ───

refreshButton.addEventListener("click", () => void refreshClips());
refreshProviders.addEventListener("click", () => void refreshProviderList());

clipSearch.addEventListener("input", (e) => {
  state.searchQuery = e.target.value.trim().toLowerCase();
  renderClipList();
});

// Cmd+K to focus search
document.addEventListener("keydown", (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key === "k") {
    e.preventDefault();
    clipSearch.focus();
    clipSearch.select();
  }
});

// Arrow key navigation for clips
document.addEventListener("keydown", (e) => {
  // Skip if in input/textarea/select
  const tag = e.target.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;

  // Get visible clips (respecting search filter)
  let visibleClips = state.clips;
  if (state.searchQuery) {
    visibleClips = state.clips.filter((c) =>
      c.name.toLowerCase().includes(state.searchQuery) ||
      (c.provider && c.provider.toLowerCase().includes(state.searchQuery)) ||
      (c.package && c.package.toLowerCase().includes(state.searchQuery)) ||
      (c.domain && c.domain.toLowerCase().includes(state.searchQuery))
    );
  }
  if (visibleClips.length === 0) return;

  const clipNames = visibleClips.map((c) => c.name);
  const idx = clipNames.indexOf(state.selectedClipName);

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
    const clip = getSelectedClip();
    if (clip) {
      const commands = getClipCommands(clip);
      if (commands.length > 0) {
        const firstCmd = typeof commands[0] === "string" ? commands[0] : commands[0].name;
        state.expandedCommand = firstCmd;
        updateHash();
        renderDetail();
      }
    }
  }
});

// Quick actions
document.getElementById("quickActions").addEventListener("click", (e) => {
  const btn = e.target.closest("[data-qa]");
  if (!btn) return;
  const action = btn.dataset.qa;
  void runQuickAction(action, btn);
});

// ─── Quick Actions ───

async function runQuickAction(action, btn) {
  // Find the clip that has this command
  const actionMap = {
    "screenshot": { command: "screenshot" },
    "speak-clipboard": { command: "speak", input: { text: "__CLIPBOARD__" } },
    "system-info": { command: "info" },
    "list-windows": { command: "listWindows" },
  };

  const spec = actionMap[action];
  if (!spec) return;

  // Find first clip that has this command
  const clip = state.clips.find((c) => getClipCommands(c).some((cmd) => {
    const cmdName = typeof cmd === "string" ? cmd : cmd.name;
    return cmdName === spec.command;
  }));

  if (!clip) {
    showToast(`No clip found with command "${spec.command}"`);
    return;
  }

  btn.disabled = true;
  try {
    let input = spec.input || {};

    // Special case: read clipboard for speak
    if (action === "speak-clipboard") {
      try {
        const clipboardText = await navigator.clipboard.readText();
        input = { text: clipboardText || "Clipboard is empty" };
      } catch {
        input = { text: "Cannot read clipboard" };
      }
    }

    const payload = await hubInvoke(clip.name, spec.command, input);

    state.lastResult = {
      ok: true,
      clip: clip.name,
      command: spec.command,
      meta: "Quick action succeeded",
      payload,
    };

    // Select the clip and expand the command so the inline result is visible
    state.selectedClipName = clip.name;
    state.selectedManifest = clip.manifest || null;
    state.expandedCommand = spec.command;
    updateHash();
    renderClipList();
    renderDetail();
  } catch (error) {
    state.lastResult = {
      ok: false,
      clip: clip.name,
      command: spec.command,
      meta: String(error.message || error),
      payload: { error: String(error.message || error) },
    };
    state.selectedClipName = clip.name;
    state.selectedManifest = clip.manifest || null;
    state.expandedCommand = spec.command;
    updateHash();
    renderClipList();
    renderDetail();
  } finally {
    btn.disabled = false;
  }
}

function showToast(message) {
  // Simple non-blocking toast
  const toast = document.createElement("div");
  toast.style.cssText = "position:fixed;bottom:20px;left:50%;transform:translateX(-50%);padding:8px 16px;border-radius:8px;background:var(--c-text);color:var(--c-bg);font-size:12px;z-index:9999;pointer-events:none;opacity:0;transition:opacity 0.2s";
  toast.textContent = message;
  document.body.appendChild(toast);
  requestAnimationFrame(() => { toast.style.opacity = "1"; });
  setTimeout(() => {
    toast.style.opacity = "0";
    setTimeout(() => toast.remove(), 200);
  }, 2000);
}

// ─── Hash routing ───

function updateHash() {
  let hash = "";
  if (state.selectedClipName) {
    hash = `clip/${encodeURIComponent(state.selectedClipName)}`;
    if (state.expandedCommand) hash += `/${encodeURIComponent(state.expandedCommand)}`;
  }
  history.replaceState(null, "", hash ? `#${hash}` : window.location.pathname);
}

function parseHash() {
  const hash = window.location.hash.slice(1);
  if (!hash) return null;
  const match = hash.match(/^clip\/([^/]+)\/?(.*)$/);
  if (!match) return null;
  return {
    clipName: decodeURIComponent(match[1]),
    command: match[2] ? decodeURIComponent(match[2]) : null,
  };
}

window.addEventListener("hashchange", () => {
  const parsed = parseHash();
  if (parsed) {
    if (state.selectedClipName !== parsed.clipName) {
      state.expandedCommand = parsed.command;
      void selectClip(parsed.clipName);
    } else if (state.expandedCommand !== parsed.command) {
      state.expandedCommand = parsed.command;
      renderDetail();
    }
  } else {
    state.selectedClipName = null;
    state.selectedManifest = null;
    state.expandedCommand = null;
    state.lastResult = null;
    renderClipList();
    renderDetail();
  }
});

// ─── Init & auto-refresh ───

void init();
setInterval(() => {
  void refreshClips(true);
  void refreshProviderList();
}, 30000);

async function init() {
  await Promise.all([
    refreshClips(),
    refreshProviderList(),
  ]);

  // Restore state from URL hash after data is loaded
  const parsed = parseHash();
  if (parsed && parsed.clipName) {
    state.selectedClipName = parsed.clipName;
    if (parsed.command) state.expandedCommand = parsed.command;
    const current = getSelectedClip();
    if (current) {
      state.selectedManifest = current.manifest || null;
      renderClipList();
      await loadManifest(parsed.clipName, { preserveResult: true });
    }
  }
}

// ─── Data fetching ───

async function refreshClips(silent = false) {
  if (!silent) {
    refreshButton.disabled = true;
    clipList.innerHTML = '<div class="loading">Loading clips...</div>';
  }

  try {
    const payload = await hubUnary(HUB_PROCEDURES.listClips, {});
    state.clips = Array.isArray(payload?.clips) ? payload.clips.map(normalizeClipInfo) : [];
    state.connected = true;

    if (state.selectedClipName) {
      const current = getSelectedClip();
      if (!current) {
        state.selectedClipName = null;
        state.selectedManifest = null;
        state.expandedCommand = null;
        state.detailError = null;
      }
    }

    updateConnectionStatus();
    renderClipList();

    if (state.selectedClipName) {
      await loadManifest(state.selectedClipName, { preserveResult: true });
    } else if (!silent) {
      renderDetail();
    }
  } catch (error) {
    state.connected = false;
    updateConnectionStatus();
    if (!silent) {
      renderClipList(String(error.message || error));
      detailView.innerHTML = renderNotice(
        "Unable to load clips",
        String(error.message || error),
      );
    }
  } finally {
    refreshButton.disabled = false;
  }
}

async function refreshProviderList() {
  try {
    const payload = await hubUnary(HUB_PROCEDURES.listProviders, {});
    state.providers = Array.isArray(payload?.providers) ? payload.providers : [];
    renderProviderList();
    providerCountEl.textContent = `${state.providers.length} provider${state.providers.length === 1 ? "" : "s"}`;
  } catch {
    // ListProviders may not be available on older hubs; ignore silently
    state.providers = [];
    renderProviderList();
  }
}

function updateConnectionStatus() {
  connectionBadge.className = `connection-badge ${state.connected ? "connected" : "disconnected"}`;
  connectionBadge.textContent = state.connected ? "Connected" : "Disconnected";
}

// ─── Clip selection & manifest ───

async function selectClip(name) {
  if (state.selectedClipName !== name) {
    state.lastResult = null;
    state.resultExpanded = false;
  }

  state.selectedClipName = name;
  state.expandedCommand = null;
  state.detailError = null;
  state.selectedManifest = getSelectedClip()?.manifest || null;

  updateHash();
  renderClipList();
  renderDetail(true);

  await loadManifest(name, { preserveResult: true });
}

async function loadManifest(name, options = {}) {
  const selected = getSelectedClip();
  if (!selected || selected.name !== name) return;

  state.detailError = null;
  if (!options.preserveResult) state.lastResult = null;

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

// ─── Smart form input handling ───

function getSmartInputKey(clipName, commandName, fieldName) {
  return `${clipName}:${commandName}:${fieldName}`;
}

function getSmartInputValue(clipName, commandName, fieldName) {
  return state.smartInputs[getSmartInputKey(clipName, commandName, fieldName)] || "";
}

function setSmartInputValue(clipName, commandName, fieldName, value) {
  state.smartInputs[getSmartInputKey(clipName, commandName, fieldName)] = value;
}

function buildSmartInput(clipName, commandName) {
  const form = SMART_FORMS[commandName];
  if (!form) return null;

  const values = {};
  for (const field of form.fields) {
    values[field.name] = getSmartInputValue(clipName, commandName, field.name);
  }
  return form.buildInput(values);
}

// ─── Command invocation ───

async function invokeCommand(commandName) {
  const clip = getSelectedClip();
  if (!clip) return;

  let input;

  // Try smart form first
  const formDef = SMART_FORMS[commandName];
  if (formDef) {
    input = buildSmartInput(clip.name, commandName);
  } else if (NO_INPUT_COMMANDS.has(commandName)) {
    input = {};
  } else {
    // JSON textarea fallback
    const inputText = getCommandInput(clip.name, commandName).trim();
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
  }

  state.runningCommand = commandName;
  state.resultExpanded = false;
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

// ─── Rendering: Provider list ───

function relativeTime(dateStr) {
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

function renderProviderList() {
  if (state.providers.length === 0) {
    providerList.innerHTML = '<div class="empty-list-sm">No providers connected</div>';
    return;
  }

  providerList.innerHTML = state.providers.map((p) => {
    const clipCount = Array.isArray(p.clips) ? p.clips.length : 0;
    const name = p.name || "unknown";
    const connectedAt = p.connectedAt || p.connected_at || "";
    const timeAgo = relativeTime(connectedAt);
    return `
      <div class="provider-item">
        <div class="provider-item-left">
          <span class="provider-dot"></span>
          <span class="provider-name" title="${escapeHTML(name)}">${escapeHTML(name)}</span>
          ${timeAgo ? `<span class="provider-time" title="${escapeHTML(connectedAt)}">${escapeHTML(timeAgo)}</span>` : ""}
        </div>
        <span class="provider-clip-count">${clipCount} clip${clipCount === 1 ? "" : "s"}</span>
      </div>
    `;
  }).join("");
}

// ─── Rendering: Clip list (grouped by provider) ───

function renderClipList(errorMessage = "") {
  const totalClips = state.clips.length;
  clipCountEl.textContent = `${totalClips} clip${totalClips === 1 ? "" : "s"}`;

  if (errorMessage) {
    clipList.innerHTML = `<div class="notice">${escapeHTML(errorMessage)}</div>`;
    return;
  }

  if (totalClips === 0) {
    clipList.innerHTML = '<div class="empty-list-sm">No clips registered.</div>';
    return;
  }

  // Filter by search
  let filteredClips = state.clips;
  if (state.searchQuery) {
    filteredClips = state.clips.filter((c) =>
      c.name.toLowerCase().includes(state.searchQuery) ||
      (c.provider && c.provider.toLowerCase().includes(state.searchQuery)) ||
      (c.package && c.package.toLowerCase().includes(state.searchQuery)) ||
      (c.domain && c.domain.toLowerCase().includes(state.searchQuery))
    );
  }

  if (filteredClips.length === 0) {
    clipList.innerHTML = '<div class="empty-list-sm">No clips match your search.</div>';
    return;
  }

  // Group by provider
  const groups = new Map();
  for (const clip of filteredClips) {
    const provider = clip.provider || "unknown";
    if (!groups.has(provider)) {
      groups.set(provider, []);
    }
    groups.get(provider).push(clip);
  }

  clipList.innerHTML = "";

  for (const [provider, clips] of groups) {
    const groupEl = document.createElement("div");
    groupEl.className = `clip-group${state.collapsedGroups[provider] ? " collapsed" : ""}`;

    const headerBtn = document.createElement("button");
    headerBtn.type = "button";
    headerBtn.className = "clip-group-header";
    headerBtn.innerHTML = `
      <span class="clip-group-label" title="${escapeHTML(provider)}">${escapeHTML(provider)}</span>
      <span class="clip-group-count">${clips.length}</span>
      <span class="clip-group-chevron">&#9660;</span>
    `;
    headerBtn.addEventListener("click", () => {
      state.collapsedGroups[provider] = !state.collapsedGroups[provider];
      groupEl.classList.toggle("collapsed");
    });

    const itemsEl = document.createElement("div");
    itemsEl.className = "clip-group-items";

    for (const clip of clips) {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = `clip-item${clip.name === state.selectedClipName ? " active" : ""}`;
      btn.addEventListener("click", () => void selectClip(clip.name));

      const commandCount = getClipCommands(clip).length;
      const pkgInfo = clip.package && clip.package !== clip.name ? clip.package : "";
      const versionInfo = clip.version ? `v${clip.version}` : "";
      const metaParts = [pkgInfo, versionInfo].filter(Boolean).join(" ");

      const webIcon = clip.hasWeb
        ? '<svg class="clip-web-icon" width="12" height="12" viewBox="0 0 16 16" fill="none"><circle cx="8" cy="8" r="6" stroke="currentColor" stroke-width="1.5"/><path d="M2 8h12M8 2c-2 2-2 10 0 12M8 2c2 2 2 10 0 12" stroke="currentColor" stroke-width="1.2"/></svg>'
        : "";

      btn.innerHTML = `
        <div class="clip-item-head">
          <span class="clip-name">${escapeHTML(clip.name)}${webIcon}</span>
          <span class="clip-cmd-count">${commandCount} cmd${commandCount === 1 ? "" : "s"}</span>
        </div>
        ${metaParts ? `<div class="clip-meta-row"><span class="clip-pkg">${escapeHTML(metaParts)}</span></div>` : ""}
      `;

      itemsEl.appendChild(btn);
    }

    groupEl.appendChild(headerBtn);
    groupEl.appendChild(itemsEl);
    clipList.appendChild(groupEl);
  }
}

// ─── Clip status ───

function clipStatusInfo(clip) {
  if (!state.connected) return { cls: "stopped", label: "hub offline" };
  const provider = state.providers.find((p) => p.name === clip.provider);
  if (!provider) return { cls: "stopped", label: "no provider" };
  return { cls: "running", label: "available" };
}

// ─── Rendering: Detail panel ───

function renderDetail(isLoading = false) {
  const clip = getSelectedClip();
  if (!clip) {
    detailView.innerHTML = `
      <div class="empty-state">
        <div class="empty-state-inner">
          <div class="empty-icon">
            <svg width="48" height="48" viewBox="0 0 48 48" fill="none"><rect x="8" y="10" width="32" height="28" rx="4" stroke="currentColor" stroke-width="2" opacity="0.3"/><path d="M16 22h16M16 28h10" stroke="currentColor" stroke-width="2" stroke-linecap="round" opacity="0.3"/></svg>
          </div>
          <h2>Select a clip</h2>
          <p>Choose a clip from the list to inspect its manifest and run commands.</p>
        </div>
      </div>
    `;
    return;
  }

  const manifest = state.selectedManifest || clip.manifest || normalizeManifest(null, clip) || { name: clip.name, commands: [] };
  const commands = Array.isArray(manifest.commands) ? manifest.commands : [];
  const hasWeb = Boolean(manifest.hasWeb ?? clip.hasWeb);
  const description = manifest.description || "";
  const status = clipStatusInfo(clip);

  detailView.innerHTML = `
    <div class="detail-wrap">
      <div class="detail-header">
        <div class="detail-title">
          <h2>${escapeHTML(clip.name)}</h2>
          ${description ? `<p class="detail-desc">${escapeHTML(description)}</p>` : ""}
        </div>
        <div class="detail-actions">
          ${hasWeb ? `<a class="detail-web-btn" href="${escapeHTML(clipWebURL(clip.name))}">Open Web UI</a>` : ""}
          <span class="status-pill ${status.cls}">${escapeHTML(status.label)}</span>
        </div>
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
      ${state.detailError ? `<div class="notice">${escapeHTML(state.detailError)}</div>` : ""}

      <section class="commands-section">
        <h3>Commands</h3>
        <div class="commands">
          ${commands.length === 0
            ? '<div class="empty-list-sm">No commands found in the manifest.</div>'
            : commands.map((cmd) => renderCommandCard(clip, cmd)).join("")}
        </div>
      </section>
    </div>
  `;

  // Bind events
  bindCommandEvents(clip, commands);
  bindResultEvents();
}

function renderCommandCard(clip, commandObj) {
  const commandName = typeof commandObj === "string" ? commandObj : commandObj.name;
  const expanded = state.expandedCommand === commandName;
  const running = state.runningCommand === commandName;
  const commandDescription = typeof commandObj === "object" ? String(commandObj.description || "").trim() : "";

  return `
    <article class="command-card${expanded ? " expanded" : ""}">
      <button type="button" class="command-toggle" data-command-toggle="${escapeHTML(commandName)}">
        <span>
          <span class="command-name">${escapeHTML(commandName)}</span>
          ${commandDescription ? `<span class="command-desc-inline">${escapeHTML(truncate(commandDescription, 60))}</span>` : ""}
        </span>
        <span class="command-arrow">${expanded ? "&#9662;" : "&#9656;"}</span>
      </button>
      ${expanded ? renderCommandBody(clip, commandName, commandDescription, running) : ""}
    </article>
  `;
}

function renderCommandBody(clip, commandName, description, running) {
  const formDef = SMART_FORMS[commandName];
  const isNoInput = NO_INPUT_COMMANDS.has(commandName);

  let formHTML = "";

  if (formDef) {
    // Smart form
    const fieldsHTML = formDef.fields.map((field) => {
      const value = getSmartInputValue(clip.name, commandName, field.name);
      if (field.inputType === "select") {
        const optionsHTML = field.options.map((opt) =>
          `<option value="${escapeHTML(opt)}"${opt === value ? " selected" : ""}>${opt || "(default)"}</option>`
        ).join("");
        return `
          <div class="form-field">
            <label class="form-label">${escapeHTML(field.label)}</label>
            <select class="form-select" data-smart-field="${escapeHTML(commandName)}:${escapeHTML(field.name)}">${optionsHTML}</select>
          </div>
        `;
      }
      if (field.inputType === "textarea") {
        return `
          <div class="form-field">
            <label class="form-label">${escapeHTML(field.label)}</label>
            <textarea class="form-textarea form-smart-textarea" data-smart-field="${escapeHTML(commandName)}:${escapeHTML(field.name)}" placeholder="${escapeHTML(field.placeholder || "")}">${escapeHTML(value)}</textarea>
          </div>
        `;
      }
      const inputType = field.inputType === "number" ? "number" : field.inputType === "date" ? "date" : "text";
      return `
        <div class="form-field">
          <label class="form-label">${escapeHTML(field.label)}</label>
          <input type="${inputType}" class="form-input" data-smart-field="${escapeHTML(commandName)}:${escapeHTML(field.name)}" value="${escapeHTML(value)}" placeholder="${escapeHTML(field.placeholder || "")}">
        </div>
      `;
    }).join("");

    formHTML = `<div class="form-row">${fieldsHTML}</div>`;
  } else if (isNoInput) {
    // No input needed
    formHTML = '<p class="command-help-text">This command requires no input.</p>';
  } else {
    // JSON textarea fallback
    let inputValue = getCommandInput(clip.name, commandName);
    if (!inputValue) {
      inputValue = "{}";
      setCommandInput(clip.name, commandName, inputValue);
    }
    formHTML = `
      <div class="form-field">
        <label class="form-label">Input JSON</label>
        <textarea
          class="form-textarea"
          data-command-input="${escapeHTML(commandName)}"
          spellcheck="false"
        >${escapeHTML(inputValue)}</textarea>
      </div>
    `;
  }

  const inlineResult = (state.lastResult?.command === commandName && state.lastResult?.clip === clip.name)
    ? renderResultSection()
    : "";

  return `
    <div class="command-body">
      ${description ? `<p class="command-help-text">${escapeHTML(description)}</p>` : ""}
      ${formHTML}
      <div class="command-footer">
        <button type="button" class="run-button" data-command-run="${escapeHTML(commandName)}" ${running ? "disabled" : ""}>
          ${running ? '<span class="spinner"></span> Running...' : "Run"}
        </button>
      </div>
      ${inlineResult}
    </div>
  `;
}

function bindCommandEvents(clip, commands) {
  for (const commandObj of commands) {
    const commandName = typeof commandObj === "string" ? commandObj : commandObj.name;

    const toggle = document.querySelector(`[data-command-toggle="${cssEscape(commandName)}"]`);
    if (toggle) {
      toggle.addEventListener("click", () => {
        state.expandedCommand = state.expandedCommand === commandName ? null : commandName;
        updateHash();
        renderDetail();
      });
    }

    const runBtn = document.querySelector(`[data-command-run="${cssEscape(commandName)}"]`);
    if (runBtn) {
      runBtn.addEventListener("click", () => void invokeCommand(commandName));
    }

    // Bind smart form inputs
    const smartFields = document.querySelectorAll(`[data-smart-field^="${cssEscape(commandName)}:"]`);
    for (const field of smartFields) {
      const [, fieldName] = field.dataset.smartField.split(":");
      field.addEventListener("input", (e) => {
        setSmartInputValue(clip.name, commandName, fieldName, e.target.value);
      });
      field.addEventListener("change", (e) => {
        setSmartInputValue(clip.name, commandName, fieldName, e.target.value);
      });
      // Enter key runs command
      if (field.tagName === "INPUT") {
        field.addEventListener("keydown", (e) => {
          if (e.key === "Enter") void invokeCommand(commandName);
        });
      }
    }

    // Bind JSON textarea
    const textarea = document.querySelector(`textarea[data-command-input="${cssEscape(commandName)}"]`);
    if (textarea) {
      textarea.addEventListener("input", (e) => {
        setCommandInput(clip.name, commandName, e.target.value);
      });
    }
  }
}

// ─── Rendering: Result section ───

function renderResultSection() {
  const result = state.lastResult;
  const resultBody = renderResultBodyHTML();
  const imagePreview = extractBase64Image(result?.payload);
  const bodyText = renderResultBodyText();
  const isLarge = bodyText.length > 1500;

  return `
    <section class="result-card">
      <div class="result-header">
        <span class="result-title">Result</span>
        <span class="result-meta ${result ? (result.ok ? "ok" : "err") : ""}">${renderResultMeta()}</span>
      </div>
      ${imagePreview ? `<div class="result-image-preview"><img src="${imagePreview}" alt="Result image"></div>` : ""}
      <pre class="result-output${isLarge && !state.resultExpanded ? " collapsed" : ""}">${resultBody}</pre>
      ${isLarge ? `<button type="button" class="result-toggle" data-result-toggle>${state.resultExpanded ? "Collapse" : "Show all"}</button>` : ""}
    </section>
  `;
}

function bindResultEvents() {
  const toggleBtn = document.querySelector("[data-result-toggle]");
  if (toggleBtn) {
    toggleBtn.addEventListener("click", () => {
      state.resultExpanded = !state.resultExpanded;
      renderDetail();
    });
  }
}

function renderResultMeta() {
  if (!state.lastResult) return "No command executed yet";
  return `${state.lastResult.clip}/${state.lastResult.command} - ${state.lastResult.meta}`;
}

function renderResultBodyText() {
  if (!state.lastResult) {
    return JSON.stringify({ status: "idle" }, null, 2);
  }
  if (typeof state.lastResult.payload === "string") {
    return state.lastResult.payload;
  }
  return JSON.stringify(state.lastResult.payload, null, 2);
}

function renderResultBodyHTML() {
  const text = renderResultBodyText();
  // Try to syntax-highlight JSON
  try {
    const parsed = JSON.parse(text);
    return syntaxHighlightJSON(parsed);
  } catch {
    return escapeHTML(text);
  }
}

// ─── JSON syntax highlighting ───

function syntaxHighlightJSON(value, indent = 0) {
  const pad = "  ".repeat(indent);
  const pad1 = "  ".repeat(indent + 1);

  if (value === null) {
    return `<span class="json-null">null</span>`;
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
    if (value.length === 0) return `<span class="json-bracket">[]</span>`;
    const items = value.map((item) => `${pad1}${syntaxHighlightJSON(item, indent + 1)}`);
    return `<span class="json-bracket">[</span>\n${items.join(",\n")}\n${pad}<span class="json-bracket">]</span>`;
  }

  if (typeof value === "object") {
    const keys = Object.keys(value);
    if (keys.length === 0) return `<span class="json-bracket">{}</span>`;
    const entries = keys.map((key) => {
      return `${pad1}<span class="json-key">"${escapeHTML(key)}"</span>: ${syntaxHighlightJSON(value[key], indent + 1)}`;
    });
    return `<span class="json-bracket">{</span>\n${entries.join(",\n")}\n${pad}<span class="json-bracket">}</span>`;
  }

  return escapeHTML(String(value));
}

// ─── Base64 image detection ───

function extractBase64Image(payload) {
  if (!payload || typeof payload !== "object") return null;

  // Check common patterns for base64 image data
  const candidates = [
    payload.image, payload.data, payload.screenshot,
    payload.imageData, payload.image_data, payload.base64,
    payload.result?.image, payload.result?.data, payload.result?.screenshot,
  ];

  for (const val of candidates) {
    if (typeof val === "string" && val.length > 100) {
      // Check if it starts with a data URI
      if (val.startsWith("data:image/")) return val;
      // Check if it looks like raw base64 (PNG magic bytes in base64: iVBOR)
      if (val.startsWith("iVBOR") || val.startsWith("/9j/") || val.startsWith("R0lGOD")) {
        // PNG, JPEG, GIF
        const prefix = val.startsWith("iVBOR") ? "data:image/png;base64," :
                       val.startsWith("/9j/") ? "data:image/jpeg;base64," :
                       "data:image/gif;base64,";
        return prefix + val;
      }
    }
  }

  return null;
}

// ─── Notices ───

function renderNotice(title, message) {
  return `
    <div class="empty-state">
      <div class="empty-state-inner">
        <h2>${escapeHTML(title)}</h2>
        <div class="notice">${escapeHTML(message)}</div>
      </div>
    </div>
  `;
}

// ─── Helpers ───

function getSelectedClip() {
  return state.clips.find((clip) => clip.name === state.selectedClipName) || null;
}

function getClipCommands(clip) {
  if (Array.isArray(clip?.manifest?.commands)) return clip.manifest.commands;
  if (Array.isArray(clip?.commands)) return clip.commands;
  return [];
}

function clipWebURL(name) {
  return `/clips/${encodeURIComponent(String(name || "").trim())}/`;
}

function getCommandInput(clipName, commandName) {
  const key = `${clipName}:${commandName}`;
  if (!(key in state.inputs)) state.inputs[key] = "{}";
  return state.inputs[key];
}

function setCommandInput(clipName, commandName, value) {
  state.inputs[`${clipName}:${commandName}`] = value;
}

function truncate(text, max) {
  if (text.length <= max) return text;
  return text.slice(0, max - 1) + "\u2026";
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

// ─── Normalization ───

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
  if (!manifest && !fallback) return null;

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
  if (!command) return null;
  const name = String(command.name || "").trim();
  if (!name) return null;
  return {
    name,
    description: String(command.description || "").trim(),
    input: String(command.input || "").trim(),
    output: String(command.output || "").trim(),
  };
}

function normalizeStringArray(values) {
  if (!Array.isArray(values)) return [];
  const result = [];
  const seen = new Set();
  for (const value of values) {
    const text = String(value || "").trim();
    if (!text || seen.has(text)) continue;
    seen.add(text);
    result.push(text);
  }
  return result.sort();
}

// ─── Connect-RPC transport (unchanged protocol logic) ───

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
  if (!text) return null;
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
      if (done) break;
      if (value && value.length > 0) {
        buffer = concatByteArrays(buffer, value);
      }

      for (;;) {
        const frame = readConnectFrame(buffer);
        if (!frame) break;

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
  if (!(buffer instanceof Uint8Array) || buffer.length < 5) return null;

  const view = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength);
  const flags = view.getUint8(0);
  const length = view.getUint32(1);
  const totalLength = 5 + length;

  if (buffer.length < totalLength) return null;

  return {
    compressed: Boolean(flags & 0b00000001),
    endStream: Boolean(flags & 0b00000010),
    payload: buffer.slice(5, totalLength),
    rest: buffer.slice(totalLength),
  };
}

function parseConnectJSON(bytes) {
  const text = textDecoder.decode(bytes);
  if (!text) return {};
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
  if (!outputs.length) return {};
  const bytes = outputs.length === 1 ? outputs[0] : concatManyByteArrays(outputs);
  const text = textDecoder.decode(bytes);
  if (!text.trim()) return {};
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function concatByteArrays(left, right) {
  if (!left.length) return right.slice();
  if (!right.length) return left.slice();
  const combined = new Uint8Array(left.length + right.length);
  combined.set(left, 0);
  combined.set(right, left.length);
  return combined;
}

function concatManyByteArrays(chunks) {
  let length = 0;
  for (const chunk of chunks) length += chunk.length;
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

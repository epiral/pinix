const state = {
  clips: [],
  capabilities: [],
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
const capabilityCount = document.getElementById("capabilityCount");
const capabilityList = document.getElementById("capabilityList");
const detailView = document.getElementById("detailView");

refreshButton.addEventListener("click", () => {
  void refreshClips();
});

void refreshClips();

async function refreshClips() {
  refreshButton.disabled = true;
  clipList.innerHTML = '<div class="loading">Loading clips...</div>';
  capabilityList.innerHTML = '<div class="loading">Loading capabilities...</div>';

  try {
    const payload = await apiJSON("/api/list");
    state.clips = Array.isArray(payload.clips) ? payload.clips : [];
    state.capabilities = Array.isArray(payload.capabilities) ? payload.capabilities : [];

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
    renderCapabilityList();

    if (state.selectedClipName) {
      await loadManifest(state.selectedClipName, { preserveResult: true });
    } else {
      renderDetail();
    }
  } catch (error) {
    renderClipList(String(error.message || error));
    renderCapabilityList(String(error.message || error));
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
    state.selectedManifest = await apiJSON(`/api/manifest?clip=${encodeURIComponent(name)}`);
    selected.manifest = state.selectedManifest;
    renderClipList();
  } catch (error) {
    state.selectedManifest = selected.manifest || null;
    state.detailError = String(error.message || error);
  }

  renderDetail();
}

async function invokeCommand(command) {
  const clip = getSelectedClip();
  if (!clip) {
    return;
  }

  const inputText = getCommandInput(clip.name, command).trim() || "{}";
  let input;

  try {
    input = JSON.parse(inputText);
  } catch (error) {
    state.lastResult = {
      ok: false,
      clip: clip.name,
      command,
      meta: "Invalid JSON input",
      payload: { error: String(error.message || error) },
    };
    renderDetail();
    return;
  }

  state.runningCommand = command;
  renderDetail();

  try {
    const payload = await apiJSON("/api/invoke", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        clip: clip.name,
        command,
        input,
      }),
    });

    state.lastResult = {
      ok: true,
      clip: clip.name,
      command,
      meta: "Invocation succeeded",
      payload,
    };
  } catch (error) {
    state.lastResult = {
      ok: false,
      clip: clip.name,
      command,
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

    const commandCount = Array.isArray(clip.manifest?.commands) ? clip.manifest.commands.length : 0;
    const stateClass = clip.running ? "running" : "stopped";
    const stateLabel = clip.running ? "running" : "stopped";

    button.innerHTML = `
      <div class="clip-item-head">
        <span class="clip-name">${escapeHTML(clip.name)}</span>
        <span class="status-pill ${stateClass}">${stateLabel}</span>
      </div>
      <p class="clip-source">${escapeHTML(clip.source || "No source")}</p>
      <div class="clip-stats">
        <span class="badge">${commandCount} command${commandCount === 1 ? "" : "s"}</span>
      </div>
    `;

    clipList.appendChild(button);
  }
}

function renderCapabilityList(errorMessage = "") {
  const onlineCount = state.capabilities.filter((capability) => capability.online).length;
  capabilityCount.textContent = `${onlineCount} online`;

  if (errorMessage) {
    capabilityList.innerHTML = `<div class="notice">${escapeHTML(errorMessage)}</div>`;
    return;
  }

  if (state.capabilities.length === 0) {
    capabilityList.innerHTML = '<div class="empty-list">No capabilities connected.</div>';
    return;
  }

  capabilityList.innerHTML = state.capabilities.map((capability) => {
    const commands = Array.isArray(capability.commands) ? capability.commands : [];
    const stateClass = capability.online ? "running" : "stopped";
    const stateLabel = capability.online ? "online" : "offline";

    return `
      <article class="capability-item">
        <div class="clip-item-head capability-item-head">
          <span class="clip-name">${escapeHTML(capability.name)}</span>
          <span class="status-pill ${stateClass}">${stateLabel}</span>
        </div>
        <p class="clip-source capability-commands">${escapeHTML(commands.join(", ") || "No commands registered")}</p>
        <div class="clip-stats">
          <span class="badge">${commands.length} command${commands.length === 1 ? "" : "s"}</span>
        </div>
      </article>
    `;
  }).join("");
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

  const manifest = state.selectedManifest || clip.manifest || { name: clip.name, domain: "", commands: [] };
  const commands = Array.isArray(manifest.commands) ? manifest.commands : [];

  detailView.innerHTML = `
    <div class="detail-wrap">
      <div class="detail-header">
        <div class="detail-title">
          <h2>${escapeHTML(clip.name)}</h2>
          <p class="detail-meta">Inspect manifest metadata and invoke clip commands from the local portal.</p>
        </div>
        <span class="status-pill ${clip.running ? "running" : "stopped"}">${clip.running ? "running" : "stopped"}</span>
      </div>

      <div class="detail-grid">
        <div class="meta-card">
          <span class="meta-label">Domain</span>
          <div class="meta-value">${escapeHTML(manifest.domain || "-")}</div>
        </div>
        <div class="meta-card">
          <span class="meta-label">Source</span>
          <div class="meta-value">${escapeHTML(clip.source || "-")}</div>
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

  for (const command of commands) {
    const toggle = document.querySelector(`[data-command-toggle="${cssEscape(command)}"]`);
    if (toggle) {
      toggle.addEventListener("click", () => {
        state.expandedCommand = state.expandedCommand === command ? null : command;
        renderDetail();
      });
    }

    const input = document.querySelector(`[data-command-input="${cssEscape(command)}"]`);
    if (input) {
      input.addEventListener("input", (event) => {
        setCommandInput(clip.name, command, event.target.value);
      });
    }

    const runButton = document.querySelector(`[data-command-run="${cssEscape(command)}"]`);
    if (runButton) {
      runButton.addEventListener("click", () => {
        void invokeCommand(command);
      });
    }
  }
}

function renderCommandCard(clip, command) {
  const expanded = state.expandedCommand === command;
  const inputValue = getCommandInput(clip.name, command);
  const running = state.runningCommand === command;

  return `
    <article class="command-card${expanded ? " expanded" : ""}">
      <button type="button" class="command-toggle" data-command-toggle="${escapeHTML(command)}">
        <span class="command-name">${escapeHTML(command)}</span>
        <span class="command-arrow">${expanded ? "Hide" : "Open"}</span>
      </button>
      ${expanded ? `
        <div class="command-body">
          <label class="command-label" for="command-input-${escapeHTML(command)}">Input JSON</label>
          <textarea
            id="command-input-${escapeHTML(command)}"
            class="command-input"
            data-command-input="${escapeHTML(command)}"
            spellcheck="false"
          >${escapeHTML(inputValue)}</textarea>
          <div class="command-footer">
            <p class="status-note">Requests are sent to <code>/api/invoke</code>.</p>
            <button type="button" class="run-button" data-command-run="${escapeHTML(command)}" ${running ? "disabled" : ""}>
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

function getCommandInput(clipName, command) {
  const key = `${clipName}:${command}`;
  if (!(key in state.inputs)) {
    state.inputs[key] = "{}";
  }
  return state.inputs[key];
}

function setCommandInput(clipName, command, value) {
  state.inputs[`${clipName}:${command}`] = value;
}

async function apiJSON(url, options = {}) {
  const response = await fetch(url, {
    headers: {
      Accept: "application/json",
      ...(options.headers || {}),
    },
    ...options,
  });

  const text = await response.text();
  let payload = null;

  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      if (!response.ok) {
        throw new Error(text);
      }
      return text;
    }
  }

  if (!response.ok) {
    throw new Error(payload?.error?.message || response.statusText || "Request failed");
  }

  return payload;
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
  return String(value).replaceAll('"', '\\"');
}

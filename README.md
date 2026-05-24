# Pinix

**Agent Harness** — an open capability layer for Agents to discover, invoke, compose, and constrain tools.

Pinix wraps devices, apps, websites, services, and workflows into **Clips**. Each Clip is a self-contained capability unit that can be called by any Agent, CLI, or MCP client. Some Clips also ship their own Web UI for direct human use.

```text
Human / Agent / CLI
  -> Hub
  -> Clips
  -> device / app / web / SaaS / workflow
```

## Quick Start

```bash
# 1. Start Pinix
pinixd

# 2. Connect to pinix.ai
pinix login

# 3. Install a Clip
pinix hub add @pinix/todo

# 4. Use it
pinix invoke todo add --title "Hello Pinix"
pinix invoke todo list

# 5. Open Console
open http://localhost:9000
```

After `pinix login`, your local Clips are accessible from any device through pinix.ai Cloud Hub.

## What Happens When You Start Pinix

```text
pinixd
  ├── Hub        — routes invoke calls to the right Clip
  ├── Runtime    — manages local Bun/TS Clip processes
  ├── Registry   — installs Clips from pinix.ai Registry
  └── Console    — web UI to manage Clips at localhost:9000
```

If BB-Browser is installed, it auto-starts a headless Chrome and registers browser capabilities as Edge Clips:

```text
pinix hub list

  github     RUNNING   trending, repo, search
  twitter    RUNNING   search, timeline
  reddit     RUNNING   search, hot
  todo       RUNNING   add, delete, list
```

## Clips

Clips are the core abstraction. They provide two key values:

1. **Lower model requirements.** Complex operations are wrapped into deterministic commands. Agents just call them.
2. **Bounded capabilities.** Agents can only access what Clips expose. Permissions, audit, and testing are built in.

Three types of Clips:

| Type | How it works | Example |
|---|---|---|
| **SDK Clip** | Bun/TS app managed by Runtime, installed via `pinix hub add` | todo, review, memex |
| **Edge Clip** | Native process implementing Provider protocol, auto-registers with Hub | BB-Browser sites, clipboard, screen |
| **API Clip** | Wraps an external API as a Clip | GitHub CLI, 12306, Amap |

## BB-Browser

BB-Browser turns any website into an Agent-callable Clip by running in the user's real Chrome — reusing login sessions, cookies, SSO, and intranet access.

```bash
pinix invoke github trending
# → structured JSON from GitHub, using your logged-in session

pinix invoke twitter search --query "AI agent"
# → Twitter search results via your account
```

Live view at `http://localhost:6111` shows what Chrome is doing in real time.

## Connect to pinix.ai

```bash
pinix login
```

This connects your local Pinix to pinix.ai Cloud Hub. Your Clips become accessible from any device. pinix.ai also provides:

- **Cloud Hub** — cross-network Clip routing
- **Cloud Registry** — discover and install Clips
- **Model Proxy** — use AI models without managing API keys
- **Console** — manage Clips from the web

## Build from Source

```bash
# Requires Go 1.22+ and Bun
go build -o pinixd ./cmd/pinixd
go build -o pinix ./cmd/pinix

pinixd
```

## Docs

- [Architecture](docs/architecture.md)
- [Getting Started](docs/getting-started.md)
- [Clip Development](docs/clip-development.md)
- [Edge Clip Development](docs/edge-clip-development.md)
- [Protocol](docs/protocol.md)
- [Deployment](docs/deployment.md)

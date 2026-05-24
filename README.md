<p align="center">
  <img src="https://pinix.ai/logo.svg" alt="Pinix" width="120" />
</p>

<h1 align="center">Pinix</h1>

<p align="center">
  <b>Agent Harness</b> — the open capability layer that lets any Agent discover, invoke, and compose tools.
</p>

<p align="center">
  <a href="https://pinixai.com">Website</a> &middot;
  <a href="docs/getting-started.md">Docs</a> &middot;
  <a href="https://discord.gg/pinix">Discord</a>
</p>

<!-- <p align="center">
  <img src="demo.gif" width="680" alt="Pinix demo" />
</p> -->

---

Pinix wraps devices, apps, websites, services, and workflows into **Clips** — self-contained capability units that any Agent can call. Some Clips also ship a Web UI for direct human use.

```
Human / Agent / CLI
  → Hub
  → Clip
  → device / app / web / SaaS / workflow
```

## Get Started

```bash
pinix start                                     # start Pinix daemon
pinix login                                     # connect to pinix.ai
pinix hub add @pinix/todo                       # install a Clip
pinix invoke todo add --title "Hello Pinix"     # use it
pinix invoke todo list                          # see results
open http://localhost:9000                       # open Console
pinix stop                                      # stop daemon
```

After `pinix login`, your local Clips are accessible from any device through pinix.ai Cloud Hub.

## What You Get

When `pinix start` runs, you get a complete local capability stack:

```
pinix start
  ├── Hub        routes invoke calls to the right Clip
  ├── Runtime    manages local Bun/TS Clip processes
  ├── Registry   installs Clips from pinix.ai
  └── Console    web UI at localhost:9000
```

If [BB-Browser](https://github.com/epiral/bb-browser) is installed, it auto-starts a headless Chrome and registers 30+ websites as Edge Clips:

```
$ pinix hub list

  github     RUNNING   trending, repo, search
  twitter    RUNNING   search, timeline
  reddit     RUNNING   search, hot
  browser    RUNNING   open, snapshot, click, fill, eval
  todo       RUNNING   add, delete, list
```

Your browser's login sessions, cookies, SSO, and intranet access work out of the box. Live view at `http://localhost:6111`.

## Features

- [x] **Clip Runtime** — install, run, and manage Bun/TS Clips locally
- [x] **Hub Routing** — route invoke calls by alias, auto-discover by package name
- [x] **Edge Clips** — BB-Browser, clipboard, screen, device APIs register automatically
- [x] **Clip Web** — Clips ship their own Web UI, served at `{alias}.hub.pinix.ai`
- [x] **Console** — manage Clips, view status, embed Clip Web via iframe
- [x] **Registry** — search, install, publish Clips to pinix.ai
- [x] **pinix.ai Cloud Hub** — cross-network routing, access Clips from any device
- [x] **`pinix login`** — device code flow, one command to connect
- [x] **Single binary** — one `pinix` binary: `start`, `stop`, `status`, `login`, `invoke`
- [x] **Auto-connect** — reads saved token, connects to Cloud Hub automatically
- [ ] **`@pinix/agent`** — default single-user Agent Clip (coming soon)
- [ ] **Pinix Desktop** — local shell + OS Edge Clips (coming soon)
- [ ] **`install.sh`** — one-line installer (coming soon)

## Why Clips

Agents need tools. Current options have gaps:

| | MCP | CLI (bash) | Clips |
|---|---|---|---|
| Discovery | all tools injected at once | open environment | on-demand via Hub/Registry |
| Token cost | high (unused tools in context) | low | low (only active Clips loaded) |
| Attention | diluted by irrelevant tools | — | focused on current task |
| Boundary | depends on tool impl | no boundary | strict — Clips define the boundary |
| Model requirement | medium | high (must understand shell) | low (deterministic commands) |

Clips do two things:
1. **Lower model requirements.** Complex ops become deterministic commands.
2. **Bound capabilities.** Agents can only use what Clips expose.

## BB-Browser

[BB-Browser](https://github.com/epiral/bb-browser) turns any website into an Agent-callable Clip. It runs in a real Chrome instance — reusing your login sessions, cookies, SSO, and intranet access.

```bash
pinix invoke github trending
# → structured JSON, using your GitHub session

pinix invoke twitter search --query "AI agent"
# → search results via your Twitter account
```

103 commands across 36 platforms out of the box. Agent can also generate new site adapters autonomously.

## Connect to pinix.ai

```bash
pinix login
```

Opens your browser for device code confirmation. After login:

- Your local Clips are visible from any device via Cloud Hub
- `pinixd` auto-connects on next start (no `--hub` flag needed)
- Cloud Registry for discovering and installing Clips
- Model proxy — use AI models without managing API keys

## Three Types of Clips

| Type | How | Example |
|---|---|---|
| **SDK Clip** | Bun/TS app, managed by Runtime, `pinix hub add` | `@pinix/todo`, `@pinix/review`, `@pinix/memex` |
| **Edge Clip** | Native process, Provider protocol, auto-registers | BB-Browser sites, clipboard, screen, notification |
| **API Clip** | Wraps external API | GitHub, 12306, Amap |

## Build from Source

```bash
# Requires Go 1.22+ and Bun
git clone https://github.com/epiral/pinix.git
cd pinix
go build -o pinix ./cmd/pinix
./pinix start
```

## Documentation

| Doc | Description |
|---|---|
| [Getting Started](docs/getting-started.md) | Installation, first Clip, first invoke |
| [Architecture](docs/architecture.md) | Hub, Runtime, Provider, Clip model |
| [Clip Development](docs/clip-development.md) | Build your own Clip |
| [Edge Clip Development](docs/edge-clip-development.md) | Build hardware/browser Edge Clips |
| [Protocol](docs/protocol.md) | Connect-RPC, ProviderStream, IPC |
| [Deployment](docs/deployment.md) | Production deployment guide |

## License

[MIT](LICENSE)

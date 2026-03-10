# Pinix

A decentralized runtime platform for Clips — with sandboxed execution via pluggable backends.

[![Release](https://img.shields.io/github/v/release/epiral/pinix?color=blue)](https://github.com/epiral/pinix/releases)

[English](README.md) | [中文](README.zh-CN.md)

## Quick Start

### Install

```bash
# Download from GitHub Releases (macOS arm64)
mkdir -p ~/bin ~/.boxlite/rootfs
curl -L https://github.com/epiral/pinix/releases/latest/download/pinix-v0.2.0-darwin-arm64.tar.gz | tar xz -C ~/bin
curl -L https://github.com/epiral/pinix/releases/latest/download/boxlite-v0.2.0-darwin-arm64.tar.gz | tar xz -C ~/bin
curl -L https://github.com/epiral/pinix/releases/latest/download/rootfs-v0.2.0.ext4.gz | gunzip > ~/.boxlite/rootfs/rootfs.ext4

# Or use Clip Dock Desktop (bundles everything)
```

### Start

```bash
boxlite serve --port 8100 &
pinix serve --addr :9875 --boxlite-rest http://localhost:8100
```

### Install a Clip

```bash
pinix clip install agent.clip --server http://localhost:9875 --token <super-token>
```

---

## Core Concepts

### Three Layers

```
Workspace (dev)  →  Package (.clip)  →  Instance (runtime)
   Git repo          ZIP archive         Deployed on Server
   Source code        Compiled binary     data/ is mutable
   go.mod, src/      commands/, bin/     seed/ → data/
```

### Clip Package

A Clip Package defines what a Clip can do:

```
my-clip/
  commands/    → Unix-style scripts (stdin/stdout/exit code)
  web/         → UI for humans
  data/        → Persistent storage (mutable)
  seed/        → Initial data template (copied to data/ on install)
  bin/         → Compiled binaries
  clip.yaml    → Metadata (name, version, schedules)
```

### Clip Instance

A running instance of a Clip Package on a Pinix Server. Each instance has:

- **URL**: Server address (host:port)
- **Token**: Access credential (routed by Server)
- **Isolated workdir**: Own commands/, web/, data/

The same Package can be deployed as multiple independent Instances across different Servers.

### Pinix Server

Hosts Clip Instances. Responsibilities:

- Clip registration & lifecycle management
- Token routing: requests routed to the correct Clip by token
- Auth: validate token legitimacy, restrict access scope
- Sandboxed execution via BoxLite micro-VMs

Fully **decentralized** — multiple Servers are independent, no central registry.

### Clip Dock

Client application (Desktop / iOS) that connects to Clips across any number of Pinix Servers via Bookmarks:

```json
{
  "name": "todo",
  "server_url": "http://100.66.47.40:9875",
  "token": "clip-token-for-todo"
}
```

---

## Clip Registry

Clip Registry is **a Clip**, not a built-in Server feature. It discovers available Clips on any Pinix Server.

Why a Clip instead of a Server feature: **decentralized**, **independently evolvable**, **cross-Server**, **consistent** (discovery itself is a Clip).

---

## Protocol Design

### ClipService — Minimal Kernel

| RPC | Description | Nature |
|-----|-------------|--------|
| **Invoke** | Execute scripts in `commands/` | Single entry point for all mutations |
| **ReadFile** | Read files from `web/` and `data/` | Read-only, supports ETag caching & streaming |
| **GetInfo** | Return Clip metadata | Name, description, commands list, hasWeb |

**Invoke consolidates all business operations.** No WriteFile — write operations are business-specific and should not be abstracted as a generic primitive.

### AdminService — Management Plane

Requires Super Token. Used by Server operators.

| RPC | Description |
|-----|-------------|
| **CreateClip** / **DeleteClip** | Register/unregister Clips |
| **ListClips** | List all Clips (Server scans workdir for metadata) |
| **GenerateToken** / **ListTokens** / **RevokeToken** | Token management |

## Token Model

| Type | Source | Bound To | Scope |
|------|--------|----------|-------|
| **Super Token** | Config file (static) | None | Full AdminService access |
| **Clip Token** | GenerateToken API | Specific Clip | ClipService only (Invoke/ReadFile/GetInfo) |

**Security:**
- Super Token cannot be generated via API — eliminates privilege escalation
- Leaked Clip Token only affects a single Clip — cannot reach AdminService

### Request Routing

```
Clip Dock
  │  Bearer: <clip-token>
  ▼
Pinix Server
  │  Token routing table
  │  ├─ clip-token-A → clip_id: todo    → workdir: /path/to/todo
  │  ├─ clip-token-B → clip_id: agent   → workdir: /path/to/agent
  │  └─ super-token  → clip_id: (none)  → full access
  ▼
Route to Clip workdir, execute commands/ in BoxLite VM or read files
```

---

## Architecture Overview

```
  ┌──────────────────────┐        ┌──────────────────────┐
  │  Pinix Server A      │        │  Pinix Server B      │
  │  (private, home)     │        │  (public)            │
  │                      │        │                      │
  │  Clip: agent         │        │  Clip: news-feed     │
  │  Clip: todo          │        │  Clip: sandbox       │
  │  Clip: registry      │        │                      │
  └──────────┬───────────┘        └──────────┬───────────┘
             │                               │
             │    Bookmark = URL + Token     │
             │                               │
        ┌────┴───────────────────────────────┴────┐
        │             Clip Dock                    │
        │          (Desktop / iOS)                 │
        │                                          │
        │  [agent]       → Server A                │
        │  [todo]        → Server A                │
        │  [news-feed]   → Server B                │
        │  [registry]    → Server A (discover more)│
        └──────────────────────────────────────────┘
```

## Sandbox Architecture

Every command runs inside an isolated BoxLite micro-VM. No fallback to host execution.

```
ClipService.Invoke
  → sandbox.Manager
    → Backend interface
      ├── RestBackend     (BoxLite REST API — production)
      ├── BoxLiteBackend  (BoxLite CLI — development)
      └── FakeBackend     (testing)
```

| Component | File | Role |
|-----------|------|------|
| Backend interface | `internal/sandbox/backend.go` | Pluggable contract |
| RestBackend | `internal/sandbox/rest.go` | HTTP calls to BoxLite serve |
| BoxLiteBackend | `internal/sandbox/boxlite.go` | Direct CLI execution |
| Manager | `internal/sandbox/manager.go` | Delegation layer |

---

## CLI Reference

```bash
pinix serve --addr :9875 --boxlite-rest http://localhost:8100
pinix clip install <file.clip>
pinix clip upgrade <file.clip>
pinix clip list
pinix clip create --name <name> --workdir <path>
pinix clip delete <clip-id>
pinix clip uninstall <name>
pinix token generate --clip <clip-id> --label <label>
pinix token list
pinix token revoke <token-id>
pinix invoke <command> [args...]
pinix read <path>
pinix info
```

## Releases

See [GitHub Releases](https://github.com/epiral/pinix/releases).

v0.2.0 includes:
- `pinix` — Server binary (signed)
- `boxlite` — Sandbox runtime (boxlite, boxlite-shim, boxlite-guest, libkrunfw)
- `rootfs` — Linux root filesystem for micro-VMs

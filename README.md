# Pinix

A decentralized runtime platform for Clips — local sandboxed execution and edge device integration via a unified Clip interface.

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

### What is a Clip?

A Clip is anything that implements three operations:

| Operation | Description |
|-----------|-------------|
| **Invoke** | Execute a named command (stdin → stdout/stderr/exit code) |
| **ReadFile** | Read a file from `web/` or `data/` namespace |
| **GetInfo** | Return metadata (name, description, commands, hasWeb) |

This is the **Clip interface** — the universal contract. How commands are executed is an implementation detail.

### Two Types of Clips

**Local Clips** — commands run in BoxLite micro-VMs on the Server:

```
my-clip/
  commands/    → Unix-style scripts (stdin/stdout/exit code)
  web/         → UI for humans
  data/        → Persistent storage (mutable)
  clip.yaml    → Metadata
```

**Edge Clips** — commands run natively on connected devices (iPhone, Raspberry Pi, ESP32):

```
Device connects via EdgeService → registers capabilities → Server routes requests to device
```

Both implement the same Clip interface. Callers cannot tell the difference.

### Three Layers

```
Workspace (dev)  →  Package (.clip)  →  Instance (runtime)
   Git repo          ZIP archive         Deployed on Server
   Source code        Compiled binary     data/ is mutable
   go.mod, src/      commands/, bin/     seed/ → data/
```

### Pinix Server

Hosts Clip Instances and routes requests. Internal architecture:

```
                    ┌──────────────────────────┐
                    │       ClipService        │  ← public API (unchanged for all clip types)
                    │       AdminService       │
                    │       EdgeService        │  ← device connection endpoint
                    └────────────┬─────────────┘
                                 │
                    ┌────────────▼─────────────┐
                    │          Hub             │
                    │     Clip Registry        │  token → clipID → Clip interface
                    └──────┬──────────┬────────┘
                           │          │
              ┌────────────▼──┐  ┌────▼────────────┐
              │    Worker     │  │      Edge        │
              │  LocalClip    │  │    EdgeClip      │
              │  BoxLite VM   │  │  device stream   │
              └───────────────┘  └──────────────────┘
```

- **Hub**: Routes requests through the Clip interface. Knows nothing about execution details.
- **Worker**: Implements `Clip` via BoxLite sandbox + local filesystem.
- **Edge**: Implements `Clip` via bidirectional stream to connected devices.

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

## Edge Clips

Devices expose native capabilities as Clips through the EdgeService protocol.

### How it works

```
1. Device connects:  EdgeService.Connect (bidirectional stream, super token auth)
2. Device registers:  sends EdgeManifest { name, commands[], description }
3. Server assigns:   clip_id + clip token, registers EdgeClip in Hub
4. Standby:          device waits for forwarded requests

--- Someone invokes the edge clip ---

5. Caller → ClipService.Invoke with clip token
6. Hub routes to EdgeClip → forwards as EdgeRequest to device
7. Device executes natively → streams EdgeResponse back
8. Hub relays to caller — transparent, identical to local Clip
```

### Supported devices

| Device | Runtime | Transport |
|--------|---------|-----------|
| iPhone / Mac | pinix-edge-swift (embed in Clip Dock) | gRPC stream |
| Raspberry Pi | pinix-edge-go (standalone daemon) | gRPC stream |
| ESP32 | pinix-edge-lite | WebSocket (planned) |

### Example: connect an edge device

```bash
# Start a test edge device that exposes "echo" and "hello" commands
go run cmd/edge-test/main.go \
  --server http://localhost:9875 \
  --token <super-token> \
  --name my-device

# From any client, invoke the edge clip like any other clip:
pinix invoke hello --server http://localhost:9875 --token <edge-clip-token>
# → "hello from edge device"
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
| **Invoke** | Execute a command | Single entry point for all mutations |
| **ReadFile** | Read files from `web/` and `data/` | Read-only, supports ETag caching & streaming |
| **GetInfo** | Return Clip metadata | Name, description, commands list, hasWeb |

**Invoke consolidates all business operations.** No WriteFile — write operations are business-specific and should not be abstracted as a generic primitive.

### AdminService — Management Plane

Requires Super Token. Used by Server operators.

| RPC | Description |
|-----|-------------|
| **CreateClip** / **DeleteClip** | Register/unregister Clips |
| **ListClips** | List all Clips with metadata |
| **GenerateToken** / **ListTokens** / **RevokeToken** | Token management |

### EdgeService — Device Connection

Requires Super Token. Used by edge devices.

| RPC | Description |
|-----|-------------|
| **Connect** | Bidirectional stream for device registration and request forwarding |

Edge protocol uses **envelope messages** with request correlation:

```
Server → Device:  EdgeRequest  { request_id, body: Invoke/ReadFile/GetInfo/Cancel }
Device → Server:  EdgeResponse { request_id, body: InvokeChunk/ReadFileChunk/GetInfo/Error/Complete }
```

## Token Model

| Type | Source | Bound To | Scope |
|------|--------|----------|-------|
| **Super Token** | Config file (static) | None | Full AdminService + EdgeService access |
| **Clip Token** | GenerateToken API | Specific Clip | ClipService only (Invoke/ReadFile/GetInfo) |

**Security:**
- Super Token cannot be generated via API — eliminates privilege escalation
- Leaked Clip Token only affects a single Clip — cannot reach AdminService or EdgeService
- Edge Clips get ephemeral Clip Tokens — cleaned up on device disconnect

### Request Routing

```
Clip Dock / Agent / Any Caller
  │  Bearer: <clip-token>
  ▼
Pinix Server (Hub)
  │  Clip Registry
  │  ├─ clip-token-A → clip_id: todo    → LocalClip (BoxLite VM)
  │  ├─ clip-token-B → clip_id: agent   → LocalClip (BoxLite VM)
  │  ├─ clip-token-C → clip_id: iphone  → EdgeClip  (device stream)
  │  └─ super-token  → clip_id: (none)  → full access
  ▼
Route to Clip implementation — caller doesn't know or care which type
```

---

## Architecture Overview

```
  ┌──────────────────────┐        ┌──────────────────────┐
  │  Pinix Server A      │        │  Pinix Server B      │
  │  (private, home)     │        │  (public)            │
  │                      │        │                      │
  │  Clip: agent (local) │        │  Clip: news (local)  │
  │  Clip: todo  (local) │        │  Clip: sandbox       │
  │  Clip: iphone (edge) │        │                      │
  └──┬──────────┬────────┘        └──────────┬───────────┘
     │          │                             │
     │    ┌─────┴─────┐                       │
     │    │  iPhone   │                       │
     │    │ (edge)    │                       │
     │    └───────────┘                       │
     │         Bookmark = URL + Token         │
     │                                        │
┌────┴────────────────────────────────────────┴────┐
│             Clip Dock (Desktop / iOS)             │
│                                                   │
│  [agent]       → Server A (local clip)            │
│  [todo]        → Server A (local clip)            │
│  [iphone]      → Server A (edge clip, same phone) │
│  [news]        → Server B                         │
└───────────────────────────────────────────────────┘
```

## Internal Architecture

```
internal/
  clip/        Clip interface + Registry (the contract)
  hub/         ClipService/AdminService/EdgeService handlers, routing
  worker/      LocalClip implementation (BoxLite sandbox + filesystem)
  edge/        EdgeClip implementation (device session management)
  sandbox/     BoxLite backends (RestBackend, BoxLiteBackend)
  auth/        Token validation interceptor
  config/      YAML persistence (clips, tokens)
  scheduler/   Cron-based command scheduling
```

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

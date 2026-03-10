# Pinix

A decentralized runtime platform for Clips — with sandboxed execution via pluggable backends.

[![Release](https://img.shields.io/github/v/release/epiral/pinix?color=blue)](https://github.com/epiral/pinix/releases)

## Quick Start

### Install

```bash
# Download from GitHub Releases (macOS arm64)
curl -L https://github.com/epiral/pinix/releases/latest/download/pinix-v0.2.0-darwin-arm64.tar.gz | tar xz -C ~/bin
curl -L https://github.com/epiral/pinix/releases/latest/download/boxlite-v0.2.0-darwin-arm64.tar.gz | tar xz -C ~/bin
curl -L https://github.com/epiral/pinix/releases/latest/download/rootfs-v0.2.0.ext4.gz | gunzip > ~/.boxlite/rootfs/rootfs.ext4

# Or use Clip Dock Desktop (bundles everything)
```

### Start

```bash
# Start BoxLite sandbox runtime
boxlite serve --port 8100 &

# Start Pinix Server
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

### Pinix Server

Hosts Clip Instances. Responsibilities:

- Clip registration & lifecycle management
- Token routing: requests routed to the correct Clip by token
- Sandboxed execution via BoxLite micro-VMs

### Clip Dock

Client application (Desktop / iOS) that connects to Clips across any number of Pinix Servers via Bookmarks (URL + Token).

---

## Protocol

### ClipService (3 RPCs)

| RPC | Description |
|-----|-------------|
| **Invoke** | Execute a command in `commands/` (streaming stdout/stderr) |
| **ReadFile** | Read files from `web/` and `data/` (ETag caching) |
| **GetInfo** | Get clip metadata (name, commands, hasWeb) |

### AdminService (requires Super Token)

| RPC | Description |
|-----|-------------|
| **CreateClip** / **DeleteClip** | Register/unregister clips |
| **ListClips** | List all clips with metadata |
| **GenerateToken** / **ListTokens** / **RevokeToken** | Token management |

## Token Model

| Type | Source | Scope |
|------|--------|-------|
| **Super Token** | Config file (static) | Full AdminService access |
| **Clip Token** | GenerateToken API | Single Clip's ClipService only |

---

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

See [GitHub Releases](https://github.com/epiral/pinix/releases) for downloads.

v0.2.0 includes:
- `pinix` — Server binary (signed)
- `boxlite` — Sandbox runtime (boxlite, boxlite-shim, boxlite-guest, libkrunfw)
- `rootfs` — Linux root filesystem for micro-VMs

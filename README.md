# Pinix

Pinix is a Clip runtime and routing Hub: run Bun/TS Clips locally, route Clip-to-Clip calls through a Go Hub, and connect Edge Clips over Connect-RPC.

[![Release](https://img.shields.io/github/v/release/epiral/pinix?color=blue)](https://github.com/epiral/pinix/releases/tag/v2.0.0)

[English](README.md) | [中文](README.zh-CN.md)

## Architecture

```text
CLI / MCP / Portal
        |
        | Connect-RPC
        v
+---------------------------+
| Hub                       |
| routes by clip name       |
+-------------+-------------+
              |
      +-------+-------+
      |               |
      |               | ProviderStream
      |               v
      |         Edge Clips / Providers
      |         (bb-browserd, devices)
      |
      v
 pinixd local runtime
      |
      | NDJSON IPC v2
      v
   Bun/TS Clips
```

## Quick Start

1. Install `bun` when you want to run local Clips, then download `pinixd` and `pinix` from the [v2.0.0 release](https://github.com/epiral/pinix/releases/tag/v2.0.0), or build them from source.
2. Start Pinix in full mode: `./pinixd --port 9000`
3. Install your first Clip: `./pinix --server http://127.0.0.1:9000 add clip-todo`
4. Invoke it: `./pinix --server http://127.0.0.1:9000 todo add -- --title "Ship docs"`
5. Open the Portal at `http://127.0.0.1:9000`, then expose MCP with `./pinix mcp --all --server http://127.0.0.1:9000`

## pinixd Modes

```bash
./pinixd --port 9000
./pinixd --port 9000 --hub-only
./pinixd --port 9001 --hub http://hub:9000
```

- Default mode: Hub + Runtime + Portal.
- `--hub-only`: Hub + Portal only, without local Clip process management.
- `--hub`: Runtime only. It connects to an external Hub as a Provider and registers locally managed Clips there.

## Docs

- [Architecture](docs/architecture.md)
- [Getting Started](docs/getting-started.md)
- [Clip Development](docs/clip-development.md)
- [Edge Clip Development](docs/edge-clip-development.md)
- [Protocol](docs/protocol.md)
- [MCP](docs/mcp.md)
- [Deployment](docs/deployment.md)
- [Decisions](docs/decisions.md)

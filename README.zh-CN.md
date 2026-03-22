# Pinix

Pinix 是一个 Clip runtime 和路由 Hub：本地运行 Bun/TS Clip，通过 Go Hub 做路由，并通过 Connect-RPC 接入 Edge Clip。

[![Release](https://img.shields.io/github/v/release/epiral/pinix?color=blue)](https://github.com/epiral/pinix/releases/tag/v2.0.0)

[English](README.md) | [中文](README.zh-CN.md)

## 架构总览

```text
CLI / MCP / Portal
        |
        | Connect-RPC
        v
+---------------------------+
| Hub                       |
| 按 clip name 路由         |
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
 pinixd 本地 runtime
      |
      | NDJSON IPC v2
      v
   Bun/TS Clips
```

## 快速开始

1. 运行本地 Clip 时需要先安装 `bun`，然后从 [v2.0.0 release](https://github.com/epiral/pinix/releases/tag/v2.0.0) 下载 `pinixd` 和 `pinix`，或从源码编译。
2. 以全包模式启动 Pinix：`./pinixd --port 9000`
3. 安装第一个 Clip：`./pinix --server http://127.0.0.1:9000 add clip-todo`
4. 调用命令：`./pinix --server http://127.0.0.1:9000 todo add -- --title "写文档"`
5. 打开 `http://127.0.0.1:9000`，再用 `./pinix mcp --all --server http://127.0.0.1:9000` 暴露 MCP

## pinixd 三种模式

```bash
./pinixd --port 9000
./pinixd --port 9000 --hub-only
./pinixd --port 9001 --hub http://hub:9000
```

- 默认模式：Hub + Runtime + Portal。
- `--hub-only`：纯 Hub + Portal，不管理本地 Clip 进程。
- `--hub`：纯 Runtime。作为 Provider 连接外部 Hub，并把自己管理的 Clips 注册上去。

## 文档

- [核心架构](docs/architecture.md)
- [快速开始](docs/getting-started.md)
- [Clip 开发](docs/clip-development.md)
- [Edge Clip 开发](docs/edge-clip-development.md)
- [协议](docs/protocol.md)
- [MCP](docs/mcp.md)
- [部署](docs/deployment.md)
- [决策索引](docs/decisions.md)

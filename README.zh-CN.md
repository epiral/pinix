<p align="center">
  <img src="https://pinix.ai/logo.svg" alt="Pinix" width="120" />
</p>

<h1 align="center">Pinix</h1>

<p align="center">
  <b>Agent Harness</b> — 一套开放能力层，让任何 Agent 发现、调用和组合工具。
</p>

<p align="center">
  <a href="https://pinixai.com">官网</a> &middot;
  <a href="docs/getting-started.md">文档</a> &middot;
  <a href="https://discord.gg/pinix">Discord</a>
</p>

<!-- <p align="center">
  <img src="demo.gif" width="680" alt="Pinix demo" />
</p> -->

---

Pinix 把设备、应用、网页、服务和工作流封装成 **Clip** — 独立的能力单元，任何 Agent 都可以调用。部分 Clip 还自带 Web UI，人也可以直接使用。

```
人 / Agent / CLI
  → Hub
  → Clip
  → 设备 / 应用 / 网页 / SaaS / 工作流
```

## 安装

```bash
# 全套安装（pinix + Node.js + Bun + bb-browser）
curl -fsSL dl.pinixai.com/install.sh | sh

# 只装 CLI
curl -fsSL dl.pinixai.com/install.sh | sh -s -- --cli

# Docker（一个 token，全部搞定）
docker run -d --shm-size=2g pinixai/pinix <your-hub-token>
```

## 快速开始

```bash
pinix start                                     # 启动 Pinix daemon
pinix login                                     # 浏览器授权登录
# 或：pinix login --token pnx_...              # token 登录
pinix invoke todo add -- --title "Hello Pinix"  # 使用 Clip
pinix invoke todo list                          # 查看结果
open http://localhost:9000                       # 打开 Console
```

`pinix login` 后，你的本地 Clips 通过 pinix.ai Cloud Hub 在任何设备上都可以访问。

## 启动后你得到什么

`pinix start` 后，你拥有一个完整的本地能力栈：

```
pinix start
  ├── Hub        路由 invoke 到正确的 Clip
  ├── Runtime    管理本地 Bun/TS Clip 进程
  ├── Registry   从 pinix.ai 安装 Clips
  └── Console    localhost:9000 管理界面
```

BB-Browser 在全套安装中已自动包含。它会自动启动 Chrome，把 30+ 网站注册为 Edge Clips：

```
$ pinix hub list

  github     RUNNING   trending, repo, search
  twitter    RUNNING   search, timeline
  reddit     RUNNING   search, hot
  browser    RUNNING   open, snapshot, click, fill, eval
  todo       RUNNING   add, delete, list
```

你浏览器里的登录态、Cookie、SSO 和内网环境直接可用。实时画面在 `http://localhost:6111`。

## 功能

- [x] **Clip Runtime** — 本地安装、运行和管理 Bun/TS Clips
- [x] **Hub 路由** — 按 alias 路由 invoke，按 package name 自动发现
- [x] **Edge Clips** — BB-Browser、clipboard、screen 等自动注册
- [x] **Clip Web** — Clip 自带 Web UI，通过 `{alias}.hub.pinix.ai` 访问
- [x] **Console** — 管理 Clips、查看状态、iframe 嵌入 Clip Web
- [x] **Registry** — 搜索、安装、发布 Clips 到 pinix.ai
- [x] **pinix.ai Cloud Hub** — 跨网络路由，从任何设备访问 Clips
- [x] **`pinix login`** — device code flow，登录后自动重启 daemon 连接 Cloud Hub
- [x] **单一 binary** — 一个 `pinix`：`start`、`stop`、`status`、`login`、`invoke`
- [x] **自动连接** — 读取已保存 token，自动连 Cloud Hub
- [x] **`install.sh`** — 一键安装（Node.js + Bun + bb-browser，或 `--cli` 最小安装）
- [x] **Docker** — `docker run pinixai/pinix <token>` — 零配置一键启动
- [x] **Console Agent** — 浏览器原生多 Agent Chat，可直接调用 Clips
- [ ] **Pinix Desktop** — 本地 Shell + OS Edge Clips（开发中）

## 为什么是 Clip

Agent 需要工具。现有方案各有缺口：

| | MCP | CLI (bash) | Clips |
|---|---|---|---|
| 发现方式 | 所有工具一次注入 | 开放环境 | 按需发现 |
| Token 成本 | 高（无关工具占上下文） | 低 | 低（只加载活跃 Clip） |
| 注意力 | 被无关工具分散 | — | 聚焦当前任务 |
| 边界 | 取决于工具实现 | 无边界 | 严格 — Clip 定义边界 |
| 模型要求 | 中 | 高（要理解 shell） | 低（确定性命令） |

Clip 做两件事：
1. **降低模型要求。** 复杂操作封装成确定性命令。
2. **圈定能力边界。** Agent 只能用 Clip 暴露的能力。

## BB-Browser

[BB-Browser](https://github.com/epiral/bb-browser) 把任何网站变成 Agent 可调用的 Clip。它运行在真实 Chrome 里，复用你的登录态、Cookie、SSO 和内网环境。

```bash
pinix invoke github trending
# → GitHub trending 结构化 JSON

pinix invoke twitter search --query "AI agent"
# → Twitter 搜索结果
```

内置 103 个命令覆盖 36 个平台。Agent 还能自主生成新的 site adapter。

## 连接 pinix.ai

```bash
pinix login
```

打开浏览器进行设备确认。登录后：

- 本地 Clips 从任何设备可见
- `pinixd` 下次启动自动连接（不需要 `--hub` 参数）
- Cloud Registry 发现和安装 Clips
- 模型代理 — 不用自己管 API key

## 三类 Clip

| 类型 | 方式 | 例子 |
|---|---|---|
| **SDK Clip** | Bun/TS 应用，Runtime 管理，`pinix hub add` | `@pinix/todo`, `@pinix/review`, `@pinix/memex` |
| **Edge Clip** | 原生进程，Provider 协议，自动注册 | BB-Browser sites, clipboard, screen, notification |
| **API Clip** | 封装外部 API | GitHub, 12306, 高德地图 |

## 从源码编译

```bash
# 需要 Go 1.22+ 和 Bun
git clone https://github.com/epiral/pinix.git
cd pinix
go build -o pinix ./cmd/pinix
./pinix start
```

## 文档

| 文档 | 说明 |
|---|---|
| [快速开始](docs/getting-started.md) | 安装、第一个 Clip、第一次 invoke |
| [核心架构](docs/architecture.md) | Hub、Runtime、Provider、Clip 模型 |
| [Clip 开发](docs/clip-development.md) | 开发自己的 Clip |
| [Edge Clip 开发](docs/edge-clip-development.md) | 开发硬件/浏览器 Edge Clip |
| [协议](docs/protocol.md) | Connect-RPC、ProviderStream、IPC |
| [部署](docs/deployment.md) | 生产部署指南 |

## License

[MIT](LICENSE)

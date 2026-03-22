# AGENTS.md — Pinix V2

Pinix V2 是一个 **Clip runtime + routing Hub**：

- `pinixd` 提供 Hub、Portal，以及可选的本地 Runtime。
- `pinix` 提供 CLI 调用和 MCP gateway。
- 本地 Bun/TS Clip 通过 **IPC v2 NDJSON** 接入 `pinixd`。
- 远端 Runtime / Edge Clip 通过 **Connect-RPC `ProviderStream`** 接入 Hub。

本文件以当前 V2 代码实现为准；不要再用旧双服务、旧 YAML 配置或旧 `serve` 命令叙事描述当前仓库。

## Source Of Truth

- V2 活跃代码以这些路径为准：
  - `cmd/pinixd/`
  - `cmd/pinix/`
  - `internal/daemon/`
  - `internal/client/`
  - `internal/ipc/`
  - `proto/pinix/v2/`
  - `web/`
- 架构与设计决策优先看：
  - `docs/architecture.md`
  - `docs/protocol.md`
  - `docs/deployment.md`
  - `docs/getting-started.md`
  - `docs/clip-development.md`
  - `docs/edge-clip-development.md`
  - `docs/mcp.md`
  - `docs/decisions.md`
- `issue #9` 是 V2 架构与术语重定义的最终讨论记录。
- 如果文档、issue 和代码实现冲突，**以当前代码和 `proto/pinix/v2/hub.proto` 为准**。
- `docs/installation*.md`、`docs/deploy-macos.md`、`docs/clip-architecture.md`、`docs/rfc/*`、`docs/boxlite-integration.md`、`docs/clip-ui-*`、`docs/clip-bridge-compat.md`、`docs/clip-debug-server.md` 含历史方案、外围工具链或 v1 语境，阅读时要注意版本背景，不要拿来覆盖 V2 主线结论。

## 项目简介

Pinix V2 的核心是：**Hub 只看到 Clip**。

- 用户、CLI、Portal、MCP 都通过 Hub 调用 Clip。
- 本地 Clip 由 `pinixd` 作为 Runtime 管理，底层用 Bun 进程运行。
- 远端设备能力或外部进程以 Provider 的形式连接 Hub，并注册自己提供的 Clip。
- Clip 之间的调用统一走 Hub 路由，不区分“本地 Clip”或“Edge Clip”。

## 核心概念

### Hub

- 实时路由表 + 消息转发器。
- 对外暴露唯一服务：`HubService`。
- 只关心 Clip 名称、Clip 挂在哪个 Provider 上、请求该转发到哪里。
- **出现在列表里 = 当前可用**；V2 不再维护单独的 offline 状态枚举。

### Clip

- Pinix 中唯一的功能单元。
- 当前模型用 `name + package + version` 描述一个 Clip。
- Hub 路由时按 `name` 查找；`package` 和 `version` 用于展示、依赖和实例化语义。

### Edge Clip

- 开发者术语，不是 Hub 内部类型。
- 指“自己实现 Provider 协议、自己直连 Hub”的 Clip。
- 典型例子：`bb-browserd`、手机/桌面设备能力、IoT 进程。

### Provider

- 协议层概念，不是用户侧核心产品概念。
- 负责建立 `ProviderStream`、注册一个或多个 Clip、发送心跳、接收 invoke / manage / get-clip-web 请求并回传结果。
- Hub 不区分 Provider 背后是 Runtime 还是 Edge Clip。

### Runtime

- 一种特殊的 Provider，由 `pinixd` 实现。
- 除了接入 Hub，还负责：
  - 安装 Clip（npm / GitHub / 本地路径）
  - 管理 Bun 进程生命周期
  - 通过 IPC v2 与本地 Clip 进程通信
  - 读取和代理 Clip `web/` 目录

### 术语约束

- 当前主线只使用：**Hub / Clip / Edge Clip / Provider / Runtime**。
- Hub 内部不要再引入额外类型分支来区分 Clip 的“来源类型”。

## 目录结构

### V2 主线

```text
pinix/
├── cmd/
│   ├── pinix/             # 当前 CLI + MCP gateway
│   ├── pinixd/            # 当前 daemon entrypoint（全包 / hub-only / --hub）
│   └── edge-test/         # Provider 协议联调用的小工具
├── internal/
│   ├── daemon/            # V2 核心：HubService、Provider 管理、Runtime、Portal HTTP、Clip Web 代理
│   ├── client/            # V2 Connect-RPC client（CLI / Provider 复用）
│   └── ipc/               # pinixd <-> 本地 Clip 的 IPC v2 NDJSON 定义
├── proto/pinix/v2/        # 当前外部协议定义
├── gen/                   # 生成代码（Go / Swift / TypeScript）
├── web/                   # 内嵌 Portal 静态资源
└── docs/                  # V2 架构、协议、开发与部署文档
```

### 非主线路径（除非专门清理，否则不要扩展）

- 根目录 `main.go`
- 根级 `cmd/*.go`（`cmd/pinix/`、`cmd/pinixd/` 之外的旧命令入口）
- `internal/hub/`
- `internal/worker/`
- `internal/edge/`
- `internal/clip/`
- `internal/auth/`
- `internal/config/`
- `internal/sandbox/`
- `internal/scheduler/`
- `proto/pinix/v1/`

这些路径主要是历史遗留或迁移期目录。**新的 V2 功能不要继续落在这些目录里。**

## Build & Dev

```bash
go build -o pinixd ./cmd/pinixd
go build -o pinix ./cmd/pinix
go test ./...
buf generate
```

本地联调常用命令：

```bash
./pinixd --port 9000
./pinixd --port 9000 --hub-only
./pinixd --port 9001 --hub http://127.0.0.1:9000

./pinix --server http://127.0.0.1:9000 list
./pinix --server http://127.0.0.1:9000 add clip-todo
./pinix --server http://127.0.0.1:9000 todo add -- --title "Ship Pinix V2"
./pinix mcp --all --server http://127.0.0.1:9000
```

- 运行本地 Runtime 时需要 `bun`；`pinixd` 会从 `PATH` 或 `~/.bun/bin/bun` 自动探测。
- `pinixd --hub-only` 不需要 Bun，因为它不托管本地 Clip 进程。
- 修改 `proto/pinix/v2/hub.proto` 后必须执行：

```bash
buf generate
go test ./...
```

## `pinixd` 三种运行模式

### 1. 全包模式（默认）

```bash
./pinixd --port 9000
```

- 一个进程同时提供 **Hub + Runtime + Portal**。
- 可直接 `pinix add` 安装并管理本地 Clip。
- 适合单机开发、demo、端到端联调。

### 2. `--hub-only`

```bash
./pinixd --port 9000 --hub-only
```

- 只提供 **Hub + Portal**，不带本地 Runtime。
- 能接受远端 Runtime Provider / Edge Clip 连接。
- 自己不运行本地 Bun Clip，因此不依赖 Bun。
- 在这个模式下执行 `pinix add/remove` 时，请求必须落到可管理的 Runtime Provider（`accepts_manage=true`）。

### 3. `--hub`

```bash
./pinixd --port 9001 --hub http://127.0.0.1:9000
```

- 作为 **纯 Runtime Provider** 连接外部 Hub。
- 不启动内嵌 Hub；本地托管的 Clip 会通过 `ProviderStream` 注册到远端 Hub。
- 会接收远端下发的 invoke / manage / get-clip-web 请求，并路由到本地 Runtime。
- `--hub` 和 `--hub-only` 互斥。
- 该模式下的 `--port` 主要用于 provider identity 和本地 Clip 进程环境，不表示本地再起一个 Hub。

## 通信协议

### 外部协议：Connect-RPC `HubService`

当前外部协议定义在 `proto/pinix/v2/hub.proto`。

- Provider ↔ Hub：`ProviderStream`
- Client / CLI / Portal ↔ Hub：
  - `ListClips`
  - `ListProviders`
  - `GetManifest`
  - `GetClipWeb`
  - `Invoke`
  - `InvokeStream`
  - `AddClip`
  - `RemoveClip`

协议职责：

- `ProviderStream` 负责 register / heartbeat / invoke / manage / clip-web 双向消息。
- `Invoke` 支持普通调用和 server-stream。
- `InvokeStream` 支持双向流。
- `GetClipWeb` 是 Clip Web 静态文件读取 RPC；Portal 负责把 HTTP Range / ETag / 路径语义适配到 RPC。

鉴权约定：

- `super_token` 当前主要用于保护 `AddClip` / `RemoveClip`，由 `Authorization: Bearer <token>` 传入。
- `clip_token` 走 `InvokeRequest.clip_token` / `InvokeCommand.clip_token`。
- 本地 Clip 由 `pinixd` Runtime 校验本地 `clip_token`；provider-backed Clip 由对应 Provider / Clip 自行校验。

### 内部协议：IPC v2 NDJSON

当前内部协议定义在 `internal/ipc/ipc.go`。

```text
pinixd runtime
  <-> stdin/stdout NDJSON
  <-> Bun/TS Clip process
```

固定 7 种消息类型：

- `register`
- `registered`
- `invoke`
- `result`
- `error`
- `chunk`
- `done`

要点：

- Clip 进程启动后先发 `register`，由自己上报 manifest。
- `pinixd` 回 `registered` 确认握手完成。
- 本地 Clip 调其他 Clip 时，依然通过 IPC `invoke` 把请求交给 `pinixd`，再由 Hub 统一路由。

### Clip Web

- Portal 下的 Clip Web UI 统一挂在 `/clips/<clip-name>/`。
- Clip Web 前端资源和 API 调用必须使用**相对路径**，不要写成前导 `/` 的绝对路径。
- Portal 会把 `POST /clips/<clip-name>/api/<command>` 代理到对应 Clip command。

## 配置文件与本地状态

V2 当前默认使用 **`~/.pinix/`**。

```text
~/.pinix/
├── config.json
├── config.json.lock
└── clips/
    ├── <clip-name>/
    └── .staging-<clip-name>/
```

- 默认配置路径：`~/.pinix/config.json`
- 可通过 `pinixd --config <path>` 覆盖。
- Runtime 安装的 Clip 默认放在 `~/.pinix/clips/`。

当前配置结构（简化示例）：

```json
{
  "super_token": "dev-secret",
  "clips": {
    "todo": {
      "name": "todo",
      "source": "clip-todo",
      "path": "/Users/you/.pinix/clips/todo",
      "token": "todo-secret",
      "manifest": {
        "name": "todo",
        "package": "clip-todo",
        "version": "0.2.0",
        "domain": "productivity",
        "has_web": true,
        "dependencies": ["browser"]
      }
    }
  }
}
```

- `super_token`：可选。设置后，`AddClip` / `RemoveClip` 需要带 Bearer token。
- `clips`：按实例名存储的本地 Runtime Clip 配置。
- 每个 Clip 条目记录 `source`、`path`、可选 `token`，以及缓存的 manifest。

## 开发规范

### 文件头

新增或修改 `.go` 文件时，顶部保持这一格式：

```go
// Role:    一句话描述文件职责
// Depends: 直接依赖（逗号分隔）
// Exports: 对外暴露的类型/接口
```

### 错误处理

- 优先返回带上下文的错误：`fmt.Errorf("load clip manifest: %w", err)`。
- daemon / provider 内部错误优先带稳定 code：
  - `invalid_argument`
  - `permission_denied`
  - `not_found`
  - `already_exists`
  - `unimplemented`
  - `internal`
- Connect-RPC 边界统一返回 `connect.NewError(...)`，并映射到明确的 `connect.Code`。
- 面向协议的错误信息要稳定、短、可路由，不要把底层临时日志格式直接暴露给调用方。

### 生成代码

- 当前主线 proto 是 `proto/pinix/v2/hub.proto`。
- 修改 proto 后必须执行：

```bash
buf generate
go test ./...
```

- `gen/` 下文件是生成产物，不手动编辑。

### 变更边界

- 新的 V2 功能优先落在：`cmd/pinixd`、`cmd/pinix`、`internal/daemon`、`internal/client`、`internal/ipc`、`proto/pinix/v2`、`web`。
- 除非明确在做遗留清理，不要把新功能继续写到 v1 遗留目录。
- 不要在注释、文档、PR 描述里把当前架构写回旧双服务、旧 YAML 配置或旧命令体系。

## 开发流程规范

所有改动必须走：

```text
Issue -> Branch -> PR -> Review -> Merge
```

### 分支

- 一律从最新 `main` 创建分支。
- 分支命名规范：
  - `feat/xxx`
  - `fix/xxx`
  - `refactor/xxx`

### Commit

- commit message 使用这一格式：
  - `feat: 简短描述`
  - `fix: 简短描述`
  - `refactor: 简短描述`
  - `docs: 简短描述`
  - `chore: 简短描述`

### PR

- PR 描述必须关联 issue，例如：`Closes #xx`
- 开 PR 前至少跑与改动相关的验证；常规 Go 改动默认跑 `go test ./...`
- review 意见在同一个分支继续修，不要直接把未经 review 的改动推到 `main`

### Merge

- PR 合并统一使用 **squash merge**。
- **不要直接 push 到 `main`**。

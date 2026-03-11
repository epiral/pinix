# Pinix

去中心化的 Clip 运行时平台 — 本地沙箱执行与边缘设备接入，统一于 Clip 接口。

[![Release](https://img.shields.io/github/v/release/epiral/pinix?color=blue)](https://github.com/epiral/pinix/releases)

[English](README.md) | [中文](README.zh-CN.md)

## 快速开始

### 安装

```bash
# 从 GitHub Releases 下载（macOS arm64）
mkdir -p ~/bin ~/.boxlite/rootfs
curl -L https://github.com/epiral/pinix/releases/latest/download/pinix-v0.2.0-darwin-arm64.tar.gz | tar xz -C ~/bin
curl -L https://github.com/epiral/pinix/releases/latest/download/boxlite-v0.2.0-darwin-arm64.tar.gz | tar xz -C ~/bin
curl -L https://github.com/epiral/pinix/releases/latest/download/rootfs-v0.2.0.ext4.gz | gunzip > ~/.boxlite/rootfs/rootfs.ext4

# 或者使用 Clip Dock Desktop（内置全部依赖）
```

### 启动

```bash
boxlite serve --port 8100 &
pinix serve --addr :9875 --boxlite-rest http://localhost:8100
```

### 安装 Clip

```bash
pinix clip install agent.clip --server http://localhost:9875 --token <super-token>
```

---

## 核心概念

### 什么是 Clip？

Clip 是实现了三个操作的任何东西：

| 操作 | 说明 |
|------|------|
| **Invoke** | 执行命名命令（stdin → stdout/stderr/exit code） |
| **ReadFile** | 读取 `web/` 或 `data/` 下的文件 |
| **GetInfo** | 返回元信息（名称、描述、命令列表、是否有 Web UI） |

这就是 **Clip 接口** — 通用合约。命令如何执行是实现细节。

### 两种 Clip

**本地 Clip** — 命令在 Server 上的 BoxLite micro-VM 中执行：

```
my-clip/
  commands/    → 面向 Agent 的可执行脚本（stdin/stdout/exit code）
  web/         → 面向人的 UI
  data/        → 持久化存储（可变）
  clip.yaml    → 元数据
```

**Edge Clip** — 命令在连接的设备上原生执行（iPhone、树莓派、ESP32）：

```
设备通过 EdgeService 连接 → 注册能力 → Server 将请求路由到设备
```

两者实现同一个 Clip 接口。调用方无法区分。

### 三层模型

```
Workspace（开发）  →  Package（.clip 分发）  →  Instance（运行时）
   Git 仓库            ZIP 压缩包              部署在 Server 上
   源代码              编译产物                data/ 可变
   go.mod, src/       commands/, bin/         seed/ → data/
```

### Pinix Server

托管 Clip Instance 并路由请求。内部架构：

```
                    ┌──────────────────────────┐
                    │       ClipService        │  ← 公共 API（对所有 Clip 类型统一）
                    │       AdminService       │
                    │       EdgeService        │  ← 设备接入端点
                    └────────────┬─────────────┘
                                 │
                    ┌────────────▼─────────────┐
                    │          Hub             │
                    │     Clip Registry        │  token → clipID → Clip 接口
                    └──────┬──────────┬────────┘
                           │          │
              ┌────────────▼──┐  ┌────▼────────────┐
              │    Worker     │  │      Edge        │
              │  LocalClip    │  │    EdgeClip      │
              │  BoxLite VM   │  │  设备双向流       │
              └───────────────┘  └──────────────────┘
```

- **Hub**：通过 Clip 接口路由请求，不知道执行细节。
- **Worker**：通过 BoxLite 沙箱 + 本地文件系统实现 `Clip`。
- **Edge**：通过到连接设备的双向流实现 `Clip`。

多个 Pinix Server 之间**完全去中心化**，互不依赖。

### Clip Dock

聚合层。客户端应用（Desktop / iOS），通过 Bookmark 连接任意 Pinix Server 上的 Clip：

```json
{
  "name": "todo",
  "server_url": "http://100.66.47.40:9875",
  "token": "clip-token-for-todo"
}
```

---

## Edge Clip

设备通过 EdgeService 协议将原生能力暴露为 Clip。

### 工作流程

```
1. 设备连接：  EdgeService.Connect（双向流，super token 认证）
2. 设备注册：  发送 EdgeManifest { name, commands[], description }
3. Server 分配：clip_id + clip token，在 Hub 注册 EdgeClip
4. 待命：      设备等待转发的请求

--- 有人调用 edge clip ---

5. 调用方 → ClipService.Invoke with clip token
6. Hub 路由到 EdgeClip → 作为 EdgeRequest 转发给设备
7. 设备原生执行 → 流式返回 EdgeResponse
8. Hub 转发给调用方 — 透明的，和本地 Clip 完全一样
```

### 支持的设备

| 设备 | 运行时 | 传输方式 |
|------|--------|----------|
| iPhone / Mac | pinix-edge-swift（嵌入 Clip Dock） | gRPC stream |
| 树莓派 | pinix-edge-go（独立 daemon） | gRPC stream |
| ESP32 | pinix-edge-lite | WebSocket（计划中） |

### 示例：连接 edge 设备

```bash
# 启动一个暴露 "echo" 和 "hello" 命令的测试 edge 设备
go run cmd/edge-test/main.go \
  --server http://localhost:9875 \
  --token <super-token> \
  --name my-device

# 从任何客户端，像调用其他 clip 一样调用 edge clip：
pinix invoke hello --server http://localhost:9875 --token <edge-clip-token>
# → "hello from edge device"
```

---

## Clip Registry

Clip Registry 是**一种 Clip**，不是 Pinix Server 的附属功能。它帮助 Clip Dock 发现任意 Server 上有哪些 Clip 可用。

将发现能力做成 Clip：**去中心化**、**可演进**、**跨 Server**、**一致性**（发现能力本身也是 Clip）。

---

## 协议设计

### ClipService — 最小内核

| RPC | 说明 | 性质 |
|-----|------|------|
| **Invoke** | 执行命令 | 业务操作的唯一入口 |
| **ReadFile** | 读取 `web/` 和 `data/` 下的文件 | 只读，支持 ETag 缓存和流式传输 |
| **GetInfo** | 返回 Clip 元信息 | 名称、描述、命令列表、是否有 Web UI |

### AdminService — 管理面

需要 Super Token。

| RPC | 说明 |
|-----|------|
| **CreateClip** / **DeleteClip** | 注册/注销 Clip |
| **ListClips** | 列出所有 Clip 及元信息 |
| **GenerateToken** / **ListTokens** / **RevokeToken** | Token 管理 |

### EdgeService — 设备接入

需要 Super Token。

| RPC | 说明 |
|-----|------|
| **Connect** | 双向流，用于设备注册和请求转发 |

Edge 协议使用**信封消息**和请求关联：

```
Server → 设备：EdgeRequest  { request_id, body: Invoke/ReadFile/GetInfo/Cancel }
设备 → Server：EdgeResponse { request_id, body: InvokeChunk/ReadFileChunk/GetInfo/Error/Complete }
```

## Token 模型

| 类型 | 来源 | 绑定对象 | 权限范围 |
|------|------|---------|---------|
| **Super Token** | 配置文件（静态） | 无 | AdminService + EdgeService 全部权限 |
| **Clip Token** | GenerateToken API（动态） | 特定 Clip | 仅该 Clip 的 ClipService |

**安全设计：**
- Super Token 不通过 API 生成，断绝升权攻击
- Clip Token 泄露仅影响单个 Clip，无法触及 AdminService 或 EdgeService
- Edge Clip 的 Token 是临时的——设备断开时自动清理

### 请求路由

```
Clip Dock / Agent / 任何调用方
  │  Bearer: <clip-token>
  ▼
Pinix Server (Hub)
  │  Clip Registry
  │  ├─ clip-token-A → clip_id: todo    → LocalClip（BoxLite VM）
  │  ├─ clip-token-B → clip_id: agent   → LocalClip（BoxLite VM）
  │  ├─ clip-token-C → clip_id: iphone  → EdgeClip（设备流）
  │  └─ super-token  → clip_id: (none)  → 全部权限
  ▼
路由到 Clip 实现 — 调用方不知道也不需要知道是哪种类型
```

---

## 架构总览

```
  ┌──────────────────────┐        ┌──────────────────────┐
  │  Pinix Server A      │        │  Pinix Server B      │
  │  (私有, home)         │        │  (公共)               │
  │                      │        │                      │
  │  Clip: agent (本地)   │        │  Clip: news (本地)    │
  │  Clip: todo  (本地)   │        │  Clip: sandbox       │
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
│  [agent]       → Server A (本地 clip)              │
│  [todo]        → Server A (本地 clip)              │
│  [iphone]      → Server A (edge clip, 同一台手机)   │
│  [news]        → Server B                         │
└───────────────────────────────────────────────────┘
```

## 内部架构

```
internal/
  clip/        Clip 接口 + Registry（核心合约）
  hub/         ClipService/AdminService/EdgeService handler，路由
  worker/      LocalClip 实现（BoxLite 沙箱 + 文件系统）
  edge/        EdgeClip 实现（设备 session 管理）
  sandbox/     BoxLite 后端（RestBackend, BoxLiteBackend）
  auth/        Token 验证拦截器
  config/      YAML 持久化（clips, tokens）
  scheduler/   基于 cron 的命令调度
```

---

## CLI 参考

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

## 发布

下载地址：[GitHub Releases](https://github.com/epiral/pinix/releases)

v0.2.0 包含：
- `pinix` — Server 二进制（已签名）
- `boxlite` — 沙箱运行时（boxlite, boxlite-shim, boxlite-guest, libkrunfw）
- `rootfs` — micro-VM 的 Linux 根文件系统

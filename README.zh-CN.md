# Pinix

去中心化的 Clip 运行时平台 — 通过可插拔沙箱后端实现隔离执行。

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

### 三层模型

```
Workspace（开发）  →  Package（.clip 分发）  →  Instance（运行时）
   Git 仓库            ZIP 压缩包              部署在 Server 上
   源代码              编译产物                data/ 可变
   go.mod, src/       commands/, bin/         seed/ → data/
```

### Clip Package

源码层。一个 Clip Package 是一份代码模板，定义了 Clip 的能力：

```
my-clip/
  commands/    → 面向 Agent 的可执行脚本（Unix 范式：stdin/stdout/exit code）
  web/         → 面向人的 UI
  data/        → 持久化存储（运行时可变）
  seed/        → 初始化模板（install 时复制到 data/）
  bin/         → 编译产物
  clip.yaml    → Clip 元数据（名称、版本、定时任务）
```

Clip Package 存在于 Git 仓库中，不依赖任何运行环境。

### Clip Instance

运行时层。一个 Clip Instance 是某个 Clip Package 在某个 Pinix Server 上的运行实例。

每个 Clip Instance 具备：

- **URL**：所在 Pinix Server 的地址（host:port）
- **Token**：Clip Dock 用于访问该 Instance 的凭证（由 Server 的 Token 路由表管理）
- **隔离的 workdir**：包含 commands/、web/、data/，互不干扰

同一个 Clip Package 可以在不同 Server 上部署多个 Instance，彼此完全独立。

### Pinix Server

运行时管理层。托管 Clip Instance 的节点服务，负责：

- Clip Instance 的注册与生命周期管理
- Token 路由：Client 发来的请求，根据 Token 路由到对应的 Clip Instance
- 鉴权：验证 Token 合法性，限制访问范围
- 沙箱执行：所有命令在 BoxLite micro-VM 中隔离运行

多个 Pinix Server 之间**完全去中心化**，互不依赖，无需中央注册服务。

### Clip Dock

聚合层。可以使用来自**任意 Pinix Server** 上的 Clip Instance 的客户端应用（Desktop / iOS）。

Clip Dock 通过 **Bookmark** 管理对各 Clip Instance 的访问：

```json
{
  "name": "todo",
  "server_url": "http://100.66.47.40:9875",
  "token": "clip-token-for-todo"
}
```

Clip Dock 可以同时持有来自多个 Server 的 Bookmark，跨 Server 自由聚合能力。

---

## Clip Registry

Clip Registry 是**一种 Clip**，不是 Pinix Server 的附属功能。它帮助 Clip Dock 发现任意 Pinix Server 上有哪些 Clip Instance 可用。

将发现能力做成 Clip 而非 Server 功能：**去中心化**、**可演进**、**跨 Server**、**一致性**（发现能力本身也是 Clip，用同样的方式访问）。

---

## 协议设计

### ClipService — 最小内核

| RPC | 说明 | 性质 |
|-----|------|------|
| **Invoke** | 执行 `commands/` 下的可执行脚本 | 业务操作的唯一入口，包含所有 mutation |
| **ReadFile** | 读取 `web/` 和 `data/` 下的文件 | 只读，无副作用，支持 ETag 缓存和流式传输 |
| **GetInfo** | 返回 Clip 元信息 | 名称、描述、可用 commands 列表、是否有 web UI |

**Invoke 收敛所有业务操作**：无论是查询数据、上传文件还是修改状态，所有业务逻辑都通过 Invoke 执行具体的 command 脚本完成。没有 WriteFile — 写操作是业务特定的，不应被抽象为通用原语。

### AdminService — 管理面

Server 运维者使用，需要 Super Token。

| RPC | 说明 |
|-----|------|
| **CreateClip** / **DeleteClip** | 注册/注销 Clip |
| **ListClips** | 列出所有 Clip（Server 侧主动扫描 workdir 获取元信息） |
| **GenerateToken** / **ListTokens** / **RevokeToken** | Token 管理 |

## Token 模型

| Token 类型 | 来源 | 绑定对象 | 权限范围 |
|-----------|------|---------|---------|
| **Super Token** | 配置文件（静态） | 无 | AdminService 全部接口 |
| **Clip Token** | GenerateToken API（动态） | 特定 Clip Instance | 仅该 Clip 的 ClipService |

**安全设计：**
- Super Token 不通过 API 生成，断绝「通过 API 升权」的攻击路径
- GenerateToken 的 `clip_id` 为必填，不存在生成 Super Token 的可能
- Clip Token 泄露仅影响单个 Clip，无法触及 AdminService

### 请求路由

```
Clip Dock
  │  Bearer: <clip-token>
  ▼
Pinix Server
  │  查 Token 路由表
  │  ├─ clip-token-A → clip_id: todo    → workdir: /path/to/todo
  │  ├─ clip-token-B → clip_id: agent   → workdir: /path/to/agent
  │  └─ super-token  → clip_id: (none)  → 全部权限
  ▼
路由到对应 Clip 的 workdir，在 BoxLite VM 中执行 commands/ 或读取文件
```

---

## 架构总览

```
  ┌──────────────────────┐        ┌──────────────────────┐
  │  Pinix Server A      │        │  Pinix Server B      │
  │  (私有, home)         │        │  (公共)               │
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
        │  [registry]    → Server A (发现更多)       │
        └──────────────────────────────────────────┘
```

## 沙箱架构

所有命令在隔离的 BoxLite micro-VM 中执行。不会 fallback 到宿主机执行。

```
ClipService.Invoke
  → sandbox.Manager
    → Backend 接口
      ├── RestBackend     (BoxLite REST API — 生产环境)
      ├── BoxLiteBackend  (BoxLite CLI — 开发环境)
      └── FakeBackend     (测试)
```

| 组件 | 文件 | 职责 |
|------|------|------|
| Backend 接口 | `internal/sandbox/backend.go` | 可插拔约定 |
| RestBackend | `internal/sandbox/rest.go` | HTTP 调用 BoxLite serve |
| BoxLiteBackend | `internal/sandbox/boxlite.go` | 直接 CLI 执行 |
| Manager | `internal/sandbox/manager.go` | 委托层 |

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

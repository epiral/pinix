# Pinix

A decentralized runtime platform for Clips.

---

## Core Concepts

### Clip Package

源码层。一个 Clip Package 是一份代码模板，定义了 Clip 的能力：

```
my-clip/
  web/         → 面向人的 UI
  commands/    → 面向 Agent 的可执行脚本（Unix 范式：stdin/stdout/exit code）
  data/        → 持久化存储
  config.yaml  → Clip 自身的配置
```

Clip Package 存在于 Git 仓库中，不依赖任何运行环境。

### Clip Instance

运行时层。一个 Clip Instance 是某个 Clip Package 在某个 Pinix Server 上的运行实例。

每个 Clip Instance 具备：

- **URL**：所在 Pinix Server 的地址（host:port）
- **Token**：Clip Client 用于访问该 Instance 的凭证（由 Server 的 Token 路由表管理）
- **隔离的 workdir**：包含 web/、commands/、data/，互不干扰

同一个 Clip Package 可以在不同 Server 上部署多个 Instance，彼此完全独立。

### Pinix Server

运行时管理层。托管 Clip Instance 的节点服务，负责：

- Clip Instance 的注册与生命周期管理
- Token 路由：Client 发来的请求，根据 Token 路由到对应的 Clip Instance
- 鉴权：验证 Token 合法性，限制访问范围

支持两种形态：

- **私有 Server**：个人部署，完全自控
- **公共 Server**：社区或团队共享，对外开放部分 Clip

多个 Pinix Server 之间**完全去中心化**，互不依赖，无需中央注册服务。一般一个人只需要一个 Pinix Server，所有 Clip Instance 都跑在上面。

### Clip Client

聚合层。可以使用来自**任意 Pinix Server** 上的 Clip Instance 的客户端应用（Desktop / iOS）。

**核心原则：Clip Client 与 Pinix Server 无绑定关系，它只与 Clip Instance 绑定。**

Client 通过 **Bookmark** 管理对各 Clip Instance 的访问。每个 Bookmark 包含：

```json
{
  "name": "todo",
  "server_url": "http://100.66.47.40:9875",
  "token": "clip-token-for-todo"
}
```

Client 可以同时持有来自多个 Server 的 Bookmark，跨 Server 自由聚合能力。

---

## Clip Registry

Clip Registry 是**一种 Clip**，不是 Pinix Server 的附属功能。

它的职责是：帮助 Clip Client 发现**任意 Pinix Server** 上有哪些 Clip Instance 可用。

### 工作方式

Registry Clip 不绑定它自身所在的 Pinix Server。用户在使用 Registry 时，配置一个**目标 Pinix Server** 的连接信息（host、port、token），Registry 连接过去获取该 Server 的 Clip 目录。

```
Clip Client
  │
  │  Clip Token
  ▼
Pinix Server X
  └─ Clip Instance: registry
       │
       │  用户配置: {target: Server A, host, port, admin_token}
       │  用户配置: {target: Server B, host, port, admin_token}
       │
       ├──→ Server A.ListClips() → 返回 Clip 列表
       └──→ Server B.ListClips() → 返回 Clip 列表
```

### 发现流程

1. 拿到某个 Pinix Server 的地址和管理 Token
2. 在 Registry Clip 中添加该 Server 的连接配置
3. Registry 连接目标 Server，列出所有可用 Clip
4. 选择感兴趣的 Clip → 为其生成 Clip Token → 在 Client 创建 Bookmark
5. Client 现在可以直接使用该 Clip

### 为什么不是 Server 内置功能？

将发现能力做成 Clip 而非 Server 功能：

- **去中心化**：不依赖任何 Server 的特殊接口，Registry 可以跑在任何地方
- **可演进**：Registry 的 UI 和逻辑独立迭代，不影响 Server 内核
- **跨 Server**：一个 Registry 可以同时连接多个 Server，提供统一的发现视图
- **一致性**：发现能力本身也是 Clip，用同样的方式访问

---

## Token Model

Pinix Server 通过 Token 管理访问权限。

| Token 类型 | 绑定对象 | 权限范围 | 持有者 |
|-----------|---------|---------|--------|
| **Super Token** | 无 | Server 全部管理接口 + 全部 Clip | Server 运维者 |
| **Clip Token** | 特定 Clip Instance | 仅该 Clip 的 Invoke / ReadFile | Clip Client |

### 请求路由

```
Clip Client
  │  Bearer: <clip-token>
  ▼
Pinix Server
  │  查 Token 路由表
  │  ├─ clip-token-A → clip_id: todo    → workdir: /path/to/todo
  │  ├─ clip-token-B → clip_id: voice   → workdir: /path/to/voice
  │  └─ super-token  → clip_id: (none)  → 全部权限
  ▼
路由到对应 Clip 的 workdir，执行 commands/ 或读取文件
```

Client 不直接连 Clip Instance，所有请求经过 Pinix Server，由 Server 根据 Token 路由。

---

## Architecture Overview

```
  ┌──────────────────────┐        ┌──────────────────────┐
  │  Pinix Server A      │        │  Pinix Server B      │
  │  (私有, home)         │        │  (公共)               │
  │                      │        │                      │
  │  Clip: todo          │        │  Clip: news-feed     │
  │  Clip: voice-inbox   │        │  Clip: gpt-proxy     │
  │  Clip: registry      │        │                      │
  └──────────┬───────────┘        └──────────┬───────────┘
             │                               │
             │    Bookmark = URL + Token     │
             │                               │
        ┌────┴───────────────────────────────┴────┐
        │             Clip Client                  │
        │          (Desktop / iOS)                 │
        │                                          │
        │  [todo]         → Server A               │
        │  [voice-inbox]  → Server A               │
        │  [news-feed]    → Server B               │
        │  [registry]     → Server A (发现更多)     │
        └──────────────────────────────────────────┘
```

---

## Roadmap

- [x] Connect-RPC 服务骨架（AdminService + ClipService）
- [x] Token 鉴权（Super / Clip Token 路由）
- [x] ETag 协商缓存（ReadFile）
- [ ] Clip Registry Clip 实现（[#5](https://github.com/epiral/pinix/issues/5)）
- [ ] Clip Client 通过 Registry 发现并添加 Bookmark
- [ ] 容器化执行层隔离（boxlite）

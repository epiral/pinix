# Core Concepts

Pinix 的核心概念，面向开源版开发者和使用者。

完整的产品愿景见 [pinix.ai docs](https://pinix.ai/docs)。

---

## What is Pinix?

Pinix 让 AI Agent 能使用任何设备的任何能力。

开源版包含：

- **pinixd** — Hub + Runtime，一个 binary 搞定
- **CLI** — `pinix add`, `pinix create`, `pinix publish` 等命令
- **Portal** — 内嵌在 pinixd 的 Web UI
- **Proto** — Connect-RPC 协议定义（ProviderStream + RuntimeStream + HubService）

---

## What is a Clip?

Clip 是 Pinix 的核心抽象，一个三合一的功能单元：

| 层 | 是什么 |
|---|---|
| **知识** | 面向 Agent 的方法论——什么场景用、怎么用、和谁配合（manifest: patterns, entities, schema） |
| **能力** | 封装的执行逻辑——浏览器操作、设备 API、业务流程 |
| **资产** | 可组合、可复用——clip 依赖 clip，形成能力网络 |

Clip 有两种类型：

- **Edge Clip**：设备驱动，直接绑定硬件/OS API（截屏、GPS、Docker 等）。自己实现 Provider 协议。
- **SDK Clip（Runtime Clip）**：应用/服务，由 Runtime 管理生命周期。通过 `pinix add` 安装。

---

## What is Hub?

Hub 是路由中心，所有 Clip 在 Hub 上被发现和调用。

- Hub 是唯一路由器——Agent 通过 Hub 调用 Clip，不直接连 Clip
- Hub 管理 alias 分配——每个 Clip 有唯一别名（自动生成 `{package}-{4hex}` 或用户指定 `--alias`）
- `pinixd --port 9000` 启动本地 Hub + Runtime

---

## What is a Provider?

Provider 是连接协议——通过 ProviderStream 把 Clip 注册到 Hub。

- Edge Clip 自己是 Provider（自实现 ProviderStream）
- Runtime 作为 Provider 管理 SDK Clip（RuntimeStream 管理生命周期，ProviderStream 注册到 Hub）

Provider 和 Runtime 是分开的协议：
- **ProviderStream**：注册 clip、转发 invoke、heartbeat
- **RuntimeStream**：install、start、stop clip

---

## Package Naming

Clip 使用 `@scope/name` 格式标识：

```
@scope/name          -> Registry 包（社区发布）
github/user/repo     -> GitHub 包
local/name           -> 本地包（仅当前 Hub）
```

安装示例：

```bash
pinix add @cp/todo                        # Registry
pinix add github/user/my-clip             # GitHub
pinix add local/dev-tool --path ./my-clip # 本地
```

---

## Dependencies and Bindings

Clip 通过 slot 机制声明依赖：

```jsonc
// clip.json
{
  "name": "@cp/twitter",
  "dependencies": {
    "browser": "@pinixai/browser"   // slot "browser" -> package constraint
  }
}
```

Bindings 存在 Clip 本地的 `bindings.json`，映射 slot 到 Hub 上的实际 alias。CLI/Portal 辅助绑定，用户决定。

---

## Protocol Stack

| 层 | 协议 | 用途 |
|---|---|---|
| External | Connect-RPC | Provider <-> Hub, Runtime <-> Hub, Client <-> Hub |
| Internal | IPC v2 NDJSON (stdin/stdout) | Clip <-> Runtime |
| Registry | REST API | search, publish, auth |

---

## Architecture Overview

```
Agent (Claude Code / Cursor / agent-clip / ...)
  |
  v
Hub (pinixd)  <--- Connect-RPC ---> Provider (Edge Clip / Runtime)
  |                                      |
  v                                      v
Clip A (alias: todo-a3f2)           Clip B (alias: browser-b1c9)
```

- Agent 通过 MCP / CLI / HTTP 连接 Hub
- Hub 路由 invoke 到目标 Clip
- Clip 执行并返回结果

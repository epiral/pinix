# Pinix V2 Decisions Index

> 这里汇总 issue #9 讨论里沉淀下来的关键决策；每行只记录结论和来源链接。

| 日期 | 结论 | 链接 |
|---|---|---|
| 2026-03-21 | Hub 只看到 Clip，不区分本地 Clip、Edge Clip、Capability。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | Capability / Capability Provider 概念收敛为 Clip / Edge Clip，Hub 代码里不再保留 Capability 类型分支。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | 在 Pinix 里"出现在列表里 = 可用"，Hub 不维护 offline 状态枚举。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | Provider 保留为连接协议层概念，而不是用户侧核心概念。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | Runtime 是一种 Provider，额外负责安装、进程管理和生命周期。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | `pinixd` 负责 standalone（Hub + Runtime + Portal），并保留纯 Hub 与外连 Hub 两种运行模式。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | `pinix` 定位为 CLI + MCP gateway，和 Hub / Runtime 解耦。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | Edge Clip 是开发者术语，指“自己实现 Provider 协议直连 Hub 的 Clip”。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | 设备抽象推荐分层：`iphone` / `macbook` 做 Edge Clip，`camera` 做普通 Clip 抽象层。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | Hub 的可用性模型改为实时路由表：连接在，Clip 在；连接断，Clip 消失。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | Clip 标识采用 `name + package + version`，并允许同 package 多实例。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | `dependencies` 存 package name，不存实例名。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | Hub 不做依赖满足判断；依赖信息主要供 Portal 展示和 Clip 运行时自判。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | Hub Token 与 Clip Token 分层：Hub 保护网络边界，Clip Token 透传给 Provider/Clip 自己校验。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | 调用模型拆成三类：`Invoke` 普通调用、`Invoke` 流式输出、`InvokeStream` 双向流。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | CLI 与 MCP 是并行 transport，都服务 agent 生态。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | Hub MCP 固定为 3 个 tool：`list`、`info`、`invoke`。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | 单 Clip MCP 暴露该 Clip 的每个 command 为独立 tool。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | `bunx clip-xxx --mcp` 与 `pinix mcp` 区分开：前者独立运行，后者经过 Hub 路由依赖。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | Provider SDK 不再做成专门 npm 包；各语言直接使用官方 Connect 库 + `hub.proto`。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-21 | `clip-twitter` 取代 `xhs-search` 成为更贴近日常使用的示例 Clip。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-22 | 外部通信统一切到 Connect-RPC，替换旧的 WebSocket + JSON。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-22 | 内部进程通信保留 `stdin/stdout` NDJSON，并扩展成 IPC v2。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-22 | IPC v2 固定为 7 种消息：`register`、`registered`、`invoke`、`result`、`error`、`chunk`、`done`。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-22 | Clip 改为自注册 manifest，不再由 `pinixd` 主动探测。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-22 | Portal 前端直接调用 Connect-RPC 端点，不再依赖旧 `/api/*` HTTP API。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-22 | `@pinixai/browser` 收敛成对 `invoke("browser", ...)` 的轻量封装。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-22 | Clip Web UI 必须使用相对静态资源路径和相对 `api/<command>` 路径。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-22 | Portal 在 `has_web=true` 时显示 “Open Web UI”，并通过 `/clips/<name>/api/<command>` 做本地代理。 | https://github.com/epiral/pinix/issues/9 |
| 2026-03-22 | 核心 binary 收敛为两个：`pinixd`、`pinix`。 | https://github.com/epiral/pinix/issues/25 |
| 2026-03-22 | `pinixd --hub-only` 提供纯 Hub + Portal；默认模式继续提供 Hub + Runtime + Portal。 | https://github.com/epiral/pinix/issues/25 |
| 2026-03-22 | `pinixd --hub <url>` 作为 Runtime Provider 连接外部 Hub，并接收 invoke / manage / heartbeat。 | https://github.com/epiral/pinix/issues/26 |

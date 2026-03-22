# Pinix Deployment

> Pinix V2 当前可运行的部署形态，以及三种 `pinixd` 模式各自适合什么场景。

## 1. `pinixd` 全包模式（已实现）

这是最完整、最直接的部署方式。

```text
pinixd
├── Hub
├── local Runtime
├── Portal
└── local Clips (Bun/TS)
```

启动：

```bash
./pinixd --port 9000
```

特点：

- 一个进程包含 Hub、Runtime、Portal。
- 可以直接 `pinix add clip-todo`。
- 本地 Clip Web UI 可通过 `/clips/<name>/` 打开。

适用：

- 本机开发。
- 单用户桌面环境。
- demo、验证、端到端联调。

## 2. `pinixd --hub-only` 中心 Hub（已实现）

`pinixd --hub-only` 是纯 Hub 模式，自带 Portal，但不带本地 Runtime。

```text
pinixd --hub-only
├── Hub
└── Portal
```

启动：

```bash
./pinixd --port 9000 --hub-only
```

特点：

- 能接受 Runtime Provider / Edge Clip 连接。
- 能列出、路由远端注册上来的 Clip。
- 自己不能直接运行本地 Bun Clip。
- 不依赖 bun，因为没有本地 Runtime。

注意：

- 在 `pinixd --hub-only` 上执行 `pinix add` 时，必须显式指定一个 `accepts_manage=true` 的 Runtime Provider，或让请求落到唯一可管理的远端 Runtime。
- 当前仓库发布的 `bb-browserd` 是 `accepts_manage=false`，因此它只能提供能力，不能接收 `AddClip` / `RemoveClip`。

## 3. `pinixd --hub` 连接外部 Hub（已实现）

`pinixd --hub <url>` 运行成纯 Runtime Provider：不启动内嵌 Hub，而是通过 `ProviderStream` 连到外部 Hub。

```text
pinixd --hub-only (cloud or LAN hub)
   ^
   | ProviderStream
   |
pinixd --hub http://hub:9000
   └── local Runtime
       └── local Clips (Bun/TS)
```

启动：

```bash
./pinixd --port 9000 --hub-only
./pinixd --port 9001 --hub http://127.0.0.1:9000
```

特点：

- Runtime 会把自己管理的所有 Clip 注册到外部 Hub。
- 收到 `InvokeCommand` 后，会路由到本地 Clip 进程并返回 `InvokeResult`。
- 收到 `ManageCommand`（add/remove）后，会在本地执行并同步 Clip 变化。
- 保持心跳和断线重连。
- `--port` 在这个模式下不暴露本地 Hub；主要用于 provider identity 和本地 Runtime 进程环境。

## 4. Edge Clip 连接到 Hub（已实现）

当前已经可运行的分布式形态，是 **中心 Hub + 外部 Provider**。

```text
pinixd --hub-only
   ^
   | ProviderStream
   |
bb-browserd / device app / custom provider
```

示例：把 `bb-browserd` 接到中心 Hub

```bash
./pinixd --port 9000 --hub-only
```

另一个终端：

```bash
bun run /Users/cp/Developer/epiral/repos/bb-browser/bin/bb-browserd.ts   --pinix http://127.0.0.1:9000   --name browser
```

再检查：

```bash
./pinix --server http://127.0.0.1:9000 list
```

如果看到 `browser`，说明这个 Edge Clip 已经注册成功。

## 5. 选择建议

| 场景 | 推荐 |
|---|---|
| 单机开发 / 本地 demo | `pinixd` |
| 中心路由 + Portal | `pinixd --hub-only` |
| 远端 Runtime 接入中心 Hub | `pinixd --hub http://hub:9000` |
| 浏览器 / 手机 / 桌面原生能力接入 | Provider / Edge Clip |

## 6. 相关讨论

- 架构讨论：https://github.com/epiral/pinix/issues/9
- binary 合并：https://github.com/epiral/pinix/issues/25
- 外部 Hub 连接模式：https://github.com/epiral/pinix/issues/26

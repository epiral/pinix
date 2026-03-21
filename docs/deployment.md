# Pinix Deployment

> Pinix V2 当前可运行的部署形态，以及哪些拓扑已经落地、哪些仍停留在架构讨论层。

## 1. `pinixd` 独立模式（已实现）

这是当前最完整、最直接的部署方式。

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

## 2. `pinix-hub` 中心 Hub（已实现）

`pinix-hub` 是纯 Hub binary，自带 Portal，但不带本地 Runtime。

```text
pinix-hub
├── Hub
└── Portal
```

启动：

```bash
./pinix-hub --port 9000
```

特点：

- 能接受 Provider / Edge Clip 连接。
- 能列出、路由远端注册上来的 Clip。
- 自己不能直接运行本地 Bun Clip。

注意：

- 在 `pinix-hub` 上执行 `pinix add` 时，必须显式指定一个 `accepts_manage=true` 的 Runtime Provider。
- 当前仓库发布的 `bb-browserd` 是 `accepts_manage=false`，因此它只能提供能力，不能接收 `AddClip` / `RemoveClip`。

## 3. Edge Clip 连接到 Hub（已实现）

当前已经可运行的分布式形态，是 **中心 Hub + 外部 Provider**。

```text
pinix-hub
   ^
   | ProviderStream
   |
bb-browserd / device app / custom provider
```

示例：把 `bb-browserd` 接到 `pinix-hub`

```bash
./pinix-hub --port 9000
```

另一个终端：

```bash
bun run /Users/cp/Developer/epiral/repos/bb-browser/bin/bb-browserd.ts \
  --pinix http://127.0.0.1:9000 \
  --name browser
```

再检查：

```bash
./pinix --server http://127.0.0.1:9000 list
```

如果看到 `browser`，说明这个 Edge Clip 已经注册成功。

## 4. `pinix-hub + 多个 pinixd` 这个拓扑的现状

issue #9 / #13 的架构讨论里，多次出现下面这个目标拓扑：

```text
pinix-hub (cloud)
   ^          ^
   |          |
 pinixd A   pinixd B
```

但需要明确：

- **当前 v2.0.0 release 的 `cmd/pinixd` 没有 `--hub` 参数。**
- 也就是说，“把多个 `pinixd` 作为外部 Runtime Provider 连到中心 Hub”这件事，目前还不是这个 release 里可直接执行的命令行能力。

因此，今天能落地的事实是：

- `pinixd` 适合单机全包。
- `pinix-hub` 适合作为中心 Hub。
- 外部 Provider / Edge Clip 可以连到 `pinix-hub`。

但“多个 `pinixd` 直连 `pinix-hub`”目前仍属于架构讨论中的目标拓扑，不应写成已实现能力。

## 5. 选择建议

| 场景 | 推荐 |
|---|---|
| 单机开发 / 本地 demo | `pinixd` |
| 中心路由 + 外部 Provider | `pinix-hub` |
| 浏览器 / 手机 / 桌面原生能力接入 | Provider / Edge Clip |

## 6. 相关讨论

- 架构讨论：https://github.com/epiral/pinix/issues/9
- 协议设计：https://github.com/epiral/pinix/issues/13

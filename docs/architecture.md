# Pinix V2 Architecture

> Pinix V2 的核心角色、路由边界和推荐抽象方式。

Pinix V2 把一切统一到 **Clip** 这个概念上：Hub 只看见 Clip，Runtime 和 Edge Clip 都只是把 Clip 接进 Hub 的不同方式。

## 全景图

```text
pinix CLI / pinix mcp / Portal
              |
              | Connect-RPC (HubService)
              v
      +--------------------------+
      | Hub                      |
      | real-time routing table  |
      | only knows Clips         |
      +-----------+--------------+
                  |
          ProviderStream
      +-----------+------------+
      |                        |
      v                        v
+-------------+        +------------------+
| Runtime     |        | Edge Clip        |
| pinixd      |        | custom Provider  |
|             |        | e.g. bb-browserd |
| local Clips |        | browser / device |
+------+------+        +------------------+
       |
       | NDJSON IPC v2
       v
  Bun/TS Clip processes
```

## 角色定义

### Hub

- 路由中心。
- 对外暴露 `HubService`。
- 维护当前在线的 Clip 路由表。
- 不区分“本地 Clip”“Edge Clip”“设备 Clip”。

### Clip

- Pinix 中唯一的功能单元。
- 由 `name`、`package`、`version` 标识。
- 可以运行在 `pinixd` 管理的本地 Bun 进程里，也可以由外部 Provider 直接实现。

### Edge Clip

- 开发者术语。
- 指“自己实现 Provider 协议、自己连 Hub”的 Clip。
- 常见场景是浏览器、手机、桌面原生能力、IoT 设备。

### Provider

- 连接协议层概念。
- 负责建立 `ProviderStream`、注册 Clip、发送心跳、接收 invoke、回传结果。
- 一个 Provider 可以注册一个或多个 Clip。

### Runtime (`pinixd`)

- 一种特殊的 Provider。
- 除了接入 Hub，它还负责安装 Clip、启动和回收 Bun 进程、通过 IPC v2 与 Clip 进程通信，以及读取 `web/` 目录并在 Portal 下暴露本地 Clip Web UI。

## Hub 只看到 Clip

从 Hub 的角度看，下面三条连接没有类型差别：

```text
connection A -> clips: [todo, twitter]
connection B -> clips: [browser]
connection C -> clips: [iphone]
```

Hub 的代码路径只关心三件事：

1. 这个 Clip 叫什么。
2. 它挂在哪个 Provider 连接上。
3. 要把调用转发到哪里。

这也是 V2 的核心约束：**Hub 代码里不应该有 “if edge” 这种分支。**

## 调用链路

### 用户调用本地 Clip

```text
pinix twitter search
  -> Hub
  -> pinixd local runtime
  -> twitter Bun process
  -> result
```

### Clip 调用另一个 Clip

```text
twitter Clip
  -> IPC invoke("browser", "evaluate", ...)
  -> pinixd
  -> Hub
  -> bb-browserd Provider
  -> browser Clip
  -> result
```

本地 Clip 不直接知道对方是在本机进程里，还是在远端 Provider 里；它只按 clip name 调用。

## 设备抽象模式

这是 issue #9 里明确下来的推荐建模方式：**设备是 Edge Clip，抽象能力是普通 Clip。**

```text
xhs-search (Clip)
  depends on: camera

camera (Clip, abstraction layer)
  depends on: iphone, macbook
  takePhoto() -> choose an available device

iPhone (Edge Clip)
  takePhoto, getSteps, getLocation

macbook (Edge Clip)
  capturePhoto, clipboard, screenCapture
```

要点：

- `iphone` / `macbook` 是原始设备驱动，适合做 Edge Clip。
- `camera` 是统一抽象层，适合做普通 Clip。
- 上层业务 Clip 依赖抽象层，而不是绑定具体设备。

`camera` 只是推荐模式，不是本仓库内置的系统 Clip。

## 包标识与来源

Pinix V2 使用三种来源前缀标识 Clip 包：

| 前缀 | 格式 | 示例 | 安装方式 |
|---|---|---|---|
| `@scope/` | `@scope/name[@version]` | `@pinixai/todo@0.3.0` | 从 Pinix Registry 下载 tarball |
| `github/` | `github/user/repo[#branch]` | `github/epiral/clip-todo` | `git clone` + `bun install` |
| `local/` | `local/name --path /abs/path` | `local/my-clip --path /home/me/clip` | 复制目录 + `bun install` |

### Registry 来源解析

当 CLI 收到 `@scope/name` 格式的来源时：

1. CLI 从 flag `--registry` > 环境变量 `PINIX_REGISTRY` > `~/.pinix/client.json` 中的 `registry` 字段 > 默认值 `https://api.pinix.ai` 解析 Registry URL。
2. 构造内部规范形式 `registry:<url>#@scope/name[@version]`，发送给 Hub。
3. Runtime 从 Registry 下载 tarball、校验 shasum、解压、`bun install`。

### 别名（Alias）

Hub 中每个 Clip 都有全局唯一的 **alias**。alias 是 Clip 在 Hub 路由表中的唯一标识。

- 用户可通过 `--alias` 指定。
- 不指定时，Hub 自动生成 `{base}-{4hex}` 格式（如 `todo-a3f1`）。
  - `@scope/name` → base 取 `name` 部分。
  - `github/user/repo` → base 取 `repo`。
  - `local/name` → base 取 `name`。

### `clip.json`

Clip 项目根目录可选放一个 `clip.json` 文件，用于 Registry 发布时补充元数据：

```json
{
  "name": "@pinixai/todo",
  "version": "0.3.2",
  "description": "Simple todo list Clip",
  "runtime": "bun",
  "main": "index.ts"
}
```

发布到 Registry 时，`name` 必须是 `@scope/name` 格式。

## Cloud Hub 连接

Runtime 可以连接到远端 Cloud Hub，通过 `ProviderStream` + `RuntimeStream` 双向流注册本地 Clip。

### 连接配置

```bash
# 命令行参数
pinixd --hub https://hub.pinix.ai --hub-token <jwt>

# 环境变量
PINIX_HUB=https://hub.pinix.ai
PINIX_HUB_TOKEN=<jwt>

# 持久化配置
pinix config set hub https://hub.pinix.ai
pinix config set hub-token <jwt>
```

优先级：命令行参数 > 环境变量 > `~/.pinix/client.json` 配置文件。

### Registry 配置

```bash
pinix config set registry https://api.pinix.ai
```

## 当前代码里的运行模式

```text
pinixd                     = Hub + Runtime + Portal
pinixd --hub-only          = Hub + Portal
pinixd --hub http://...    = Runtime Provider (ProviderStream + RuntimeStream)
pinix                      = CLI + MCP gateway
```

当前实现里：

- `pinixd` 默认模式适合单机全包。Hub + Runtime 在同一进程中，Runtime 通过 Connect-RPC 连本地 Hub。
- `pinixd --hub-only` 适合中心 Hub。不依赖 Bun。
- `pinixd --hub <url>` 连接外部 Hub，同时建立两条流：`ProviderStream`（注册 Clip、转发 invoke）和 `RuntimeStream`（接受 install/remove 命令）。
- `bb-browserd` 是 Provider / Edge Clip 的参考实现。

## 设计记录

- 架构讨论与最终定稿：https://github.com/epiral/pinix/issues/9
- binary 合并：https://github.com/epiral/pinix/issues/25
- 外部 Hub 连接模式：https://github.com/epiral/pinix/issues/26

# Pinix MCP

> `pinix mcp` 把 Pinix Hub 或单个 Clip 暴露成 MCP server，供 Claude Desktop、Cursor 等 agent 工具使用。

MCP 设计讨论见：

- https://github.com/epiral/pinix/issues/16

## 1. 两种模式

```text
pinix mcp --all     -> Hub MCP
pinix mcp <clip>    -> Single Clip MCP
```

## 2. Hub MCP：`pinix mcp --all`

Hub MCP 不把所有 Clip command 一次性摊平成几十个 tool，而是固定暴露 3 个元层 tool：

| Tool | 作用 |
|---|---|
| `list()` | 列出当前 Clip |
| `info(clip)` | 查看某个 Clip 的 manifest 和 command schema |
| `invoke(clip, command, input)` | 调用任意 Clip command |

启动：

```bash
./pinix mcp --all --server http://127.0.0.1:9000
```

适用场景：

- Hub 上 Clip 很多。
- 想让 agent 自己先发现再调用。
- 不希望一次暴露几十个 MCP tools。

## 3. 单 Clip MCP：`pinix mcp <clip>`

单 Clip MCP 会把某个 Clip 的每个 command 直接注册成 MCP tool。

例如：

```bash
./pinix mcp twitter --server http://127.0.0.1:9000
```

如果 `twitter` Clip 有这些 commands：

- `search`
- `getProfile`
- `getTweet`

那么 MCP server 就会直接暴露同名 tools。

适用场景：

- 目标明确，只想操作一个 Clip。
- 想让 agent 直接看到具体 command，而不是先 `list -> info -> invoke`。

## 4. 与独立 Clip MCP 的区别

`@pinixai/core` 也支持独立 Clip 自己起 MCP：

```bash
bunx clip-todo --mcp
```

与 `pinix mcp` 的区别：

| 方式 | 是否经过 Hub | 是否能路由依赖 | 适用 |
|---|---|---|---|
| `bunx clip-xxx --mcp` | 否 | 否 | 无依赖、单 Clip |
| `pinix mcp --all` | 是 | 是 | 多 Clip、动态发现 |
| `pinix mcp <clip>` | 是 | 是 | 指定一个 Clip |

只要 Clip 依赖 `browser` 之类的其他 Clip，就应该优先使用 `pinix mcp`。

## 5. Claude Desktop 配置

Hub MCP：

```json
{
  "mcpServers": {
    "pinix": {
      "command": "/absolute/path/to/pinix",
      "args": ["mcp", "--all", "--server", "http://127.0.0.1:9000"]
    }
  }
}
```

单 Clip MCP：

```json
{
  "mcpServers": {
    "twitter": {
      "command": "/absolute/path/to/pinix",
      "args": ["mcp", "twitter", "--server", "http://127.0.0.1:9000"]
    }
  }
}
```

## 6. Cursor 配置

Cursor 的 MCP 设置界面里填同样的命令即可：

```text
command: /absolute/path/to/pinix
args:    mcp --all --server http://127.0.0.1:9000
```

如果你只想给 Cursor 暴露一个 Clip：

```text
command: /absolute/path/to/pinix
args:    mcp twitter --server http://127.0.0.1:9000
```

## 7. 认证说明

`pinix mcp` 当前只有一个 `--auth-token` 全局参数：

```bash
./pinix mcp --all --server http://127.0.0.1:9000 --auth-token dev-secret
```

这个参数会用于 Hub 访问。
如果目标 Clip 开启了 clip token 保护，当前 `pinix mcp` 实现也会把同一个 `--auth-token` 值透传给 `InvokeRequest.clip_token`。
也就是说，当前 CLI 还没有把 Hub token 和 clip token 分开的 MCP 专用参数。

## 8. 如何选择

如果你不确定用哪种：

- 先用 `pinix mcp --all`。
- 确认某个 Clip 是高频入口后，再给它单独开 `pinix mcp <clip>`。

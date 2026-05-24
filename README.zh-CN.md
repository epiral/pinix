# Pinix

**Agent Harness** — 一套开放能力层，让 Agent 发现、调用、组合和约束工具。

Pinix 把设备、应用、网页、服务和工作流封装成 **Clip**。每个 Clip 是独立的能力单元，可以被任何 Agent、CLI 调用。部分 Clip 还自带 Web UI，让人直接使用。

```text
人 / Agent / CLI
  -> Hub
  -> Clips
  -> 设备 / 应用 / 网页 / SaaS / 工作流
```

## 快速开始

```bash
# 1. 启动 Pinix
pinixd

# 2. 连接 pinix.ai
pinix login

# 3. 安装一个 Clip
pinix hub add @pinix/todo

# 4. 使用
pinix invoke todo add --title "Hello Pinix"
pinix invoke todo list

# 5. 打开 Console
open http://localhost:9000
```

`pinix login` 后，你的本地 Clips 通过 pinix.ai Cloud Hub 在任何设备上都可以访问。

## 启动 Pinix 后发生了什么

```text
pinixd
  ├── Hub        — 路由 invoke 调用到正确的 Clip
  ├── Runtime    — 管理本地 Bun/TS Clip 进程
  ├── Registry   — 从 pinix.ai Registry 安装 Clips
  └── Console    — localhost:9000 的 Web 管理界面
```

如果安装了 BB-Browser，它会自动启动一个 headless Chrome，并把浏览器能力注册为 Edge Clips：

```text
pinix hub list

  github     RUNNING   trending, repo, search
  twitter    RUNNING   search, timeline
  reddit     RUNNING   search, hot
  todo       RUNNING   add, delete, list
```

## Clips

Clip 是 Pinix 的核心抽象。它提供两个关键价值：

1. **降低模型要求。** 复杂操作被封装成确定性命令，Agent 只需要调用，不需要理解实现细节。
2. **圈定能力边界。** Agent 只能通过 Clip 获取能力，权限、审计和测试都是内置的。

三类 Clip：

| 类型 | 工作方式 | 例子 |
|---|---|---|
| **SDK Clip** | Bun/TS 应用，由 Runtime 管理，通过 `pinix hub add` 安装 | todo, review, memex |
| **Edge Clip** | 原生进程，自实现 Provider 协议，自动注册到 Hub | BB-Browser sites, clipboard, screen |
| **API Clip** | 封装外部 API 为 Clip | GitHub CLI, 12306, 高德地图 |

## BB-Browser

BB-Browser 把任何网站变成 Agent 可调用的 Clip。它运行在用户真实的 Chrome 里，复用已有的登录态、Cookie、SSO 和内网环境。

```bash
pinix invoke github trending
# → GitHub trending 的结构化 JSON，使用你已登录的账号

pinix invoke twitter search --query "AI agent"
# → Twitter 搜索结果，通过你的账号
```

实时画面在 `http://localhost:6111`，可以看到 Chrome 正在做什么。

## 连接 pinix.ai

```bash
pinix login
```

这会把你的本地 Pinix 连接到 pinix.ai Cloud Hub。你的 Clips 在任何设备上都可以访问。pinix.ai 还提供：

- **Cloud Hub** — 跨网络 Clip 路由
- **Cloud Registry** — 发现和安装 Clips
- **模型代理** — 使用 AI 模型不需要自己管 API key
- **Console** — 从网页管理 Clips

## 从源码编译

```bash
# 需要 Go 1.22+ 和 Bun
go build -o pinixd ./cmd/pinixd
go build -o pinix ./cmd/pinix

pinixd
```

## 文档

- [核心架构](docs/architecture.md)
- [快速开始](docs/getting-started.md)
- [Clip 开发](docs/clip-development.md)
- [Edge Clip 开发](docs/edge-clip-development.md)
- [协议](docs/protocol.md)
- [部署](docs/deployment.md)

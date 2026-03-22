# Pinix V2 Getting Started

> 从零装好 Pinix、启动 `pinixd`、安装第一个 Clip、打开 Portal，并接入 MCP。

## 前置要求

- Go 1.25+：从源码构建时需要。
- Bun：运行本地 Clip 时需要；`pinixd` 会在 `PATH` 或 `~/.bun/bin/bun` 里查找它。

## 1. 安装 binary

### 方式 A：下载 release

Pinix V2.0.0 已发布到：

- https://github.com/epiral/pinix/releases/tag/v2.0.0

示例：macOS arm64

```bash
curl -L https://github.com/epiral/pinix/releases/download/v2.0.0/pinixd-darwin-arm64.gz -o pinixd.gz
gunzip pinixd.gz
chmod +x pinixd

curl -L https://github.com/epiral/pinix/releases/download/v2.0.0/pinix-darwin-arm64.gz -o pinix.gz
gunzip pinix.gz
chmod +x pinix
```

示例：Linux amd64

```bash
curl -L https://github.com/epiral/pinix/releases/download/v2.0.0/pinixd-linux-amd64.gz -o pinixd.gz
gunzip pinixd.gz
chmod +x pinixd

curl -L https://github.com/epiral/pinix/releases/download/v2.0.0/pinix-linux-amd64.gz -o pinix.gz
gunzip pinix.gz
chmod +x pinix
```

### 方式 B：从源码编译

```bash
git clone https://github.com/epiral/pinix.git
cd pinix

go build -o pinixd ./cmd/pinixd
go build -o pinix ./cmd/pinix
```

## 2. 启动 `pinixd`

### 最小启动：全包模式

```bash
./pinixd --port 9000
```

### 带 super token

```bash
./pinixd --port 9000 --super-token dev-secret
```

### 其他两种模式

```bash
./pinixd --port 9000 --hub-only
./pinixd --port 9001 --hub http://127.0.0.1:9000
```

常用参数：

```text
--port         默认模式 / --hub-only 时是 Portal 和 HubService 的 HTTP 端口；--hub 模式下主要用于 provider identity
--config       配置文件路径，默认 ~/.pinix/config.json
--bun          Bun binary 路径；不传则自动探测
--super-token  保护 add/remove
--hub-only     纯 Hub + Portal
--hub          连接外部 Hub，运行成纯 Runtime Provider
```

启动后，Pinix 会把本地状态写到：

```text
~/.pinix/config.json
~/.pinix/clips/
```

## 3. 安装第一个 Clip

安装 npm 上的 `clip-todo`：

```bash
./pinix --server http://127.0.0.1:9000 add clip-todo
```

如果你启动 `pinixd` 时设置了 `--super-token`，则需要携带：

```bash
./pinix --server http://127.0.0.1:9000 --auth-token dev-secret add clip-todo
```

也可以安装本地 Clip 或 GitHub Clip：

```bash
./pinix --server http://127.0.0.1:9000 add /absolute/path/to/my-clip
./pinix --server http://127.0.0.1:9000 add github:owner/repo
```

如果你当前连的是 `pinixd --hub-only`，则需要把 add 请求发到一个可管理的 Runtime Provider。

## 4. 用 CLI 调用它

列出当前 Clip：

```bash
./pinix --server http://127.0.0.1:9000 list
```

调用命令：

```bash
./pinix --server http://127.0.0.1:9000 todo list
./pinix --server http://127.0.0.1:9000 todo add -- --title "Ship Pinix V2 docs"
./pinix --server http://127.0.0.1:9000 todo delete -- --id 1
```

注意：Clip command 的参数要放在 `--` 之后。`pinix` 会把 `--title`、`--id` 解析成 JSON input。

## 5. 打开 Portal

浏览器打开：

```text
http://127.0.0.1:9000
```

Portal 当前能力：

- 列出 Clip。
- 查看 manifest。
- 直接 invoke command。
- 对 `has_web=true` 的 Clip 显示 “Open Web UI” 按钮。
- 本地 `pinixd` Clip 的 Web UI 可以直接打开；provider-backed Clip 的 Web 代理当前返回 `unimplemented`。

## 6. 配置 MCP

### Hub MCP

把整个 Hub 暴露为 3 个固定 tool：

```bash
./pinix mcp --all --server http://127.0.0.1:9000
```

Claude Desktop 配置示例：

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

### 单 Clip MCP

只暴露某个 Clip 的 commands：

```bash
./pinix mcp todo --server http://127.0.0.1:9000
```

Cursor 的 MCP 配置界面使用同样的 `command` 和 `args` 即可。

## 7. 下一步

- 架构总览：`docs/architecture.md`
- Clip 开发：`docs/clip-development.md`
- Edge Clip 开发：`docs/edge-clip-development.md`
- MCP 细节：`docs/mcp.md`

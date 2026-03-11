# AGENTS.md — Pinix

Pinix 是一个去中心化的 Clip runtime platform，使用 Go + Connect-RPC 提供管理面与 Clip 访问面。服务端负责 Clip 注册、Token 管理、沙箱执行、文件读取与调度；CLI 负责本地安装/升级/卸载 Clip 包，以及远程调用服务。

## 项目概览

- **AdminService**：管理 Clip 与 Token（Create/List/Delete Clip，Generate/List/Revoke Token）
- **ClipService**：面向 Clip 访问（Invoke、ReadFile、GetInfo）
- **运行时沙箱**：通过可插拔 `Backend` 接口接入 BoxLite CLI 或 BoxLite REST
- **调度器**：进程内 cron，按各 Clip 的 `clip.yaml` 中 `schedules` 自动执行命令
- **配置存储**：`~/.config/pinix/config.yaml`，保存 `super_token`、`clips`、`tokens`

## Build & Dev

```bash
go build -o pinix .
go test ./...
pinix serve --addr :8080 --boxlite-rest http://localhost:8100
buf generate
```

Proto 修改后按需执行：

```bash
buf generate
go test ./...
```

## 架构

### 服务与鉴权

- `AdminService` 只接受 `super_token`
- `ClipService` 接受 `super_token` 或 clip-scoped token
- Clip token 绑定单个 `clip_id`，不能访问 `AdminService`
- Token 从 `Authorization: Bearer <token>` 读取，由 Connect interceptor 统一校验

### 沙箱与调度

- `internal/sandbox.Backend` 是统一抽象，`sandbox.Manager` 负责执行
- `BoxLiteBackend`：调用本地 `boxlite` CLI
- `RestBackend`：通过 HTTP 连接外部 BoxLite REST 服务
- 服务启动时读取各 Clip 的 `clip.yaml`，将 `schedules` 注册到进程内 cron
- 调度任务通过同一套 sandbox 执行，并避免同一 clip/command 重叠运行

## 配置文件

路径：`~/.config/pinix/config.yaml`

```yaml
super_token: "<admin-token>"
clips:
  - id: "clip_xxx"
    name: "demo"
    workdir: "/path/to/clip"
    mounts: []
    image: "debian:12-slim"
tokens:
  - id: "tok_xxx"
    clip_id: "clip_xxx"
    label: "ci"
    token: "<secret>"
    created_at: "2026-03-11T00:00:00Z"
```

- `super_token`：管理员 Token；不会通过 API 生成
- `clips`：已注册 Clip
- `tokens`：clip-scoped tokens；由 `AdminService` 管理

## Clip 概念

### clip.yaml

当前代码会读取这些字段：`name`、`version`、`description`、`schedules`。

`schedules` 是 `cron 表达式 -> command 名` 的映射，供服务启动时注册定时任务。

### 三层形态

- **Workspace**：开发中的 Clip 目录，包含 `clip.yaml`、`commands/`、可选 `web/` / `data/`
- **Package**：`.clip` ZIP 包，CLI 用于安装/升级
- **Instance**：安装并注册到 Pinix 的 Clip（存在于本地目录，并在 config 中有条目）

### commands/

- 命令在运行时从 `commands/` 目录发现
- 不需要在 `clip.yaml` 中声明
- `GetInfo` / `ListClips` 会动态扫描命令列表

## 目录结构

```text
pinix/
├── cmd/                   # Cobra CLI：serve、clip、invoke、read、info、token
├── internal/server/       # AdminService + ClipService handlers 与辅助逻辑
├── internal/sandbox/      # Backend 接口、BoxLiteBackend、RestBackend、shared helpers
├── internal/scheduler/    # 基于 cron 的进程内调度器
├── internal/config/       # YAML 持久化存储（super_token / clips / tokens）
├── internal/auth/         # Bearer Token interceptor
├── proto/pinix/v1/        # Protobuf 定义
├── gen/                   # 生成代码（Go / Swift / TypeScript）
└── main.go                # CLI 入口
```

`cmd/` 当前包含：`serve`、`clip install/upgrade/uninstall`、`clip create/list/delete`、`invoke`、`read`、`info`、`token`。

## 开发规范

### 文件头

每个 `.go` 文件顶部保留：

```go
// Role:    一句话描述文件职责
// Depends: 直接依赖（逗号分隔）
// Exports: 对外暴露的类型/接口
```

### 错误处理

- 优先返回带上下文的错误：`fmt.Errorf("read clip.yaml: %w", err)`
- RPC handler 使用 `connect.NewError(...)` 返回明确错误码
- 常见错误码：`CodeInvalidArgument`、`CodeNotFound`、`CodeInternal`、`CodePermissionDenied`

### 生成代码

- `proto/` 修改后执行 `buf generate`
- `gen/` 下文件为生成产物，不手动编辑

# Pinix

Pinix 是一个去中心化的 Clip 运行时平台。

---

## 核心概念

### Pinix Server

托管 Clip Instance 的运行时服务。支持两种形态：

- **私有 Server**：个人部署，完全自控
- **公共 Server**：社区或团队共享

多个 Pinix Server 之间完全去中心化，互不依赖，无需中央注册服务。

### Clip Instance

运行在某个 Pinix Server 上的功能单元。每个 Clip Instance 具备：

- 唯一访问地址（URL + Token）
- 隔离的 `workdir`（包含 `commands/`、`data/`、`web/`）
- 对外暴露 RPC 接口（`Invoke` 执行命令，`ReadFile` 读文件）

同一份 Clip 代码（Clip Package）可以部署在多个 Server 上，产生多个 Instance，彼此独立。

### Clip Registry

**Clip Registry 是 Clip 的一种**，而非 Pinix Server 的附属功能。

职责：声明同一 Pinix Server 上有哪些 Clip Instance 可用。

- 数据源：读取本 Server 的 `config.yaml`
- 对外暴露 `list` command，返回所有 Clip 的描述 JSON
- Web UI：展示 Clip 卡片，提供一键生成 Bookmark 的入口

去中心化特性：每个 Server 自带一个 Registry Instance，无需中央目录。

### Clip Client

可添加来自**任意 Pinix Server** 的 Clip Instance 书签（Bookmark）的客户端（Desktop / iOS）。

**核心原则：Clip Client 与 Pinix Server 无绑定关系，只与 Clip Instance 绑定。**

通过 URL + Token 直接访问各 Instance，跨 Server 自由聚合能力。

---

## 架构

```
Pinix Server A（私有, home）          Pinix Server B（公共）
┌────────────────────────────┐        ┌──────────────────────┐
│  Clip Instance: todo       │        │  Clip Instance: news  │
│  Clip Instance: voice-inbox│        │  Clip Instance: gpt   │
│  Clip Instance: daily      │        │                       │
│  Clip Instance: registry ──┼──┐     │  Clip Instance: registry│
└────────────────────────────┘  │     └──────────────────────┘
                                │               ↑
                  Bookmark（直接绑定 URL + Token）
                                │
                       ┌─────────────────┐
                       │   Clip Client   │   Desktop / iOS
                       │                 │
                       │  [todo]         │ → Server A
                       │  [voice-inbox]  │ → Server A
                       │  [news]         │ → Server B
                       │  [registry-A]   │ → Server A（用于发现）
                       └─────────────────┘
```

---

## 发现流程

1. 拿到某个 Clip Registry Instance 的 URL + Token
2. 在 Clip Client 将其添加为 Bookmark
3. 打开 Registry，浏览该 Server 上的所有 Clip
4. 点击 [添加] → Client 创建对应 Clip Instance 的 Bookmark

---

## 鉴权模型

| Token 类型 | clip_id | 权限范围 |
|-----------|---------|---------|
| **Super Token** | 空 | 全部接口（PinixService + ClipService） |
| **Clip Token** | 非空 | 仅 ClipService，workdir 限定为该 Clip |

---

## RPC 接口

### PinixService（需要 Super Token）

| RPC | 说明 |
|-----|------|
| `CreateClip` | 注册 Clip（name + workdir） |
| `ListClips` | 列出所有 Clip |
| `DeleteClip` | 按 clip_id 删除 Clip |
| `GenerateToken` | 生成 Token（clip_id 为空 = Super Token） |
| `RevokeToken` | 撤销 Token |

### ClipService（Super Token 或 Clip Token）

| RPC | 说明 |
|-----|------|
| `Invoke` | 执行 `commands/` 下的可执行文件 |
| `ReadFile` | 流式读取 workdir 下的文件（支持 ETag 缓存） |

---

## 快速开始

```bash
# 启动
go run .
# 默认监听 :8080，可通过 PORT 覆盖
PORT=9090 go run .
```

初始化 Super Token：

```bash
mkdir -p ~/.config/pinix
cat > ~/.config/pinix/config.yaml << 'EOF'
clips: []
tokens:
  - token: "my-bootstrap-super-token"
    clip_id: ""
    label: "bootstrap"
EOF
chmod 600 ~/.config/pinix/config.yaml
```

---

## 路线图

- [x] Connect-RPC 服务骨架（PinixService + ClipService）
- [x] Token 鉴权（Super / Clip Token）
- [x] ETag 协商缓存（ReadFile）
- [ ] Clip Registry Clip 实现（Issue #5）
- [ ] Clip Client 通过 Registry 发现并添加 Bookmark
- [ ] 容器化执行层（boxlite，Phase 2）

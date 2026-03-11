# Clip Architecture: Workspace / Package / Instance

> Pinix Clip 三层架构规范 v1.1 — 2026-03-01

## 设计原则

### 最小开发成本原则（Minimum Development Cost）

技术选型的首要判据不是「架构纯粹性」，而是**开发成本最小化**。

> 决策方法：面对两个可行方案，选择**需要新写代码最少、能复用现有资产最多**的那个。

核心推论：

1. **复用优先** — 已有代码（SQL、脚本、组件）是资产。方案 A 复用 80% 现有代码，方案 B 从零写但架构更纯，选 A。
2. **零依赖优先** — macOS 自带 `sqlite3`、`jq`、`python3`，能用系统工具解决的不引入 npm/pip 包。
3. **最少文件数** — 每多一个文件就多一个维护成本。能用 3 行 shell 搞定的不写 30 行 Node.js。
4. **data/ 不限格式** — `data/` 是 Instance 的可变数据目录，SQLite、JSON、纯文本均可。选择取决于开发成本，不受「必须是纯文本」的教条约束。
5. **架构服务于交付** — 规范是为了减少沟通和返工成本，不是为了满足审美。当规范与交付效率冲突时，修正规范。

**实证**：Todo Clip 迁移中，SQLite 方案复用现有 13 条 SQL，每个 command 3-5 行 shell；JSON 方案需全部重写，每个 command 15-20 行。开发成本差 4 倍。选 SQLite。

---

## 概述

Clip 是 Pinix 的能力单元。一个 Clip 从开发到运行经历三个阶段：

```
Workspace（开发） → Package（分发） → Instance（运行）
```

类比：Docker 的 **build context → Image → Container**。

---

## 1. Workspace（开发环境）

开发者的项目目录，技术栈自由（Vite、Go、纯 Shell 均可），不做任何约束。

```
my-clip/                    ← Git 仓库
├── package.json            ← Vite 项目（举例）
├── src/
│   └── App.tsx
├── commands/               ← 命令脚本（直接可用于 Package）
│   └── append
├── cmd/                    ← Go 源码（需编译为 bin）
│   └── main.go
├── seed/                   ← 初始数据模板
│   └── config.json
└── ...                     ← node_modules, go.mod, 随便放
```

**Workspace 不等于 Package。** 开发者自行负责构建（`pnpm build`、`go build`），按 Package 规范组装产物。打包规则后续用 Skill 沉淀，暂不内建 `pinix clip pack`。

---

## 2. Package（分发单元）

`.clip` 后缀的 zip 包。**不可变，可分发，可校验。**

### 2.1 目录结构

```
<name>-<version>.clip       ← zip 文件
├── clip.yaml               ← 必须：包元信息
├── commands/               ← 可选：脚本命令（每个文件 = 一个命令）
├── bin                     ← 可选：编译型单二进制
├── lib/                    ← 可选：依赖与共享代码
├── web/                    ← 可选：静态前端（构建产物）
│   └── index.html
└── seed/                   ← 可选：初始数据模板
```

### 2.2 clip.yaml

**仅包元信息。不声明命令列表。**

```yaml
name: clip-registry
version: 1.0.0
description: Discover and manage clips across servers
```

字段：

| 字段 | 必须 | 说明 |
|------|------|------|
| name | ✅ | 包名，全局唯一标识 |
| version | ✅ | SemVer 版本号 |
| description | 可选 | 一句话描述 |

为什么不在 clip.yaml 里列命令？**拒绝双重维护。** 命令的权威来源是文件系统和二进制自省（见 §4）。

### 2.3 命令的两种形态

**脚本模式（commands/）**：适合 Shell/Python/Node。每个文件即一个命令。

```
commands/
├── append        ← #!/bin/sh
├── write         ← #!/usr/bin/env python3
└── tts           ← #!/bin/sh（可内部调用 bin）
```

**编译模式（bin）**：适合 Go/Rust/C。单个可执行文件，通过子命令分发。

```
bin               ← 单个二进制，内部路由子命令
```

**混合模式**：两者共存。`commands/` 中的脚本可以 wrap `bin` 中的命令（调试/扩展）。

### 2.4 依赖管理（lib/）

`lib/` 存放脚本命令的外部依赖和共享代码。Server 不扫描此目录。

**原则：命令脚本原则上应自包含。** 能 bundle 成单文件就别用 `lib/`。`lib/` 是无法自包含时的兜底方案。

**Node.js**（无法 bundle 的复杂依赖）：

```
commands/
  analyze              ← #!/usr/bin/env node
lib/
  node_modules/        ← npm install 的产物
    cheerio/
  utils.js             ← 共享代码
```

```js
#!/usr/bin/env node
// commands/analyze — 通过相对路径引用 lib/
const cheerio = require('../lib/node_modules/cheerio');
const { parse } = require('../lib/utils');
```

**Python**（第三方包）：

```
commands/
  analyze              ← #!/usr/bin/env python3
lib/
  site-packages/       ← pip install --target 的产物
    requests/
  helpers.py
```

```python
#!/usr/bin/env python3
import sys, os
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..', 'lib', 'site-packages'))
import requests
```

**Go/Rust** — 编译成 `bin`，不需要 `lib/`。

**Shell 调用 Node** — 脚本入口 wrap 实际逻辑：

```bash
#!/bin/sh
# commands/analyze
node "$(dirname "$0")/../lib/analyze-main.js" "$@"
```

| 场景 | 推荐做法 |
|------|----------|
| 简单脚本（无依赖） | 直接放 `commands/` |
| Node.js 能 bundle | esbuild 打成单文件放 `commands/` |
| Node.js 依赖太复杂 | `commands/` 入口 + `lib/node_modules/` |
| Python 有第三方包 | `commands/` 入口 + `lib/site-packages/` |
| Go/Rust/C | 编译成 `bin`，不用 `lib/` |

---

## 3. Instance（运行时）

Package 解压后的运行目录。**程序与数据分离。**

### 3.1 目录结构

```
<clips-dir>/<instance-name>/
├── clip.yaml               ← 来自 Package（只读）
├── commands/               ← 来自 Package（只读）
├── bin                     ← 来自 Package（只读）
├── lib/                    ← 来自 Package（只读）
├── web/                    ← 来自 Package（只读）
└── data/                   ← Instance 独有（可变）
    └── config.json         ← install 时从 seed/ 拷入
```

### 3.2 核心不变量

| 规则 | 说明 |
|------|------|
| `data/` 只属于 Instance | Package 里没有 `data/`，永远不会 |
| `upgrade` 不碰 `data/` | 只替换 clip.yaml + commands/ + bin + lib/ + web/ |
| `install` 从 `seed/` 初始化 `data/` | seed/ 是模板，data/ 是实例 |
| 同一 Package 可多实例 | 通过 `--name` 区分 |

---

## 4. 命令发现与分发协议

### 4.1 自省协议（Introspection Protocol）

编译型二进制**必须**实现一个约定：

```bash
bin --commands
```

输出：每行一个命令名，stdout，exit 0。

```
add-server
list
list-servers
generate-bookmark
remove-server
```

这是二进制对外暴露能力的**唯一方式**。无需配置文件，无需注册中心。

Go 实现示例：

```go
func main() {
    if len(os.Args) > 1 && os.Args[1] == "--commands" {
        for _, name := range []string{"add-server", "list", "remove-server"} {
            fmt.Println(name)
        }
        return
    }
    // 正常子命令分发
    subCmd, args := os.Args[1], os.Args[2:]
    switch subCmd {
    case "add-server":  addServer(args)
    case "list":        list(args)
    // ...
    default:
        fmt.Fprintf(os.Stderr, "unknown command: %s\n", subCmd)
        os.Exit(1)
    }
}
```

### 4.2 Server 发现逻辑

```
listCommands(workdir):
    cmds = {}

    // 1. bin 自省（底层）
    if exists(workdir/bin):
        names = exec(workdir/bin --commands)
        for name in names:
            cmds[name] = "bin"

    // 2. commands/ 扫描（覆盖层）
    for file in scanDir(workdir/commands/):
        cmds[file.name] = "commands"

    return cmds
```

**优先级：`commands/` > `bin`。** 脚本覆盖同名二进制命令，允许开发者用脚本 wrap 二进制。

### 4.3 Server 调用逻辑

```
invoke(name, args, stdin):
    if exists(commands/<name>):
        exec commands/<name> args...     # 脚本优先
    else if exists(bin):
        exec bin <name> args...          # fallback 到二进制
    else:
        error "command not found"

    # stdin 透传，stdout/stderr 捕获返回
```

---

## 5. 生命周期 CLI

```bash
# Package → Instance
pinix clip install <file.clip>               # 解压 → 创建 data/（从 seed/ 初始化）→ 注册
pinix clip install <file.clip> --name my-reg  # 同一 Package，不同实例名

# 升级（保留 data/）
pinix clip upgrade <file.clip>               # 替换代码层，data/ 不动

# 查看
pinix clip list                              # 列出所有 instances
pinix clip info <name>                       # 元信息 + 版本 + 命令列表

# 清除
pinix clip uninstall <name>                  # 删除 instance（提示是否保留 data/）
```

### install 流程

```
1. 读取 clip.yaml → 确定 name, version
2. 解压到 <clips-dir>/<name>/
3. 删除 seed/ 目录（如果有）→ 将内容拷贝到 data/
4. 注册到 config.yaml（id, name, workdir）
```

### upgrade 流程

```
1. 读取 clip.yaml → 匹配已有 instance
2. 替换 clip.yaml, commands/, bin, lib/, web/ → 不碰 data/
3. 更新 config.yaml version 信息
```

### uninstall 流程

```
1. 注销 config.yaml 注册（删 clip 条目 + 关联 tokens）
2. 删除 instance 目录（--keep-data 可保留 data/）
```

---

## 6. 总结：三层对照表

| | Workspace | Package | Instance |
|--|-----------|---------|----------|
| 形态 | Git 仓库 | `.clip` zip 包 | 磁盘目录 |
| 可变性 | 开发者随意改 | 不可变 | data/ 可变 |
| 包含 data/ | ❌ | ❌（只有 seed/） | ✅ |
| 包含 lib/ | 开发依赖（如 node_modules） | 打包后的运行时依赖 | 和 Package 一致 |
| 命令来源 | 源码 | 构建产物 | 和 Package 一致 |
| clip.yaml | 不需要 | 必须（仅元信息） | 来自 Package |
| 谁关心 | 开发者 | 分发/传输 | Pinix Server |

### Package 标准目录总览

| 目录/文件 | 角色 | Server 感知？ | 升级时 |
|-----------|------|--------------|--------|
| `clip.yaml` | 包元信息 | 读 name/version/desc | 替换 |
| `commands/` | 脚本命令入口 | 扫描文件名 | 替换 |
| `bin` | 编译命令入口 | 调 `--commands` | 替换 |
| `lib/` | 依赖与共享代码 | **不管** | 替换 |
| `web/` | 前端静态资源 | 检测 index.html | 替换 |
| `seed/` | data/ 初始模板 | 不管 | 不管（仅 install 时） |
| `data/` | 运行时状态 | 读写沙箱 | **不动** |

---

---

## 7. Clip 接口与 Edge Clip

### 7.1 Clip 是接口，不是文件系统

上述 Workspace/Package/Instance 模型描述的是**本地 Clip**（Local Clip）——命令以文件系统上的脚本或二进制形式存在，在 BoxLite VM 中执行。

但 Clip 的本质是一个**接口**：

```go
type Clip interface {
    GetInfo(ctx)                          → Info
    Invoke(ctx, command, args, stdin)     → stdout/stderr/exit_code (streaming)
    ReadFile(ctx, path, offset, length)   → file data (streaming)
}
```

实现了这三个方法的任何东西就是 Clip。文件系统只是一种实现方式。

### 7.2 Edge Clip

Edge Clip 是 Clip 接口的第二种实现。设备（iPhone、树莓派、ESP32）通过 EdgeService 双向流连接到 Pinix Server，注册其原生能力为 Clip。

```
Local Clip:  commands/ → BoxLite VM → stdout
Edge Clip:   EdgeRequest → device stream → EdgeResponse
```

两者在 Hub 层面完全对等——路由、Token、调用方式一致。调用方无法区分。

详见 [RFC-001: Clip Interface & Hub/Worker Separation](./rfc/001-clip-interface-hub-worker.md)。

### 7.3 内部架构

```
internal/
  clip/        Clip 接口 + Registry（Hub 唯一依赖的抽象）
  hub/         RPC handler，通过 Clip 接口路由
  worker/      LocalClip（sandbox + filesystem）
  edge/        EdgeClip（设备 session）
  sandbox/     BoxLite 后端（Worker 内部依赖）
```

---

## 相关文档

- [RFC-001: Clip Interface & Hub/Worker Separation](./rfc/001-clip-interface-hub-worker.md) — Clip 接口抽象与 Edge Clip 设计
- [Bridge 跨端兼容规范](./clip-bridge-compat.md) — Clip 前端 JS 调用 Bridge 时的 iOS/Desktop 适配
- [Clip UI 开发 SOP](./clip-ui-dev-sop.md) — CC 主导的 UI 开发完整流程

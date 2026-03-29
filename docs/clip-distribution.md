# Clip Distribution

Clip 的包标识、安装方式、Registry 架构和发布流程。

---

## 包标识格式

Package name 分三种类型，格式本身标识来源：

```
@scope/name          -> Registry 包（社区发布）
github/user/repo     -> GitHub 包
local/name           -> 本地包（仅当前 Hub）
```

- `@scope/name`：scope 是用户在 Pinix Registry 注册的账号或组织名。示例：`@cp/todo`, `@pinixai/browser`, `@somebuilder/twitter`
- `github/user/repo`：直接从 GitHub 仓库安装
- `local/name`：本地开发用，仅在当前 Hub 可见

### clip.json 示例

```jsonc
{
  "name": "@cp/todo",
  "version": "0.3.2",
  "dependencies": {
    "browser": "@pinixai/browser"
  }
}
```

---

## 安装

```bash
pinix add @cp/todo                           # Registry（默认）
pinix add @cp/todo@0.3.2                     # Registry，指定版本
pinix add github/user/my-clip                # GitHub
pinix add local/dev-tool --path ./my-clip    # 本地路径
```

所有来源都需要校验 Clip 合法性：

```
pinix add X
  |
  |-- 解析来源（Registry / 本地 / GitHub）
  |-- 下载或读取
  |-- 校验：有 clip.json？name/version/commands 合法？
  |   |-- 是 -> 安装，注册到 Hub
  |   └── 否 -> 报错："not a valid Clip"
  └── 完成
```

Hub 不区分包类型，统一路由。依赖声明直接写 package name，解析只看 Hub 上有没有匹配实例。

发布到 Registry 的 Clip 不允许依赖 `local/` 包。

---

## 来源标识

```
@scope/name          -> 社区包（Pinix Registry）  [community]
./path               -> 本地                      [local]
github/user/repo     -> GitHub                   [github]
```

---

## Registry 架构

```
registry.pinix.ai    -> API 地址（pinix add / publish / search 解析到这里）
clips.pinix.ai       -> Web 网站（人在浏览器浏览/发现 Clip）
```

类比 npm：
```
registry.npmjs.org   -> API
www.npmjs.com        -> 网站
```

### Registry API

纯 Pinix 协议，不兼容 npm：

```
# 包查询
GET  registry.pinix.ai/packages/@scope/name              -> 包信息（所有版本）
GET  registry.pinix.ai/packages/@scope/name/:version      -> 特定版本
GET  registry.pinix.ai/packages/@scope/name/:version/download -> 下载 tarball
GET  registry.pinix.ai/search?q=:query&domain=:domain     -> 搜索

# 发布
PUT  registry.pinix.ai/packages/@scope/name/versions      -> 发布新版本

# 认证
# CLI 登录后拿到 JWT，Registry 和 API 共用同一套 token
```

### Registry 配置

```bash
# 默认（不用配）
pinix add @cp/todo  -> registry.pinix.ai

# 企业自建（可选）
pinix config set registry https://registry.my-company.com
```

---

## 发布流程

### 前提

- 在 Pinix Registry 注册账号（获得 scope）
- Clip 有合法的 `clip.json`（name, version, commands）

### 步骤

```bash
# 1. 登录
pinix login
# -> POST /auth/login，token 存本地

# 2. 发布
pinix publish
# -> PUT /packages/{scope}/{name}/versions
# -> multipart 上传 tarball + metadata
```

### Scope 权限

- 用户 scope（`@cp`）：用户自己可发布
- 组织 scope（`@pinixai`）：组织 owner 可发布，可添加 member

---

## 依赖与 Bindings

Clip 的依赖通过 slot 机制实现：

### 依赖声明（clip.json）

```jsonc
{
  "name": "@cp/twitter",
  "dependencies": {
    "browser": "@pinixai/browser"
  }
}
```

`"browser"` 是 slot 名（逻辑名），`"@pinixai/browser"` 是包约束。同一个包可以有多个 slot。

### Bindings

Bindings 存在 Clip 本地的 `bindings.json` 中，映射 slot 到实际的 alias：

```jsonc
{
  "browser": {
    "alias": "browser-a3f2",
    "hub": "localhost:9000"
  }
}
```

### invoke 解析流程

SDK 解析 slot -> 查 binding -> 拿到 alias -> 发送到 Hub -> Hub 路由到目标 Clip。

支持跨 Hub binding（一个 Clip 可以依赖另一个 Hub 上的 Clip）。

# Pinix Registry Protocol v1

开放协议，任何组织可自建 Registry。官方实现：registry.pinix.io

## Manifest

Clip manifest 不再依赖独立的 manifest 文件（pinix.json 已移除）。发布时，manifest 由以下来源自动组装：

- `name`、`version`、`main` 来自 `package.json`
- `domain`、`commands`、`dependencies`、`patterns`、`entities`、`description` 由 SDK 代码在运行时通过 IPC register 上报
- `web` 目录自动检测
- `pinix publish` 会启动 Clip 进程提取上述信息，合并后生成发布用 manifest

## Registry REST API

所有请求带 `Authorization: Bearer <token>` 认证（publish/manage 操作需要）。

### 包查询

#### GET /packages/:name

返回完整包文档（所有版本）。

```json
{
  "name": "todo",
  "type": "clip",
  "description": "待办管理",
  "domain": "productivity",
  "author": { "name": "epiral" },
  "license": "MIT",
  "repository": "https://github.com/epiral/clip-todo",
  "readme": "# clip-todo\n...",
  "dist-tags": {
    "latest": "0.2.0",
    "beta": "0.3.0-beta.1"
  },
  "versions": {
    "0.1.0": {
      "pinix": { /* 发布时自动生成的 manifest */ },
      "dist": {
        "tarball": "https://registry.pinix.io/packages/todo/-/todo-0.1.0.tgz",
        "shasum": "abc123...",
        "size": 2048
      }
    },
    "0.2.0": { "..." : "..." }
  },
  "access": "public",
  "created": "2026-03-20T00:00:00Z",
  "modified": "2026-03-22T00:00:00Z"
}
```

兼容 npm：GET /:name 返回同样结构（npm client 能解析）。

#### GET /packages/:name/:version

返回特定版本信息。

#### GET /packages/:name/-/:name-:version.tgz

下载 tarball。Edge Clip 包返回 404（无 tarball）。

#### GET /search?q=:query&domain=:domain&type=:type

搜索包。返回：

```json
{
  "results": [
    { "name": "todo", "version": "0.2.0", "description": "待办管理", "type": "clip", "domain": "productivity" }
  ],
  "total": 1
}
```

### 发布

#### PUT /packages/:name

发布新版本。需认证。

Request body：multipart 或 JSON，包含 manifest + tarball（Clip）或 manifest（Edge Clip）。

成功返回 201。包名已被其他用户占用返回 403。版本已存在返回 409。

#### DELETE /packages/:name/:version

撤回特定版本。需认证 + 包所有者。

#### GET /packages/:name/dist-tags

列出包的所有 dist-tag。返回 `{ "latest": "0.2.0", "beta": "0.3.0-beta.1" }`

#### PUT /packages/:name/dist-tags/:tag

设置 dist-tag。如 `PUT /packages/todo/dist-tags/beta` body: `"0.3.0-beta.1"`

#### PUT /packages/:name/deprecate (未实现)

标记废弃。body: `{"version": "0.1.0", "message": "use 0.2.0"}`

### 认证

#### POST /auth/register

```json
{ "username": "epiral", "email": "dev@epiral.com", "password": "..." }
```

返回 `{ "token": "..." }`

#### POST /auth/login

```json
{ "username": "epiral", "password": "..." }
```

返回 `{ "token": "...", "username": "..." }`

#### GET /auth/whoami

返回 `{ "username": "epiral" }`

### 组织（未实现）

#### POST /orgs

```json
{ "name": "epiral" }
```

#### PUT /orgs/:org/members

```json
{ "username": "newmember", "role": "member" }
```

role: `owner` | `member`

#### PUT /packages/:name/access (未实现)

```json
{ "access": "public" }
```

access: `public` | `restricted`

### npm 兼容

Registry 同时实现 npm registry 协议的核心部分：

- `GET /:name` → 包文档（npm 格式兼容）
- `GET /:name/-/:tarball` → 下载
- `PUT /:name` → 发布
- `/-/whoami` → 当前用户

非 Pinix 包请求透传到 npm upstream（https://registry.npmjs.org），可缓存。

## CLI 命令

```bash
# 认证
pinix register
pinix login
pinix whoami
pinix logout

# 发布
pinix publish                    # 自动生成 manifest → 打包 → 上传
pinix publish --tag beta         # 发布为 beta

# 搜索
pinix search todo
pinix search --domain social
pinix search --type edge-clip

# 安装
pinix add todo                   # latest
pinix add todo:0.2.0             # 指定版本
pinix add todo:beta              # dist-tag
pinix add todo --name todo-work  # 自定义实例名
pinix add ./my-clip              # 本地路径

# 版本管理
pinix deprecate todo:0.1.0 "use 0.2.0"
pinix dist-tag add todo:0.2.0 stable
pinix dist-tag ls todo

# 配置
pinix config set registry https://registry.pinix.io
```

## 安装流程

### Clip

```
pinix add todo
  → CLI → Hub: AddClip { source: "todo" }
  → Hub → Runtime: ManageCommand { add }
  → Runtime:
    1. GET registry/packages/todo → 包文档
    2. 解析 dist-tags.latest
    3. GET registry/.../todo-0.2.0.tgz → 下载
    4. 校验 shasum
    5. 解压到 ~/.pinix/clips/todo/
    6. bun install
    7. spawn Clip 进程
    8. 等 register IPC
  → Runtime → Hub: ClipAdded
  → CLI: "todo 已安装 (0.2.0)"
```

### Edge Clip

```
pinix add browser
  → CLI → Registry: GET /packages/browser
  → type=edge-clip, 读 install 字段
  → CLI 尝试自动安装：
    npm → npm install -g bb-browser
    binary → 下载对应平台 binary
  → CLI 提示：运行 bb-browserd --hub http://... 连接
```

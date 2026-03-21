# Clip Development

> 用 `@pinixai/core` 开发 Bun/TS Clip，并通过 IPC v2、Hub 路由、`web/` 目录把它接进 Pinix。

## 1. 开发框架

当前 Clip 开发框架是 `@pinixai/core@0.3.0`。

它把同一个 Clip 暴露成四种运行方式：

- CLI：`bun run index.ts list`
- MCP：`bun run index.ts --mcp`
- IPC：`bun run index.ts --ipc`
- HTTP Web：`bun run index.ts --web`

最小 `package.json`：

```json
{
  "name": "clip-todo",
  "type": "module",
  "dependencies": {
    "@pinixai/core": "^0.3.0"
  }
}
```

## 2. `Clip` 类、`@command` 装饰器和 `handler`

一个最小可运行 Clip：

```ts
import { Clip, command, handler, z } from "@pinixai/core";

const TodoSchema = z.object({
  id: z.number(),
  title: z.string(),
});

class TodoClip extends Clip {
  name = "todo";
  domain = "productivity";
  patterns = ["list -> add -> list"];

  todos = [{ id: 1, title: "Ship Pinix V2 docs" }];
  nextId = 2;

  @command("List todos")
  list = handler(
    z.object({}),
    z.object({ todos: z.array(TodoSchema) }),
    async () => ({ todos: this.todos }),
  );

  @command("Add a todo")
  add = handler(
    z.object({ title: z.string().min(1) }),
    z.object({ todo: TodoSchema }),
    async ({ title }) => {
      const todo = { id: this.nextId++, title };
      this.todos.push(todo);
      return { todo };
    },
  );
}

if (import.meta.main) {
  await new TodoClip().start();
}
```

本地运行：

```bash
bun run index.ts list
bun run index.ts add --title "Write docs"
bun run index.ts --manifest
bun run index.ts --mcp
bun run index.ts --ipc
bun run index.ts --web
```

## 3. manifest 与开发者字段

`Clip` 基类当前会从实例上读取这些字段：

- `name`
- `domain`
- `patterns`
- `entities`
- `dependencies`（如果存在）

在 `pinixd` 中，IPC `register` 只上传运行时 manifest。runtime 会再从本地目录补充元数据：当前实际会读取 `clip.yaml` 里的 `version`、`description`、`dependencies`、`patterns`，并在运行时 manifest 缺字段时用 `name` 兜底；如果没有 `clip.yaml`，这些字段就保持运行时值或从 source 推导。

一个本地 Clip 常见目录：

```text
my-clip/
├── clip.yaml
├── index.ts
├── package.json
└── web/
    ├── index.html
    ├── app.js
    └── style.css
```

如果你需要补充版本、描述和依赖，可以放一个 `clip.yaml`：

```yaml
name: todo
version: 0.1.0
description: Simple todo Clip
dependencies:
  - browser
patterns:
  - list -> add -> list
```

## 4. IPC v2：`register / registered / invoke / result / error / chunk / done`

Pinix 内部的进程协议是 **NDJSON over stdin/stdout**。

```text
pinixd <-> Bun Clip process
```

### 消息类型

| type | 方向 | 作用 |
|---|---|---|
| `register` | Clip -> pinixd | 进程启动后自注册 manifest |
| `registered` | pinixd -> Clip | 确认注册完成 |
| `invoke` | 双向 | 调用 command；Clip 也用它请求其他 Clip |
| `result` | 响应 | 单次结果 |
| `error` | 响应 | 失败 |
| `chunk` | 响应 | 流式输出块 |
| `done` | 响应 | 流结束 |

### 典型握手

```text
Clip                                 pinixd
 | -- {type:"register",...} -------> |
 | <------ {type:"registered"} ----- |
 | -- {id:"1",type:"invoke",...} -> |
 | <- {id:"1",type:"result",...} -- |
```

### 消息示例

注册：

```json
{"type":"register","manifest":{"name":"todo","domain":"productivity","commands":["list","add"],"dependencies":[]}}
```

调用本 Clip command：

```json
{"id":"1","type":"invoke","command":"list","input":{}}
```

Clip 调用另一个 Clip：

```json
{"id":"2","type":"invoke","clip":"browser","command":"evaluate","input":{"js":"document.title"}}
```

结果：

```json
{"id":"2","type":"result","output":{"result":"Pinix Portal"}}
```

当前 `@pinixai/core` 的 `serveIPC()` 默认处理普通 unary 路径；`chunk` / `done` 是 IPC v2 wire protocol 的完整消息集，供 runtime 的流式路径使用。

## 5. 调用其他 Clip：`invoke()`

`@pinixai/core` 暴露了 `invoke(clip, command, input)`，本质上就是向父进程发一条 IPC `invoke` 消息。

最小示例：

```ts
import { Clip, command, handler, invoke, z } from "@pinixai/core";

class PingClip extends Clip {
  name = "ping";
  domain = "demo";
  patterns = [];

  @command("Ask browser for document.title")
  title = handler(
    z.object({}),
    z.object({ result: z.unknown() }),
    async () => {
      const result = await invoke("browser", "evaluate", { js: "document.title" });
      return { result };
    },
  );
}

if (import.meta.main) {
  await new PingClip().start();
}
```

## 6. `@pinixai/browser` 能力包

`@pinixai/browser` 是对 `invoke("browser", ...)` 的轻量封装。当前代码非常薄：每个 API 只是把输入转发给名为 `browser` 的 Clip。

可直接使用：

```ts
import { Clip, command, handler, z } from "@pinixai/core";
import { browser } from "@pinixai/browser";

class BrowserDemoClip extends Clip {
  name = "browser-demo";
  domain = "demo";
  patterns = ["open -> evaluate"];
  dependencies = ["browser"];

  @command("Open X and read title")
  run = handler(
    z.object({}),
    z.object({ title: z.string() }),
    async () => {
      await browser.navigate({
        url: "https://x.com/home",
        waitUntil: "domcontentloaded",
      });

      const result = await browser.evaluate({
        js: "document.title",
      });

      return { title: String(result.result) };
    },
  );
}

if (import.meta.main) {
  await new BrowserDemoClip().start();
}
```

当前内置的 browser command 包括：

- `navigate`
- `click`
- `type`
- `evaluate`
- `screenshot`
- `getCookies`
- `waitForSelector`

## 7. Clip Web UI：`web/` 目录与相对路径规范

本地 Clip 可以带一个 `web/` 目录。

```text
web/
├── index.html
├── app.js
└── style.css
```

### 路径规范

必须使用**相对路径**，不要带前导 `/`。

正确：

```html
<link rel="stylesheet" href="style.css" />
<script src="app.js" defer></script>
```

```js
fetch("api/list")
fetch("api/add", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ title: "Write docs" }),
})
```

错误：

```html
<link rel="stylesheet" href="/style.css" />
<script src="/app.js"></script>
```

```js
fetch("/api/list")
```

### 两种运行方式

独立运行：

```bash
bun run index.ts --web
```

Pinix Portal 下访问本地 Clip：

```text
http://127.0.0.1:9000/clips/<clip-name>/
```

Portal 会把 `POST /clips/<clip-name>/api/<command>` 代理到本地 Clip command。


## 8. 参考实现

### `clip-todo-web`

- 位置：`pinixai-core/examples/clip-todo-web/`
- 展示 `web/` 目录、相对路径、`api/<command>` 调用方式。

### `clip-twitter`

- 位置：`/Users/cp/Developer/epiral/clips/twitter/`
- 展示 `dependencies = ["browser"]`。
- 展示用 `@pinixai/browser` 调 Twitter GraphQL 的实际写法。

`clip-twitter` 当前公开的 commands：

- `search`
- `getProfile`
- `getTweet`

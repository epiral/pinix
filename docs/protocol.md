# Pinix V2 Protocol

> Pinix V2 使用两层协议：外部是 Connect-RPC，内部是 NDJSON IPC v2。

协议设计讨论见：

- https://github.com/epiral/pinix/issues/13

## 1. 两层协议

```text
External
  CLI / Portal / Provider
        <-> Connect-RPC HubService

Internal
  pinixd runtime
        <-> NDJSON over stdin/stdout
        <-> Bun/TS Clip process
```

## 2. 外部协议：Connect-RPC `HubService`

`proto/pinix/v2/hub.proto` 中的服务定义如下：

```protobuf
service HubService {
  rpc ProviderStream(stream ProviderMessage) returns (stream HubMessage);

  rpc ListClips(ListClipsRequest) returns (ListClipsResponse);
  rpc ListProviders(ListProvidersRequest) returns (ListProvidersResponse);
  rpc GetManifest(GetManifestRequest) returns (GetManifestResponse);
  rpc GetClipWeb(GetClipWebRequest) returns (GetClipWebResponse);
  rpc Invoke(InvokeRequest) returns (stream InvokeResponse);
  rpc InvokeStream(stream InvokeStreamMessage) returns (stream InvokeResponse);
  rpc AddClip(AddClipRequest) returns (AddClipResponse);
  rpc RemoveClip(RemoveClipRequest) returns (RemoveClipResponse);
}
```

### 作用分层

| RPC | 作用 |
|---|---|
| `ProviderStream` | Provider / Edge Clip 接入 Hub |
| `ListClips` | 列出当前可用 Clip |
| `ListProviders` | 列出当前在线 Provider |
| `GetManifest` | 获取 Clip manifest |
| `GetClipWeb` | 读取 Clip Web 静态资源 |
| `Invoke` | unary 或 server-stream 调用 |
| `InvokeStream` | bidi 调用 |
| `AddClip` | 安装并注册 Clip |
| `RemoveClip` | 卸载并移除 Clip |

### 当前实现说明

- `AddClip` / `RemoveClip` 会读取 `Authorization: Bearer <token>`，并与 registry 中的 `super_token` 比对。
- `InvokeRequest.clip_token` 和 `InvokeCommand.clip_token` 已经在 proto 中存在，Hub 只透传；本地 `pinixd` runtime 已实现 per-Clip token 校验。
- `GetClipWeb` 当前只对本地 `pinixd` Clip 生效；provider-backed Clip 的 Web 代理返回 `unimplemented`。

## 3. Provider 协议：`ProviderMessage` / `HubMessage`

### Provider -> Hub

```protobuf
message ProviderMessage {
  oneof payload {
    RegisterRequest register = 1;
    ClipAdded clip_added = 2;
    ClipRemoved clip_removed = 3;
    InvokeResult invoke_result = 4;
    Heartbeat ping = 5;
    ManageResult manage_result = 6;
  }
}
```

### Hub -> Provider

```protobuf
message HubMessage {
  oneof payload {
    RegisterResponse register_response = 1;
    InvokeCommand invoke_command = 2;
    InvokeInput invoke_input = 3;
    ManageCommand manage_command = 4;
    Heartbeat pong = 5;
  }
}
```

### ProviderStream 时序

```text
Provider                              Hub
  | -- register --------------------> |
  | <- register_response ------------ |
  | -- ping ------------------------> |
  | <- pong ------------------------- |
  | <- invoke_command --------------- |
  | -- invoke_result ----------------> |
```

## 4. `Invoke` 与 `InvokeStream`

Pinix V2 把调用分成两类：

| 模式 | RPC | 适用 |
|---|---|---|
| 普通 / server-stream | `Invoke` | `todo.list`、LLM token 流 |
| 双向流 | `InvokeStream` | 音频流、实时会话 |

当前 `pinix` CLI 的 `client.Invoke()` 会把 `Invoke` 返回的 chunks 聚合成最终 JSON 输出。

## 5. 内部协议：IPC v2 NDJSON

`pinixd` 和本地 Bun/TS Clip 进程之间使用一行一条 JSON：

```text
{"type":"register",...}\n
{"id":"1","type":"invoke",...}\n
{"id":"1","type":"result",...}\n
```

### 7 种消息类型

| type | 方向 | 必要字段 | 说明 |
|---|---|---|---|
| `register` | Clip -> pinixd | `type`, `manifest` | Clip 自注册 |
| `registered` | pinixd -> Clip | `type` | 注册确认 |
| `invoke` | 双向 | `id`, `type`, `command` 或 `clip+command` | 调用自身或其他 Clip |
| `result` | 响应 | `id`, `type`, `output` | 单次结果 |
| `error` | 响应 | `id`, `type`, `error` | 错误 |
| `chunk` | 响应 | `id`, `type`, `output` | 流式块 |
| `done` | 响应 | `id`, `type` | 流结束 |

### register

Clip 启动后**必须**发送的第一条消息。在 `register` 之前发送任何其他消息会导致进程被终止。

```json
{
  "type": "register",
  "manifest": {
    "name": "todo",
    "domain": "productivity",
    "description": "",
    "commands": ["list", "add", "delete"],
    "dependencies": [],
    "package": "clip-todo",
    "version": "0.2.0"
  }
}
```

| manifest 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `name` | string | 是 | Clip 实例名 |
| `domain` | string | 是 | 功能领域 |
| `description` | string | 否 | 描述 |
| `commands` | string[] | 是 | 支持的命令列表 |
| `dependencies` | string[] | 否 | 依赖的其他 Clip |
| `package` | string | 否 | 包名（来自 package.json 或 pinix.json） |
| `version` | string | 否 | 版本号 |

`pinixd` 收到后会回：

```json
{"type":"registered"}
```

> **注意**：Clip 不应发送 `registered` 消息，pinixd 收到 Clip 发的 `registered` 会视为协议错误并终止进程。

### invoke

调用本 Clip command：

```json
{"id":"1","type":"invoke","command":"list","input":{}}
```

本地 Clip 调别的 Clip：

```json
{"id":"2","type":"invoke","clip":"browser","command":"evaluate","input":{"js":"document.title"}}
```

### result / error / chunk / done

普通结果：

```json
{"id":"1","type":"result","output":{"todos":[]}}
```

错误：

```json
{"id":"1","type":"error","error":"unknown command: list"}
```

流式输出：

```json
{"id":"7","type":"chunk","output":"Hel"}
{"id":"7","type":"chunk","output":"lo"}
{"id":"7","type":"done"}
```

## 6. 一条完整链路

```text
pinix twitter search
  -> Hub Invoke
  -> pinixd local runtime
  -> twitter Clip process
  -> IPC invoke("browser", "evaluate", ...)
  -> pinixd
  -> Hub ProviderStream
  -> bb-browserd
  -> Chrome CDP
  -> result back through the same path
```

这也是 Pinix V2 的核心：外部一律走 HubService，内部本地进程之间走 NDJSON IPC v2。

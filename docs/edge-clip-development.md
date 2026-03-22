# Edge Clip Development

> 实现自己的 Provider，通过 `ProviderStream` 把原生能力或外部进程接入 Pinix Hub。

## 1. 什么是 Edge Clip

Edge Clip 是开发者术语，指：

- 不跑在 `pinixd` 管理的 Bun runtime 里。
- 自己实现 Provider 协议。
- 直接连接 Hub。

典型例子：

- `bb-browserd`：把浏览器能力注册成 `browser` Clip。
- 手机 App：把 camera、health、location 注册成 Clip。
- 桌面常驻进程：把 screen、clipboard、applescript 注册成 Clip。

Hub 不会特殊对待 Edge Clip；它只看见 Provider 注册上来的普通 Clip。

## 2. 协议位置

Proto 定义在：

```text
proto/pinix/v2/hub.proto
```

生成代码已放在：

```text
gen/go/pinix/v2/
gen/ts/pinix/v2/
gen/swift/pinix/v2/
```

## 3. ProviderStream 生命周期

核心 RPC：

```protobuf
rpc ProviderStream(stream ProviderMessage) returns (stream HubMessage);
```

典型生命周期：

```text
Provider                              Hub
  | ---- RegisterRequest -----------> |
  | <--- RegisterResponse ----------- |
  | ---- Heartbeat(ping) -----------> |
  | <--- Heartbeat(pong) ------------ |
  | <--- InvokeCommand -------------- |
  | ---- InvokeResult --------------> |
```

## 4. 注册、心跳、invoke

### 注册

Provider 建连后第一条消息必须是 `register`。

以 `bb-browserd` 为例，它会注册一个 `browser` Clip：

```json
{
  "register": {
    "providerName": "bb-browserd-browser",
    "acceptsManage": false,
    "clips": [
      {
        "name": "browser",
        "package": "browser",
        "version": "0.9.0",
        "domain": "浏览器",
        "hasWeb": false,
        "dependencies": [],
        "tokenProtected": false
      }
    ]
  }
}
```

关键点：

- `provider_name` 在 Hub 内唯一。
- `clips[].name` 在 Hub 内唯一。
- `accepts_manage=false` 表示这个 Provider 不能接受 `AddClip` / `RemoveClip`。
- `bb-browserd` 的版本号来自它自己的 `package.json`；这里示例对应当前仓库里的 `0.9.0`。

### 心跳

`bb-browserd` 当前每 15 秒发一次 heartbeat。

```text
ProviderMessage.ping -> HubMessage.pong
```

如果连接断开，Hub 会移除该连接下注册的 Clip。

### invoke

Hub 发给 Provider：

```json
{
  "invokeCommand": {
    "requestId": "req-42",
    "clipName": "browser",
    "command": "evaluate",
    "input": "{\"js\":\"document.title\"}",
    "clipToken": ""
  }
}
```

Provider 回给 Hub：

```json
{
  "invokeResult": {
    "requestId": "req-42",
    "output": "{\"result\":\"Pinix Portal\"}",
    "done": true
  }
}
```

## 5. `bb-browserd` 参考实现

参考文件：

```text
/Users/cp/Developer/epiral/repos/bb-browser/bin/bb-browserd.ts
```

它做了几件事：

1. 用 Connect-RPC 客户端连到 Hub。
2. 发送 `register`。
3. 周期性发送 `ping`。
4. 收到 `invokeCommand` 后，执行 Chrome CDP 调用。
5. 用 `invokeResult` 回传结果。

直接运行：

```bash
bun run /Users/cp/Developer/epiral/repos/bb-browser/bin/bb-browserd.ts \
  --pinix http://127.0.0.1:9000 \
  --name browser
```

`bb-browserd` 当前注册的 commands：

- `navigate`
- `click`
- `type`
- `evaluate`
- `screenshot`
- `getCookies`
- `waitForSelector`

## 6. 最小实现要点

如果你自己写一个 Edge Clip，最少要实现：

1. 建立 `ProviderStream` 双向流。
2. 首帧发送 `RegisterRequest`。
3. 处理 `RegisterResponse`。
4. 处理 `InvokeCommand`，执行本地能力。
5. 把结果封装成 `InvokeResult` 发回去。
6. 定期发送 `Heartbeat`。

## 7. 一个最小可跑的连接命令

如果你的 Provider 已经能发出注册和心跳，那么最小验证方式是：

```bash
./pinixd --port 9000 --hub-only
```

另一个终端：

```bash
bun run /Users/cp/Developer/epiral/repos/bb-browser/bin/bb-browserd.ts \
  --pinix http://127.0.0.1:9000 \
  --name browser
```

再列出 Hub 上的 Clip：

```bash
./pinix --server http://127.0.0.1:9000 list
```

如果看到 `browser`，说明 Provider 连接、注册和路由都已经通了。

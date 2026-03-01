# Clip Bridge 跨端兼容规范

> Clip 前端 JS 调用 Bridge API 时，iOS 和 Desktop 的签名不同。本文档定义统一适配方案。

## 问题

| | iOS (WKWebView) | Desktop (Electron) |
|--|--|--|
| Bridge 注入 | `window.webkit.messageHandlers.pinix` | `contextBridge.exposeInMainWorld` |
| Pinix RPC 调用 | `Bridge.invoke('invoke', { name, args, stdin })` | `Bridge.invoke(name, { args, stdin })` |
| 原因 | iOS JSBridge 统一路由，`action` 只认 `"invoke"` | Electron preload 直接透传 action 到 IPC |

## 检测方法

```js
const isIOS = !!window.webkit;
```

## 统一调用封装

所有 Clip 前端应使用以下 helper，禁止直接裸调 `Bridge.invoke`：

```js
/**
 * 跨端 Pinix RPC 调用
 * @param {string} command - 命令名（如 'list', 'add-server'）
 * @param {string} stdin   - JSON 字符串输入（默认 '{}'）
 * @param {string[]} args  - 命令参数数组（默认 []）
 * @returns {Promise<{stdout: string, stderr: string, exitCode: number}>}
 */
async function pinixInvoke(command, stdin = '{}', args = []) {
    const isIOS = !!window.webkit;
    if (isIOS) {
        return Bridge.invoke('invoke', { name: command, args, stdin });
    } else {
        return Bridge.invoke(command, { args, stdin });
    }
}
```

## Clipboard 写入

`navigator.clipboard.writeText()` 在 WKWebView 自定义 scheme（`pinix-web://`）下不可用（非安全上下文）。

```js
async function clipboardWrite(text) {
    if (window.webkit) {
        // iOS: 走原生 Bridge
        await Bridge.invoke('ios.clipboardWrite', { text });
    } else {
        // Desktop: 标准 API
        await navigator.clipboard.writeText(text);
    }
}
```

## iOS 专属 Bridge 能力

iOS 提供 `ios.*` 前缀的平台专属 API，Desktop 不支持：

| Action | 功能 |
|--------|------|
| `ios.clipboardRead` | 读剪贴板 |
| `ios.clipboardWrite` | 写剪贴板 |
| `ios.haptic` | 触觉反馈 |
| `ios.notify` | 本地通知 |
| `ios.locationGet` | 获取位置 |
| `ios.cameraCapture` | 拍照 |
| `ios.microphoneRecord` | 录音 |

使用前必须检测 `!!window.webkit`，Desktop 端调用会报 `Unknown action`。

## 流式调用（invokeStream）

`Bridge.invokeStream` 用于 Server Streaming RPC（`ClipService.Invoke` 返回 `stream InvokeChunk`）。
两端签名一致，无需平台判断：

```js
/**
 * 流式 Pinix RPC 调用
 * @param {string} command - 命令名（如 'send-message'）
 * @param {object} opts    - { args?: string[], stdin?: string }
 * @param {function} onChunk - 每个 stdout chunk 回调 (text: string) => void
 * @param {function} onDone  - 流结束回调 (exitCode: number) => void
 * @returns {string} streamId
 */
Bridge.invokeStream(command, opts, onChunk, onDone)
```

| | iOS (WKWebView) | Desktop (Electron) |
|--|--|--|
| 底层实现 | `evaluateJavaScript` 回调 `window.__streamCallbacks[streamId]` | IPC `pinix:stream-chunk` / `pinix:stream-done` 事件 |
| streamId 生成 | JS 侧 `'stream_' + Date.now() + '_' + random` | preload 侧 `'s' + counter++` |
| cleanup | Swift 收到 `exit_code` 后 `delete __streamCallbacks[id]` | 收到 done 后 `removeListener` |

## 规则总结

1. **禁止裸调 `Bridge.invoke(cmd, ...)`** — 必须通过 `pinixInvoke()` 封装
2. **禁止裸用 `navigator.clipboard`** — 必须通过 `clipboardWrite()` 封装
3. **iOS 专属能力必须守卫** — `if (window.webkit)` 检测
4. **所有 Clip 前端代码必须同时在 iOS 和 Desktop 上测试**

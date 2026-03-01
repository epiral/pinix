# Clip Debug Server — 接口规范与 Agent 测试 SOP

> 物理位置：`clip-dock-desktop` 内置，运行于 `http://127.0.0.1:9876`
> 启动方式：随 `dev.sh` 自动启动，生产包不包含此服务

---

## 测试范围边界（必读）

**Desktop Debug Server 可测：**
- ✅ Web UI 渲染与交互（DOM、事件、样式）
- ✅ Clip 业务命令（通过 `Bridge.invoke()`）
- ✅ JS 逻辑、状态管理、常规 API
- ✅ 跨窗口并发操作（多 Clip 同时测试）

**必须在 iOS 真机验证：**
- ❌ 任何调用 `ios.*` 命名空间的 Bridge 操作（如 `ios.clipboardWrite`、`ios.haptic`）
- ❌ 依赖 iOS 系统 API 的功能（相册、推送、健康数据等）
- ❌ WKWebView 特有行为（自定义 Scheme 下的权限限制等）

**判断规则：** Clip 代码中凡出现 `window.Bridge.invoke("invoke", {action: "ios.*"})` 的路径，均属 iOS 专有测试范围，不在 Desktop Debug Server 可验证范围内。

---

## 接口全量清单

所有接口通过 `alias` 参数指定目标窗口（窗口 title）。  
先调用 `/windows` 获取当前窗口列表，再用 alias 精确路由。

### 窗口管理

| 接口 | 方法 | 参数 | 返回 | 说明 |
|------|------|------|------|------|
| `/windows` | GET | 无 | `[{id, title, bounds}]` | 列出所有窗口 |
| `/open-clip` | POST | body: `ClipBookmark` | `{windowId, title, existing}` | 打开 Clip 窗口，已存在则聚焦 |
| `/reload` | GET | `alias` | `{ok: true}` | 刷新指定窗口（部署新代码后用） |
| `/resize` | POST | `{alias?, preset?, width?, height?}` | `{ok, bounds}` | 调整窗口尺寸，preset: `iphone15`/`ipad`/`desktop` |
| `/close` | POST | `{alias?, windowId?, force?}` | `{ok, destroyed}` | 关闭窗口 |

```bash
# ClipBookmark 格式
{ "name": "todo", "server_url": "http://192.168.1.79:9875", "token": "<clip-token>" }
```

### 快照 & 元素寻址（Ref 系统核心）

| 接口 | 方法 | 参数 | 返回 | 说明 |
|------|------|------|------|------|
| `/snapshot` | GET | `alias`, `interactive?` | 见下 | 获取页面语义树 + refs 映射 |
| `/screenshot` | GET | `alias` | PNG 图片 | 截图，配合 gemini-vision 使用 |
| `/dom` | GET | `alias` | HTML 文本 | 完整 DOM（通常太重，优先用 snapshot） |

**`/snapshot` 返回格式：**
```json
{
  "tree": { "...嵌套语义树..." },
  "refs": {
    "0": { "xpath": "/html/body/button[1]", "role": "button", "name": "提交", "tagName": "button" },
    "1": { "xpath": "/html/body/input[1]", "role": "textbox", "name": "搜索", "tagName": "input" }
  },
  "interactive": "- button \"提交\" [ref=0]\n- textbox \"搜索\" [ref=1]"
}
```

**`interactive=true` 模式：** 只返回 `interactive` 字段（纯文本），大幅减少 token 消耗，是 Agent 操作前的标准调用方式。

### 元素交互

所有交互接口均支持 `ref`（推荐）或 `selector`（CSS selector，兼容）两种寻址方式。**必须先调用 `/snapshot` 建立 refs 映射，再使用 ref 操作。**

| 接口 | 方法 | 参数 | 返回 | 说明 |
|------|------|------|------|------|
| `/click` | POST | `{ref?, selector?, alias?}` | `{result, role, name}` | 点击元素 |
| `/fill` | POST | `{ref?, selector?, value, alias?}` | `{result, role, name}` | 清空并填入文本 |
| `/hover` | POST | `{ref?, selector?, alias?}` | `{result, role?, name?}` | 鼠标悬停（触发 hover 菜单/tooltip） |
| `/press` | POST | `{key, modifiers?, alias?}` | `{ok: true}` | 发送键盘事件 |
| `/scroll` | POST | `{selector?, deltaX?, deltaY?, alias?}` | `{ok: true}` | 滚动页面 |

**`/press` 支持的 key：**  
`Enter` `Tab` `Escape` `Space` `Backspace` `Delete`  
`ArrowUp` `ArrowDown` `ArrowLeft` `ArrowRight`  
`F1`-`F12`，单字符如 `"a"` `"A"`  
modifiers: `["Ctrl", "Shift", "Alt", "Meta"]`

### JS 执行 & 调试

| 接口 | 方法 | 参数 | 返回 | 说明 |
|------|------|------|------|------|
| `/eval` | POST | `{script, alias?}` | `{result}` 或 `{error}` | 执行任意 JS，返回值可序列化 |
| `/console` | GET | `alias`, `clear?` | `{messages, count}` | 读取 console 消息（log/warn/error） |
| `/errors` | GET | `alias`, `clear?` | `{errors, count}` | 读取 JS 未捕获错误 |

---

## Ref-based 交互工作流（标准 SOP）

```
步骤 1: 确认窗口
GET /windows
→ 找到目标窗口 title（如 "todo"）

步骤 2: 打开 Clip（如未开启）
POST /open-clip {"name":"todo", "server_url":"...", "token":"..."}

步骤 3: 获取可交互元素列表
GET /snapshot?alias=todo&interactive=true
→ 返回如：
  - button "Add Task" [ref=0]
  - textbox "Task name" [ref=1]

步骤 4: 操作目标元素（用 ref）
POST /fill {"alias":"todo", "ref":1, "value":"Buy groceries"}
POST /click {"alias":"todo", "ref":0}
→ 返回 {"result":"ok", "role":"button", "name":"Add Task"}

步骤 5: 截图验证
GET /screenshot?alias=todo → 传给 gemini-vision 分析

步骤 6（可选）: 检查错误
GET /errors?alias=todo
GET /console?alias=todo

步骤 7（部署新代码后）: 刷新
GET /reload?alias=todo → 重新加载，再回步骤 3
```

---

## 端到端 Clip 开发闭环示例

```bash
BASE="http://127.0.0.1:9876"

# 1. 打开 Clip 窗口
curl -s -X POST $BASE/open-clip \
  -H "Content-Type: application/json" \
  -d '{"name":"my-clip","server_url":"http://192.168.1.79:9875","token":"<token>"}'

# 2. 调整为移动端尺寸（模拟 iOS 视口）
curl -s -X POST $BASE/resize \
  -d '{"alias":"my-clip","preset":"iphone15"}'

# 3. 截图 → gemini-vision 分析初始状态
curl -s "$BASE/screenshot?alias=my-clip" -o /tmp/before.png
gemini-vision /tmp/before.png "描述当前界面，找出 UI 问题"

# 4. 获取可交互元素
curl -s "$BASE/snapshot?alias=my-clip&interactive=true"

# 5. 操作：填写 + 提交
curl -s -X POST $BASE/fill -d '{"alias":"my-clip","ref":0,"value":"Hello"}'
curl -s -X POST $BASE/click -d '{"alias":"my-clip","ref":1}'

# 6. 验证操作结果
curl -s "$BASE/screenshot?alias=my-clip" -o /tmp/after.png
gemini-vision /tmp/after.png "操作是否成功？界面是否符合预期？"

# 7. 检查有无 JS 错误
curl -s "$BASE/errors?alias=my-clip"

# 8. 修改源码后部署，刷新验证
cd ~/Developer/epiral/repos/clip-my-clip && pnpm build
pinix clip upgrade my-clip my-clip-1.1.0.clip
curl -s "$BASE/reload?alias=my-clip"
```

---

## 多窗口并发测试

```bash
# 同时操作两个 Clip，互不干扰
curl -s "$BASE/snapshot?alias=todo&interactive=true"      # Todo 窗口
curl -s "$BASE/snapshot?alias=registry&interactive=true"  # Registry 窗口

curl -s -X POST $BASE/click -d '{"alias":"todo","ref":0}'
curl -s -X POST $BASE/click -d '{"alias":"registry","ref":2}'
```

---

## 注意事项

- **Refs 会失效**：页面导航或 DOM 大幅变化后，必须重新调用 `/snapshot` 建立新的 refs 映射
- **alias 大小写敏感**：与窗口 title 精确匹配（Todo ≠ todo）
- **Debug Server 仅开发用**：`127.0.0.1` 绑定，不对外暴露，生产包不包含

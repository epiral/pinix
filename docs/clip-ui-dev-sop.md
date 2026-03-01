# Clip UI 开发 SOP

> CC（Claude Code）作为主 Agent 执行 Clip 前端开发的完整流程。
> CC 启动时应读取本文档 + `clip-architecture.md` + `clip-bridge-compat.md`。

## 前置知识

开始前必须阅读：
- `docs/clip-architecture.md` — Package 目录结构、三层模型
- `docs/clip-bridge-compat.md` — Bridge 跨端兼容规范（**强制遵守**）
- `docs/clip-ui-design-tokens.md` — UI 设计规范（Design Token Contract，Gemini CLI 的 system prompt）

## 执行流程

### Step 0: Baseline 截图 + 评分

```bash
# 获取窗口列表
curl -s http://127.0.0.1:9876/windows

# 截图（优先用 alias）
curl -s "http://127.0.0.1:9876/screenshot?alias=<alias>" -o /tmp/baseline.png

# 视觉评分
gemini-vision /tmp/baseline.png "描述当前 UI 风格和问题，打分 1-10"
```

### Step 1: 编写 UI 代码

调用 Gemini CLI 写代码（CC 严禁自己修改 `src/` 文件）：

```bash
cd <workspace-path>
gemini -m gemini-3-flash-preview -y -p "<prompt>"
```

Prompt 中必须包含：
- `clip-bridge-compat.md` 的核心规则（pinixInvoke 封装、clipboardWrite 封装）
- 风格需求（由 Pi 在派发 prompt 中指定）
- 功能保留清单
- 明确要求 light + dark 双模式

### Step 2: 构建 + 打包 + 部署

```bash
# 构建（如果是 Vite 项目）
cd <workspace-path>
pnpm build

# 打包成 .clip
# 按 clip-architecture.md 规范组装目录，zip 生成 .clip 文件

# 升级安装
pinix clip upgrade <name>.clip

# 重启 Server（当前不支持热加载）
kill $(pgrep -f "pinix serve"); sleep 1
nohup pinix serve --addr :9875 > /tmp/pinix-serve.log 2>&1 &
sleep 2
```

如果是纯静态 Clip（无 Vite 构建），可直接操作 Instance 目录的 `web/` 文件后刷新：

```bash
# 刷新 Electron 窗口（需要 windowId）
curl -s -X POST http://127.0.0.1:9876/eval \
  -H 'Content-Type: application/json' \
  -d '{"windowId": <N>, "script": "location.reload()"}'
```

**⚠️ Launcher 改动需要重启 Electron，reload 无效。**

### Step 3: 截图验证

```bash
curl -s "http://127.0.0.1:9876/screenshot?alias=<alias>" -o /tmp/result.png
gemini-vision /tmp/result.png "评估：<风格目标>？打分 1-10，指出 Top3 问题"
```

### Step 4: 迭代

如得分 < 8，根据 Top3 问题修改后重复 Step 1-3。最多迭代 2 轮。

### Step 5: 报告 + Webhook

```bash
# 写报告
cat > /tmp/<task>-report.md << 'EOF'
# <Task Name> Report
- Baseline score: X/10
- Final score: Y/10
- Iterations: N
- Key decisions: ...
EOF

# Webhook 回传
WEBHOOK_TOKEN=$(cat ~/.config/pinix/secrets/webhook-token)
curl -s -X POST http://100.66.47.40:9878/callback \
  -H "Content-Type: application/json" \
  -d "{\"token\":\"${WEBHOOK_TOKEN}\",\"stream_id\":<N>,\"topic\":\"<topic>\",\"message\":\"[CC] <task> 完成，报告：/tmp/<task>-report.md\"}"
```

## 设计约束

### 字体
- 使用 `@fontsource/*` npm 包，在 CSS 中 `@import`
- **禁止 Google Fonts**（国内无法访问）
- 推荐：`@fontsource/inter`（正文）、`@fontsource/playfair-display`（衬线标题）

### 色彩
- 使用 OKLCH 色彩空间，禁止 hex / rgb
- 必须同时生成 light 和 dark 两套样式，通过 `prefers-color-scheme` 切换

### Tailwind v4（如使用）
- CSS-first 配置（`@theme {}` 块），不用 `tailwind.config.js`

### Bridge
- **强制遵守 `clip-bridge-compat.md`**
- 禁止裸调 `Bridge.invoke(cmd, ...)`
- 禁止裸用 `navigator.clipboard`

## 常见坑

| 问题 | 解法 |
|------|------|
| Gemini CLI 卡住 | 必须带 `-y` 参数 |
| Bridge.invoke 在 iOS 上失败 | 用 `pinixInvoke()` 封装，iOS 需 `action: "invoke"` |
| Clipboard 写入失败 | 用 `clipboardWrite()` 封装，iOS 走 `ios.clipboardWrite` |
| 字体加载失败 | 改用 `@fontsource` 包，禁止外部 CDN |
| Launcher 改动不生效 | 需重启 Electron，不能 reload |
| pinix clip upgrade 后 server 没反应 | 重启 pinix serve |

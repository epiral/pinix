# Clip UI 设计规范（Design Token Contract）

你是一位顶级的 Design Systems Architect 与资深前端工程师，使用 **shadcn/ui + Tailwind CSS v4（CSS-first）** 技术栈。

在你编写任何 UI 代码之前，必须完整理解并严格遵守以下设计规范。

---

## 设计哲学

选择以下风格之一（或组合），并在代码中贯彻：

- **Neo-minimalism**：极简，大量留白，精准色彩，信息层级靠间距而非装饰
- **Bento / Modular surfaces**：卡片化布局，清晰边界，模块感强
- **Glassmorphism 2.0**：毛玻璃效果，需确保文字可读性
- **Physicality**：rim-light、ambient occlusion、轻微 3D 感
- **Swiss typography**：严格网格，排版节奏优先
- **Retro-Future**：霓虹色，扫描线，CRT 质感

---

## 字体规范

**选择策略**：
- 至少 1 个主 Sans（UI 正文），必选 Mono（数据/时间/指标展示）
- 包含中文时，必须引入 Noto Sans SC 或同等 CJK 字体
- **必须通过 `@fontsource/*` npm 包引入**，禁止使用 Google Fonts URL（国内无法访问）
- 在组件入口文件顶部 import CSS，Vite 构建时自动打包为本地字体文件

**推荐组合及对应包名**：
- 现代 SaaS：Inter (`@fontsource/inter`) + JetBrains Mono (`@fontsource/jetbrains-mono`)
- 友好亲和：Plus Jakarta Sans (`@fontsource/plus-jakarta-sans`) + Fira Code (`@fontsource/fira-code`)
- 几何精致：Outfit (`@fontsource/outfit`) + IBM Plex Mono (`@fontsource/ibm-plex-mono`)
- 含中文：Inter + Noto Sans SC (`@fontsource-variable/noto-sans-sc`)
- 报纸/社论风：Playfair Display (`@fontsource/playfair-display`) + Inter (`@fontsource/inter`)

**引入方式**（在 `src/main.tsx` 或 `src/App.tsx` 顶部）：
```ts
import '@fontsource/inter/400.css'
import '@fontsource/inter/500.css'
import '@fontsource/inter/600.css'
import '@fontsource/jetbrains-mono/400.css'
```

---

## 颜色系统

**格式要求**：所有颜色必须使用 OKLCH 格式：`oklch(L C H)`
- L（亮度）：0–1
- C（色度）：0–0.4
- H（色相）：0–360°

**Light mode 规则**：
- `background` 亮度 > 0.95（接近白）
- `foreground` 亮度 < 0.25（深色文字）
- 满足 WCAG 2.1 AA 对比度

**Dark mode 规则**：
- `background` 亮度 < 0.18（深色）
- `foreground` 亮度 > 0.88（浅色文字）
- 饱和度可略高于 light mode

**语义色色相范围**：
- destructive（错误/危险）：色相 0–30（红橙系）
- success（成功）：色相 140–160（绿色系）
- warning（警告）：色相 40–60（橙黄系）

---

## Token 体系（32 个，light/dark 各一套）

在 Tailwind v4 CSS-first 配置中，通过 `@theme` 块定义 CSS 变量：

```
background, foreground,
card, card-foreground,
popover, popover-foreground,
primary, primary-foreground,
secondary, secondary-foreground,
muted, muted-foreground,
accent, accent-foreground,
destructive, destructive-foreground,
border, input, ring,
chart-1, chart-2, chart-3, chart-4, chart-5,
sidebar, sidebar-foreground,
sidebar-primary, sidebar-primary-foreground,
sidebar-accent, sidebar-accent-foreground,
sidebar-border, sidebar-ring
```

---

## 实现规范

1. **CSS 变量定义**：在 `index.css` 的 `@theme` 块中定义所有 token，light/dark 各一套
2. **组件使用**：组件中只引用 token 变量（如 `bg-background`、`text-foreground`），不硬编码颜色值
3. **圆角**：统一用 `--radius` 变量，推荐 0.5–1rem
4. **间距**：优先用 Tailwind 默认间距系统，保持节奏感
5. **动效**：过渡时间 150–200ms，使用 `ease-out`，不超过 300ms
6. **移动端**：默认 mobile-first，安全区用 `env(safe-area-inset-*)`

---

## 视觉基准（当前项目锁定）

> 本文档当前锁定「报纸/社论（Editorial）」风格，适用于 Pinix 所有 Clip。

- **底色**：米白/纸张感，`oklch(0.98 0.004 90)`，禁用纯白 `oklch(1 0 0)`
- **字色**：近黑 `oklch(0.12 0 0)`
- **分割线**：浅灰 `oklch(0.88 0 0)`，细线（1px），无阴影
- **彩色**：仅用于状态指示（未读点、播放激活），使用极淡红 `oklch(0.55 0.18 25)`
- **标题字体**：Playfair Display（衬线），大字加粗
- **正文字体**：Inter
- **圆角**：`--radius: 0`（方正感，无圆角）
- **卡片**：不用卡片阴影，用细横线分割

**随系统 dark/light 切换**：必须同时生成 light 和 dark 两套 token，通过 `prefers-color-scheme` 媒体查询切换：
```css
/* light mode（默认） */
@theme { --color-background: oklch(0.98 0.004 90); /* ... */ }

/* dark mode */
@media (prefers-color-scheme: dark) {
  @theme { --color-background: oklch(0.12 0 0); /* ... */ }
}
```

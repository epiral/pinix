# Pinix Vision

Pinix 的完整愿景，从核心洞察到增长飞轮的六层框架。

---

## L0 Insight: Agent 今天只服务"精英"

**AI 的鸿沟不是模型的可及性（GPT-4、Claude 谁都能用），而是使用模型的能力。**

使用 Agent 需要：好的 prompt、工具设置、纠错、领域知识。这构成了一个"智力税"，把普通人挡在外面。

**Pinix 的核心愿景：让 Agent 普适化。** 谁能消除这个鸿沟，谁就打开最大的市场。

---

## L1 Abstraction: Clip = 知识 + 能力 + 可组合资产

Clip 是 Pinix 的核心抽象，一个三合一的功能单元：

| 层 | 是什么 | 服务谁 |
|---|---|---|
| **知识** | 面向 Agent 的方法论——什么场景用、怎么用、和谁配合 | Agent（让它知道怎么用） |
| **能力** | 封装的执行逻辑——浏览器操作、设备 API、业务流程 | Agent（让它能执行） |
| **资产** | 可组合、可复用、可沉淀——clip 用 clip，越用越厚 | Builder + 生态 |

Clip 不是 skill prompt（有真实执行能力），Clip 不是 tool（有知识/方法论层）。Clip 比 MCP Tool、LangChain Tool、GPTs 都更完整。

**效果：Clip 把 Agent 的能力边界从 `模型能力 x 用户能力` 变成 `模型能力 x Clip 能力`。**

- 降模型要求（Sonnet 级别就够，不需要 SOTA）
- 省 token（复杂逻辑在 Clip 里跑，不在模型推理里）
- 降用户认知门槛（不需要理解"怎么做"，只要说"做什么"）

---

## L2 Market: Builder <-> User 双边市场

### 两类用户

| | Builder | User |
|---|---|---|
| 是谁 | 懂 code agent，有领域专长 | 普通人，用普通模型 |
| 做什么 | 创造 Clip | 通过 Agent + Clip 受益 |
| 需要什么 | DX、harness、分发、名、利 | 事情被搞定、安装简单 |

**价值转化：Builder 的智慧 -> Clip -> User 的能力。**

---

## L3 Platform: 开箱即用 + 可集成

Pinix 是高度产品化的平台（不只是协议）。

- **Agent-friendly**：MCP / CLI / HTTP，任何 Agent 可接入
- **Builder-friendly**：SDK / harness / create 工具链
- **Marketplace 一体化**：Registry + 发现 + 安装 + 展示（不是分开的东西）
- **本地 Portal 连接官方 Marketplace**：不是孤岛

```
开箱即用                          可集成
├── 装 clip-dock                  ├── pinix mcp -> Claude Code / Cursor
├── 内置 agent-clip               ├── CLI -> 任何 shell 环境
│   (设个 API key 就能用)          ├── HTTP -> 任何 Agent 框架
├── Portal 里直接对话              └── SDK -> 开发者自定义
└── 零依赖，不需要先装别的工具
```

> "什么都没有也能用，有自己生态也能接。"

---

## L4 Product: 任何 Agent + Pinix = AI 私人助理

```
Claude Code + pinix = 私人助理
Cursor + pinix = 私人助理
自建 Agent + pinix = 私人助理
agent-clip (内置) + pinix = 私人助理（开箱即用保底）
```

**Pinix 不替代 Agent，Pinix 赋能 Agent。** Pinix 骑在所有 Agent 的增长上，不和任何 Agent 竞争。

启动策略："加上 Pinix，你的 Agent 就是私人助理。"

> "私人助理"是 Go-to-Market 叙事，不是永久北极星。北极星始终是 L0：Agent 普适化。

### 实现路径

browser + 设备 Edge Clip（macOS / Windows / iOS / Android）= 给任何 Agent 加设备能力

### 用户获取 Clip 的三条路径

1. **Portal Marketplace**（主动搜索）— 类比 App Store
2. **Onboarding 引导**（被动推荐）— 装 clip-dock 时推荐核心 Clip
3. **SNS 传播**（口碑）— 看到推荐，一键安装

---

## L5 Growth: 飞轮

```
更多 Clip -> 更多 User -> 更多 Builder -> 更多 Clip
```

- **护城河**：协议 -> 生态（网络效应，同 MCP 逻辑）
- **能力网络**：clip 组合 clip，生态越来越厚
- **商业模式**：开源建生态（pinixd / CLI / SDK），商业卖方便（Cloud Hub / Marketplace 分成）

---

## 决策检验框架

任何决策都可以用六层来校验：

| 层 | 检验问题 |
|---|---|
| L0 Insight | 它让 Agent 更普适了吗？ |
| L1 Abstraction | 知识、能力、资产三层都覆盖了吗？ |
| L2 Market | 当前瓶颈在 Builder 供给还是 User 需求？ |
| L3 Platform | 符合"开箱即用 + 可集成"吗？ |
| L4 Product | 它让更多 Agent 因为加了 Pinix 而变强了吗？ |
| L5 Growth | 能让飞轮转得更快吗？ |

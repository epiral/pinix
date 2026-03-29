# Core Concepts

Pinix 的核心概念，面向任何初次接触 Pinix 的人。

---

## What is Pinix?

Pinix 让 AI Agent 能使用任何设备的任何能力。

AI 的鸿沟不是模型本身，而是使用模型的能力——知道问什么、怎么设置工具、怎么纠错、怎么拆解任务。Pinix 通过 Clip 生态消除这个鸿沟，让 Agent 普适化。

Pinix 既是基础设施（Hub + 协议），也是产品（Portal + clip-dock）。

---

## What is a Clip?

Clip 是 Pinix 的核心抽象——一个三合一的功能单元：

### 知识层（Knowledge Layer）

面向 Agent 的方法论——什么场景用、怎么用、和谁配合。

Clip 的 manifest 告诉 Agent：我能做什么（commands + patterns）、什么时候该用我（patterns + entities）、怎么调用我（input schema）。Builder 的智慧编码在知识层，降低了对 Agent 模型能力的要求。

### 能力层（Capability Layer）

封装的执行逻辑——浏览器操作、设备 API、业务流程。

复杂逻辑在 Clip 里跑，不在模型推理里。Agent 不需要知道"怎么做"，只需要知道"调谁"。

### 资产层（Asset Layer）

可组合、可复用、可沉淀——clip 用 clip，越用越厚。

Clip 可以依赖其他 Clip，形成能力网络。一个 twitter Clip 可以依赖 browser Clip，一个 agent-clip 可以调度所有已安装的 Clip。

### Clip vs MCP Tool vs GPTs

| | Clip | MCP Tool | GPTs |
|---|---|---|---|
| 知识层 | manifest（patterns, entities, schema） | 无（靠 tool description） | system prompt |
| 能力层 | 完整执行逻辑 | 函数调用 | 受限（Actions） |
| 可组合 | clip 依赖 clip | 不支持 | 不支持 |
| 省 token | 复杂逻辑在 Clip 里 | 取决于实现 | 全在模型推理 |
| 分发 | Registry + pinix add | 无标准分发 | GPT Store |
| 设备能力 | Edge Clip 直接绑定 | 需额外实现 | 不支持 |

---

## What is Hub?

Hub 是路由中心，所有 Clip 在 Hub 上被发现和调用。

- Hub 是唯一路由器。Agent 通过 Hub 调用 Clip，不直接连 Clip。
- Hub 管理 alias 分配——每个 Clip 在 Hub 上有唯一别名。
- `pinixd --port 9000` 启动本地 Hub。Cloud Hub（hub.pinix.ai）提供云端版本。

---

## What is a Provider / Edge Clip?

### Edge Clip

Edge Clip = 设备驱动——直接绑定硬件/OS API 的 Clip。

Edge Clip 暴露设备的原始能力：截屏、GPS、Docker、剪贴板、通知、系统信息等。Edge Clip 自己实现 Provider 协议，直接连接 Hub。

clip-dock（如 clip-dock-macos）是设备端产品——一装就暴露这台设备的所有 Edge Clip 能力。

### Provider

Provider 是连接协议——通过 ProviderStream 把 Clip 注册到 Hub、转发 invoke、维护心跳。

- Edge Clip 自己是 Provider
- Runtime Clip（SDK Clip）由 Runtime 管理，Runtime 作为 Provider

### SDK Clip（Runtime Clip）

SDK Clip = 应用/服务——由 Runtime 管理生命周期，通过 `pinix add` 安装，crash recovery。

SDK Clip 不依赖特定硬件，跑在 Runtime 上。示例：twitter clip、todo clip、agent-clip。

---

## What is agent-clip?

agent-clip 是 Pinix 专属 Agent——围绕 Clip 生态设计的 AI Agent。

### 核心原则：Agent 薄，Clip 厚

```
agent-clip 的职责：
  ├── 意图识别 -> 理解用户想做什么
  ├── Clip 选择 -> 从 manifest（patterns/entities/schema）选对 Clip
  ├── 调度执行 -> 调对 Command，传对参数
  ├── 记忆 -> memory clip（跨会话上下文）
  └── 技能 -> clip 就是 skill，不需要内建任何能力

agent-clip 不做的事：
  ├── 不做复杂业务逻辑（在 Clip 里）
  ├── 不做设备操作（在 Edge Clip 里）
  └── 不需要 SOTA 模型（Sonnet 级别就够，因为只做路由）
```

### 为什么 Sonnet 级别就够？

Clip 的知识层（manifest）已经告诉了 Agent：我能做什么、什么时候该用我、怎么调用我。Agent 只要读懂 manifest + 匹配意图 + 正确调用，不需要推理复杂逻辑。

**Builder 的智慧编码在 Clip 的知识层，降低了对 Agent 模型能力的要求。**

### 工作流程示例

```
用户："帮我搜一下推特上 AI 的热门"
  |
agent-clip：
  1. 读 manifest -> twitter clip 有 search command，pattern 匹配
  2. 构造参数 -> {query: "AI", sort: "hot"}
  3. 调用 twitter.search -> Hub 路由 -> twitter clip 执行
  4. 返回结果给用户
  |
所有复杂逻辑（打开浏览器、滚动、解析）都在 twitter clip 里
Agent 不需要知道这些
```

---

## Builder vs User

| | Builder | User |
|---|---|---|
| 是谁 | 懂 code agent，有领域专长 | 普通人，用普通模型 |
| 做什么 | 创造 Clip | 通过 Agent + Clip 受益 |
| 需要什么 | DX、harness、分发、名、利 | 事情被搞定、安装简单、不需要技术知识 |

### Builder 旅程

理解 Clip 概念 -> `pinix create` 脚手架 -> 开发 -> `pinix add` 测试 -> `pinix publish` 发布

### User 旅程

安装 clip-dock（设备能力自动暴露）-> `pinix add` 场景 Clip -> Agent 里说一句话 -> 事情被完成了

**价值转化：Builder 的智慧 -> Clip -> User 的能力。**

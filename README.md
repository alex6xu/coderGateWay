# CodeGateway

> **个人云端 AI 操作系统** — 一处部署，随处使用。
> **A personal, cloud-native AI operating system** — deploy once, use everywhere.

集大模型网关、Coding Agent、对话知识库于一体。跑在你自己的云端服务上，桌面 / Web / 手机 / 终端多端接入，把你与 AI 的每一次对话都留存、分类、沉淀为可检索的长期记忆；在服务器上执行长程 coding 任务，边缘端随时查看进度。

A unified **LLM gateway + coding agent + conversation knowledge base**. Run it on your own cloud server, access it from desktop / web / mobile / terminal. Every conversation is persisted, classified, and distilled into searchable long-term memory. Long-running coding tasks execute on the server while you check progress from any edge device.

---

## 目标愿景 / Vision

CodeGateway 想解决的核心问题：**让"我和 AI 的关系"从一次性、碎片化、锁死在单个 App 里，变成持续、连贯、属于自己的资产。**

1. **一处部署，随处使用** — 单二进制 + SQLite，部署在云端，桌面 / Web / 手机 / 终端共享同一份状态。
2. **全量对话资产化** — 每条消息落库，自动分类打标，构建可检索的个人知识库。
3. **云端长程任务** — 复杂 coding / 研究任务提交到服务器异步执行，关机也不中断，边缘端轮询进度。
4. **三级记忆协调** — 妥善处理**短期记忆**（当前对话窗口）、**长期记忆**（跨会话 FTS 检索）、**大模型 context**（有限窗口预算）三者的关系。
5. **定时自主整理** — 定时任务在后台归纳、摘要、重组内容，让记忆越用越有序。

> 这是一份**愿景 + 现状**文档。下方的能力矩阵如实标注了每项功能的成熟度（✅ 已可用 / 🚧 进行中 / 📋 规划中），不夸大已完成的部分。

---

## 能力矩阵 / Capability Matrix

图例：✅ 已可用（wired & working）· 🚧 进行中（partial / stub）· 📋 规划中（planned）

### API 网关 / Gateway

| 能力 | 状态 | 说明 |
|---|---|---|
| OpenAI 兼容 relay (`/v1/chat/completions`) | ✅ | 端到端可用，含流式 |
| 多 Provider | ✅ | OpenAI · Claude · DeepSeek · Ollama · MiMo · GLM · Agnes · Custom |
| 路由 failover（多渠道自动切换） | ✅ | route-profile 失败切换（见 `docs/multi-channel-failover-routing.md`） |
| 多账号隔离 | ✅ | channel / session 按 `user_id` 隔离，有测试覆盖 |
| Gemini Provider | 🚧 | 仅 ListModels，`ChatCompletion` 未实现 |
| Claude / Gemini 原生 endpoint | 🚧 | `handleClaudeMessages` / `handleGemini` 为 501 stub |
| 格式转换（响应侧） | 🚧 | `ConvertResponse` 为 TODO stub，未接入请求路径 |
| Token 计费 / 配额扣减 | 🚧 | 计费数学存在，但**未在请求路径强制执行**；仅记录 usage |

### Coding Agent

| 能力 | 状态 | 说明 |
|---|---|---|
| Agent 循环（LLM + 工具循环） | ✅ | `cmd/server/coder_agent.go`，最多 N 轮（`agent.max_iterations`），检测并执行工具调用 |
| 工具系统（chroot 沙箱） | ✅ | `bash · read_file · write_file · list_directory · search_files · grep`，限制在工作区根目录 |
| 云端工作区 | ✅ | Code 页上传本地目录到云端，Agent 在隔离目录读写 |
| GitHub clone / pull / push | ✅ | Code 工作区对接远程仓库 |
| 后台长程任务 | ✅ | 任务 worker + REST 提交/轮询（`POST /v1/agent/tasks`、`GET .../:id`），重启可恢复 |
| 并行只读工具 · prompt 缓存 · 流式事件 | ✅ | agent 循环内置；thinking/reasoning 可展示 |
| 工具循环超限处理 | 🚧 | 触顶报 `max tool iterations reached`；缺优雅收束 / 续跑 / 用户可配提示（见 TODO） |
| `edit` / `apply_patch` 增量编辑 | 📋 | 目前 write 为整文件覆盖 |
| 子 Agent（Actor 模式） | 📋 | `internal/agent/actor/` 已有骨架但**零调用者**，未接入 |

### 记忆与知识库 / Memory & Knowledge Base

| 能力 | 状态 | 说明 |
|---|---|---|
| 全量对话存储 | ✅ | 每条消息落库（`messages` 表），角色标记 |
| FTS5 全文记忆 | ✅ | session / project / global 三层，bm25 排序检索 |
| Context 构建器 | ✅ | 滑动窗口（turn 上限）+ token 预算截断 + checkpoint 注入 |
| 会话 checkpoint | ✅ | 每 N 轮生成，带 cutoff 标记 |
| 对话分类打标 | ✅ | `question_tags` / `message_tags`，**关键词/正则**分类 |
| LLM 摘要（非截断折叠） | 🚧 | 当前折叠为**截断式**（拼接消息行），非 LLM 摘要 |
| 短期/长期记忆显式分层 | 🚧 | 目前靠 `type='checkpoint'` 隐式区分，无显式冷热分层 |
| 本地知识图谱 / 向量库 | 📋 | 规划接入 graphify / Obsidian / sqlite-vec 等（见 TODO） |
| 定时自主整理 | 📋 | Cron 调度器 `calculateNextRun` 为 stub 且未接入 |

### 多端接入 / Multi-Client

| 能力 | 状态 | 说明 |
|---|---|---|
| Web（React + TS + Tailwind） | ✅ | Chat / Coder / Sessions / Tags / Tasks / Channels / Accounts 等页面 |
| WebSocket 实时对话 | ✅ | `/ws`，流式 chat |
| 跨端查看任务进度 | ✅ | 任一客户端提交的任务可被任意认证客户端轮询（DB 共享状态） |
| 页面导航后恢复会话 | ✅ | Code/Chat 用 localStorage 记住当前 workspace/session，并重连进行中的 run |
| **切换会话后恢复完整内容** | 🚧 | 切换到另一会话时，消息 / 工具步骤 / 进行中 run 的还原仍不完整（见 TODO） |
| Channels 请求日志 | ✅ | 网关管理页「请求日志」Tab，审计 LLM 请求/响应 |
| 边缘端看**实时步骤** | 🚧 | 任务 worker 不推 WebSocket，跨端只能轮询终态，看不到实时步骤流 |
| 移动端 / 响应式 | 🚧 | 桌面优先，响应式适配极少 |
| Telegram Bot | 📋 | 仅 config struct，无接线（`internal/platform/` 为死代码） |
| 终端 TUI | 📋 | 未接入 |

### 认证与部署 / Auth & Deploy

| 能力 | 状态 | 说明 |
|---|---|---|
| 用户认证 | ✅ | bcrypt + 不透明 session token（**非 JWT**），7 天 TTL |
| 默认管理员 | ✅ | `admin` / `admin123`，可用 `CODEGATEWAY_ADMIN_PASSWORD` 覆盖 |
| Docker / docker-compose | ✅ | 基础镜像可构建（静态二进制） |
| 一处部署随处使用（云端故事） | 🚧 | 前端未 embed 进二进制，需单独构建；`deploy/` 目录为空，无 systemd/云脚本 |

---

## 三级记忆模型 / Three-Tier Memory Model

CodeGateway 的核心设计假设：**大模型的 context 是稀缺、昂贵、有限的**，必须在"短期上下文"与"长期知识"之间建立清晰的搬运机制。

```
┌─────────────────────────────────────────────────────────────┐
│  短期记忆 Short-term  →  大模型 Context  ←  长期记忆 Long-term │
│  （当前对话滑窗）         （有限 token 预算）    （FTS 全库检索）  │
└─────────────────────────────────────────────────────────────┘
        │                        ↑                     ↑
   最近 N 轮原文          按预算截断/折叠         bm25 检索相关片段
   (HistoryMaxTurns)     (ContextBudgetTokens)   (SearchRelevant)
        │                        │                     │
        └──── 每 N 轮 checkpoint 摘要 ──→ 沉淀为长期记忆 ──┘
```

- **短期记忆**：最近 N 轮对话原文，直接进 context（`HistoryMaxTurns`，默认 8 轮）。
- **大模型 Context**：受 `ContextBudgetTokens`（默认 8000）约束，超预算时折叠早期消息并注入 checkpoint 摘要。
- **长期记忆**：FTS5 全文库，按 bm25 相关度检索历史知识片段注入 context（`SearchRelevant`，带分数下限）。
- **搬运机制**：每 N 轮触发 checkpoint，把滑出窗口的对话摘要沉淀到长期记忆。

> **现状诚实说明**：checkpoint 折叠目前是**截断式拼接**而非 LLM 摘要；短期/长期无显式冷热分层。把折叠升级为真正的 LLM 摘要、并引入显式记忆分层，是路线图的核心项（见下）。

---

## 技术栈 / Tech Stack

| 组件 | 技术 |
|------|------|
| 后端 Backend | Go 1.22+ · Gin |
| 数据库 Database | SQLite（`modernc.org/sqlite`，纯 Go，含 FTS5） |
| 前端 Frontend | React · TypeScript · Tailwind · Vite |
| Agent 运行时 | 自研 coder loop（`cmd/server/coder_agent.go`） |

---

## 快速开始 / Quick Start

```bash
# 克隆并编译 / clone & build
git clone https://github.com/alex/codegateway.git
cd codegateway
CGO_ENABLED=0 go build -o codegateway ./cmd/server/

# 运行（默认配置）/ run
./codegateway

# 自定义配置 / custom config
CODEGATEWAY_CONFIG=/path/to/config.yaml ./codegateway
```

前端（独立构建，暂未 embed）/ Frontend (built separately, not yet embedded):

```bash
cd web && npm install && npm run dev   # 开发 dev
cd web && npm run build                # 生产构建 → web/dist
```

### 配置 / Configuration

编辑 `codegateway.yaml`。Agent 相关常用项：

```yaml
server:
  host: "0.0.0.0"
  port: 8080

database:
  driver: "sqlite"
  dsn: "./data/codegateway.db"

agent:
  # LLM↔工具循环的最大轮次（每轮可含多个并行工具调用）
  # 简单问答 8–12；一般改代码 20–32；大范围探索 40–50
  # 触顶会报: max tool iterations reached
  max_iterations: 24
  max_tokens: 4096
  context_budget_tokens: 8000

gateway:
  enabled: true
  routing:
    strategy: "auto"   # auto · cost · latency · quality

platforms:
  web:
    enabled: true
```

> `max_iterations` 是 **LLM 轮次**，不是工具调用总次数。额度越大，耗时与费用越高；若模型在空转（反复读同一文件），加额度不如把任务说得更具体。

---

## API 速览 / API Overview

```bash
# OpenAI 兼容网关 / OpenAI-compatible gateway
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}'

# 模型列表 / list models
curl http://localhost:8080/v1/models

# 提交后台任务 / submit a background task
curl -X POST http://localhost:8080/v1/agent/tasks \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"prompt":"重构 auth 模块并跑测试"}'

# 轮询任务进度（跨端可用）/ poll task progress (works from any client)
curl http://localhost:8080/v1/agent/tasks/<id> \
  -H "Authorization: Bearer <token>"
```

### 认证 / Auth

```bash
# 登录（默认 admin / admin123）/ login
curl -X POST http://localhost:8080/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}'

# 当前用户 / current user
curl http://localhost:8080/v1/auth/me -H "Authorization: Bearer <token>"
```

受保护接口需携带 `Authorization: Bearer <token>`。管理员可用 `X-Account-ID` 代管其他账号。
Protected endpoints require `Authorization: Bearer <token>`. Admins can impersonate via `X-Account-ID`.

---

## 项目结构 / Project Layout

```
codegateway/
├── cmd/server/            # ★ 主服务入口（真正运行的实现）
│   ├── coder_agent.go     #   Agent 循环
│   ├── handlers.go        #   网关 relay + 路由 failover
│   ├── agent_tasks.go     #   后台任务 REST
│   ├── websocket.go       #   /ws 实时对话
│   └── auth_handlers.go   #   认证
├── internal/
│   ├── gateway/           # 网关（relay/convert/router/billing — 部分为参考实现/stub）
│   ├── agent/
│   │   ├── memory/        # FTS5 记忆服务 ✅
│   │   ├── promptctx/     # Context 构建器 ✅
│   │   ├── task/          # 任务 worker + store ✅
│   │   ├── tags/          # 关键词分类 ✅
│   │   ├── cron/          # 定时调度（stub，未接入）🚧
│   │   ├── actor/         # 子 Agent（死代码，未接入）📋
│   │   └── agent.go       # 旧版 Agent 骨架（死代码）
│   ├── tool/              # chroot 工具沙箱 ✅
│   ├── provider/          # LLM Provider 抽象
│   ├── session/           # 会话 + 认证存储
│   └── db/                # SQLite + migrations（含 FTS5）
├── web/                   # React 前端（Chat / Coder / ...）
├── docs/                  # 设计文档
├── deploy/                # 部署配置（当前为空）
├── Dockerfile · docker-compose.yml
└── codegateway.yaml
```

> **注意**：`internal/agent/agent.go`、`internal/agent/actor/`、`internal/platform/` 目前是**未接入的骨架/死代码**——真正运行的实现全部在 `cmd/server/`。清理或接入这些包是路线图的一部分。

---

## 开发路线图 / Roadmap

按"离愿景最近 + 投入产出比"排序。

### 近期 / Near-term（打磨已有雏形）

- **[会话] 切换会话后完整恢复内容** — 在会话列表切换时还原消息、工具步骤、进行中 run 的 SSE；与「页面导航恢复」互补。
- **[Agent] 工具循环超限体验** — 默认额度调到合适区间；触顶时给出可续跑/摘要收束，而不是硬失败。
- **[记忆] LLM 摘要替代截断折叠** — 把 checkpoint 折叠从"拼接消息行"升级为真正的 LLM 摘要，显著提升长期记忆质量。
- **[记忆] 显式短期/长期分层** — 引入冷热分层与"记忆晋升"机制，明确 context 搬运策略。
- **[Agent] `edit` / `apply_patch` 工具** — 从整文件覆盖升级为增量编辑，减少 token 与出错面。
- **[多端] 任务进度实时推送** — 让任务 worker 向 WebSocket 推步骤流，边缘端看到实时进度而非仅终态。
- **[网关] 计费/配额真正接入请求路径** — 让 billing 在请求流中强制扣减，而非仅记录。

### 中期 / Mid-term（补齐愿景关键项）

- **[知识库] 本地知识图谱 + 向量检索** — 在 FTS 之上叠加 sqlite-vec；探索 graphify / Obsidian 互通，形成可浏览的个人知识图。
- **[定时] 落地 Cron 调度** — 实现真正的 cron 表达式解析并接入，支撑"定时自主整理"（每日摘要、记忆重组、知识库归档）。
- **[网关] 格式转换 + Claude/Gemini 原生 endpoint** — 补全 `ConvertResponse`、Gemini provider、Claude messages endpoint。
- **[多端] Telegram / 移动端** — 接入 Telegram bot，前端响应式适配移动端。
- **[部署] "一处部署随处使用"** — 前端 embed 进二进制，补 `deploy/`（systemd + 云脚本 + 一键部署）。

### 远期 / Long-term（放大愿景）

- **[Agent] 子 Agent 调度** — 接入 Actor 模式，支持并行子任务分解。
- **[知识库] LLM 分类替代关键词** — 用 LLM 对对话做主题聚类与自动归档。
- **[Agent] 自我进化** — 从使用模式中学习并沉淀技能。

---

## 待处理任务（技术债 / TODO）

> 从代码库审计与近期使用反馈中提炼的可执行清单，供后续 session 直接认领。

### 优先待办（近期反馈）

> 展开说明见 [`docs/todos-session-knowledge-tool-loop.md`](docs/todos-session-knowledge-tool-loop.md)。

- [ ] **切换会话后恢复内容** — 页面导航恢复（localStorage + run 重连）已有；会话列表切换时仍需完整还原：历史消息、工具步骤面板、进行中/最近 run 事件流。涉及 `CoderPage` / `ChatPage`、session API、`sessionPersist`。
- [ ] **构建本地知识库 / 知识图谱** — 在现有 FTS5 之上规划：sqlite-vec 向量检索、graphify 风格实体关系图、可选 Obsidian vault 导入/导出。目标：对话与代码上下文沉淀为可浏览、可检索的本地知识资产。
- [ ] **工具调用循环超限** — 现状：触顶直接 `max tool iterations reached`。改进方向：
  - 文档与默认值：一般改代码建议 `max_iterations: 24`～`32`（`codegateway.yaml` 已调至 24）
  - 产品：触顶时返回已产生的过程输出 +「继续」续跑，或强制让模型基于已有工具结果做摘要收束
  - 可观测：Channels「请求日志」已可审计每轮 LLM 调用，便于排查空转

### 清理 / 修复

- [ ] 清理死代码：`internal/agent/agent.go`、`internal/agent/actor/`、`internal/platform/`（Telegram/Web adapter 全无调用者）
- [ ] 修复测试缺陷 `TestWorkerFailsTaskWhenOwnedWorkspaceCannotBeLoaded`（`worker_test.go:84`，缺 `ProviderForTask`+`Run` 配置，触发 fail-fast）
- [ ] 网关核心零测试覆盖：`relay` / `relay/convert` / `relay/router` / `gateway/proxy` / `gateway/billing` 全部 `[no test files]`

### 功能落地

- [ ] Cron `calculateNextRun` 真实 cron 解析 + 接入 `cmd/server`（`cron.go:106`）
- [ ] `ConvertResponse` 补全（`convert/converter.go:44`）
- [ ] Gemini `ChatCompletion` 实现（`gemini.go:26`）
- [ ] `handleClaudeMessages` / `handleGemini` 从 501 stub 补全（`handlers.go:1198,1204`）
- [ ] 计费扣减接入请求路径（当前仅 `logUsage`，无配额强制）
- [ ] 任务进度 → WebSocket 实时推送
- [ ] checkpoint 折叠升级为 LLM 摘要（`promptctx/builder.go` FoldMessages）
- [ ] 前端 `embed` 进 Go 二进制，补 `deploy/`

---

## 参考项目 / Inspirations

- **new-api** — 网关架构、Channel 管理、计费
- **sub2api** — 订阅配额分发、智能调度
- **MiMo-Code** — Agent 循环、记忆系统、技能、任务树
- **OmniRoute** — 多账号 failover 路由、熔断、冷却（见 `docs/multi-channel-failover-routing.md` 对比）
- **hermes-agent** — 消息平台集成、自我进化
- **crush** — Go TUI、SQLite 集成

## 许可证 / License

MIT License

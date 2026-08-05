# Session Run 后台执行与重连续流设计

**日期：** 2026-08-05  
**范围：** Web Agent Chat / Coder（`POST /v1/agent/chat`）关页续跑、重连续流、排队与 tool 边界注入。  
**非目标：** Agent Tasks 页重构、多实例分布式 claim、硬取消飞行中的上游 HTTP。

## 问题

1. 当前对话执行绑在 HTTP/SSE 请求生命周期上；浏览器关闭后 `Request.Context` 取消，Coder/Chat 循环易中断。  
2. 历史 messages 已持久化，但**进行中**的 delta / tool_step 未作为可重放事件流保存，重连无法「接上直播」。  
3. 进行中再发消息没有明确语义（拒绝 / 取消 / 排队 / 注入）。

## 目标

- Web 关掉后，**服务端继续**跑完当前一轮（含 tool 循环）。  
- 新 Web 端打开同一 session：恢复历史对话 + **从断点续订**进行中 run 的事件流。  
- 同一 session：**排队**后续用户消息；若当前 run 正在 tool 循环，在**每次 tool 执行完、下一次调模型前**把 inbox 中的消息注入本轮上下文。

## 非目标

- 改造 `/v1/agent/tasks` 产品页（可后续对齐同一事件模型）。  
- 多副本下的 run claim / 租约（一期默认单进程 worker）。  
- 保证立即取消已发出的上游 LLM 请求（cancel 为协作式检查点）。  
- 进程崩溃后自动从半截 tool 循环无缝续跑。

## 架构概览

```text
Browser                    API                         Worker / Run runtime
   |                        |                                |
   | POST /agent/chat       |  persist user msg              |
   |----------------------->|  enqueue inbox or start run    |
   |  {session_id,run_id}   |------------------------------->|
   |                        |                                | loop: LLM + tools
   | GET .../runs/:id/events|  append events (seq)           |
   |  ?after_seq=N  (SSE)   |<-------------------------------|
   |<-----------------------|  replay then live              |
   |  (disconnect OK)       |                                | continues
   |  reconnect after_seq   |                                |
   |----------------------->|  same event log                |
```

**原则：** SSE 只是事件订阅；**权威状态**在 DB（run + events + messages）。

## 数据模型

### `session_runs`

| 字段 | 说明 |
|------|------|
| `id` | run UUID |
| `session_id` | 所属会话 |
| `user_id` | 租户 |
| `workspace_id` | 可选；coder 模式需要 |
| `mode` | `chat` / `coder` |
| `status` | `queued` \| `running` \| `succeeded` \| `failed` \| `cancelled` |
| `trigger_message_id` | 启动本轮时的主 user message（首条） |
| `error` | 失败原因 |
| `last_seq` | 已写入的最大 event seq |
| `created_at` / `started_at` / `finished_at` | 时间戳 |

约束（应用层）：同一 `session_id` 最多一个 `queued|running` 的 active run（排队消息进 inbox，不并行开多个 running）。

### `session_run_events`

| 字段 | 说明 |
|------|------|
| `run_id` | 所属 run |
| `seq` | 从 1 递增，run 内唯一 |
| `type` | `meta` \| `delta` \| `tool_step` \| `user_injected` \| `done` \| `error` |
| `payload` | JSON（content / step / model 等） |
| `created_at` | 时间 |

订阅方使用 `after_seq` 做增量重放；payload 形状与现有 `AgentEvent` 对齐，便于前端少改。

### `session_run_inbox`

| 字段 | 说明 |
|------|------|
| `id` | UUID |
| `session_id` | 会话 |
| `run_id` | 可选；注入到哪一轮；为空表示等待新开 run |
| `message_id` | 已写入 `messages` 的 user 行 |
| `content` | 冗余便于 worker 读取 |
| `status` | `pending` \| `injected` \| `consumed_as_run` |
| `created_at` | 时间 |

## 生命周期

### 发送消息（`POST /v1/agent/chat`）

1. 鉴权；创建或校验 `session_id`。  
2. 用户文本写入 `messages`（role=user）。  
3. 若存在 active run（`queued|running`）：  
   - inbox 插入 `pending`，绑定该 `run_id`（若尚无 run 则仅 session 级 pending）。  
   - 返回 `{ session_id, run_id, status: "accepted_queued" }`。  
4. 否则：  
   - 创建 `session_runs`（`queued`→立即由 worker 置 `running`），启动后台执行。  
   - 返回 `{ session_id, run_id, status: "running" }`。  
5. **不再**用客户端 `Request.Context` 驱动 LLM；执行 ctx = `context.WithoutCancel(parent)` 或进程级 worker ctx（可带超时/取消令牌）。

可选：若 `stream=true`，响应可升级为对该 `run_id` 的 SSE（`after_seq=0`），与显式订阅接口等价。

### 后台执行循环

与现有 `runCoderAgent` / chat completion 同源逻辑，差异：

1. 每次调用上游前：`EnsureWithinBudget`（保持现有上下文管控）。  
2. **Tool 边界注入点**：一批 tool 执行完毕、组装下一次请求前：  
   - drain 本 run 的 `pending` inbox（按 `created_at`）。  
   - 将内容追加进 in-memory messages（建议 role=user，或带前缀的 system/user 约定一种并写死）。  
   - 标记 inbox `injected`；写 event `user_injected`；确保 messages 表已有对应用户行（发送时已写则不再插）。  
3. 每个对外事件：先 **append event（分配 seq）**，再 fan-out 给当前订阅者。  
4. 终态：写 assistant `messages`；event `done` 或 `error`；run → `succeeded`/`failed`。  
5. 若 session 仍有未绑定/pending 且应新开轮的 inbox：启动下一个 run（FIFO）。

### 重连

1. `GET` session messages → 渲染历史。  
2. `GET` session 得 active run + `last_event_seq`（或客户端本地 seq）。  
3. `GET /v1/agent/runs/:id/events?after_seq=N`：  
   - 先重放 DB 中 `seq > N`；  
   - 再挂 live（或短轮询）；  
   - 客户端更新本地 seq。  
4. 若 run 已终态：重放至 `done`/`error` 即可，无需常驻连接。

### 进程重启

- 启动时将所有 `running` 标为 `failed`（或 `interrupted`），error 文案说明需用户重试。  
- **不**自动续跑半截 tool 循环（与 `agent_tasks.RecoverInterrupted` 一致）。  
- `pending` inbox 保留；一期**仅在用户再次 `POST /v1/agent/chat` 时**，若无 active run 则开新 run 消化 pending（避免打开页面就静默耗额度）。

## API 一览

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/agent/chat` | 入队或启动 run；返回 session/run |
| GET | `/v1/agent/sessions/:id` | 摘要 + active run + last_seq |
| GET | `/v1/agent/sessions/:id/messages` | 历史消息（已有则复用/对齐） |
| GET | `/v1/agent/runs/:id/events?after_seq=` | SSE 重放 + 订阅 |
| POST | `/v1/agent/runs/:id/cancel` | 可选；queued 直接取消；running 设取消标志，在检查点退出 |

鉴权：run/session 必须属于当前 account。

## 前端行为

1. 发消息后以 `run_id` 订阅事件；UI 状态「生成中」绑定 run status，而非 fetch 是否结束。  
2. 卸载页面不 Abort 服务端（可 Abort 本地 reader，不影响 run）。  
3. 进入页面：拉 messages + active run → 续订。  
4. 进行中再发：同一 API；展示「已排队 / 将在下一工具轮后注入」；处理 `user_injected` 提示。

## 错误处理

| 场景 | 行为 |
|------|------|
| 上游 LLM 失败 | event `error`；run `failed`；已产生的 tool 副作用不回滚（与现网一致） |
| 上下文超预算 | 沿用 `EnsureWithinBudget` 错误路径 |
| 订阅无关 run | 404 |
| 重复启动 | 有 active run 则只 inbox，不报错 |

## 测试要点

- 单元：inbox drain 顺序；seq 单调；同一 session 单一 active run。  
- 集成：启动 run → 断开订阅 → 继续写 events → 用 `after_seq` 重放完整。  
- 注入：running 中 POST 第二条消息 → 下一 LLM 请求 messages 含注入内容且有 `user_injected` event。  
- 重启：running → failed；pending inbox 仍在。

## 验收标准

1. Coder 发消息后关闭 Web，服务端跑完并写入最终 assistant message。  
2. 新开 Web 进入同 session：历史完整；若仍 running，可继续看到后续 delta/tool_step。  
3. running 期间再发消息：不启动第二 running；tool 边界后模型能看到新内容。  
4. HTTP 客户端断开不取消 run。

## 实现顺序建议

1. DB migration + store（runs / events / inbox）  
2. Run worker：从现有 `handleAgentChat` / `runCoderAgent` 抽出，事件持久化  
3. 订阅 API + chat 入队语义改造  
4. 前端 Coder（及 Chat）重连与 inbox UX  
5. RecoverInterrupted + 测试

# Agent 实现对比研究：pigo / MiMo-Code / hermes-agent

> 目的：横向对比上层目录中三个 agent 项目的核心实现，提炼可复用的优秀设计，并结合本项目 `codegateway`（Go，LLM 网关）给出可借鉴的落地建议。
>
> 调研对象：
> - **pigo**（Go）：`/Users/alex/Documents/github/pigo` —— 《用 Go 编写 pi Agent》配套实现，教学级清晰，与本项目同为 Go，借鉴价值最高。
> - **MiMo-Code**（TypeScript）：`/Users/alex/Documents/github/MiMo-Code`（基于 opencode 架构）—— 上下文管理最成熟。
> - **hermes-agent**（Python）：`/Users/alex/Documents/github/hermes-agent` —— 工程化 / 生产踩坑沉淀最丰富。

---

## 0. 总览对比

| 维度 | pigo (Go) | MiMo-Code (TS) | hermes-agent (Python) |
|---|---|---|---|
| 定位 | 教学级简洁 | 产品级完整 | 生产级重工程 |
| 代码规模 | 精炼（agentcore ~1.4k 行） | 中大（session 层丰富） | 巨大（单文件数千行） |
| 主循环 | `internal/runtime/loop.go` 两层循环 | `session/processor.ts` | `agent/conversation_loop.py` (~5.3k 行) |
| 上下文压缩 | `runtime/compaction.go` | `session/compaction.ts` + `overflow.ts`（四级压力分级）★ | `context_compressor.py` (~3.2k) + `curator.py` 记忆策展 ★ |
| 工具重试 | `agenttool/tool_retry.go`（分级）★ | `session/retry.ts`（单一真相源）★ | `error_classifier.py`（细粒度 failover）★ |
| 并行工具 | `agenttool/batch_executor.go` ★ | 支持 | 支持 |
| 子代理 | `runtime/subagent.go` 双隔离★ | 支持 | `IterationBudget` 父子预算★ |
| 死循环护栏 | 基础 | 基础 | `tool_guardrails.py` before/after★ |
| Prompt Cache | — | 有 | `prompt_caching.py` 4 断点★ |
| 完成校验 | — | — | `verification_stop.py` 编辑→验证闭环★ |

★ = 特别值得学习的实现。

---

## 1. Agent 主循环（Agentic Loop）

### pigo —— 清晰的两层循环
`internal/runtime/loop.go`。外层是「对话轮次」，内层是「工具调用回填」。终止条件明确：LLM 不再返回 tool_call 即结束；有硬性最大迭代上限防跑飞。

**好在哪**：结构最容易读懂，是理解 agentic loop 的最佳范本。循环边界、终止条件、迭代上限三者分离清晰。

### hermes —— 迭代预算（IterationBudget）
`agent/iteration_budget.py`。不是简单的「最大轮次」计数，而是把「迭代次数」当成一种**可分配、可退还的预算资源**：

- 父 agent 预算（如 90），子 agent 从父预算**扣减**（如 50）；
- subagent 结束后未用完的预算可 **refund** 回父；
- 防止 subagent 递归调用把全局预算耗尽。

**好在哪**：解决了多层 subagent 场景下「谁来限制总步数」的难题。相比 pigo 的单层 maxIter，更适合复杂多 agent 协作。

### 借鉴建议（codegateway）
本项目是网关，本身没有 agentic loop，但可提供**服务端侧的迭代/预算护栏**：为经过网关的一次会话统计工具调用轮次、透传/限制 `max_tool_rounds`，防止下游 agent 失控刷量。

---

## 2. 工具系统与重试

### pigo —— 分级重试 + 批量并行 ★
- `internal/agenttool/tool_retry.go`：区分**工具级可重试错误**（如临时 IO、网络抓取失败）与**不可重试错误**（参数错误、文件不存在），只对前者退避重试。
- `internal/agenttool/batch_executor.go`：同一轮里多个**相互独立**的工具调用并行执行，有依赖或写冲突的串行化。

**好在哪**：把「连接级重试」和「工具级重试」两个层次分开，避免对参数错误做无意义重试；并行执行显著降低多工具轮次的墙钟时间。

### MiMo-Code —— 重试单一真相源 ★
`session/retry.ts`：所有「是否可重试」判断收敛到一个 `isRetryableTransientError()`：
- 尊重上游 `retry-after` / `retry-after-ms` 响应头；
- 对 4xx 中的 `upstream_error` 也重试（网关转发场景常见）；
- 统一指数退避 + 抖动。

**好在哪**：重试逻辑不散落各处，唯一入口，易审计、易调参。**对网关项目极具参考价值**。

### hermes —— 细粒度失败分类 ★
`agent/error_classifier.py`：用 `FailoverReason` 枚举把错误细分：`auth / rate_limit / upstream_rate_limit / overloaded / context_length / billing / timeout / ssl ...`，并处理大量生产歧义：
- **429 到底是「限流」还是「服务过载」**——分别对应不同退避与 failover 策略；
- **5xx 但响应体是格式错误** → 不该重试；
- **SSL 证书验证失败**（不可重试）vs **SSL 握手抖动**（可重试）。

**好在哪**：这是真实生产环境踩坑的沉淀，直接决定 failover 质量。

### 借鉴建议（codegateway）★★★
本项目刚重写了 `cmd/server/failover.go` + `internal/provider/errors.go`。强烈建议：
1. 用 hermes 式**枚举化 `FailoverReason`** 替代粗粒度布尔判断，区分「限流 vs 过载 vs 计费 vs 上下文超限」；
2. 采纳 MiMo 的**单一真相源** `isRetryable()`，并**尊重上游 `retry-after` 头**驱动熔断冷却时长（而非固定 backoff）；
3. 对 4xx `upstream_error` 场景做 failover（转发网关的典型情形）。

---

## 3. 上下文管理（Context Management）

### MiMo-Code —— 四级压力分级 + prune/compaction 分离 ★★
`session/compaction.ts` + `session/overflow.ts`：
- **四级压力分级** `pressureLevel` 0–3，按 token 占用率 0.5 / 0.7 / 0.85 分档递进处理；
- **usable 窗口计算**：从 context window 预留 output token + buffer，`OUTPUT_CAP` 防止超大输出配置挤占输入；
- **prune 与 compaction 分离**：
  - *prune*：只擦除旧的工具输出（保留最近 2 轮、保护 skill 类工具结果），轻量、不丢对话语义；
  - *compaction*：真正的 LLM 总结，输出**结构化模板**（Goal / Instructions / Discoveries / Accomplished / Files）。

**好在哪**：分级 + 分离让上下文治理「按需、渐进、可逆」，而不是一到阈值就粗暴总结。

### hermes —— 记忆策展（Curator）★
`agent/curator.py` + `memory_manager.py`：不仅压缩当前对话，还把有价值的信息**策展为长期记忆**，跨会话复用。

### 借鉴建议（codegateway）
网关可提供**上下文预算可观测性**：在响应中回填 token 用量分级、预警下游「接近窗口上限」；甚至提供可选的服务端 prune（擦除历史 tool 输出）以帮助瘦身请求。

---

## 4. Provider 抽象与流式

### pigo —— 双失败模型 + 连接级重试 ★
`internal/provider/provider_interface.go` + `transport.go`：统一 Provider 接口对接 OpenAI / Anthropic 等；**连接级重试**（transport 层）与工具级重试解耦；流式 SSE 解析独立封装。

**好在哪**：分层干净，与 codegateway 的 provider 抽象目标一致，可直接对照。

### 借鉴建议（codegateway）★★
本项目 `internal/provider/provider.go` 刚清理了 `Registry`。可参考 pigo 的 `provider_interface.go`：把「协议适配（OpenAI/Anthropic/Gemini 格式互转）」与「传输重试」两层清晰分开，transport 层统一做连接级退避重试。

---

## 5. 子代理（Subagent）

### pigo —— 双隔离模式 ★
`internal/runtime/subagent.go`：支持两种隔离：
- **goroutine 内隔离**：轻量，共享进程，独立上下文；
- **子进程 JSON-RPC 隔离**：强隔离，subagent 崩溃不影响主进程。

**好在哪**：按信任/风险选隔离级别，是 Go 项目实现 subagent 的优秀范式。

### hermes —— 预算下发（见 §1）
subagent 的步数受父预算约束并可退还，避免递归爆炸。

---

## 6. 死循环护栏（hermes 独有）★

`agent/tool_guardrails.py`：`before_call` / `after_call` 钩子：
- 同一工具重复失败 N 次 → **block**，注入合成 tool result 打断；
- 只读工具反复返回**相同结果、无进展** → block。

**好在哪**：agent 最常见的翻车就是死循环刷同一个失败工具。这是低成本高收益的稳定性护栏。

---

## 7. Prompt Cache（hermes 独有）★

`agent/prompt_caching.py`：Anthropic `cache_control` 4 断点策略（system + 最近 3 条非 system 消息），并处理 **OpenRouter 对 `role:tool` 顶层 `cache_control` 静默拒绝**的兼容坑。

### 借鉴建议（codegateway）★★
作为网关，**替下游自动注入/规整 `cache_control` 断点**是极高价值的增值能力：既能显著降低下游 token 成本，又能屏蔽各上游对 cache 字段的兼容差异。

---

## 8. 完成校验（hermes 独有）★

`agent/verification_stop.py`：agent 改了代码但未验证时，turn 结束前注入合成 nudge，强制「编辑 → 验证」闭环；且智能过滤纯文档/散文路径（无需验证）。

**好在哪**：把「改完不验证就收工」这一常见失败模式在框架层堵住。

---

## 9. 给 codegateway 的落地优先级建议

按「投入产出比」排序：

1. **failover 错误分级枚举化**（借 hermes `error_classifier` + MiMo 单一真相源）—— 直接提升本项目刚重写的 failover 质量。**最高优先**。
2. **尊重上游 `retry-after` 头驱动熔断冷却** —— 小改动，大幅改善限流场景表现。
3. **provider 分层：协议适配 vs 传输重试解耦**（借 pigo `transport.go`）。
4. **自动注入 Anthropic `cache_control` 断点**（借 hermes `prompt_caching`）—— 为下游省钱的增值能力。
5. **上下文用量可观测 / 可选服务端 prune**（借 MiMo `overflow.ts` 分级思想）。

---

## 附：关键文件索引

**pigo（Go，最相关）**
- `internal/runtime/loop.go` —— 两层 agentic loop
- `internal/runtime/subagent.go` —— 双隔离 subagent
- `internal/agenttool/tool_retry.go` —— 分级重试
- `internal/agenttool/batch_executor.go` —— 并行工具
- `internal/provider/provider_interface.go`、`transport.go` —— provider 抽象 + 连接级重试

**MiMo-Code（TS）**
- `packages/opencode/src/session/compaction.ts` —— 结构化总结
- `packages/opencode/src/session/overflow.ts` —— 四级压力分级 / usable 窗口
- `packages/opencode/src/session/retry.ts` —— 重试单一真相源

**hermes-agent（Python）**
- `agent/error_classifier.py` —— 细粒度 failover 分类
- `agent/iteration_budget.py` —— 父子迭代预算
- `agent/tool_guardrails.py` —— 死循环护栏
- `agent/prompt_caching.py` —— Anthropic 4 断点缓存
- `agent/verification_stop.py` —— 编辑→验证闭环
- `agent/curator.py` / `memory_manager.py` —— 记忆策展

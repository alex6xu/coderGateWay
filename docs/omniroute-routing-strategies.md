# OmniRoute API 路由策略整理

**日期:** 2026-08-07  
**参考仓库:** `d:\github\OmniRoute`（上游 [diegosouzapw/OmniRoute](https://github.com/diegosouzapw/OmniRoute)）  
**主要源码:** `src/shared/constants/routingStrategies.ts`、`src/sse/handlers/chat.ts`、`docs/routing/AUTO-COMBO.md`  
**对照:** CodeGateway 现行网关（`cmd/server/handlers.go` + Route Profile + failover）

---

## 1. 总览

OmniRoute 是本地优先的多 Provider AI 网关。对外提供 OpenAI 兼容 `/v1`，请求进入后通过 **Combo（模型组合）+ Strategy（选路策略）** 决定：

1. 候选池里有哪些 **provider connection（连接/账号）**
2. 每个连接上用哪个 **model**
3. 按什么顺序或打分尝试，失败后如何切换

核心对象关系：

```text
Client request.model
        │
        ├─ 具体 provider/model     → 直连该连接
        ├─ 已保存的 Combo 名       → 读 Combo.targets + Combo.strategy
        └─ auto / auto/...         → 内存虚拟 Combo（扫全部可用连接）
                │
                ▼
        Strategy 选出 target
                │
                ▼
        Provider Connection → 上游 HTTP / OAuth
```

韧性分层（OmniRoute 强调）：**model ⊂ connection ⊂ provider**  
某模型锁定、某连接冷却、某 Provider 熔断，可分别生效，不必整条链路一起挂。

---

## 2. 请求入口：`model` 如何被解释

| 客户端 `model` | 行为 |
|----------------|------|
| `provider/model` 或通道内模型 ID | 尽量直连对应 Provider 连接 |
| **Combo 名称** | 使用该 Combo 配置的 targets + strategy |
| `auto` | 零配置：全部可用连接组成虚拟候选池，默认均衡 + LKGP |
| `auto/<variant>` | 同上，但换权重/目标（coding / fast / cheap 等） |
| `auto/<category>:<tier>` | 先按能力过滤候选，再按 tier 权重选路（如 `auto/coding:fast`） |

`auto/*` 路径（概念步骤）：

1. Chat handler 识别 `auto/` 前缀  
2. 查询所有 **active provider connections**  
3. 过滤有效凭证（API Key / OAuth）  
4. 每连接确定模型（`defaultModel` 或该 Provider 首个模型）  
5. 内存构建虚拟 Combo（不写库）  
6. 按 variant 权重 + 策略（常配合 LKGP）选目标并 failover  

可用只读端点查看候选池：`GET /v1/auto-combo/{channel}/candidates`。  
响应常带决策头：`X-OmniRoute-Decision`（策略 / provider / 延迟等）。

### 2.1 `auto` 变体速查

| Model ID | 行为概要 |
|----------|----------|
| `auto` | 全连接池，均衡权重，LKGP 粘性 |
| `auto/lkgp` | 显式 LKGP（与默认 `auto` 类似） |
| `auto/coding` | 偏质量，适合代码生成 |
| `auto/fast` | 低延迟加权 |
| `auto/cheap` | 成本优先 |
| `auto/offline` | 偏配额剩余多的连接 |
| `auto/smart` | 质量优先 + 更高探索率 |

### 2.2 Category × Tier 组合示例

- **Category（过滤能力池）:** `coding` · `reasoning` · `vision` · `chat` · `multimodal`  
- **Tier（优化目标）:** `fast` · `cheap`/`floor` · `reliable` · `free` · `pro`  

| 示例 | 含义 |
|------|------|
| `auto/coding:fast` | 编码能力池 + 低延迟权重 |
| `auto/coding:cheap` | 编码能力池 + 成本优先 |
| `auto/reasoning:pro` | 推理模型 + 偏高级档 |
| `auto/vision` | 视觉模型池（无 tier → 均衡） |

过滤失败时通常 **fail-open**：约束匹配不到则退回全池，避免路由硬失败。

---

## 3. 公开路由策略一览（19 种）

声明位置：`ROUTING_STRATEGY_VALUES`。Combo 步骤上可配置其中之一；未识别策略会归一为 `priority`。

### 3.1 顺序与配额消耗

| Strategy | 含义 |
|----------|------|
| `priority` | 按 targets 顺序尝试；优先打前面的，失败/不可用再下一个（默认兜底） |
| `fill-first` | 尽量把当前 target 配额打满，再切下一个 |

### 3.2 负载均衡

| Strategy | 含义 |
|----------|------|
| `weighted` | 按每 target 权重加权随机 |
| `round-robin` | 轮询 |
| `p2c` | Power of Two Choices：随机抽两个再选更优 |
| `least-used` | 选当前负载最低的（别名 `usage` 会归一到此） |
| `random` | 均匀随机（会去重重复项） |
| `strict-random` | 随机且不去重重复 target |

### 3.3 成本与配额窗口

| Strategy | 含义 |
|----------|------|
| `cost-optimized` | 按目录定价尽量压低单次费用 |
| `headroom` | 选剩余配额最多的 |
| `reset-window` | 偏好配额窗口即将重置的（别名含 `weekly-reset` 等） |
| `reset-aware` | 按重置时间排序，短窗口优先 |

### 3.4 上下文与缓存

| Strategy | 含义 |
|----------|------|
| `context-relay` | 长会话跨 target 交接上下文 |
| `context-optimized` | 按当前上下文长度选最合适窗口的 target（别名 `context`） |
| `cache-optimized` | 尽量打到仍持有 prompt-cache 前缀的同一连接 |

### 3.5 智能与多模型

| Strategy | 含义 |
|----------|------|
| `auto` | Auto-Combo 多因子实时打分（健康、配额、成本、延迟、成功率等） |
| `lkgp` | Last-Known-Good Path：粘上次成功路径 |
| `fusion` | 多模型并行/小组输出，再由 judge 合成一个答案 |
| `pipeline` | 串联：上一步输出作为下一步输入 |

### 3.6 内部策略（不对 UI 暴露）

| Strategy | 含义 |
|----------|------|
| `quota-share` | 系统生成 Combo 用（如配额共享），不在公开可选列表中 |

账号级 fallback 子集（`ACCOUNT_FALLBACK_STRATEGY_VALUES`）通常包含：  
`priority` · `weighted` · `fill-first` · `round-robin` · `p2c` · `random` · `least-used` · `cost-optimized` · `strict-random`。

---

## 4. Auto-Combo 打分（概念）

`strategy: "auto"` 或 `model: "auto/*"` 时，引擎对候选连接打分，常见因子包括：

- 健康度 / 熔断状态  
- 剩余配额、配额重置时间  
- 成本  
- 延迟与稳定性  
- 成功率  
- 新鲜度 / 探索  
- 缓存亲和（`cacheAffinity`，与 `cache-optimized` 思路一致）  
- （可选）Arena ELO / models.dev 等外部质量信号  

权重可按 mode pack（coding / fast / cheap…）或持久化 Combo 的 `config.auto.weights` 调整。  
也可创建持久 Combo：`POST /api/combos`，`strategy: "auto"`，并配置 `candidatePool` 等。

---

## 5. 与 CodeGateway 对比

| 维度 | OmniRoute | CodeGateway（现状） |
|------|-----------|---------------------|
| 路由单元 | Combo targets + Strategy；或 `auto` 虚拟 Combo | Route Profile 有序 `models[]` + Channel 匹配 |
| 如何选 Provider | 连接级候选 + 策略/打分选出 connection | 匹配到的 **Channel**，由 `channel.Type` 决定 Provider 实现 |
| 如何选 Model | 连接 `defaultModel` / 候选上的模型 ID / Combo target | 请求 `model` 或 Profile 中的模型名（经 `resolveModelForChannel`） |
| 请求里的 `model` | 模型、Combo 名、或 `auto/...` | 模型名，或 **同名 Route Profile**（展开为多模型候选） |
| 策略数量 | 公开 **19** 种 + 内部 `quota-share` + auto 变体 | 实质为：**有序候选 + 非流式 failover**（无独立 19 策略引擎） |
| Failover | 策略驱动；Provider / Connection / Model 分层熔断与冷却 | `completeWithCandidates`：按候选顺序试；429/5xx/网络可切；401/402/403 长冷却；400 不切 |
| 流式 | 视 Combo/策略实现，生态完整 | 流式目前取**第一个**可用候选，不做完整 failover 链 |
| 会话粘性 | `lkgp`、`cache-optimized` 等一等公民 | 无专用 LKGP；同 Channel 依赖客户端反复传同一 model |
| 成本/配额感知 | `cost-optimized` / `headroom` / `reset-*` / auto 因子 | 无内置按价/配额排序（Channel 仅 priority/weight/`is_default`） |
| 多模型合成 | `fusion`、`pipeline` | 无；一次请求一个上游补全 |
| Agent/Coder | 同一套网关路由 | Session Run：`findChannelForModel`（偏单通道）；Tasks 可用 Profile |
| 决策可观测 | `X-OmniRoute-Decision`、auto candidates API | 日志 + `gateway_request_logs`；无统一 Decision 头 |
| 实现位置 | `OmniRoute` TS 服务（如 `:20128`） | `cmd/server/handlers.go`、`failover.go`、`internal/gateway/profile` |

### 5.1 选型直觉

- **要自动在多账号/多厂商间打分、粘缓存、控成本** → OmniRoute 的 Combo/`auto` 更完整。  
- **要账号隔离 + Route Profile 明确有序列表 + 简单 failover** → CodeGateway 现状更直接、可控。  
- 若要在 CodeGateway 逼近 OmniRoute：优先补的是「多策略排序器 + 流式 failover + 决策可观测」，而不是先搬全部 19 种策略。

---

## 6. CodeGateway 现行路由（对照摘要）

便于和上文对照，现行路径简述：

```text
POST /v1/chat/completions
  req.model
    → 若为本账号 Route Profile 名：展开 Models[]
    → 否则：[req.model]
  对每个模型名 findChannelForModel（账号启用通道，priority/weight，is_default 优先）
  → 候选 [(channel, upstreamModel), ...]
  → 非流式：failoverPass；流式：首个候选
  → createProviderFromChannel(channel.Type / BaseURL / Key|OAuth)
```

详情见：[multi-channel-failover-routing.md](./multi-channel-failover-routing.md)。

---

## 7. 参考链接

| 资源 | 路径 |
|------|------|
| 策略枚举 | `OmniRoute/src/shared/constants/routingStrategies.ts` |
| Chat 入口 / auto | `OmniRoute/src/sse/handlers/chat.ts` |
| Auto-Combo 文档 | `OmniRoute/docs/routing/AUTO-COMBO.md` |
| CodeGateway 候选解析 | `cmd/server/handlers.go` → `resolveChatCompletionCandidates` |
| CodeGateway Failover | `cmd/server/failover.go` |
| CodeGateway Profile | `internal/gateway/profile` |

---

## 8. 修订记录

| 日期 | 说明 |
|------|------|
| 2026-08-07 | 初版：整理 OmniRoute 19 策略、`auto` 变体，并加入与 CodeGateway 对比表 |

# 多 Channel 自动切换路由设计

状态：草案 · 2026-07-31

## 1. 目标

当一个模型请求可由多个 channel（渠道）服务时，网关应：

1. 按策略把候选 channel **排序**成一个有序序列（而非只选一个）。
2. 依次尝试，**任一 channel 失败时自动切换到下一个**（failover）。
3. 对失败的 channel 做**临时熔断 + 自动恢复**，避免持续把流量打到坏渠道。
4. 区分错误类型：只有"换渠道有意义"的错误才触发切换。

## 2. 现状与缺陷

当前存在两套并行且割裂的选路逻辑，且都不支持失败切换：

- `relay.ChannelRepository.GetBestChannel`（`internal/gateway/relay/handler.go:252`）
  —— handler 实际调用的路径。单次打分选出**一个** channel；
  `prov.ChatCompletion` 一旦失败直接返回 `500`，**无 retry、无 failover**。
- `router.SmartRouter.SelectChannel`（`internal/gateway/relay/router/router.go:71`）
  —— 有健康状态与多策略，但 handler **未用它选路**，仅在成功后调用
  `UpdateChannelHealth`。同样是"选一个"，不产出候选序列。

核心缺陷：**选路是"选一个"而非"给一个有序候选序列"**，且 handler 里
没有"失败 → 标记 → 取下一个 → 重试"的循环。这是自动切换缺失的根因。

## 3. 设计

### 3.1 核心思想

```
请求 → 路由器产出【有序候选 channel 列表】→ handler 依次尝试
        ↓ 每次失败                              ↓ 成功即返回
   记录失败 / 熔断该 channel            更新健康度、计费
```

把"选一个"改为"排序候选 + failover 循环"。

### 3.2 路由层：SelectChannel → RankChannels

返回从单个 channel 改为按优先级从高到低排好序的切片；各策略从"选择器"
变为"排序器"。

```go
// 返回按优先级降序排列的候选，handler 依次 failover
func (r *SmartRouter) RankChannels(modelName string) ([]*model.Channel, error)
```

`filterChannels`（router.go:97）已过滤 `Healthy && Status==1 && supportsModel`，
可复用；只需把 `selectByCost/Latency/Quality/Weight` 改为对应的 `sortBy*`。

### 3.3 熔断状态：ChannelState 增加冷却字段

现有 `Healthy / FailedRequests / TotalRequests` 缺"临时熔断 + 自动恢复"。新增：

```go
type ChannelState struct {
    // ...existing...
    ConsecutiveFails int
    CooldownUntil    time.Time // 熔断到期时间；到点自动重新进入候选
}
```

在 `filterChannels` 增加一条：`if time.Now().Before(cs.CooldownUntil) { continue }`。
"自动切回来"由此免费获得——冷却过期后该 channel 自然重新参与排序。

### 3.4 错误分类：决定是否切换 / 是否熔断

并非所有错误都应 failover。

| 错误类型 | 是否切换 | 冷却 |
|---|---|---|
| 429 限流 / 5xx / 网络超时 | 切换 | 短冷却 |
| 401/403 鉴权失败 / 余额不足 | 切换 | 长冷却（配置类问题）|
| 400 请求本身非法 | 不切换，直接返回 | 无（换渠道也无用）|
| context canceled（客户端断开）| 不切换 | 无 |

建议实现：`classifyError(err) -> {retryable bool, cooldown time.Duration}`，
放在 provider 层或 handler 层。

### 3.5 handler：failover 循环替换单次调用

替换 `handler.go:59-99` 的"选一个 → 调用 → 失败即 500"：

```go
candidates, _ := h.router.RankChannels(req.Model)
var lastErr error
for _, channel := range candidates {
    prov, _ := h.providerRegistry.Get(channel.Name)
    start := time.Now()
    resp, err := prov.ChatCompletion(ctx, &req)
    if err == nil {
        h.router.ReportSuccess(channel.ID, time.Since(start))
        // 计费、返回
        return
    }
    decision := classifyError(err)
    h.router.ReportFailure(channel.ID, decision.cooldown)
    lastErr = err
    if !decision.retryable {
        break // 400 类：直接停止
    }
}
// 候选耗尽 → free proxy 兜底或 503（附 lastErr）
```

`UpdateChannelHealth` 拆成语义更清晰的 `ReportSuccess` / `ReportFailure`：
成功清零 `ConsecutiveFails`；失败递增并按 `cooldown` 设置 `CooldownUntil`。

## 4. 待决策问题

- **流式请求的 failover**：首个 chunk 写出后响应头已发，无法透明切换。
  约定：流式仅在"建立连接 / 首字节前"失败才切；已开始吐字后失败即中断。
- **重试上限**：耗尽全部候选，还是最多试 N 个（如 3）？
- **两套逻辑合并**：`ChannelRepository.GetBestChannel` 与 `SmartRouter` 重复。
  建议废弃前者，handler 统一走 router。属较大重构，需确认是否一并做。

## 5. 涉及文件

- `internal/gateway/relay/router/router.go` — RankChannels、排序器、熔断状态、ReportSuccess/Failure
- `internal/gateway/relay/handler.go` — failover 循环、classifyError、废弃 GetBestChannel
- （新增测试）`router` 与 `relay` 包目前零测试覆盖，落地时应补：
  排序正确性、熔断/恢复、错误分类、failover 循环。

## 6. 参考实现对比：OmniRoute

OmniRoute（`/Users/alex/Documents/github/OmniRoute`，TypeScript）是一个成熟的多账号
路由网关。以下基于其源码（`open-sse/services/` 下 `auth.ts`、`accountFallback.ts`、
`accountSelector.ts`、`chat.ts`、`cooldownAwareRetry.ts`）与真实运行日志
（`~/.omniroute/logs`、`storage.sqlite`）分析，它在 429 限流场景下的实际行为：

```
Account 57484d82... unavailable (429), trying fallback
kiro | all 1 active accounts rate limited (reset after 5s)
COOLDOWN_RETRY: waiting 5s before retry 3/3
cooldown elapsed — restarting request attempt 3/3
```

### 6.1 OmniRoute 的机制

1. **选择顺序（可配策略）** — `accountSelector.ts` 支持 `fill-first`（默认）、
   `round-robin`、`random`、`p2c`（power-of-two-choices 健康度）。生产路径
   （`auth.ts:1482+`）是 **sticky-LRU**：先过滤有配额的候选 → 优先命中会话亲和 →
   同一账号在 `consecutiveUseCount < stickyLimit(默认3)` 内继续粘用 → 否则按
   `lastUsedAt` 选最久未用，排序以 `backoffLevel` 升序为主键（被限流过的账号
   排后），`priority` 作 tiebreak。

2. **冷却状态机** — `markConnectionRateLimitedUntil` 写 DB 的
   `rate_limited_until = now + retryAfterMs` 与 `backoff_level`；选择时
   `isAccountUnavailable` 跳过。**自动恢复纯靠时间**：`now >= rate_limited_until`
   即重新可选，且 `backoffLevel` 自动衰减到 0。启动时清理陈旧冷却。
   模型级锁定（`modelLockouts`）与连接级冷却分离。

3. **backoff 计算** — 指数：`base(1000ms) * 2^level`，上限 2 分钟，maxLevel 15。
   **上游 hint 优先**：解析 `Retry-After` / `x-ratelimit-reset` / 响应体，若有则
   直接采用并把 `backoffLevel` 归 0；权威上游 reset 可突破上限。**无 jitter**。

4. **failover 双层循环**：
   - 内层（`chat.ts` requestAttemptLoop）：失败账号加入 `excludedConnectionIds`，
     重新选下一个账号。全部冷却时返回 `{allRateLimited, retryAfter}`。
   - 外层（`cooldownAwareRetry.ts`）：当全部账号冷却，决定**等待**冷却过期再重试
     （即日志的 "waiting 5s before retry 3/3"）。硬上限 `MAX_REQUEST_RETRY=10`、
     单次等待上限 300s、累计预算 5 分钟；超预算即放弃。

5. **错误分类** — `checkFallbackError` / `classifyError`：可重试集
   {408,429,500,502,503,504}；401/403 → AUTH_ERROR（apikey 403 冷却连接，
   封号信号 → permanent 约 1 年冷却）；402/额度 → QUOTA_EXHAUSTED（1h 或到午夜）；
   400 仅在 context 溢出/参数/模型不可用等模式下 fallback（cooldown 0），否则硬停。

6. **会话亲和**：选择前先尝试把会话粘到同一账号；但一旦进入 fallback
   （`excludedConnectionIds` 非空），sticky 逻辑整体跳过以免重选到坏账号；
   combo 连续失败超阈值自动清除亲和 pin。

### 6.2 与本方案（§3）的对比

| 维度 | 本方案（CodeGateway） | OmniRoute |
|---|---|---|
| 选择粒度 | **channel（渠道）级** | **connection/account（账号）级**，同 provider 多账号轮换 |
| 选择策略 | 单一排序（按 cost/latency/quality/weight）| 可插拔：fill-first / round-robin / random / p2c |
| 粘性/亲和 | 无 | sticky-LRU + 会话亲和，fallback 时自动关闭 |
| 冷却恢复 | `CooldownUntil` 时间到自动恢复 ✅ 一致 | 同左，且 `backoffLevel` 自动衰减 |
| backoff | 固定冷却（长/短两档）| 指数 `base*2^level` + 上限 + 上游 hint 优先 |
| 上游 Retry-After | **未利用**（缺口）| 解析并优先于自算 backoff |
| failover 层次 | 单层：遍历候选 | 双层：账号遍历 + 全冷却时定时等待重试 |
| 全部不可用 | 直接 503 / free proxy 兜底 | 等待冷却过期再重试（预算内）|
| 错误分类 | 三类（retryable/长冷却/不切换）| 更细：permanent 封号、额度到午夜、400 子模式 |
| 可观测 | 无决策记录 | `routing_decisions` 表 + 结构化日志 |

### 6.3 可借鉴、可精简

**建议吸收进本方案（低成本高价值）**：
- **上游 Retry-After 优先**：429 响应带 `Retry-After` 时用它设 `CooldownUntil`，
  比固定档位精准得多。这是当前方案最明显的缺口。
- **指数 backoff + 上限**：把"长/短两档"升级为 `base*2^ConsecutiveFails` 并封顶，
  避免抖动式重复限流。
- **"全部候选冷却"时的定时等待**：单层遍历耗尽后，与其立即 503，可选择在
  最近的 `CooldownUntil` 处等待一次再重试（设总预算上限，如 30s），
  显著提升单账号/少渠道场景的成功率。

**建议暂缓（对当前规模过度设计）**：
- 会话亲和、p2c 选择、combo 多级链、自适应学习分数（`combo_adaptation_state`）、
  routing_decisions 决策落库 —— 这些在多账号大规模场景才划算，
  CodeGateway 当前 channel 级 + 单层 failover 已能覆盖主要诉求。

### 6.4 结论

本方案方向与 OmniRoute 一致（有序候选 + failover + 时间驱动的冷却自动恢复），
差异主要在**成熟度**而非**正确性**。落地时优先补三点：**Retry-After 优先**、
**指数 backoff 封顶**、**全冷却定时等待**；其余高级特性按需再加。

## 7. 最终方案（落地版）

状态：已定稿 · 2026-07-31

**决策：以本方案（§3，channel 级 + 单层 failover + 内存态）为骨架，只吸收
OmniRoute 中"低成本、高价值、正确性关键"的三点，不照搬其多账号/持久化/审计体系。**

理由：OmniRoute 完整设计面向多账号、大规模、需审计的生产网关，其高级表在实际
部署里全是空的（单 `kiro` 账号透传）。对 CodeGateway 这个个人项目，持久化熔断、
会话亲和、自适应评分、决策落库都是**当前规模下的负债**，留待路线图按需再加。

### 7.1 保留（方案 A 原样）

channel 级粒度 · `RankChannels` 有序候选 · handler 单层 failover 循环 ·
`CooldownUntil` 时间驱动自动恢复 · 三分类 `classifyError` · 内存态状态（不落库）。

### 7.2 吸收三点（核心增量）

**① 上游 Retry-After 优先** —— 本方案最明显的缺口。

429 响应带 `Retry-After` / `X-RateLimit-Reset` 时，用它设 `CooldownUntil`，
比固定档位精准得多；无 hint 时回落到自算 backoff。

```go
// cooldownFromResponse: 有上游 hint 则优先采用，并把 ConsecutiveFails 归零
func cooldownFromResponse(h http.Header, fails int) time.Duration {
    if d, ok := parseRetryAfter(h); ok { // Retry-After(秒或HTTP-date) / X-RateLimit-Reset
        return d
    }
    return backoff(fails) // 回落到指数 backoff
}
```

**② 指数 backoff 封顶** —— 替换"长/短两档"固定冷却。

```go
const (
    backoffBase = 1 * time.Second
    backoffCap  = 2 * time.Minute
)
func backoff(fails int) time.Duration {
    d := backoffBase << fails          // base * 2^fails
    if d > backoffCap || d <= 0 {      // 封顶 + 溢出保护
        return backoffCap
    }
    return d
}
```

配置类错误（401/403/余额）仍走独立长冷却档（如 30min），不参与指数计算。

**③ 全候选冷却时的一次定时等待** —— 单账号/少渠道场景救命。

单层遍历耗尽后，与其立即 503，可在最近的 `CooldownUntil` 处等待一次再重试
（设总预算上限，如 30s；超预算或客户端取消即放弃）。**只等一次，不做 OmniRoute
那样的多轮外层循环**，把复杂度控制住。

```go
// 遍历候选后：若全部因冷却被跳过，且最近冷却在预算内，等待一次再重试
if allSkippedByCooldown {
    wait := timeUntilNearestCooldown(candidates)
    if wait > 0 && wait <= maxWaitBudget { // maxWaitBudget = 30s
        select {
        case <-time.After(wait):
            // 重跑一次 failover 循环（仅一次）
        case <-ctx.Done():
            return ctx.Err()
        }
    }
}
```

### 7.3 明确不吸收（暂缓，留路线图）

持久化熔断状态（重启保留）· 会话亲和 · p2c 选择 · combo 多级链 ·
自适应 `learned_score` · `routing_decisions` 决策落库。
—— 多账号大规模 + 需审计时才划算，当前是加法可后补，非本次范围。

### 7.4 落地清单（对应代码改动）

已实现（2026-07-31）。真实生效路径是 `cmd/server/`，`internal/gateway/relay/*`
为参考实现/死代码，故改动落在实际路径上，而非死代码 `router.go`。

`internal/provider/errors.go`（新增）：
- [x] `ProviderError{StatusCode, Header, Body}` 结构化错误 + `RetryAfter()`（解析 `Retry-After` 秒/HTTP-date 与 `X-RateLimit-Reset`）。
- [x] openai / claude / claude_stream 在非 200 时返回 `NewProviderError`（deepseek/mimo/ollama/custom 包装 openai 自动继承）。

`cmd/server/failover.go`（新增）：
- [x] `backoff(fails)`：指数 `base<<fails`，封顶 2min，含溢出保护。
- [x] `classifyError(err, fails)`：429/5xx→切+短冷（Retry-After 优先）；401/403/402→切+长冷 30min；400→不切；ctx 取消→不切不冷。
- [x] 进程级熔断器单例 `breakerRegistry`（按 channel ID）：`reportSuccess/reportFailure/isCoolingDown/cooledDownUntil`（内存态，不落库）。

`cmd/server/handlers.go`：
- [x] `completeWithCandidates` 增强为带熔断/分类的 failover 循环（`failoverPass`），跳过冷却中的 channel。
- [x] 7.2③ 全冷却一次等待：`timeUntilNearestCooldown` + 单次 `time.After`（预算 30s，尊重 ctx 取消）。

测试（`cmd/server/failover_test.go`，此前零覆盖）：
- [x] 指数 backoff 封顶 · Retry-After 解析优先（秒 + X-RateLimit-Reset）· 错误分类（429/503/401/400/canceled/generic）· failover 切换 · 400 不切 · 冷却跳过 · 全冷却等待一次。全绿。

顺带修复：`internal/agent/task/worker_test.go` 预存测试缺陷（补 `ProviderForTask`+`Run` stub，使其真正命中 workspace-load 失败路径）。

后续（暂缓，见 §7.3）：策略排序器化（`RankChannels`）、持久化熔断、会话亲和、自适应评分、决策落库。

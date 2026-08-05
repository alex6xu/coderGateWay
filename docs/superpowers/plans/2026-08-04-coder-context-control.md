# Coder Context Control Implementation Plan

> **For agentic workers:** Implement task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Code 模式下用 A/B/C 布局 + 75% 压缩闸 + 窗口化读文件，保证不超上下文且利于 prompt cache。

**Architecture:** 工具层硬限制输出；Coder 循环内估算利用率并 `CompactToolMessages`；checkpoint 在 75% 或 N 轮时更新；配置写入 `AgentConfig` / yaml。

**Tech Stack:** Go, existing `promptctx` / `tool` / `coder_agent`.

**Spec:** [docs/superpowers/specs/2026-08-04-coder-context-control-design.md](../specs/2026-08-04-coder-context-control-design.md)

## Global Constraints

- 不改网关 `/v1/chat/completions` 透传语义。  
- 压缩不得打断 tool_use / tool_result 闭环。  
- system 身份文案除指引追加外保持稳定。

---

### Task 1: Config knobs

**Files:** `internal/config/config.go`, `codegateway.yaml`

- [x] Add `ContextCompactRatio`, `ContextTargetRatio`, `ReadFileDefaultLines`, `ReadFileMaxBytes`, `GrepMaxBytes` with defaults `0.75`, `0.55`, `400`, `32768`, `16384`.
- [x] Document in `codegateway.yaml` under `agent:`.

### Task 2: Windowed read_file / grep

**Files:** `internal/tool/chroot.go`, `internal/tool/tool_test.go` (or new chroot test)

- [x] `read_file`: if `limit==0`, use `ReadFileDefaultLines` (pass via registry options or package-level defaults from config at registry create — prefer constructor option on `NewChrootedRegistry` or set defaults in handler constants matching config defaults; wire from coder when creating registry).
- [x] Cap output by `ReadFileMaxBytes`.
- [x] `grep`: cap total bytes by `GrepMaxBytes`.
- [x] Update tool description to require offset/limit for large files.
- [x] Tests for default line cap and byte cap.

**Simplification:** `NewChrootedRegistry(root, opts ToolLimits)` with zero-value meaning defaults.

### Task 3: Wire CompactToolMessages + budget gate in coder loop

**Files:** `cmd/server/coder_agent.go`, `internal/agent/promptctx/builder.go`

- [x] Before each `ChatCompletion` in the loop, estimate tokens; if `used/budget >= compactRatio`, call `CompactToolMessages`.
- [x] Add `EnsureWithinBudget(messages, budget, compactRatio, keepRecent, maxChars) []provider.Message` helper that mutates/compacts and returns messages (or works in-place).
- [x] Pass `ToolResultKeepRecent` and `ContextBudgetTokens` / ratios via `coderOptions`.
- [x] If still `> 0.95 * budget` after compact, return error to caller (do not call API).

### Task 4: MaybeCheckpoint on utilization or N turns

**Files:** `internal/agent/promptctx/builder.go`, `cmd/server/handlers.go`

- [x] Extend `MaybeCheckpoint` to accept optional `force bool` or new `MaybeCheckpointEx(..., every int, force bool)`.
- [x] After coder/agent turn, if last request was compacted due to 75%, force checkpoint.
- [x] Keep existing every-N-turns behavior.

### Task 5: Coder system prompt guidance

**Files:** `cmd/server/handlers.go` (`coderSystemPrompt`)

- [x] Append rules: explore with list/grep first; windowed read_file; no bulk whole-file reads.

### Task 6: Verify

- [x] `go test ./internal/tool/ ./internal/agent/promptctx/ ./cmd/server/ -count=1`
- [x] Manual sanity: defaults in yaml parse.

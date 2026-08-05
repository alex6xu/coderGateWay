# Session Run Background Execution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detach Coder/Chat agent turns from HTTP so runs continue after the browser closes, and clients can resume live event streams via `after_seq`.

**Architecture:** Persist `session_runs` + ordered `session_run_events` + `session_run_inbox`. Chat POST enqueues or starts a run on a process-local worker using a non-request context. SSE only replays/subscribes to the event log. Inbox drains at tool-loop boundaries before the next LLM call.

**Tech Stack:** Go, SQLite/migrations, Gin SSE, existing `runCoderAgent` / `promptctx`, React Coder/Chat pages.

**Spec:** [docs/superpowers/specs/2026-08-05-session-run-background-design.md](../specs/2026-08-05-session-run-background-design.md)

## Global Constraints

- One active (`queued|running`) run per session; extra user messages go to inbox.
- Do not cancel runs when the HTTP client disconnects.
- On process restart, mark `running` as `failed`/`interrupted`; do not auto-resume half tool loops.
- Pending inbox is only consumed into a new run on the next `POST /v1/agent/chat` when no active run exists.
- Event payload shape stays compatible with existing `AgentEvent` JSON.

## File map

| File | Responsibility |
|------|----------------|
| `internal/db/migrations.go` | Tables: `session_runs`, `session_run_events`, `session_run_inbox` |
| `internal/agent/sessionrun/types.go` | Status/event types |
| `internal/agent/sessionrun/store.go` | CRUD, append event, inbox, claim, recover |
| `internal/agent/sessionrun/hub.go` | In-process live fan-out after persist |
| `internal/agent/sessionrun/worker.go` | Poll/claim queued runs; invoke executor |
| `cmd/server/session_run_exec.go` | Executor wiring (provider, coder/chat, emit) |
| `cmd/server/handlers.go` | Chat → enqueue/start; session summary; events SSE; cancel |
| `cmd/server/server.go` | Start worker + RecoverInterrupted |
| `web/src/pages/CoderPage.tsx` | Subscribe by run_id; reconnect; queue UX |
| `web/src/pages/ChatPage.tsx` | Same subscribe/reconnect pattern if it uses agent chat |

---

### Task 1: Migration + store

**Files:** `internal/db/migrations.go`, `internal/agent/sessionrun/*`, tests

- [x] Add tables and indexes per spec.
- [x] Implement Store: CreateRun, GetRun, ActiveRunForSession, AppendEvent, ListEventsAfter, EnqueueInbox, DrainPendingInbox, MarkInjected, ClaimNextQueued, FinishRun, RecoverInterrupted.
- [x] Tests: seq monotonic; one active run helper; inbox drain order; recover marks running failed.

### Task 2: Hub + worker skeleton

**Files:** `internal/agent/sessionrun/hub.go`, `worker.go`

- [x] Hub: Subscribe(runID, afterSeq) → channel; Publish after AppendEvent.
- [x] Worker.Run loop claims queued runs and calls injected Executor.
- [x] RecoverInterrupted on startup before accepting work.

### Task 3: Executor (detach from request ctx)

**Files:** `cmd/server/session_run_exec.go`, refactor `coder_agent.go` / chat path from `handlers.go`

- [x] Executor loads run, builds seed like current handleAgentChat, runs coder or plain chat.
- [x] Emit path: AppendEvent + Hub.Publish for meta/delta/tool_step/done/error.
- [x] Before each LLM call (after tools): DrainPendingInbox → inject user messages → event `user_injected`.
- [x] Use worker ctx / WithoutCancel; persist final assistant message; FinishRun.
- [x] After success/fail, if session has pending inbox with no active run, CreateRun for next (or leave for next POST per spec — **prefer next POST only**).

### Task 4: HTTP API

**Files:** `cmd/server/handlers.go`, `server.go`

- [x] Change `POST /v1/agent/chat`: persist user msg; if active run → inbox + return run_id; else create run queued + return; optional stream = subscribe SSE to that run.
- [x] `GET /v1/agent/sessions/:id` → session + active_run + last_seq.
- [x] `GET /v1/agent/runs/:id/events?after_seq=` → replay then live SSE.
- [x] `POST /v1/agent/runs/:id/cancel` → cancel queued / set cancel flag for running.
- [x] Wire worker in server main.

### Task 5: Frontend reconnect

**Files:** `web/src/pages/CoderPage.tsx`, `ChatPage.tsx` as needed

- [x] On send: use returned run_id; subscribe events (fetch stream or EventSource-equivalent via fetch).
- [x] On mount/session select: load messages; if active run, subscribe after_seq.
- [x] Show queued/injected status lightly.
- [x] Do not treat client abort as fatal server failure.

### Task 6: Verify

- [x] `go test ./internal/agent/sessionrun/ ./cmd/server/ -count=1`
- [ ] Manual: start run, kill tab conceptually (abort fetch), confirm run completes and events replay.

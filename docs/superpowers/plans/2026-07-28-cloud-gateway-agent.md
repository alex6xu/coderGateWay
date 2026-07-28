# Cloud Gateway Agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver tenant-scoped route profiles with model fallback and durable, sandboxed cloud workspace Agent Tasks.

**Architecture:** Persist route profiles and tasks in SQLite. Resolve profile candidates before provider creation and retry the next candidate only while no response data has been forwarded. A durable in-process worker claims queued tasks, resolves the requested profile, and invokes the existing chrooted coder loop against the owned workspace.

**Tech Stack:** Go 1.22, Gin, SQLite (`modernc.org/sqlite`), existing `workspace.Manager`, `runCoderAgent`, and provider abstractions.

## Global Constraints

- Scope every profile, task, and workspace query by `user_id`; cross-account access returns 404.
- Preserve direct-model behavior and existing workspace/GitHub APIs.
- Profiles require a nonempty, ordered, de-duplicated candidate list and purpose `coding`, `documentation`, or `general`.
- Never expose channel credentials to Agent Tasks or logs.
- Task state transitions are `queued -> running -> succeeded|failed`; a task is claimed only once.
- The worker runs asynchronously and receives only the owned workspace supplied by `workspace.Manager`.
- MVP never auto-pushes or creates a PR.

---

### Task 1: Complete Route Profile storage and HTTP API

**Files:** `internal/gateway/profile/store.go`, `internal/gateway/profile/store_test.go`, `cmd/server/route_profiles.go`, `cmd/server/server.go`, `internal/db/migrations.go`.

- [ ] Write focused failing tests for normalized/deduplicated models, tenant isolation, update/delete, duplicate names, and all invalid purposes.
- [ ] Run `go test ./internal/gateway/profile` and confirm the new cases fail for missing behavior.
- [ ] Implement validated CRUD with JSON model persistence, SQL error mapping, and authenticated `/v1/admin/route-profiles` routes.
- [ ] Run `go test ./internal/gateway/profile ./internal/db ./cmd/server`.
- [ ] Commit the route profile API.

### Task 2: Add profile candidate resolution and completion fallback

**Files:** `cmd/server/handlers.go`, `cmd/server/models_test.go` plus new handler/unit tests as appropriate.

- [ ] Write failing tests proving a profile is resolved only for its owner, candidates preserve order, direct model names remain unchanged, and a failed non-stream completion tries the next eligible candidate.
- [ ] Run the focused server tests and verify failure.
- [ ] Add a reusable profile resolver that checks enabled, account-owned channels; run non-stream attempts in candidate order; preserve the chosen model for usage logging. Stream requests select the first eligible candidate and are never retried after response forwarding.
- [ ] Run `go test ./cmd/server ./internal/gateway/profile`.
- [ ] Commit the gateway routing behavior.

### Task 3: Persist Agent Tasks and expose tenant-safe task APIs

**Files:** create `internal/agent/task/store.go`, `internal/agent/task/store_test.go`, `cmd/server/agent_tasks.go`; modify `internal/db/migrations.go`, `cmd/server/server.go`.

- [ ] Write failing tests for create/list/get ownership, profile/workspace validation, accepted task types, and atomic queued-to-running claim.
- [ ] Run `go test ./internal/agent/task` and confirm failures.
- [ ] Implement task persistence, recovery of interrupted running tasks, and authenticated `POST/GET /v1/agent/tasks` endpoints. Return 202 only after queueing a task.
- [ ] Run `go test ./internal/agent/task ./internal/db ./cmd/server`.
- [ ] Commit durable task API support.

### Task 4: Execute queued Agent Tasks in the owned workspace

**Files:** create `internal/agent/task/worker.go`, `internal/agent/task/worker_test.go`; modify `cmd/server/server.go` and narrow shared server helpers as needed.

- [ ] Write failing tests for one-time claim, queued/running terminal transitions, profile selection, task-type-specific prompts, result/error persistence, and worker use of the owned workspace only.
- [ ] Run `go test ./internal/agent/task` and confirm failures.
- [ ] Implement the in-process worker using the existing provider/channel selection and `runCoderAgent`; persist sanitized tool steps and refresh workspace statistics. Mark recovery-time running tasks failed before accepting work.
- [ ] Run `go test ./internal/agent/task ./cmd/server`.
- [ ] Commit the worker.

### Task 5: Add UI integration and verify end-to-end compatibility

**Files:** existing React admin/workspace pages and their tests; route handlers as necessary.

- [ ] Write failing UI/API-facing tests for creating a route profile and submitting/polling an Agent Task.
- [ ] Implement minimal UI controls that call the new APIs without exposing credentials or adding implicit git publishing.
- [ ] Run `npm run build --prefix web`, `go test ./...`, and a handler-level smoke test for all acceptance criteria.
- [ ] Commit UI integration and verification fixes.

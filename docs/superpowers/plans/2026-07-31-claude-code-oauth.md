# Claude Code OAuth Implementation Plan

> **For agentic workers:** Implement task-by-task. Steps use checkbox syntax.

**Goal:** Account-level Claude Code subscription OAuth with hybrid callback/paste, usable by Claude channels.

**Architecture:** Mirror GitHub OAuth (`githubvcs` + handlers). PKCE public client; store refreshable tokens; ClaudeProvider Bearer auth when `auth_mode=oauth`.

**Tech Stack:** Go, Gin, SQLite, React/TS

## Global Constraints

- PKCE S256; no client_secret required for default public client
- One `claude_connections` row per account
- Refresh rotates refresh_token — always persist new tokens
- Do not commit secrets

---

### Task 1: Config + DB + claudeoauth package

**Files:**
- Create `internal/claudeoauth/oauth.go`, `oauth_test.go`
- Modify `internal/config/config.go`, `codegateway.yaml`
- Modify `internal/db/migrations.go`
- Modify `internal/model/model.go` (`AuthMode` on Channel)

### Task 2: HTTP handlers + routes + provider auth

**Files:**
- Create `cmd/server/claude_oauth_handlers.go`
- Modify `cmd/server/server.go`, `handlers.go` (`createProviderFromChannel`)
- Modify `internal/provider/claude.go`, `provider.go` (`AuthMode` / Bearer)

### Task 3: Frontend

**Files:**
- Modify `web/src/pages/SettingsPage.tsx`, `ChannelsPage.tsx`

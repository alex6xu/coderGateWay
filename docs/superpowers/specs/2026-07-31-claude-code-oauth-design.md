# Claude Code 订阅 OAuth 设计

**日期:** 2026-07-31  
**状态:** 已批准并实施

## 目标

账号级接入 Claude Code / Claude.ai 订阅 OAuth（PKCE），Claude Channel 可选用订阅鉴权，无需 API Key。

## 决策

- Web UI 浏览器授权（方案 A）
- 账号级 connection，Channel 勾选「使用订阅 OAuth」（绑定方式 1）
- 混合回调：优先网关 callback，失败回落粘贴 `code` / `code#state`（方式 3）

## 架构

1. `internal/claudeoauth`：PKCE、authorize、exchange、refresh、connection CRUD（镜像 `githubvcs`）
2. 表：`claude_oauth_states`、`claude_connections`；`channels.auth_mode`（`api_key`|`oauth`）
3. 路由：`/v1/claude/oauth/{status,authorize,callback,exchange,disconnect}`
4. `ClaudeProvider`：oauth → `Authorization: Bearer` + beta；api_key → `x-api-key`
5. UI：Settings 连接/断开/粘贴；Channels Claude 勾选 oauth

## 端点常量（Claude Code 公开客户端）

- Client ID: `9d1c250a-e61b-44d9-88ed-5944d1962f5e`
- Authorize: `https://claude.ai/oauth/authorize`
- Token: `https://platform.claude.com/v1/oauth/token`
- Platform redirect（粘贴模式）: `https://platform.claude.com/oauth/code/callback`
- Scopes: `user:inference user:profile user:file_upload user:mcp_servers user:sessions:claude_code`

## 合规说明

订阅 OAuth token 用于非官方客户端可能受 Anthropic 条款限制；本功能按用户明确需求实现，由运营方自行评估合规风险。

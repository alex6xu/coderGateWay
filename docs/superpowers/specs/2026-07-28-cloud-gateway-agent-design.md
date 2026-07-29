# 云端 LLM 网关与沙箱 Agent 设计

## 目标

在现有 CodeGateway 中交付一个可自托管的云端 LLM API 网关 MVP，并提供通过网页提交、在隔离云工作区中执行的代码修改与文档生成任务。外部调用者和 Agent 均通过同一个 OpenAI 兼容网关访问模型。

## 非目标

本期不实现 Kubernetes 调度、多节点队列、供应商 OAuth、自动合并 PR、浏览器自动化或任意网络访问。Pi 作为可选的未来 runner；MVP 复用现有 Go coder loop，避免在服务进程中引入外部 CLI 依赖。

## 用户角色与流程

1. 管理员批量创建渠道，配置模型列表、权重、优先级和状态。
2. 管理员创建 Route Profile，例如 `coding-auto` 或 `docs-cheap`，并按优先级排列候选模型。
3. 应用使用 `model: <profile-id>` 调用 `/v1/chat/completions`；网关依次尝试候选模型，失败时自动回退。
4. 已登录用户从网页/API 创建 Agent Task，选择已有 workspace、Route Profile 与任务类型。
5. Worker 使用 workspace 根目录运行现有受限 coder 工具；用户查看状态、结果、工具步骤和文件 diff。
6. GitHub 已连接时，用户可在现有 Git 工作流中把审查后的 workspace 改动推送为分支/PR。

## 架构

```text
Client / Web / SDK
       |
       v
Auth + account isolation
       |
       +--> OpenAI-compatible gateway --> Route Profile selector --> existing channel/provider
       |
       +--> Agent Task API --> in-process durable task manager --> chrooted cloud workspace
                                                        |
                                                        +--> existing coder loop --> gateway-selected provider
```

### 网关

- 保留现有渠道模型：渠道保存供应商凭据，调用方不直接获得凭据。
- 新增 Route Profile：一个账号拥有多个 profile；每个 profile 是有序候选模型列表与任务用途。
- 选路先验证候选模型在该账号存在已启用渠道，再按 profile 顺序尝试。一次失败标记本次调用失败并尝试下一候选模型。
- 直接模型名保持现有行为，确保兼容现有 SDK。

### Agent 任务

- 新增持久化任务表，任务至少记录：ID、账号、workspace、任务类型、route profile、提示词、状态、输出、错误、创建/开始/结束时间。
- 状态机：`queued -> running -> succeeded | failed`。任务只允许从 queued 启动一次。
- MVP 采用进程内 goroutine worker；重启后 running 任务恢复为 failed，queued 任务可重新执行。
- Agent 只取得工作区根目录，不取得渠道密钥；工具仍由 chrooted registry 限制路径。
- 任务类型仅有 `code_change` 与 `documentation`；二者使用不同系统提示词，均可编辑工作区。

### Git 与审批

- 复用已有 GitHub OAuth、仓库导入和 workspace 能力。
- MVP 不自动推送或创建 PR。任务 API 返回 workspace ID，用户在现有 GitHub UI/接口中主动执行后续操作。
- 后续将“创建分支/PR”实现为显式审批动作，绝不由模型隐式触发。

## API

### Route profiles

- `GET /v1/admin/route-profiles`：列出当前账号的 profiles。
- `POST /v1/admin/route-profiles`：创建 `{name, purpose, models:["model-a","model-b"]}`。
- `PUT /v1/admin/route-profiles/:id`：更新。
- `DELETE /v1/admin/route-profiles/:id`：删除。

名称在单账号内唯一，模型列表不能为空且去重。`purpose` 为 `coding`、`documentation` 或 `general`。

### Agent tasks

- `POST /v1/agent/tasks`：创建 `{workspace_id, route_profile, type, prompt}`，返回 202 与 task。
- `GET /v1/agent/tasks`：按创建时间倒序列出当前账号任务。
- `GET /v1/agent/tasks/:id`：读取任务与工具步骤。

创建时验证 workspace 所属账号、profile 所属账号以及任务类型。

## 数据库

```sql
CREATE TABLE route_profiles (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  name TEXT NOT NULL,
  purpose TEXT NOT NULL,
  models TEXT NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  UNIQUE(user_id, name)
);

CREATE TABLE agent_tasks (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL,
  workspace_id TEXT NOT NULL,
  route_profile_id INTEGER NOT NULL,
  type TEXT NOT NULL,
  prompt TEXT NOT NULL,
  status TEXT NOT NULL,
  result TEXT NOT NULL DEFAULT "",
  error TEXT NOT NULL DEFAULT "",
  tool_steps TEXT NOT NULL DEFAULT "[]",
  created_at DATETIME NOT NULL,
  started_at DATETIME,
  finished_at DATETIME
);
```

## 安全约束

- 所有资源均按 `user_id` 查询，禁止跨账号 ID 访问。
- task 的可执行路径唯一来自 workspace manager；不得接受任意本地路径。
- 任务不会在 API 响应 goroutine 中运行，避免请求取消中断执行。
- Prompt、模型输出与工具输出作为潜在敏感数据，仅对任务所属账号返回。
- 不引入 Docker socket、宿主机挂载、外网 shell 或供应商密钥注入。

## 验收标准

1. 管理员可为当前账号创建一个含多个模型的 profile。
2. profile 作为 chat 请求模型名时，优先使用第一个可用模型；该请求失败时回退下一个候选模型。
3. 用户可创建 code_change/documentation 任务，并可轮询获得 queued、running 和终态。
4. 不属于当前账号的 workspace/profile/task 均返回 404。
5. Agent 在 workspace 内工作，结果与 tool steps 被持久化。
6. 已有直接模型请求、工作区和 GitHub 接口保持兼容。

## 后续演进

- 使用 Pi runner 取代或并列现有 coder loop，并将其 OpenAI base URL 指向内部网关。
- 将 in-process worker 替换为持久队列和独立 sandbox worker。

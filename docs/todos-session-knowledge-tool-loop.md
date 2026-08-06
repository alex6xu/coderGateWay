# 待办说明：会话恢复 · 知识图谱 · 工具循环超限

> 与 `README.md`「优先待办」对应的展开说明。实现时认领对应条目并回写状态。

## 1. 切换会话后恢复内容

**现状**

- 页面导航离开再回来：Code/Chat 已通过 `localStorage`（`web/src/lib/sessionPersist.ts`）记住 workspace/session，并尝试重连进行中的 run SSE。
- **会话列表切换**（A → B）：消息列表、工具步骤面板、进行中/最近 run 事件流的还原仍不完整或体验不一致。

**目标**

- 切换到任意已有会话时，立即展示该会话的历史消息与工具步骤。
- 若该会话有进行中的 run，自动订阅事件并续播过程输出。
- 与导航恢复共用同一套 hydrate 逻辑，避免两套状态机。

**涉及**

- `web/src/pages/CoderPage.tsx`、`ChatPage.tsx`
- `web/src/lib/sessionPersist.ts`
- session / run / events API（`cmd/server`、`internal/agent/sessionrun`）

---

## 2. 构建本地知识库 / 知识图谱

**现状**

- 全量消息落库 + FTS5（bm25）可用。
- 无向量检索、无实体关系图、无 Obsidian 等外部知识库互通。

**方向（候选技术）**

| 方向 | 候选 | 作用 |
|------|------|------|
| 向量检索 | **sqlite-vec** | 与现有 SQLite 同库，语义召回补充 FTS |
| 知识图谱 | **graphify** 风格 / 自研边表 | 实体–关系可视化与多跳查询 |
| 外部互通 | **Obsidian** vault 导入/导出 | 把对话沉淀写成可编辑笔记，或从笔记注入记忆 |

**分期建议**

1. sqlite-vec + embedding 管线（写入对话/代码摘要，检索注入 `promptctx`）
2. 轻量实体/主题边表 + 简单图谱浏览页
3. Obsidian 双向同步（可选）

---

## 3. 工具调用循环超限

**现状**

- `agent.max_iterations` 控制 LLM↔工具轮次；触顶错误：`max tool iterations reached`（`cmd/server/coder_agent.go`）。
- 默认已调至 **24**（见 `codegateway.yaml`）；一般改代码 20–32 合适，大范围探索可到 40–50。

**仍缺**

- 触顶时硬失败，未强制基于已有工具结果做摘要收束。
- 无「继续」续跑同一 run（在已有 messages/tools 状态上加额度）。
- 前端缺少清晰提示（已用轮次 / 建议提高配置或缩小任务）。

**改进清单**

- [ ] UI/文案：展示触顶原因与 `max_iterations` 提示
- [ ] 触顶收束：最后一轮禁止工具，要求模型根据已有结果回答
- [ ] 可选续跑：用户确认后增加 N 轮继续同一会话上下文
- [ ] 可观测：结合 Channels「请求日志」排查空转（重复读同一文件等）

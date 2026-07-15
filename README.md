# CodeGateway

个人 Code Agent + API 网关平台

## 项目简介

CodeGateway 融合了 MiMo-Code（Agent 能力）+ new-api/sub2api（网关能力）+ hermes-agent（消息平台集成）的架构精华，构建一个**企业内部可用的云端 Agent + API 网关平台**。

## 核心功能

### API 网关
- **多 Provider 支持**: OpenAI、Claude、Gemini、DeepSeek、Ollama、MiMo、Agnes、GLM（智谱）、自定义端点
- **格式转换**: OpenAI ⇄ Claude ⇄ Gemini ⇄ DeepSeek
- **智能路由**: 根据 prompt 内容自动选择最优 provider
- **负载均衡**: 多渠道权重轮询
- **Token 计费**: 精确的 token 级计费和配额管理
- **反向代理**: 支持免费 API 端点代理

### Coder Agent
- **Agent 循环**: 基于 LLM 的智能对话
- **工具系统**: 可扩展的工具注册和执行
- **记忆系统**: FTS5 全文检索 + BM25 排序
- **技能系统**: SKILL.md 格式的技能定义
- **子 Agent**: Actor 模式的子任务调度
- **任务树**: SQLite 持久化的任务管理
- **定时任务**: Cron 表达式的定时调度
- **自我进化**: 自动学习和改进

### 多平台接入
- **Web**: React + TypeScript 前端（含 Chat 与 Code 代码开发页）
- **Telegram**: Bot API 集成
- **终端**: TUI 界面
- **微信**: 后续支持

## 技术栈

| 组件 | 技术 |
|------|------|
| 后端 | Go 1.22+ |
| Web 框架 | Gin |
| 数据库 | SQLite (modernc.org/sqlite, 纯 Go) |
| 前端 | React + TypeScript + Tailwind |
| TUI | Bubble Tea |

## 快速开始

### 安装

```bash
# 克隆项目
git clone https://github.com/alex/codegateway.git
cd codegateway

# 编译
CGO_ENABLED=0 go build -o codegateway ./cmd/server/
```

### 运行

```bash
# 使用默认配置运行
./codegateway

# 使用自定义配置
CODEGATEWAY_CONFIG=/path/to/config.yaml ./codegateway
```

### 配置

编辑 `codegateway.yaml`:

```yaml
server:
  host: "0.0.0.0"
  port: 8080

database:
  driver: "sqlite"
  dsn: "./data/codegateway.db"

gateway:
  enabled: true
  routing:
    strategy: "auto"  # auto, cost, latency, quality

platforms:
  telegram:
    enabled: true
    bot_token: "YOUR_BOT_TOKEN"
  web:
    enabled: true
```

## API 接口

### 网关接口

```bash
# OpenAI 兼容接口
curl http://localhost:8080/v1/gateway/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello"}]
  }'

# 列出可用模型（OpenAI 兼容）
curl http://localhost:8080/v1/models

# 查询单个模型
curl http://localhost:8080/v1/models/mimo-auto
```

模型列表响应格式与 OpenAI 一致：

```json
{
  "object": "list",
  "data": [
    {
      "id": "mimo-auto",
      "object": "model",
      "created": 1715367049,
      "owned_by": "mimo"
    }
  ]
}
```

SDK 可将 Base URL 设为 `http://localhost:8080/v1`，直接调用 `client.models.list()`。

### Agent 接口

```bash
# Agent 对话
curl http://localhost:8080/v1/agent/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "帮我写一个 Hello World"
  }'
```

### 账号认证

```bash
# 注册
curl -X POST http://localhost:8080/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","email":"alice@example.com","password":"secret1"}'

# 登录（默认管理员 admin / admin123，可用 CODEGATEWAY_ADMIN_PASSWORD 覆盖）
curl -X POST http://localhost:8080/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}'

# 当前用户
curl http://localhost:8080/v1/auth/me \
  -H "Authorization: Bearer <token>"

# 修改密码
curl -X POST http://localhost:8080/v1/auth/change-password \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"current_password":"admin123","new_password":"newpass1"}'
```

受保护接口需携带 `Authorization: Bearer <token>`。管理员可用 `X-Account-ID` 切换代管账号。

### 管理接口

```bash
# 列出账号（需 admin）
curl http://localhost:8080/v1/admin/accounts \
  -H "Authorization: Bearer <token>"

# 管理员创建账号
curl -X POST http://localhost:8080/v1/admin/accounts \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","email":"alice@example.com","password":"secret1"}'

# 列出当前账号的渠道
curl http://localhost:8080/v1/admin/channels \
  -H "Authorization: Bearer <token>"

# 创建渠道
curl -X POST http://localhost:8080/v1/admin/channels \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "openai",
    "type": 1,
    "key": "sk-xxx",
    "base_url": "https://api.openai.com/v1",
    "models": ["gpt-4o", "gpt-3.5-turbo"]
  }'

# 创建 Agnes 渠道（type=9，OpenAI 兼容）
curl -X POST http://localhost:8080/v1/admin/channels \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Agnes",
    "type": 9,
    "key": "YOUR_AGNES_API_KEY",
    "base_url": "https://apihub.agnes-ai.com/v1",
    "models": "[\"agnes-2.0-flash\",\"agnes-1.5-flash\"]"
  }'

# 创建 GLM / 智谱渠道（type=10，OpenAI 兼容）
curl -X POST http://localhost:8080/v1/admin/channels \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "GLM",
    "type": 10,
    "key": "YOUR_ZHIPU_API_KEY",
    "base_url": "https://open.bigmodel.cn/api/paas/v4",
    "models": "[\"glm-5.2\",\"glm-4.7\",\"glm-4-flash\"]"
  }'
```

每个账号拥有独立的 **channels** 与 **sessions** 数据；登录后按会话用户隔离，管理员可通过 `X-Account-ID` 代管其他账号。

## 项目结构

```
codegateway/
├── cmd/
│   ├── server/          # 主服务入口
│   ├── agent/           # Agent CLI 入口
│   └── migrate/         # 数据库迁移
├── internal/
│   ├── gateway/         # API 网关核心
│   ├── agent/           # Agent 核心
│   ├── tool/            # 工具系统
│   ├── provider/        # LLM Provider 抽象
│   ├── session/         # 会话管理
│   ├── platform/        # 消息平台适配
│   ├── model/           # 数据模型
│   ├── db/              # 数据库层
│   ├── config/          # 配置管理
│   └── ui/              # TUI 界面
├── web/                 # Web 前端
├── skills/              # 内置技能
├── deploy/              # 部署配置
├── codegateway.yaml     # 默认配置
├── main.go              # 入口文件
└── README.md
```

## 开发计划

### Phase 1: 基础框架 ✅
- [x] Go 项目初始化
- [x] SQLite 数据库层
- [x] 配置系统
- [x] 基础 HTTP 服务器
- [x] 用户认证框架
- [x] 多账号隔离（channels / sessions 按账号存储）

### Phase 2: API 网关 (进行中)
- [x] Channel 模型
- [x] OpenAI 适配器
- [x] Provider 抽象
- [ ] 格式转换层
- [ ] 智能路由
- [ ] Token 计费

### Phase 3: Agent 核心 (进行中)
- [x] Agent 循环框架
- [x] 工具系统框架
- [ ] 完整工具实现
- [ ] 会话管理
- [ ] 上下文优化

### Phase 4: 记忆和技能
- [ ] FTS5 记忆系统
- [ ] 技能发现和加载
- [ ] 任务树
- [ ] 定时任务

### Phase 5: 消息平台
- [ ] Web 平台 (WebSocket)
- [ ] Telegram 适配器
- [ ] 微信适配器

### Phase 6: 高级功能
- [ ] 子 Agent 调度
- [ ] 自我进化
- [ ] 内部 RAG
- [ ] 自动化测试

## 参考项目

- **new-api**: API 网关架构、Channel 管理、计费系统
- **sub2api**: 订阅配额分发、智能调度
- **MiMo-Code**: Agent 循环、记忆系统、技能系统、任务树
- **hermes-agent**: 消息平台集成、自我进化
- **crush**: Go TUI、SQLite 集成

## 许可证

MIT License

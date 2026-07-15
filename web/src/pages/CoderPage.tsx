import { useState, useEffect, useRef } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { apiFetch, useAccount } from '../context/AccountContext'

interface Message {
  id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  timestamp: Date
  model?: string
}

interface ModelOption {
  id: string
}

const quickTasks = [
  {
    id: 'implement',
    title: '实现功能',
    prompt: '请帮我实现以下功能，给出完整可运行代码，并说明关键设计：\n\n',
  },
  {
    id: 'review',
    title: '代码审查',
    prompt: '请审查以下代码，指出 bug、安全风险、性能问题，并给出改进建议与修改后代码：\n\n```\n\n```',
  },
  {
    id: 'refactor',
    title: '重构优化',
    prompt: '请重构以下代码，提升可读性与可维护性，保持行为不变，并说明改动点：\n\n```\n\n```',
  },
  {
    id: 'debug',
    title: '排查 Bug',
    prompt: '以下代码/报错有问题，请定位原因并给出修复方案与修复后代码：\n\n',
  },
  {
    id: 'explain',
    title: '解释代码',
    prompt: '请逐段解释以下代码的作用、数据流与潜在边界情况：\n\n```\n\n```',
  },
  {
    id: 'tests',
    title: '补测试',
    prompt: '请为以下代码编写单元测试，覆盖主流程与边界情况：\n\n```\n\n```',
  },
]

export default function CoderPage() {
  const { currentAccount } = useAccount()
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [sessionId, setSessionId] = useState('')
  const [models, setModels] = useState<ModelOption[]>([])
  const [selectedModel, setSelectedModel] = useState('')
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    fetchModels()
    setMessages([])
    setSessionId('')
  }, [currentAccount?.id])

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages, isLoading])

  const fetchModels = async () => {
    try {
      const response = await apiFetch('/v1/models', {}, currentAccount?.id)
      if (!response.ok) return
      const data = await response.json()
      const list: ModelOption[] = (data.data || []).map((m: { id: string }) => ({ id: m.id }))
      setModels(list)
      if (list.length > 0) {
        const preferred =
          list.find((m) => m.id.includes('mimo-auto')) ||
          list.find((m) => m.id.includes('glm')) ||
          list.find((m) => m.id.includes('coder') || m.id.includes('code')) ||
          list[0]
        setSelectedModel(preferred.id)
      } else {
        setSelectedModel('')
      }
    } catch (error) {
      console.error('Failed to fetch models:', error)
    }
  }

  const applyQuickTask = (prompt: string) => {
    setInput(prompt)
    requestAnimationFrame(() => {
      textareaRef.current?.focus()
      const len = prompt.length
      textareaRef.current?.setSelectionRange(len, len)
    })
  }

  const sendMessage = async () => {
    if (!input.trim() || isLoading) return

    const content = input
    const userMessage: Message = {
      id: Date.now().toString(),
      role: 'user',
      content,
      timestamp: new Date(),
    }

    setMessages((prev) => [...prev, userMessage])
    setInput('')
    setIsLoading(true)

    try {
      const response = await apiFetch(
        '/v1/agent/chat',
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            message: content,
            session_id: sessionId,
            mode: 'coder',
            model: selectedModel || undefined,
          }),
        },
        currentAccount?.id,
      )
      const data = await response.json()

      if (data.session_id) {
        setSessionId(data.session_id)
      }

      let reply = data.response || data.error || 'No response'
      if (reply.includes('MiMoCode backend') || reply.includes('create session failed')) {
        reply =
          '⚠️ ' +
          reply +
          '\n\n如需使用 MiMoCode 本地代理，请先启动：\n`mimo serve --hostname 127.0.0.1 --port 10001`\n默认免费通道（类型 7）无需本地服务，可直接调用 mimo-auto。'
      } else if (
        reply.includes('bootstrap failed') ||
        reply.includes('mimo-auto token') ||
        reply.includes('Illegal access')
      ) {
        reply =
          '⚠️ ' +
          reply +
          '\n\nMiMo Free 通道直连小米免费 API，请确认网络可访问 api.xiaomimimo.com，且 Channels 中存在类型 7 渠道。'
      } else if (reply.includes('no available channel')) {
        reply = '⚠️ 暂无可用渠道。请先到 Channels 页面添加 API Provider。'
      }

      setMessages((prev) => [
        ...prev,
        {
          id: (Date.now() + 1).toString(),
          role: 'assistant',
          content: reply,
          timestamp: new Date(),
          model: selectedModel || data.model,
        },
      ])
    } catch {
      setMessages((prev) => [
        ...prev,
        {
          id: (Date.now() + 1).toString(),
          role: 'assistant',
          content: 'Error: Failed to send message. Is the backend running?',
          timestamp: new Date(),
        },
      ])
    } finally {
      setIsLoading(false)
    }
  }

  const clearChat = () => {
    setMessages([])
    setSessionId('')
  }

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      sendMessage()
    }
  }

  return (
    <div className="flex flex-col h-full">
      <header className="h-14 flex items-center justify-between px-6 border-b border-border">
        <div>
          <h2 className="text-sm font-semibold text-foreground">Code</h2>
          <p className="text-[11px] text-muted-foreground">
            代码开发助手
            {sessionId && <span className="ml-2">Session: {sessionId.substring(0, 8)}...</span>}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <select
            value={selectedModel}
            onChange={(e) => setSelectedModel(e.target.value)}
            className="h-8 max-w-[200px] px-2 bg-card border border-border rounded-md text-[12px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
          >
            {models.length === 0 ? (
              <option value="">默认模型</option>
            ) : (
              models.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.id}
                </option>
              ))
            )}
          </select>
          <button
            onClick={clearChat}
            className="h-8 px-3 text-[12px] text-muted-foreground hover:text-foreground border border-border rounded-md hover:bg-accent transition-colors"
          >
            New Task
          </button>
        </div>
      </header>

      <div className="flex-1 overflow-auto p-6 space-y-4">
        {messages.length === 0 ? (
          <div className="flex items-center justify-center h-full">
            <div className="text-center animate-fade-in max-w-2xl w-full">
              <div className="w-12 h-12 rounded-xl bg-primary/10 flex items-center justify-center mx-auto mb-4">
                <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#3b82f6" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <polyline points="16 18 22 12 16 6" />
                  <polyline points="8 6 2 12 8 18" />
                </svg>
              </div>
              <h3 className="text-base font-semibold text-foreground mb-1.5">CodeGateway Code</h3>
              <p className="text-[13px] text-muted-foreground mb-6">
                专注写代码、审查、重构、调试与补测试。选择一个快捷任务开始，或直接描述你的开发需求。
              </p>
              <div className="grid grid-cols-2 md:grid-cols-3 gap-2 text-left">
                {quickTasks.map((task) => (
                  <button
                    key={task.id}
                    onClick={() => applyQuickTask(task.prompt)}
                    className="px-3 py-3 rounded-lg border border-border bg-card hover:bg-accent transition-colors text-[13px] font-medium text-foreground"
                  >
                    {task.title}
                  </button>
                ))}
              </div>
            </div>
          </div>
        ) : (
          messages.map((msg) => (
            <div
              key={msg.id}
              className={`flex ${msg.role === 'user' ? 'justify-end' : 'justify-start'} animate-fade-in`}
            >
              <div
                className={`max-w-[80%] rounded-xl px-4 py-2.5 ${
                  msg.role === 'user'
                    ? 'bg-primary text-primary-foreground'
                    : 'bg-card border border-border'
                }`}
              >
                {msg.role === 'user' ? (
                  <p className="text-[13px] leading-relaxed whitespace-pre-wrap">{msg.content}</p>
                ) : (
                  <div className="markdown-body text-[13px]">
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>{msg.content}</ReactMarkdown>
                  </div>
                )}
                <div
                  className={`flex items-center gap-2 mt-1.5 text-[10px] ${
                    msg.role === 'user' ? 'text-primary-foreground/60' : 'text-muted-foreground'
                  }`}
                >
                  <span>
                    {msg.timestamp.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                  </span>
                  {msg.model && (
                    <>
                      <span>·</span>
                      <span>{msg.model}</span>
                    </>
                  )}
                </div>
              </div>
            </div>
          ))
        )}

        {isLoading && (
          <div className="flex justify-start animate-fade-in">
            <div className="bg-card border border-border rounded-xl px-4 py-3">
              <div className="flex items-center gap-1.5">
                <div className="w-2 h-2 rounded-full bg-muted-foreground animate-bounce"></div>
                <div
                  className="w-2 h-2 rounded-full bg-muted-foreground animate-bounce"
                  style={{ animationDelay: '0.1s' }}
                ></div>
                <div
                  className="w-2 h-2 rounded-full bg-muted-foreground animate-bounce"
                  style={{ animationDelay: '0.2s' }}
                ></div>
              </div>
            </div>
          </div>
        )}

        <div ref={messagesEndRef} />
      </div>

      {messages.length > 0 && (
        <div className="px-4 pt-3 flex flex-wrap gap-2">
          {quickTasks.map((task) => (
            <button
              key={task.id}
              onClick={() => applyQuickTask(task.prompt)}
              className="h-7 px-2.5 text-[11px] text-muted-foreground border border-border rounded-md hover:bg-accent hover:text-foreground transition-colors"
            >
              {task.title}
            </button>
          ))}
        </div>
      )}

      <div className="p-4 border-t border-border">
        <div className="flex gap-2 items-end">
          <textarea
            ref={textareaRef}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={onKeyDown}
            placeholder="描述你的开发任务，或粘贴代码…（Enter 发送，Shift+Enter 换行）"
            rows={3}
            className="flex-1 px-4 py-2.5 bg-card border border-border rounded-lg text-[13px] text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent transition-colors resize-none font-mono"
            disabled={isLoading}
          />
          <button
            onClick={sendMessage}
            disabled={isLoading || !input.trim()}
            className="h-10 px-4 bg-primary text-primary-foreground rounded-lg text-[13px] font-medium hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors flex items-center gap-2"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <line x1="22" y1="2" x2="11" y2="13" />
              <polygon points="22 2 15 22 11 13 2 9 22 2" />
            </svg>
            Run
          </button>
        </div>
      </div>
    </div>
  )
}

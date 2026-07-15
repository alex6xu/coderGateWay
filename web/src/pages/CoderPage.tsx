import { useState, useEffect, useRef, ChangeEvent } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { apiFetch, useAccount } from '../context/AccountContext'

interface Message {
  id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  timestamp: Date
  model?: string
  toolSteps?: { tool: string; args: string; result: string }[]
}

interface ModelOption {
  id: string
}

interface WorkspaceInfo {
  id: string
  name: string
  file_count: number
  size_bytes: number
  created_at: string
  updated_at: string
}

const quickTasks = [
  {
    id: 'implement',
    title: '实现功能',
    prompt: '请在当前项目中实现以下功能，直接修改/新增文件，并说明改动点：\n\n',
  },
  {
    id: 'review',
    title: '代码审查',
    prompt: '请审查当前项目代码，指出 bug、安全风险、性能问题，必要时直接提交修复文件。重点关注：\n\n',
  },
  {
    id: 'refactor',
    title: '重构优化',
    prompt: '请重构当前项目相关模块，提升可读性与可维护性，保持行为不变，并写入修改后的文件。范围：\n\n',
  },
  {
    id: 'debug',
    title: '排查 Bug',
    prompt: '以下报错/现象有问题，请在项目中定位原因并直接修复文件：\n\n',
  },
  {
    id: 'explain',
    title: '解释结构',
    prompt: '请先浏览项目目录，解释整体结构、关键模块与数据流。',
  },
  {
    id: 'tests',
    title: '补测试',
    prompt: '请为当前项目补充单元测试，覆盖主流程与边界情况，并写入测试文件。目标模块：\n\n',
  },
]

function formatBytes(n: number) {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

export default function CoderPage() {
  const { currentAccount } = useAccount()
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [sessionId, setSessionId] = useState('')
  const [models, setModels] = useState<ModelOption[]>([])
  const [selectedModel, setSelectedModel] = useState('')
  const [workspaces, setWorkspaces] = useState<WorkspaceInfo[]>([])
  const [workspaceId, setWorkspaceId] = useState('')
  const [uploading, setUploading] = useState(false)
  const [uploadError, setUploadError] = useState('')
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const dirInputRef = useRef<HTMLInputElement>(null)

  const activeWorkspace = workspaces.find((w) => w.id === workspaceId) || null

  useEffect(() => {
    fetchModels()
    fetchWorkspaces()
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

  const fetchWorkspaces = async () => {
    try {
      const response = await apiFetch('/v1/workspaces', {}, currentAccount?.id)
      if (!response.ok) return
      const data = await response.json()
      const list: WorkspaceInfo[] = data.workspaces || []
      setWorkspaces(list)
      if (list.length > 0) {
        setWorkspaceId((prev) => (prev && list.some((w) => w.id === prev) ? prev : list[0].id))
      } else {
        setWorkspaceId('')
      }
    } catch (error) {
      console.error('Failed to fetch workspaces:', error)
    }
  }

  const onSelectDirectory = async (e: ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files
    if (!files || files.length === 0) return

    setUploading(true)
    setUploadError('')
    try {
      const form = new FormData()
      const firstRel = (files[0] as File & { webkitRelativePath?: string }).webkitRelativePath || files[0].name
      const top = firstRel.split('/')[0] || 'project'
      form.append('name', top)

      let count = 0
      for (let i = 0; i < files.length; i++) {
        const file = files[i] as File & { webkitRelativePath?: string }
        const rel = file.webkitRelativePath || file.name
        if (!rel || rel.includes('node_modules/') || rel.includes('/.git/') || rel.startsWith('.git/')) continue
        form.append('files', file, rel)
        count++
        if (count >= 5000) break
      }
      if (count === 0) {
        setUploadError('目录为空或被过滤（如 node_modules/.git）')
        return
      }

      const token = localStorage.getItem('codegateway_auth_token')
      const headers: Record<string, string> = {}
      if (token) headers['Authorization'] = `Bearer ${token}`
      if (currentAccount?.id) headers['X-Account-ID'] = String(currentAccount.id)

      const response = await fetch('/v1/workspaces/upload', {
        method: 'POST',
        headers,
        body: form,
      })
      const data = await response.json().catch(() => ({}))
      if (!response.ok) {
        setUploadError(data.error || '上传失败')
        return
      }
      await fetchWorkspaces()
      if (data.workspace?.id) {
        setWorkspaceId(data.workspace.id)
        setMessages((prev) => [
          ...prev,
          {
            id: Date.now().toString(),
            role: 'system',
            content: `已上传云端工作区「${data.workspace.name}」（${data.workspace.file_count} 个文件，${formatBytes(data.workspace.size_bytes)}）。现在可以直接描述要改的功能，Agent 会在云端目录里读改文件。`,
            timestamp: new Date(),
          },
        ])
      }
    } catch (error) {
      console.error(error)
      setUploadError('上传失败，请重试')
    } finally {
      setUploading(false)
      if (dirInputRef.current) dirInputRef.current.value = ''
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

    const assistantId = (Date.now() + 1).toString()
    setMessages((prev) => [
      ...prev,
      {
        id: assistantId,
        role: 'assistant',
        content: '',
        timestamp: new Date(),
        model: selectedModel || undefined,
        toolSteps: [],
      },
    ])

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
            workspace_id: workspaceId || undefined,
            stream: true,
          }),
        },
        currentAccount?.id,
      )

      if (!response.ok || !response.body) {
        const data = await response.json().catch(() => ({}))
        throw new Error(data.error || `HTTP ${response.status}`)
      }

      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      let fullText = ''
      const steps: { tool: string; args: string; result: string }[] = []

      const applyAssistant = (patch: Partial<Message>) => {
        setMessages((prev) =>
          prev.map((m) => (m.id === assistantId ? { ...m, ...patch } : m)),
        )
      }

      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const parts = buffer.split('\n\n')
        buffer = parts.pop() || ''
        for (const part of parts) {
          const line = part.trim()
          if (!line.startsWith('data:')) continue
          const payload = line.replace(/^data:\s*/, '')
          if (payload === '[DONE]') continue
          let ev: {
            type?: string
            content?: string
            session_id?: string
            model?: string
            step?: { tool: string; args: string; result: string }
            tool_steps?: { tool: string; args: string; result: string }[]
          }
          try {
            ev = JSON.parse(payload)
          } catch {
            continue
          }
          if (ev.session_id) setSessionId(ev.session_id)
          if (ev.type === 'delta' && ev.content) {
            fullText += ev.content
            applyAssistant({ content: fullText, model: ev.model || selectedModel })
          } else if (ev.type === 'tool_step' && ev.step) {
            steps.push(ev.step)
            applyAssistant({ toolSteps: [...steps] })
          } else if (ev.type === 'done') {
            if (ev.content) fullText = ev.content
            if (ev.tool_steps?.length) {
              steps.splice(0, steps.length, ...ev.tool_steps)
            }
            applyAssistant({
              content: fullText,
              model: ev.model || selectedModel,
              toolSteps: steps.length ? [...steps] : undefined,
            })
          } else if (ev.type === 'error') {
            fullText = ev.content || 'Agent error'
            applyAssistant({ content: fullText })
          }
        }
      }

      if (!fullText) {
        applyAssistant({ content: 'No response' })
      } else if (
        fullText.includes('MiMoCode backend') ||
        fullText.includes('create session failed')
      ) {
        applyAssistant({
          content:
            '⚠️ ' +
            fullText +
            '\n\n如需使用 MiMoCode 本地代理，请先启动：\n`mimo serve --hostname 127.0.0.1 --port 10001`\n默认免费通道（类型 7）无需本地服务，可直接调用 mimo-auto。',
        })
      } else if (fullText.includes('no available channel')) {
        applyAssistant({
          content: '⚠️ 暂无可用渠道。请先到 Channels 页面添加 API Provider。',
        })
      }
    } catch (err) {
      setMessages((prev) =>
        prev.map((m) =>
          m.id === assistantId
            ? {
                ...m,
                content:
                  err instanceof Error
                    ? `Error: ${err.message}`
                    : 'Error: Failed to send message. Is the backend running?',
              }
            : m,
        ),
      )
    } finally {
      setIsLoading(false)
    }
  }

  const clearChat = () => {
    setMessages([])
    setSessionId('')
  }

  const downloadWorkspace = () => {
    if (!workspaceId) return
    const token = localStorage.getItem('codegateway_auth_token')
    const url = `/v1/workspaces/${workspaceId}/download`
    // open with token via fetch blob
    fetch(url, {
      headers: {
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
        ...(currentAccount?.id ? { 'X-Account-ID': String(currentAccount.id) } : {}),
      },
    })
      .then((r) => r.blob())
      .then((blob) => {
        const a = document.createElement('a')
        a.href = URL.createObjectURL(blob)
        a.download = `${activeWorkspace?.name || 'project'}.zip`
        a.click()
        URL.revokeObjectURL(a.href)
      })
      .catch((err) => console.error(err))
  }

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      sendMessage()
    }
  }

  return (
    <div className="flex flex-col h-full">
      <header className="h-14 flex items-center justify-between px-6 border-b border-border gap-3">
        <div className="min-w-0">
          <h2 className="text-sm font-semibold text-foreground">Code</h2>
          <p className="text-[11px] text-muted-foreground truncate">
            选择本地目录上传云端后，用自然语言让 Agent 改代码
            {sessionId && <span className="ml-2">Session: {sessionId.substring(0, 8)}...</span>}
          </p>
        </div>
        <div className="flex items-center gap-2 flex-shrink-0">
          <select
            value={selectedModel}
            onChange={(e) => setSelectedModel(e.target.value)}
            className="h-8 max-w-[180px] px-2 bg-card border border-border rounded-md text-[12px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
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

      <div className="px-6 py-3 border-b border-border bg-card/40 flex flex-wrap items-center gap-2">
        <input
          ref={dirInputRef}
          type="file"
          className="hidden"
          // @ts-expect-error webkitdirectory is supported in Chromium
          webkitdirectory=""
          multiple
          onChange={onSelectDirectory}
        />
        <button
          onClick={() => dirInputRef.current?.click()}
          disabled={uploading}
          className="h-8 px-3 text-[12px] bg-primary text-primary-foreground rounded-md hover:bg-primary/90 disabled:opacity-50"
        >
          {uploading ? '上传中…' : '选择本地目录并上传云端'}
        </button>
        <select
          value={workspaceId}
          onChange={(e) => setWorkspaceId(e.target.value)}
          className="h-8 min-w-[180px] px-2 bg-card border border-border rounded-md text-[12px]"
        >
          <option value="">未选择工作区</option>
          {workspaces.map((w) => (
            <option key={w.id} value={w.id}>
              {w.name} ({w.file_count} files)
            </option>
          ))}
        </select>
        {workspaceId && (
          <button
            onClick={downloadWorkspace}
            className="h-8 px-3 text-[12px] border border-border rounded-md hover:bg-accent text-muted-foreground"
          >
            下载修改后的 zip
          </button>
        )}
        {activeWorkspace && (
          <span className="text-[11px] text-muted-foreground">
            云端：{activeWorkspace.name} · {activeWorkspace.file_count} 文件 · {formatBytes(activeWorkspace.size_bytes)}
          </span>
        )}
        {uploadError && <span className="text-[12px] text-red-500">{uploadError}</span>}
      </div>

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
              <h3 className="text-base font-semibold text-foreground mb-1.5">AI 编码工作流</h3>
              <p className="text-[13px] text-muted-foreground mb-4 text-left leading-relaxed">
                1. 点击「选择本地目录并上传云端」把项目同步到服务器工作区<br />
                2. 用快捷任务或自然语言描述需求（例如：给 user 模块加分页 API）<br />
                3. Agent 会在云端目录里 list/read/grep/write 文件完成修改<br />
                4. 用「下载修改后的 zip」拿回结果，或继续多轮对话迭代
              </p>
              {!workspaceId && (
                <p className="text-[12px] text-amber-600 mb-4">尚未选择工作区：仍可聊天要代码片段，但无法直接改你的项目文件。</p>
              )}
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
                    : msg.role === 'system'
                      ? 'bg-amber-500/10 border border-amber-500/30'
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
                {msg.toolSteps && msg.toolSteps.length > 0 && (
                  <details className="mt-2 text-[11px] text-muted-foreground">
                    <summary className="cursor-pointer">工具调用 {msg.toolSteps.length} 步</summary>
                    <ul className="mt-1 space-y-1">
                      {msg.toolSteps.map((s, idx) => (
                        <li key={idx} className="font-mono truncate">
                          {s.tool}
                        </li>
                      ))}
                    </ul>
                  </details>
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
                <div className="w-2 h-2 rounded-full bg-muted-foreground animate-bounce" style={{ animationDelay: '0.1s' }}></div>
                <div className="w-2 h-2 rounded-full bg-muted-foreground animate-bounce" style={{ animationDelay: '0.2s' }}></div>
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
            placeholder={
              workspaceId
                ? '描述要改的功能，例如：给登录接口加上失败次数限制…（Enter 发送）'
                : '先上传项目目录，或直接粘贴代码提问…'
            }
            rows={3}
            className="flex-1 px-4 py-2.5 bg-card border border-border rounded-lg text-[13px] text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent transition-colors resize-none font-mono"
            disabled={isLoading}
          />
          <button
            onClick={sendMessage}
            disabled={isLoading || !input.trim()}
            className="h-10 px-4 bg-primary text-primary-foreground rounded-lg text-[13px] font-medium hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors flex items-center gap-2"
          >
            Run
          </button>
        </div>
      </div>
    </div>
  )
}

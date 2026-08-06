import { useState, useEffect, useRef, useCallback } from 'react'
import { apiFetch, useAccount } from '../context/AccountContext'
import VoiceInputButton from '../components/VoiceInputButton'
import { useVoiceInput } from '../hooks/useVoiceInput'
import {
  attachToolStepsToMessages,
  chatSessionKey,
  clearLocal,
  readLocal,
  writeLocal,
  type SessionRestorePayload,
} from '../lib/sessionPersist'

interface Message {
  id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  timestamp: Date
  model?: string
  toolSteps?: { tool: string; args: string; result: string }[]
}

export default function ChatPage() {
  const { currentAccount } = useAccount()
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [connected, setConnected] = useState(false)
  const [sessionId, setSessionId] = useState('')
  const [runId, setRunId] = useState('')
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const runAbortRef = useRef<AbortController | null>(null)
  const restoreGenRef = useRef(0)

  const storageKey = currentAccount?.id ? chatSessionKey(currentAccount.id) : ''

  useEffect(() => {
    if (!currentAccount?.id || !storageKey) return

    const saved =
      readLocal(storageKey) ||
      (typeof sessionStorage !== 'undefined' ? sessionStorage.getItem(storageKey) || '' : '')

    const gen = ++restoreGenRef.current
    let cancelled = false

    ;(async () => {
      // Lightweight connectivity probe via models list (no WS dependency for chat persistence).
      try {
        const probe = await apiFetch('/v1/models', {}, currentAccount.id)
        if (!cancelled) setConnected(probe.ok)
      } catch {
        if (!cancelled) setConnected(false)
      }

      if (!saved) {
        setMessages([])
        setSessionId('')
        setRunId('')
        setIsLoading(false)
        return
      }

      try {
        const res = await apiFetch(`/v1/agent/sessions/${saved}`, {}, currentAccount.id)
        if (!res.ok || cancelled || gen !== restoreGenRef.current) return
        const data = (await res.json()) as SessionRestorePayload
        if (cancelled || gen !== restoreGenRef.current) return

        setSessionId(saved)
        writeLocal(storageKey, saved)

        let restored: Message[] = (data.messages || []).map((m) => ({
          id: m.id,
          role: m.role as Message['role'],
          content: m.content || '',
          timestamp: m.created_at ? new Date(m.created_at) : new Date(),
          model: m.model,
        }))

        const active = data.active_run
        if (active && (active.status === 'running' || active.status === 'queued')) {
          const assistantId = `run-${active.id}`
          if (!restored.some((m) => m.id === assistantId)) {
            restored = [
              ...restored,
              {
                id: assistantId,
                role: 'assistant',
                content: '',
                timestamp: new Date(),
                model: active.model,
                toolSteps: data.active_run_tool_steps || [],
              },
            ]
          } else {
            restored = attachToolStepsToMessages(restored, data.active_run_tool_steps)
          }
          setMessages(restored)
          setRunId(active.id)
          setIsLoading(true)
          await consumeRunEvents(active.id, 0, assistantId, active.model)
        } else {
          restored = attachToolStepsToMessages(restored, data.latest_run_tool_steps)
          setMessages(restored)
          setRunId('')
          setIsLoading(false)
        }
      } catch (e) {
        if (!cancelled) console.error('restore chat session failed', e)
      }
    })()

    return () => {
      cancelled = true
      runAbortRef.current?.abort()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentAccount?.id, storageKey])

  useEffect(() => {
    if (sessionId && storageKey) {
      writeLocal(storageKey, sessionId)
      try {
        sessionStorage.setItem(storageKey, sessionId)
      } catch {
        /* ignore */
      }
    }
  }, [sessionId, storageKey])

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages, isLoading])

  const appendVoiceText = useCallback((text: string) => {
    setInput((prev) => {
      const base = prev.trimEnd()
      if (!base) return text
      const needsSpace = !/[\s\n]$/.test(base) && !/^[，。！？、,.!?]/.test(text)
      return base + (needsSpace ? ' ' : '') + text
    })
  }, [])

  const voice = useVoiceInput({
    lang: 'zh-CN',
    accountId: currentAccount?.id,
    onTranscript: (text, meta) => {
      if (meta.final) appendVoiceText(text)
    },
  })

  type AgentStreamEvent = {
    type?: string
    content?: string
    session_id?: string
    model?: string
    step?: { tool: string; args: string; result: string }
    tool_steps?: { tool: string; args: string; result: string }[]
  }

  const consumeRunEvents = async (
    targetRunId: string,
    afterSeq: number,
    assistantId: string,
    fallbackModel?: string,
  ) => {
    runAbortRef.current?.abort()
    const ac = new AbortController()
    runAbortRef.current = ac
    const response = await apiFetch(
      `/v1/agent/runs/${targetRunId}/events?after_seq=${afterSeq}`,
      { signal: ac.signal },
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
      setMessages((prev) => prev.map((m) => (m.id === assistantId ? { ...m, ...patch } : m)))
    }
    try {
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
          let ev: AgentStreamEvent
          try {
            ev = JSON.parse(payload)
          } catch {
            continue
          }
          if (ev.session_id) setSessionId(ev.session_id)
          if (ev.type === 'delta' && ev.content) {
            fullText += ev.content
            applyAssistant({ content: fullText, model: ev.model || fallbackModel })
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
              model: ev.model || fallbackModel,
              toolSteps: steps.length ? [...steps] : undefined,
            })
          } else if (ev.type === 'error') {
            fullText = ev.content || 'error'
            applyAssistant({ content: fullText })
          }
        }
      }
    } catch (err) {
      if ((err as Error).name === 'AbortError') return
      throw err
    } finally {
      if (runAbortRef.current === ac) {
        setIsLoading(false)
        setRunId('')
      }
    }
    if (!fullText) {
      applyAssistant({ content: 'No response' })
    }
  }

  const sendMessage = async () => {
    if (!input.trim() || isLoading) return
    if (voice.listening) {
      await voice.stop()
    }

    const text = input
    const userMessage: Message = {
      id: Date.now().toString(),
      role: 'user',
      content: text,
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
          body: JSON.stringify({
            message: text,
            session_id: sessionId,
            stream: false,
          }),
        },
        currentAccount?.id,
      )
      const data = await response.json()
      if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`)

      if (data.session_id) {
        setSessionId(data.session_id)
        if (storageKey) writeLocal(storageKey, data.session_id)
      }

      const assistantId = data.run_id ? `run-${data.run_id}` : Date.now().toString()
      setMessages((prev) => [
        ...prev,
        {
          id: assistantId,
          role: 'assistant',
          content: '',
          timestamp: new Date(),
          toolSteps: [],
        },
      ])

      if (data.run_id) {
        setRunId(data.run_id)
        await consumeRunEvents(data.run_id, 0, assistantId)
      } else {
        setMessages((prev) =>
          prev.map((m) =>
            m.id === assistantId ? { ...m, content: data.response || data.error || 'No response' } : m,
          ),
        )
        setIsLoading(false)
      }
    } catch (error) {
      setMessages((prev) => [
        ...prev,
        {
          id: Date.now().toString(),
          role: 'assistant',
          content: 'Error: Failed to send message. Is the backend running?',
          timestamp: new Date(),
        },
      ])
      setIsLoading(false)
    }
  }

  const clearChat = () => {
    runAbortRef.current?.abort()
    setMessages([])
    setSessionId('')
    setRunId('')
    setIsLoading(false)
    if (storageKey) {
      clearLocal(storageKey)
      try {
        sessionStorage.removeItem(storageKey)
      } catch {
        /* ignore */
      }
    }
  }

  return (
    <div className="flex flex-col h-full">
      <header className="h-14 flex items-center justify-between px-6 border-b border-border">
        <div>
          <h2 className="text-sm font-semibold text-foreground">Chat</h2>
          <p className="text-[11px] text-muted-foreground">
            {connected ? (
              <span className="flex items-center gap-1.5">
                <span className="w-1.5 h-1.5 rounded-full bg-success"></span>
                Connected
                {sessionId && <span className="ml-2">Session: {sessionId.substring(0, 8)}...</span>}
                {runId && <span className="ml-2 text-amber-600">运行中</span>}
              </span>
            ) : (
              <span className="flex items-center gap-1.5">
                <span className="w-1.5 h-1.5 rounded-full bg-destructive"></span>
                Disconnected
              </span>
            )}
          </p>
        </div>
        <button
          onClick={clearChat}
          className="h-8 px-3 text-[12px] text-muted-foreground hover:text-foreground border border-border rounded-md hover:bg-accent transition-colors"
        >
          Clear
        </button>
      </header>

      <div className="flex-1 overflow-auto p-6 space-y-4">
        {messages.length === 0 ? (
          <div className="flex items-center justify-center h-full">
            <div className="text-center animate-fade-in">
              <div className="w-12 h-12 rounded-xl bg-primary/10 flex items-center justify-center mx-auto mb-4">
                <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#3b82f6" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
                </svg>
              </div>
              <h3 className="text-base font-semibold text-foreground mb-1.5">Start a conversation</h3>
              <p className="text-[13px] text-muted-foreground mb-4 max-w-sm">
                Ask anything. Switching pages will keep this session and reopen any in-progress reply.
              </p>
              <div className="text-left bg-card border border-border rounded-xl p-4 max-w-sm mx-auto">
                <p className="text-[12px] font-medium text-foreground mb-2">Tips:</p>
                <ul className="text-[12px] text-muted-foreground space-y-1.5">
                  <li>1. Go to Channels page and add your API provider</li>
                  <li>2. Set a channel as default</li>
                  <li>3. Come back and start chatting</li>
                </ul>
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
                <p className="text-[13px] whitespace-pre-wrap">{msg.content}</p>
                {msg.toolSteps && msg.toolSteps.length > 0 && (
                  <div className="mt-2 space-y-1 border-t border-border/60 pt-2">
                    {msg.toolSteps.map((step, idx) => (
                      <details key={`${msg.id}-tool-${idx}`} className="text-[11px] text-muted-foreground">
                        <summary className="cursor-pointer hover:text-foreground">
                          {step.tool}
                        </summary>
                        <pre className="mt-1 whitespace-pre-wrap break-all opacity-80">
                          {step.args}
                          {'\n---\n'}
                          {step.result}
                        </pre>
                      </details>
                    ))}
                  </div>
                )}
                <p className={`text-[11px] mt-1.5 ${msg.role === 'user' ? 'text-primary-foreground/60' : 'text-muted-foreground'}`}>
                  {msg.timestamp.toLocaleTimeString()}
                </p>
              </div>
            </div>
          ))
        )}
        {isLoading && (
          <div className="flex justify-start animate-fade-in">
            <div className="bg-card border border-border rounded-xl px-4 py-3">
              <div className="flex items-center gap-1.5">
                <div className="w-1.5 h-1.5 bg-muted-foreground rounded-full animate-pulse"></div>
                <div className="w-1.5 h-1.5 bg-muted-foreground rounded-full animate-pulse" style={{ animationDelay: '0.2s' }}></div>
                <div className="w-1.5 h-1.5 bg-muted-foreground rounded-full animate-pulse" style={{ animationDelay: '0.4s' }}></div>
              </div>
            </div>
          </div>
        )}
        <div ref={messagesEndRef} />
      </div>

      <div className="p-4 border-t border-border">
        <div className="flex gap-2 max-w-3xl mx-auto">
          <textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                void sendMessage()
              }
            }}
            placeholder="Type a message..."
            rows={1}
            className="flex-1 px-4 py-2.5 bg-card border border-border rounded-xl text-[13px] text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring resize-none"
          />
          <VoiceInputButton
            listening={voice.listening}
            supported={voice.supported}
            disabled={isLoading}
            title={
              voice.engine === 'server'
                ? '语音输入（服务端 ASR）'
                : '语音输入（浏览器 Web Speech）'
            }
            onClick={() => void voice.toggle()}
          />
          <button
            onClick={() => void sendMessage()}
            disabled={!input.trim() || isLoading}
            className="h-10 px-4 bg-primary text-primary-foreground rounded-xl text-[13px] font-medium hover:bg-primary/90 disabled:opacity-50 transition-colors"
          >
            Send
          </button>
        </div>
      </div>
    </div>
  )
}

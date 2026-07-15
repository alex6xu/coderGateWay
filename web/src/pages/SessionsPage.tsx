import { useState, useEffect } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { apiFetch, useAccount } from '../context/AccountContext'

interface Session {
  id: string
  title: string
  platform: string
  message_count: number
  created_at: string
  updated_at: string
}

interface Message {
  id: string
  role: string
  content: string
  model?: string
  provider?: string
  created_at: string
}

export default function SessionsPage() {
  const { currentAccount } = useAccount()
  const [sessions, setSessions] = useState<Session[]>([])
  const [selectedSession, setSelectedSession] = useState<string | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (currentAccount) {
      setSelectedSession(null)
      setMessages([])
      fetchSessions()
    }
  }, [currentAccount?.id])

  const fetchSessions = async () => {
    try {
      const response = await apiFetch('/v1/agent/sessions', {}, currentAccount?.id)
      if (response.ok) {
        const data = await response.json()
        setSessions(data.sessions || [])
      }
    } catch (error) {
      console.error('Failed to fetch sessions:', error)
    }
  }

  const fetchSessionDetail = async (sessionId: string) => {
    setLoading(true)
    setSelectedSession(sessionId)
    try {
      const response = await apiFetch(`/v1/agent/sessions/${sessionId}`, {}, currentAccount?.id)
      if (response.ok) {
        const data = await response.json()
        setMessages(data.messages || [])
      }
    } catch (error) {
      console.error('Failed to fetch session detail:', error)
    } finally {
      setLoading(false)
    }
  }

  const platformIcons: Record<string, string> = {
    web: '🌐',
    coder: '🛠️',
    telegram: '📱',
    terminal: '💻',
    wechat: '💬',
  }

  const formatDate = (dateStr: string) => {
    const date = new Date(dateStr)
    return date.toLocaleDateString() + ' ' + date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }

  return (
    <div className="flex h-full">
      {/* Sessions List */}
      <div className={`border-r border-border ${selectedSession ? 'w-80' : 'flex-1'} overflow-auto`}>
        <div className="p-4 border-b border-border">
          <h2 className="text-base font-semibold text-foreground">Sessions</h2>
          <p className="text-[13px] text-muted-foreground mt-0.5">
            {sessions.length} sessions · {currentAccount?.username || 'account'}
          </p>
        </div>

        <div className="divide-y divide-border">
          {sessions.length === 0 ? (
            <div className="p-8 text-center">
              <div className="w-10 h-10 rounded-lg bg-muted flex items-center justify-center mx-auto mb-3">
                <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#71717a" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
                </svg>
              </div>
              <p className="text-[13px] text-muted-foreground">No sessions yet</p>
            </div>
          ) : (
            sessions.map((session) => (
              <div
                key={session.id}
                onClick={() => fetchSessionDetail(session.id)}
                className={`p-4 cursor-pointer hover:bg-accent/50 transition-colors ${
                  selectedSession === session.id ? 'bg-accent' : ''
                }`}
              >
                <div className="flex items-start justify-between mb-1">
                  <h3 className="text-[13px] font-medium text-foreground truncate flex-1">
                    {session.title || 'Untitled Session'}
                  </h3>
                  <span className="text-sm ml-2">{platformIcons[session.platform] || '📋'}</span>
                </div>
                <div className="flex items-center gap-2 text-[12px] text-muted-foreground">
                  <span>{session.message_count} messages</span>
                  <span>·</span>
                  <span>{session.platform}</span>
                </div>
                <p className="text-[11px] text-muted-foreground/60 mt-1">
                  {formatDate(session.updated_at)}
                </p>
              </div>
            ))
          )}
        </div>
      </div>

      {/* Session Detail */}
      {selectedSession && (
        <div className="flex-1 flex flex-col overflow-hidden">
          <div className="p-4 border-b border-border flex items-center justify-between">
            <div>
              <h3 className="text-sm font-semibold text-foreground">
                {sessions.find(s => s.id === selectedSession)?.title || 'Session Detail'}
              </h3>
              <p className="text-[12px] text-muted-foreground">
                {messages.length} messages · ID: {selectedSession.substring(0, 8)}...
              </p>
            </div>
            <button
              onClick={() => { setSelectedSession(null); setMessages([]); }}
              className="h-8 px-3 text-[12px] text-muted-foreground hover:text-foreground border border-border rounded-md hover:bg-accent transition-colors"
            >
              Close
            </button>
          </div>

          <div className="flex-1 overflow-auto p-4 space-y-4">
            {loading ? (
              <div className="flex items-center justify-center h-full">
                <div className="flex items-center gap-2 text-muted-foreground text-[13px]">
                  <div className="w-4 h-4 border-2 border-primary border-t-transparent rounded-full animate-spin"></div>
                  Loading...
                </div>
              </div>
            ) : messages.length === 0 ? (
              <div className="flex items-center justify-center h-full text-muted-foreground text-[13px]">
                No messages in this session
              </div>
            ) : (
              messages.map((msg) => (
                <div
                  key={msg.id}
                  className={`flex ${msg.role === 'user' ? 'justify-end' : 'justify-start'}`}
                >
                  <div
                    className={`max-w-[75%] rounded-xl px-4 py-2.5 ${
                      msg.role === 'user'
                        ? 'bg-primary text-primary-foreground'
                        : 'bg-card border border-border'
                    }`}
                  >
                    {msg.role === 'user' ? (
                      <p className="text-[13px] leading-relaxed whitespace-pre-wrap">{msg.content}</p>
                    ) : (
                      <div className="markdown-body text-[13px]">
                        <ReactMarkdown remarkPlugins={[remarkGfm]}>
                          {msg.content}
                        </ReactMarkdown>
                      </div>
                    )}
                    <div className={`flex items-center gap-2 mt-1.5 text-[10px] ${msg.role === 'user' ? 'text-primary-foreground/60' : 'text-muted-foreground'}`}>
                      <span>{formatDate(msg.created_at)}</span>
                      {msg.model && (
                        <>
                          <span>·</span>
                          <span>{msg.model}</span>
                        </>
                      )}
                      {msg.provider && (
                        <>
                          <span>·</span>
                          <span>{msg.provider}</span>
                        </>
                      )}
                    </div>
                  </div>
                </div>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  )
}

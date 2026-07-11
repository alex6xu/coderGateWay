import { useState, useEffect } from 'react'

interface Session {
  id: string
  title: string
  platform: string
  message_count: number
  created_at: string
  updated_at: string
}

export default function SessionsPage() {
  const [sessions, setSessions] = useState<Session[]>([])

  useEffect(() => {
    fetchSessions()
  }, [])

  const fetchSessions = async () => {
    try {
      const response = await fetch('/v1/agent/sessions')
      if (response.ok) {
        const data = await response.json()
        setSessions(data.sessions || [])
      }
    } catch (error) {
      console.error('Failed to fetch sessions:', error)
    }
  }

  const platformIcons: Record<string, string> = {
    web: '🌐',
    telegram: '📱',
    terminal: '💻',
    wechat: '💬',
  }

  return (
    <div className="p-6">
      <div className="mb-6">
        <h2 className="text-base font-semibold text-foreground">Sessions</h2>
        <p className="text-[13px] text-muted-foreground mt-0.5">View and manage your conversation sessions</p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
        {sessions.length === 0 ? (
          <div className="col-span-full bg-card border border-border rounded-xl p-12">
            <div className="flex flex-col items-center">
              <div className="w-10 h-10 rounded-lg bg-muted flex items-center justify-center mb-3">
                <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#71717a" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
                </svg>
              </div>
              <p className="text-[13px] text-muted-foreground">No sessions yet</p>
              <p className="text-[12px] text-muted-foreground/60 mt-1">Start a chat to create a session</p>
            </div>
          </div>
        ) : (
          sessions.map((session) => (
            <div
              key={session.id}
              className="bg-card border border-border rounded-xl p-4 hover:border-border/80 cursor-pointer transition-colors group"
            >
              <div className="flex items-start justify-between mb-2">
                <h3 className="text-[13px] font-medium text-foreground truncate group-hover:text-primary transition-colors">
                  {session.title || 'Untitled Session'}
                </h3>
                <span className="text-lg ml-2">{platformIcons[session.platform] || '📋'}</span>
              </div>
              <div className="flex items-center gap-3 text-[12px] text-muted-foreground">
                <span className="flex items-center gap-1">
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
                  </svg>
                  {session.message_count} messages
                </span>
                <span>{session.platform}</span>
              </div>
              <p className="text-[11px] text-muted-foreground/60 mt-2">
                {new Date(session.updated_at).toLocaleDateString()} {new Date(session.updated_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
              </p>
            </div>
          ))
        )}
      </div>
    </div>
  )
}

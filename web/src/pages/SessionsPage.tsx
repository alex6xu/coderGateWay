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

  return (
    <div className="p-6">
      <h2 className="text-2xl font-bold text-white mb-6">Sessions</h2>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {sessions.length === 0 ? (
          <div className="col-span-full text-center py-12 text-gray-500">
            No sessions yet
          </div>
        ) : (
          sessions.map((session) => (
            <div
              key={session.id}
              className="bg-gray-800 rounded-lg p-4 border border-gray-700 hover:border-gray-600 cursor-pointer"
            >
              <div className="flex items-start justify-between mb-2">
                <h3 className="text-white font-medium truncate">
                  {session.title || 'Untitled Session'}
                </h3>
                <span className="text-xs px-2 py-1 bg-gray-700 text-gray-400 rounded">
                  {session.platform}
                </span>
              </div>
              <p className="text-sm text-gray-500 mb-3">
                {session.message_count} messages
              </p>
              <p className="text-xs text-gray-600">
                {new Date(session.updated_at).toLocaleString()}
              </p>
            </div>
          ))
        )}
      </div>
    </div>
  )
}

import { useState, useEffect, useRef } from 'react'

interface Message {
  id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  timestamp: Date
}

export default function ChatPage() {
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [ws, setWs] = useState<WebSocket | null>(null)
  const [connected, setConnected] = useState(false)
  const [sessionId, setSessionId] = useState('')
  const messagesEndRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    connectWebSocket()
    return () => {
      if (ws) ws.close()
    }
  }, [])

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  const connectWebSocket = () => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/ws`
    
    const websocket = new WebSocket(wsUrl)
    
    websocket.onopen = () => {
      setWs(websocket)
      setConnected(true)
    }
    
    websocket.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)
        
        if (data.type === 'connected') {
          setSessionId(data.session_id)
          return
        }
        
        if (data.role === 'assistant') {
          setMessages(prev => [...prev, {
            id: Date.now().toString(),
            role: 'assistant',
            content: data.content,
            timestamp: new Date(),
          }])
          setIsLoading(false)
        }
      } catch (e) {
        console.error('Failed to parse message:', e)
      }
    }
    
    websocket.onclose = () => {
      setWs(null)
      setConnected(false)
      // Reconnect after 3 seconds
      setTimeout(connectWebSocket, 3000)
    }
    
    websocket.onerror = () => {
      setConnected(false)
    }
  }

  const sendMessage = async () => {
    if (!input.trim() || isLoading) return

    const userMessage: Message = {
      id: Date.now().toString(),
      role: 'user',
      content: input,
      timestamp: new Date(),
    }

    setMessages(prev => [...prev, userMessage])
    setInput('')
    setIsLoading(true)

    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({
        role: 'user',
        content: input,
      }))
    } else {
      // Fallback to HTTP API
      try {
        const response = await fetch('/v1/agent/chat', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ 
            message: input,
            session_id: sessionId,
          }),
        })
        const data = await response.json()
        
        if (data.session_id) {
          setSessionId(data.session_id)
        }
        
        // Check for API key error
        let content = data.response || data.error || 'No response'
        if (content.includes('API Key is required') || content.includes('API Key is invalid')) {
          content = '⚠️ ' + content + '\n\n请前往 https://platform.xiaomimimo.com 获取免费 API Key，然后在 Channels 页面配置。'
        }
        
        setMessages(prev => [...prev, {
          id: Date.now().toString(),
          role: 'assistant',
          content: content,
          timestamp: new Date(),
        }])
      } catch (error) {
        setMessages(prev => [...prev, {
          id: Date.now().toString(),
          role: 'assistant',
          content: 'Error: Failed to send message. Is the backend running?',
          timestamp: new Date(),
        }])
      }
      setIsLoading(false)
    }
  }

  const clearChat = () => {
    setMessages([])
    setSessionId('')
  }

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <header className="h-14 flex items-center justify-between px-6 border-b border-border">
        <div>
          <h2 className="text-sm font-semibold text-foreground">Chat</h2>
          <p className="text-[11px] text-muted-foreground">
            {connected ? (
              <span className="flex items-center gap-1.5">
                <span className="w-1.5 h-1.5 rounded-full bg-success"></span>
                Connected
              </span>
            ) : (
              <span className="flex items-center gap-1.5">
                <span className="w-1.5 h-1.5 rounded-full bg-destructive"></span>
                Disconnected
              </span>
            )}
            {sessionId && <span className="ml-2">Session: {sessionId.substring(0, 8)}...</span>}
          </p>
        </div>
        <button
          onClick={clearChat}
          className="h-8 px-3 text-[12px] text-muted-foreground hover:text-foreground border border-border rounded-md hover:bg-accent transition-colors"
        >
          New Chat
        </button>
      </header>

      {/* Messages */}
      <div className="flex-1 overflow-auto p-6 space-y-4">
        {messages.length === 0 ? (
          <div className="flex items-center justify-center h-full">
            <div className="text-center animate-fade-in">
              <div className="w-12 h-12 rounded-xl bg-primary/10 flex items-center justify-center mx-auto mb-4">
                <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#3b82f6" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <polyline points="16 18 22 12 16 6" />
                  <polyline points="8 6 2 12 8 18" />
                </svg>
              </div>
              <h3 className="text-base font-semibold text-foreground mb-1.5">
                Welcome to CodeGateway
              </h3>
              <p className="text-[13px] text-muted-foreground max-w-sm">
                Start a conversation with your AI assistant. Make sure you have added a channel with an API key first.
              </p>
              <div className="mt-4 text-[12px] text-muted-foreground/60">
                <p>1. Go to Channels page and add your API provider</p>
                <p>2. Come back here and start chatting</p>
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
                className={`max-w-[70%] rounded-xl px-4 py-2.5 ${
                  msg.role === 'user'
                    ? 'bg-primary text-primary-foreground'
                    : 'bg-card border border-border'
                }`}
              >
                <p className="text-[13px] leading-relaxed whitespace-pre-wrap">{msg.content}</p>
                <p className={`text-[10px] mt-1.5 ${msg.role === 'user' ? 'text-primary-foreground/60' : 'text-muted-foreground'}`}>
                  {msg.timestamp.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                </p>
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

      {/* Input */}
      <div className="p-4 border-t border-border">
        <div className="flex gap-2">
          <input
            type="text"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyPress={(e) => e.key === 'Enter' && !e.shiftKey && sendMessage()}
            placeholder={connected ? "Type your message..." : "Connecting..."}
            className="flex-1 h-10 px-4 bg-card border border-border rounded-lg text-[13px] text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring focus:border-transparent transition-colors"
            disabled={isLoading || !connected}
          />
          <button
            onClick={sendMessage}
            disabled={isLoading || !input.trim() || !connected}
            className="h-10 px-4 bg-primary text-primary-foreground rounded-lg text-[13px] font-medium hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed transition-colors flex items-center gap-2"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <line x1="22" y1="2" x2="11" y2="13" />
              <polygon points="22 2 15 22 11 13 2 9 22 2" />
            </svg>
            Send
          </button>
        </div>
      </div>
    </div>
  )
}

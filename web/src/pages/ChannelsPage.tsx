import { useState, useEffect } from 'react'

interface Channel {
  id: number
  name: string
  type: number
  key: string
  base_url: string
  models: string
  status: number
  weight: number
  priority: number
}

export default function ChannelsPage() {
  const [channels, setChannels] = useState<Channel[]>([])
  const [showAdd, setShowAdd] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [form, setForm] = useState({
    name: '',
    type: 1,
    key: '',
    base_url: '',
    models: '',
    weight: 1,
    priority: 0,
  })

  useEffect(() => {
    fetchChannels()
  }, [])

  const fetchChannels = async () => {
    try {
      const response = await fetch('/v1/admin/channels')
      if (response.ok) {
        const data = await response.json()
        setChannels(data.channels || [])
      }
    } catch (error) {
      console.error('Failed to fetch channels:', error)
    }
  }

  const handleSubmit = async () => {
    try {
      const url = editingId ? `/v1/admin/channels/${editingId}` : '/v1/admin/channels'
      const method = editingId ? 'PUT' : 'POST'
      
      const response = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(form),
      })

      if (response.ok) {
        setShowAdd(false)
        setEditingId(null)
        resetForm()
        fetchChannels()
      }
    } catch (error) {
      console.error('Failed to save channel:', error)
    }
  }

  const handleEdit = (channel: Channel) => {
    setForm({
      name: channel.name,
      type: channel.type,
      key: channel.key,
      base_url: channel.base_url,
      models: channel.models,
      weight: channel.weight,
      priority: channel.priority,
    })
    setEditingId(channel.id)
    setShowAdd(true)
  }

  const handleDelete = async (id: number) => {
    if (!confirm('Are you sure you want to delete this channel?')) return
    
    try {
      await fetch(`/v1/admin/channels/${id}`, { method: 'DELETE' })
      fetchChannels()
    } catch (error) {
      console.error('Failed to delete channel:', error)
    }
  }

  const resetForm = () => {
    setForm({
      name: '',
      type: 1,
      key: '',
      base_url: '',
      models: '',
      weight: 1,
      priority: 0,
    })
  }

  const channelTypes: Record<number, { name: string; color: string }> = {
    1: { name: 'OpenAI', color: 'text-green-400 bg-green-500/10' },
    2: { name: 'Claude', color: 'text-purple-400 bg-purple-500/10' },
    3: { name: 'Gemini', color: 'text-blue-400 bg-blue-500/10' },
    4: { name: 'DeepSeek', color: 'text-cyan-400 bg-cyan-500/10' },
    5: { name: 'Ollama', color: 'text-amber-400 bg-amber-500/10' },
    6: { name: 'MiMo', color: 'text-orange-400 bg-orange-500/10' },
    7: { name: 'MiMo Free', color: 'text-red-400 bg-red-500/10' },
    8: { name: 'MiMoCode', color: 'text-pink-400 bg-pink-500/10' },
    99: { name: 'Custom', color: 'text-gray-400 bg-gray-500/10' },
  }

  const defaultBaseURLs: Record<number, string> = {
    1: 'https://api.openai.com/v1',
    2: 'https://api.anthropic.com',
    3: 'https://generativelanguage.googleapis.com/v1beta',
    4: 'https://api.deepseek.com/v1',
    6: 'https://api.xiaomimimo.com/v1',
    7: 'https://api.xiaomimimo.com',
    8: 'http://127.0.0.1:10001',
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h2 className="text-base font-semibold text-foreground">Channels</h2>
          <p className="text-[13px] text-muted-foreground mt-0.5">Manage your API provider channels</p>
        </div>
        <button 
          onClick={() => { resetForm(); setEditingId(null); setShowAdd(true); }}
          className="h-9 px-4 bg-primary text-primary-foreground rounded-lg text-[13px] font-medium hover:bg-primary/90 transition-colors flex items-center gap-2"
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
            <line x1="12" y1="5" x2="12" y2="19" />
            <line x1="5" y1="12" x2="19" y2="12" />
          </svg>
          Add Channel
        </button>
      </div>

      {/* Add/Edit Modal */}
      {showAdd && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-card border border-border rounded-xl p-6 w-full max-w-md">
            <h3 className="text-base font-semibold text-foreground mb-4">
              {editingId ? 'Edit Channel' : 'Add Channel'}
            </h3>
            
            <div className="space-y-4">
              <div>
                <label className="block text-[13px] font-medium text-foreground mb-1.5">Name</label>
                <input
                  type="text"
                  value={form.name}
                  onChange={(e) => setForm({...form, name: e.target.value})}
                  placeholder="My OpenAI"
                  className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </div>

              <div>
                <label className="block text-[13px] font-medium text-foreground mb-1.5">Type</label>
                <select
                  value={form.type}
                  onChange={(e) => {
                    const type = parseInt(e.target.value)
                    setForm({
                      ...form, 
                      type,
                      base_url: defaultBaseURLs[type] || form.base_url
                    })
                  }}
                  className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                >
                  <option value={1}>OpenAI</option>
                  <option value={2}>Claude</option>
                  <option value={3}>Gemini</option>
                  <option value={4}>DeepSeek</option>
                  <option value={5}>Ollama</option>
                  <option value={6}>MiMo (API)</option>
                  <option value={7}>MiMo Free (mimo-auto 直连)</option>
                  <option value={8}>MiMoCode (本地 mimo serve)</option>
                  <option value={99}>Custom</option>
                </select>
              </div>

              <div>
                <label className="block text-[13px] font-medium text-foreground mb-1.5">API Key</label>
                <input
                  type="password"
                  value={form.key}
                  onChange={(e) => setForm({...form, key: e.target.value})}
                  placeholder="sk-..."
                  className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </div>

              <div>
                <label className="block text-[13px] font-medium text-foreground mb-1.5">Base URL</label>
                <input
                  type="text"
                  value={form.base_url}
                  onChange={(e) => setForm({...form, base_url: e.target.value})}
                  placeholder="https://api.openai.com/v1"
                  className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </div>

              <div>
                <label className="block text-[13px] font-medium text-foreground mb-1.5">Models (comma separated, empty for all)</label>
                <input
                  type="text"
                  value={form.models}
                  onChange={(e) => setForm({...form, models: e.target.value})}
                  placeholder="gpt-4o, gpt-3.5-turbo"
                  className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-[13px] font-medium text-foreground mb-1.5">Weight</label>
                  <input
                    type="number"
                    value={form.weight}
                    onChange={(e) => setForm({...form, weight: parseInt(e.target.value)})}
                    className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </div>
                <div>
                  <label className="block text-[13px] font-medium text-foreground mb-1.5">Priority</label>
                  <input
                    type="number"
                    value={form.priority}
                    onChange={(e) => setForm({...form, priority: parseInt(e.target.value)})}
                    className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </div>
              </div>
            </div>

            <div className="flex justify-end gap-2 mt-6">
              <button
                onClick={() => { setShowAdd(false); setEditingId(null); }}
                className="h-9 px-4 text-[13px] text-muted-foreground hover:text-foreground border border-border rounded-md hover:bg-accent transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handleSubmit}
                disabled={!form.name || !form.key}
                className="h-9 px-4 bg-primary text-primary-foreground rounded-lg text-[13px] font-medium hover:bg-primary/90 disabled:opacity-50 transition-colors"
              >
                {editingId ? 'Update' : 'Create'}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Channels Table */}
      <div className="bg-card border border-border rounded-xl overflow-hidden">
        <table className="w-full">
          <thead>
            <tr className="border-b border-border">
              <th className="px-4 py-3 text-left text-[12px] font-medium text-muted-foreground">ID</th>
              <th className="px-4 py-3 text-left text-[12px] font-medium text-muted-foreground">Name</th>
              <th className="px-4 py-3 text-left text-[12px] font-medium text-muted-foreground">Type</th>
              <th className="px-4 py-3 text-left text-[12px] font-medium text-muted-foreground">Base URL</th>
              <th className="px-4 py-3 text-left text-[12px] font-medium text-muted-foreground">Status</th>
              <th className="px-4 py-3 text-left text-[12px] font-medium text-muted-foreground">Actions</th>
            </tr>
          </thead>
          <tbody>
            {channels.length === 0 ? (
              <tr>
                <td colSpan={6} className="px-4 py-12 text-center">
                  <div className="flex flex-col items-center">
                    <div className="w-10 h-10 rounded-lg bg-muted flex items-center justify-center mb-3">
                      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#71717a" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
                        <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
                      </svg>
                    </div>
                    <p className="text-[13px] text-muted-foreground">No channels configured</p>
                    <p className="text-[12px] text-muted-foreground/60 mt-1">Click "Add Channel" to get started</p>
                  </div>
                </td>
              </tr>
            ) : (
              channels.map((channel) => {
                const typeInfo = channelTypes[channel.type] || { name: 'Unknown', color: 'text-gray-400 bg-gray-500/10' }
                return (
                  <tr key={channel.id} className="border-b border-border hover:bg-accent/50 transition-colors">
                    <td className="px-4 py-3 text-[13px] text-muted-foreground tabular-nums">{channel.id}</td>
                    <td className="px-4 py-3 text-[13px] text-foreground font-medium">{channel.name}</td>
                    <td className="px-4 py-3">
                      <span className={`inline-flex items-center px-2 py-0.5 rounded-md text-[12px] font-medium ${typeInfo.color}`}>
                        {typeInfo.name}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-[13px] text-muted-foreground max-w-[200px] truncate">{channel.base_url}</td>
                    <td className="px-4 py-3">
                      <span className={`inline-flex items-center gap-1.5 text-[12px] font-medium ${
                        channel.status === 1 ? 'text-success' : 'text-destructive'
                      }`}>
                        <span className={`w-1.5 h-1.5 rounded-full ${
                          channel.status === 1 ? 'bg-success' : 'bg-destructive'
                        }`}></span>
                        {channel.status === 1 ? 'Active' : 'Inactive'}
                      </span>
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2">
                        <button 
                          onClick={() => handleEdit(channel)}
                          className="text-[13px] text-primary hover:text-primary/80 font-medium transition-colors"
                        >
                          Edit
                        </button>
                        <button 
                          onClick={() => handleDelete(channel.id)}
                          className="text-[13px] text-destructive hover:text-destructive/80 font-medium transition-colors"
                        >
                          Delete
                        </button>
                      </div>
                    </td>
                  </tr>
                )
              })
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

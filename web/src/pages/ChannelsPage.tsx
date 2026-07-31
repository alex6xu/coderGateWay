import { useState, useEffect, useRef } from 'react'
import { Link } from 'react-router-dom'
import { apiFetch, useAccount } from '../context/AccountContext'

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
  is_default: number
  auth_mode?: string
}

const CHANNEL_TYPE_SLUGS: Record<number, string> = {
  1: 'openai',
  2: 'claude',
  3: 'gemini',
  4: 'deepseek',
  5: 'ollama',
  6: 'mimo',
  9: 'agnes',
  10: 'glm',
  99: 'custom',
}

function typeSlug(type: number): string {
  return CHANNEL_TYPE_SLUGS[type] || 'custom'
}

function firstModel(models: string): string {
  const raw = models.trim()
  if (!raw) return ''
  if (raw.startsWith('[')) {
    try {
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed) && parsed.length > 0) {
        return String(parsed[0]).trim().toLowerCase()
      }
    } catch {
      // fall through to comma parsing
    }
  }
  const first = raw.split(',')[0]?.trim() || ''
  return first.toLowerCase()
}

function defaultChannelName(type: number, models: string): string {
  const slug = typeSlug(type)
  const model = firstModel(models)
  return model ? `${slug}/${model}` : slug
}

export default function ChannelsPage() {
  const { currentAccount } = useAccount()
  const [channels, setChannels] = useState<Channel[]>([])
  const [showAdd, setShowAdd] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [nameTouched, setNameTouched] = useState(false)
  const [form, setForm] = useState({
    name: '',
    type: 1,
    key: '',
    base_url: '',
    models: '',
    weight: 1,
    priority: 0,
    is_default: 0,
    auth_mode: 'api_key' as string,
  })
  const nameTouchedRef = useRef(false)

  const fetchChannels = async () => {
    try {
      const response = await apiFetch('/v1/admin/channels', {}, currentAccount?.id)
      if (response.ok) {
        const data = await response.json()
        setChannels(data.channels || [])
      }
    } catch (error) {
      console.error('Failed to fetch channels:', error)
    }
  }

  const syncAutoName = (next: typeof form, touched = nameTouchedRef.current) => {
    if (touched) return next
    return { ...next, name: defaultChannelName(next.type, next.models) }
  }

  const handleSubmit = async () => {
    try {
      let payload = { ...form }
      if (!payload.name.trim()) {
        payload.name = defaultChannelName(payload.type, payload.models)
      }
      payload.name = payload.name.trim().toLowerCase()

      const duplicate = channels.find(
        (ch) => ch.name.toLowerCase() === payload.name && ch.id !== editingId
      )
      let targetId = editingId

      if (duplicate) {
        const ok = confirm(`名称 ${payload.name} 已存在，是否覆盖？`)
        if (!ok) return
        targetId = duplicate.id
      }

      const url = targetId ? `/v1/admin/channels/${targetId}` : '/v1/admin/channels'
      const method = targetId ? 'PUT' : 'POST'

      const response = await apiFetch(url, {
        method,
        body: JSON.stringify(payload),
      }, currentAccount?.id)

      if (response.ok) {
        if (payload.is_default === 1 && targetId) {
          await apiFetch(`/v1/admin/channels/${targetId}/set-default`, { method: 'PUT' }, currentAccount?.id)
        } else if (payload.is_default === 1 && !targetId) {
          const data = await response.json()
          if (data.id) {
            await apiFetch(`/v1/admin/channels/${data.id}/set-default`, { method: 'PUT' }, currentAccount?.id)
          }
        }
        setShowAdd(false)
        setEditingId(null)
        resetForm()
        fetchChannels()
      } else {
        const data = await response.json().catch(() => ({}))
        alert(data.error || '保存失败')
      }
    } catch (error) {
      console.error('Failed to save channel:', error)
    }
  }

  const handleEdit = (channel: Channel) => {
    nameTouchedRef.current = true
    setNameTouched(true)
    setForm({
      name: channel.name,
      type: channel.type === 7 ? 6 : channel.type,
      key: channel.key,
      base_url: channel.base_url,
      models: channel.models,
      weight: channel.weight,
      priority: channel.priority,
      is_default: channel.is_default,
      auth_mode: channel.auth_mode === 'oauth' ? 'oauth' : 'api_key',
    })
    setEditingId(channel.id)
    setShowAdd(true)
  }

  const handleDelete = async (id: number) => {
    if (!confirm('Are you sure you want to delete this channel?')) return

    try {
      await apiFetch(`/v1/admin/channels/${id}`, { method: 'DELETE' }, currentAccount?.id)
      fetchChannels()
    } catch (error) {
      console.error('Failed to delete channel:', error)
    }
  }

  const handleSetDefault = async (id: number) => {
    try {
      await apiFetch(`/v1/admin/channels/${id}/set-default`, { method: 'PUT' }, currentAccount?.id)
      fetchChannels()
    } catch (error) {
      console.error('Failed to set default channel:', error)
    }
  }

  const [fetchingModels, setFetchingModels] = useState(false)
  const [claudeConfigured, setClaudeConfigured] = useState(false)
  const [claudeConnected, setClaudeConnected] = useState(false)
  const [claudeBusy, setClaudeBusy] = useState(false)
  const [claudeHint, setClaudeHint] = useState('')

  const [claudePaste, setClaudePaste] = useState('')

  const fetchClaudeStatus = async () => {
    try {
      const res = await apiFetch('/v1/claude/oauth/status', {}, currentAccount?.id)
      if (!res.ok) return
      const data = await res.json()
      setClaudeConfigured(!!data.configured)
      setClaudeConnected(!!data.connected)
    } catch {
      // ignore
    }
  }

  useEffect(() => {
    if (currentAccount) {
      fetchChannels()
      fetchClaudeStatus()
    }
  }, [currentAccount?.id])

  const connectClaude = async () => {
    setClaudeHint('')
    setClaudeBusy(true)
    try {
      const res = await apiFetch('/v1/claude/oauth/authorize?mode=paste', {}, currentAccount?.id)
      const data = await res.json()
      if (!res.ok) {
        setClaudeHint(data.error || '无法开始授权')
        return
      }
      window.open(data.authorize_url, '_blank', 'noopener,noreferrer')
      setClaudeHint('已在新标签页打开授权。复制 code#state 粘贴到下方并提交。')
    } catch {
      setClaudeHint('网络错误')
    } finally {
      setClaudeBusy(false)
    }
  }

  const submitClaudePaste = async () => {
    setClaudeHint('')
    setClaudeBusy(true)
    try {
      const res = await apiFetch('/v1/claude/oauth/exchange', {
        method: 'POST',
        body: JSON.stringify({ code: claudePaste }),
      }, currentAccount?.id)
      const data = await res.json()
      if (!res.ok) {
        setClaudeHint(data.error || '换取 token 失败')
        return
      }
      setClaudePaste('')
      setClaudeHint('Claude 订阅已连接')
      fetchClaudeStatus()
    } catch {
      setClaudeHint('网络错误')
    } finally {
      setClaudeBusy(false)
    }
  }

  const handleFetchModels = async () => {
    if (!editingId) return
    setFetchingModels(true)
    try {
      const response = await apiFetch(`/v1/admin/channels/${editingId}/fetch-models`, { method: 'POST' }, currentAccount?.id)
      if (response.ok) {
        const data = await response.json()
        const modelStr = (data.models || []).join(', ')
        setForm((prev) => syncAutoName({ ...prev, models: modelStr }))
      }
    } catch (error) {
      console.error('Failed to fetch models:', error)
    } finally {
      setFetchingModels(false)
    }
  }

  const resetForm = () => {
    nameTouchedRef.current = false
    setNameTouched(false)
    const initial = {
      name: '',
      type: 1,
      key: '',
      base_url: defaultBaseURLs[1],
      models: '',
      weight: 1,
      priority: 0,
      is_default: 0,
      auth_mode: 'api_key',
    }
    setForm(syncAutoName(initial, false))
  }

  const channelTypes: Record<number, { name: string; color: string }> = {
    1: { name: 'OpenAI', color: 'text-green-400 bg-green-500/10' },
    2: { name: 'Claude', color: 'text-purple-400 bg-purple-500/10' },
    3: { name: 'Gemini', color: 'text-blue-400 bg-blue-500/10' },
    4: { name: 'DeepSeek', color: 'text-cyan-400 bg-cyan-500/10' },
    5: { name: 'Ollama', color: 'text-amber-400 bg-amber-500/10' },
    6: { name: 'MiMo', color: 'text-orange-400 bg-orange-500/10' },
    9: { name: 'Agnes', color: 'text-teal-400 bg-teal-500/10' },
    10: { name: 'GLM', color: 'text-sky-400 bg-sky-500/10' },
    99: { name: 'Custom', color: 'text-gray-400 bg-gray-500/10' },
  }

  const defaultBaseURLs: Record<number, string> = {
    1: 'https://api.openai.com/v1',
    2: 'https://api.anthropic.com',
    3: 'https://generativelanguage.googleapis.com/v1beta',
    4: 'https://api.deepseek.com/v1',
    6: 'https://api.xiaomimimo.com/v1',
    9: 'https://apihub.agnes-ai.com/v1',
    10: 'https://open.bigmodel.cn/api/paas/v4',
  }

  const canSubmit = Boolean(form.name.trim() || firstModel(form.models)) && (form.auth_mode === 'oauth' || Boolean(form.key))

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h2 className="text-base font-semibold text-foreground">Channels</h2>
          <p className="text-[13px] text-muted-foreground mt-0.5">
            Manage API channels for {currentAccount?.username || 'current account'}
          </p>
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

      <div className="mb-4 bg-card border border-border rounded-xl p-4 space-y-3">
        <div className="flex flex-col sm:flex-row sm:items-center gap-3 justify-between">
          <div className="min-w-0">
            <p className="text-[13px] font-medium text-foreground">Claude 订阅 OAuth</p>
            <p className="text-[12px] text-muted-foreground mt-0.5">
              {!claudeConfigured
                ? '服务端未启用 claude_oauth'
                : claudeConnected
                  ? '已连接 — 添加 Claude 通道时可勾选「使用订阅 OAuth」'
                  : '打开授权页 → 粘贴 code#state（与 OmniRoute 相同）'}
            </p>
            {claudeHint && <p className="text-[12px] text-amber-500 mt-1">{claudeHint}</p>}
          </div>
          <div className="flex flex-wrap items-center gap-2 shrink-0">
            {claudeConfigured && !claudeConnected && (
              <button
                type="button"
                disabled={claudeBusy}
                onClick={connectClaude}
                className="h-8 px-3 bg-primary text-primary-foreground rounded-lg text-[12px] font-medium hover:bg-primary/90 disabled:opacity-50"
              >
                打开授权页
              </button>
            )}
            {claudeConnected && (
              <span className="inline-flex items-center px-2 py-0.5 rounded-md text-[12px] font-medium text-green-400 bg-green-500/10">
                Connected
              </span>
            )}
            <Link
              to="/settings"
              className="h-8 px-3 inline-flex items-center border border-border rounded-lg text-[12px] text-muted-foreground hover:text-foreground hover:bg-accent"
            >
              在 Settings 管理
            </Link>
          </div>
        </div>
        {claudeConfigured && !claudeConnected && (
          <div className="flex gap-2">
            <input
              value={claudePaste}
              onChange={(e) => setClaudePaste(e.target.value)}
              placeholder="粘贴 code#state"
              className="flex-1 h-8 px-3 bg-background border border-border rounded-lg text-[12px] focus:outline-none focus:ring-2 focus:ring-ring"
            />
            <button
              type="button"
              disabled={claudeBusy || !claudePaste.trim()}
              onClick={submitClaudePaste}
              className="h-8 px-3 border border-primary/30 text-primary rounded-lg text-[12px] hover:bg-primary/10 disabled:opacity-50"
            >
              提交
            </button>
          </div>
        )}
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
                <label className="block text-[13px] font-medium text-foreground mb-1.5">Type</label>
                <select
                  value={form.type}
                  onChange={(e) => {
                    const type = parseInt(e.target.value)
                    setForm((prev) => syncAutoName({
                      ...prev,
                      type,
                      base_url: defaultBaseURLs[type] || prev.base_url,
                    }))
                  }}
                  className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                >
                  <option value={1}>OpenAI</option>
                  <option value={2}>Claude</option>
                  <option value={3}>Gemini</option>
                  <option value={4}>DeepSeek</option>
                  <option value={5}>Ollama</option>
                  <option value={6}>MiMo (API)</option>
                  <option value={9}>Agnes (OpenAI 兼容)</option>
                  <option value={10}>GLM / 智谱 (OpenAI 兼容)</option>
                  <option value={99}>Custom</option>
                </select>
              </div>

              <div>
                <label className="block text-[13px] font-medium text-foreground mb-1.5">API Key</label>
                <input
                  type="password"
                  value={form.key}
                  onChange={(e) => setForm({ ...form, key: e.target.value })}
                  placeholder={form.auth_mode === 'oauth' ? 'OAuth 模式可不填' : 'sk-...'}
                  disabled={form.type === 2 && form.auth_mode === 'oauth'}
                  className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50"
                />
              </div>

              {form.type === 2 && (
                <div className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    id="auth_oauth"
                    checked={form.auth_mode === 'oauth'}
                    onChange={(e) => setForm({
                      ...form,
                      auth_mode: e.target.checked ? 'oauth' : 'api_key',
                      key: e.target.checked ? '' : form.key,
                    })}
                    className="w-4 h-4 rounded border-border"
                  />
                  <label htmlFor="auth_oauth" className="text-[13px] font-medium text-foreground">
                    使用 Claude 订阅 OAuth
                    {!claudeConnected && (
                      <span className="ml-1 font-normal text-amber-500">（尚未连接，请先点页面上方「连接订阅」）</span>
                    )}
                  </label>
                </div>
              )}

              <div>
                <label className="block text-[13px] font-medium text-foreground mb-1.5">Base URL</label>
                <input
                  type="text"
                  value={form.base_url}
                  onChange={(e) => setForm({ ...form, base_url: e.target.value })}
                  placeholder="https://api.openai.com/v1"
                  className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                />
              </div>

              <div>
                <label className="block text-[13px] font-medium text-foreground mb-1.5">Models (comma separated, empty for all)</label>
                <div className="flex gap-2">
                  <input
                    type="text"
                    value={form.models}
                    onChange={(e) => {
                      const models = e.target.value
                      setForm((prev) => syncAutoName({ ...prev, models }))
                    }}
                    placeholder="gpt-4o, gpt-3.5-turbo"
                    className="flex-1 h-9 px-3 bg-background border border-border rounded-lg text-[13px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                  {editingId && (
                    <button
                      type="button"
                      onClick={handleFetchModels}
                      disabled={fetchingModels}
                      className="h-9 px-3 text-[12px] text-primary border border-primary/30 rounded-lg hover:bg-primary/10 disabled:opacity-50 transition-colors whitespace-nowrap"
                    >
                      {fetchingModels ? '...' : 'Fetch Models'}
                    </button>
                  )}
                </div>
              </div>

              <div className="flex items-center gap-2">
                <input
                  type="checkbox"
                  id="is_default"
                  checked={form.is_default === 1}
                  onChange={(e) => setForm({ ...form, is_default: e.target.checked ? 1 : 0 })}
                  className="w-4 h-4 rounded border-border"
                />
                <label htmlFor="is_default" className="text-[13px] font-medium text-foreground">
                  Set as default channel
                </label>
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-[13px] font-medium text-foreground mb-1.5">Weight</label>
                  <input
                    type="number"
                    value={form.weight}
                    onChange={(e) => setForm({ ...form, weight: parseInt(e.target.value) })}
                    className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </div>
                <div>
                  <label className="block text-[13px] font-medium text-foreground mb-1.5">Priority</label>
                  <input
                    type="number"
                    value={form.priority}
                    onChange={(e) => setForm({ ...form, priority: parseInt(e.target.value) })}
                    className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                </div>
              </div>

              <div>
                <label className="block text-[13px] font-medium text-foreground mb-1.5">
                  Name
                  {!nameTouched && (
                    <span className="ml-2 font-normal text-muted-foreground">默认 type/model</span>
                  )}
                </label>
                <input
                  type="text"
                  value={form.name}
                  onChange={(e) => {
                    nameTouchedRef.current = true
                    setNameTouched(true)
                    setForm({ ...form, name: e.target.value.toLowerCase() })
                  }}
                  placeholder={defaultChannelName(form.type, form.models) || 'openai/gpt-4o'}
                  className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
                />
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
                disabled={!canSubmit}
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
              <th className="px-4 py-3 text-left text-[12px] font-medium text-muted-foreground">Default</th>
              <th className="px-4 py-3 text-left text-[12px] font-medium text-muted-foreground">Actions</th>
            </tr>
          </thead>
          <tbody>
            {channels.length === 0 ? (
              <tr>
                <td colSpan={7} className="px-4 py-12 text-center">
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
                      {channel.is_default === 1 ? (
                        <span className="inline-flex items-center gap-1 text-[12px] font-medium text-yellow-400">
                          <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" stroke="none">
                            <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26" />
                          </svg>
                          Default
                        </span>
                      ) : (
                        <button
                          onClick={() => handleSetDefault(channel.id)}
                          className="text-[12px] text-muted-foreground hover:text-yellow-400 transition-colors"
                        >
                          Set
                        </button>
                      )}
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

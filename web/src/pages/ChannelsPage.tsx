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

type ProviderMode = 'api_key' | 'oauth'

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

const channelTypeOptions = [
  { value: 1, label: 'OpenAI' },
  { value: 2, label: 'Claude' },
  { value: 3, label: 'Gemini' },
  { value: 4, label: 'DeepSeek' },
  { value: 5, label: 'Ollama' },
  { value: 6, label: 'MiMo (API)' },
  { value: 9, label: 'Agnes (OpenAI 兼容)' },
  { value: 10, label: 'GLM / 智谱 (OpenAI 兼容)' },
  { value: 99, label: 'Custom' },
]

type Tab = 'endpoints' | 'providers'

export default function ChannelsPage() {
  const { currentAccount } = useAccount()
  const [tab, setTab] = useState<Tab>('endpoints')

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h2 className="text-base font-semibold text-foreground">网关管理</h2>
          <p className="text-[13px] text-muted-foreground mt-0.5">
            Manage gateway endpoints and providers for {currentAccount?.username || 'current account'}
          </p>
        </div>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 mb-5 border-b border-border">
        <TabButton active={tab === 'endpoints'} onClick={() => setTab('endpoints')}>端点管理</TabButton>
        <TabButton active={tab === 'providers'} onClick={() => setTab('providers')}>提供商</TabButton>
      </div>

      {tab === 'endpoints' && <EndpointManager accountId={currentAccount?.id} />}
      {tab === 'providers' && <ProvidersPage accountId={currentAccount?.id} />}
    </div>
  )
}

function TabButton({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={`px-4 py-2.5 text-[13px] font-medium border-b-2 -mb-px transition-colors ${
        active
          ? 'border-primary text-foreground'
          : 'border-transparent text-muted-foreground hover:text-foreground'
      }`}
    >
      {children}
    </button>
  )
}

// ============ Endpoint Manager ============

interface ApiKey {
  id: number
  name: string
  key: string
  status: number
  unlimited_quota: boolean
  created_at: string
}

function EndpointManager({ accountId }: { accountId?: number }) {
  const gatewayURL = `${window.location.origin}/v1`
  const [keys, setKeys] = useState<ApiKey[]>([])
  const [newKey, setNewKey] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [copied, setCopied] = useState<string | null>(null)

  const fetchKeys = async () => {
    try {
      const res = await apiFetch('/v1/admin/tokens', {}, accountId)
      if (res.ok) {
        const data = await res.json()
        setKeys(data.tokens || [])
      }
    } catch (e) {
      console.error('Failed to fetch tokens:', e)
    }
  }

  useEffect(() => {
    if (accountId) fetchKeys()
  }, [accountId])

  const copy = (text: string, tag: string) => {
    navigator.clipboard?.writeText(text)
    setCopied(tag)
    setTimeout(() => setCopied(null), 1500)
  }

  const handleCreate = async () => {
    setCreating(true)
    try {
      const res = await apiFetch('/v1/admin/tokens', { method: 'POST', body: JSON.stringify({ name: 'api-key' }) }, accountId)
      if (res.ok) {
        const data = await res.json()
        setNewKey(data.key)
        fetchKeys()
      } else {
        const data = await res.json().catch(() => ({}))
        alert(data.error || '生成失败')
      }
    } catch (e) {
      console.error('Failed to create token:', e)
    } finally {
      setCreating(false)
    }
  }

  const handleToggle = async (k: ApiKey) => {
    try {
      const res = await apiFetch(`/v1/admin/tokens/${k.id}`, {
        method: 'PUT',
        body: JSON.stringify({ status: k.status === 1 ? 0 : 1 }),
      }, accountId)
      if (res.ok) fetchKeys()
    } catch (e) {
      console.error('Failed to update token:', e)
    }
  }

  const handleDelete = async (id: number) => {
    if (!confirm('确认删除该 API Key？删除后使用该 Key 的客户端将无法连接。')) return
    try {
      const res = await apiFetch(`/v1/admin/tokens/${id}`, { method: 'DELETE' }, accountId)
      if (res.ok) fetchKeys()
    } catch (e) {
      console.error('Failed to delete token:', e)
    }
  }

  return (
    <div className="space-y-6">
      {/* Current gateway URL */}
      <div className="bg-card border border-border rounded-xl p-5">
        <p className="text-[13px] font-medium text-foreground mb-1">当前网关地址</p>
        <p className="text-[12px] text-muted-foreground mb-3">
          客户端（Claude Code / 各类 IDE 插件）使用该地址连接到本代理，并配合下方的 API Key 鉴权。
        </p>
        <div className="flex items-center gap-2">
          <code className="flex-1 px-3 py-2 bg-background border border-border rounded-lg text-[13px] font-mono text-foreground">
            {gatewayURL}
          </code>
          <button
            onClick={() => copy(gatewayURL, 'url')}
            className="h-9 px-3 border border-border rounded-lg text-[13px] text-muted-foreground hover:text-foreground hover:bg-accent"
          >
            {copied === 'url' ? '已复制' : '复制'}
          </button>
        </div>
      </div>

      {/* API Keys */}
      <div className="bg-card border border-border rounded-xl">
        <div className="flex items-center justify-between px-5 py-4 border-b border-border">
          <div>
            <p className="text-[13px] font-medium text-foreground">API Key 列表</p>
            <p className="text-[12px] text-muted-foreground mt-0.5">点击「新增 API Key」由后端自动生成并展示，生成后请立即复制保存。</p>
          </div>
          <button
            onClick={handleCreate}
            disabled={creating}
            className="h-9 px-4 bg-primary text-primary-foreground rounded-lg text-[13px] font-medium hover:bg-primary/90 disabled:opacity-50 flex items-center gap-2"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <line x1="12" y1="5" x2="12" y2="19" />
              <line x1="5" y1="12" x2="19" y2="12" />
            </svg>
            新增 API Key
          </button>
        </div>

        {newKey && (
          <div className="px-5 py-4 bg-primary/5 border-b border-border">
            <p className="text-[12px] font-medium text-primary mb-2">新生成的 API Key（仅展示一次，请妥善保存）：</p>
            <div className="flex items-center gap-2">
              <code className="flex-1 px-3 py-2 bg-background border border-primary/30 rounded-lg text-[13px] font-mono text-foreground break-all">
                {newKey}
              </code>
              <button
                onClick={() => copy(newKey, 'newkey')}
                className="h-9 px-3 border border-primary/30 text-primary rounded-lg text-[13px] hover:bg-primary/10"
              >
                {copied === 'newkey' ? '已复制' : '复制'}
              </button>
            </div>
          </div>
        )}

        <table className="w-full">
          <thead>
            <tr className="border-b border-border">
              <th className="px-5 py-3 text-left text-[12px] font-medium text-muted-foreground">ID</th>
              <th className="px-5 py-3 text-left text-[12px] font-medium text-muted-foreground">名称</th>
              <th className="px-5 py-3 text-left text-[12px] font-medium text-muted-foreground">Key</th>
              <th className="px-5 py-3 text-left text-[12px] font-medium text-muted-foreground">配额</th>
              <th className="px-5 py-3 text-left text-[12px] font-medium text-muted-foreground">状态</th>
              <th className="px-5 py-3 text-left text-[12px] font-medium text-muted-foreground">创建时间</th>
              <th className="px-5 py-3 text-left text-[12px] font-medium text-muted-foreground">操作</th>
            </tr>
          </thead>
          <tbody>
            {keys.length === 0 ? (
              <tr>
                <td colSpan={7} className="px-5 py-12 text-center">
                  <p className="text-[13px] text-muted-foreground">暂无 API Key，点击「新增 API Key」生成一个。</p>
                </td>
              </tr>
            ) : (
              keys.map((k) => (
                <tr key={k.id} className="border-b border-border hover:bg-accent/50">
                  <td className="px-5 py-3 text-[13px] text-muted-foreground tabular-nums">{k.id}</td>
                  <td className="px-5 py-3 text-[13px] text-foreground font-medium">{k.name}</td>
                  <td className="px-5 py-3 text-[13px] text-muted-foreground font-mono">
                    {k.key}
                  </td>
                  <td className="px-5 py-3 text-[13px] text-muted-foreground">
                    {k.unlimited_quota ? '无限制' : '有限'}
                  </td>
                  <td className="px-5 py-3">
                    <span className={`inline-flex items-center gap-1.5 text-[12px] font-medium ${k.status === 1 ? 'text-success' : 'text-destructive'}`}>
                      <span className={`w-1.5 h-1.5 rounded-full ${k.status === 1 ? 'bg-success' : 'bg-destructive'}`}></span>
                      {k.status === 1 ? '启用' : '停用'}
                    </span>
                  </td>
                  <td className="px-5 py-3 text-[13px] text-muted-foreground">
                    {new Date(k.created_at).toLocaleString()}
                  </td>
                  <td className="px-5 py-3">
                    <div className="flex items-center gap-2">
                      <button
                        onClick={() => handleToggle(k)}
                        className="text-[13px] text-primary hover:text-primary/80 font-medium"
                      >
                        {k.status === 1 ? '停用' : '启用'}
                      </button>
                      <button
                        onClick={() => handleDelete(k.id)}
                        className="text-[13px] text-destructive hover:text-destructive/80 font-medium"
                      >
                        删除
                      </button>
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// ============ Providers Page (merged: API Key + OAuth) ============

function ProvidersPage({ accountId }: { accountId?: number }) {
  const [channels, setChannels] = useState<Channel[]>([])
  const [claudeConfigured, setClaudeConfigured] = useState(false)
  const [claudeConnected, setClaudeConnected] = useState(false)
  const [claudeBusy, setClaudeBusy] = useState(false)
  const [claudeHint, setClaudeHint] = useState('')
  const [claudePaste, setClaudePaste] = useState('')

  // modal state
  const [modalMode, setModalMode] = useState<ProviderMode>('api_key')
  const [editingId, setEditingId] = useState<number | null>(null)
  const [showModal, setShowModal] = useState(false)
  const [nameTouched, setNameTouched] = useState(false)
  const nameTouchedRef = useRef(false)
  // oauth connect step inside modal
  const [oauthStep, setOauthStep] = useState<'connect' | 'form'>('connect')
  const [oauthHint, setOauthHint] = useState('')
  const [oauthBusy, setOauthBusy] = useState(false)
  const [oauthPaste, setOauthPaste] = useState('')
  const [form, setForm] = useState({
    name: '',
    type: 1,
    key: '',
    base_url: defaultBaseURLs[1],
    models: '',
    weight: 1,
    priority: 0,
    is_default: 0,
    auth_mode: 'api_key' as string,
  })
  const [fetchingModels, setFetchingModels] = useState(false)

  const fetchChannels = async () => {
    try {
      const res = await apiFetch('/v1/admin/channels', {}, accountId)
      if (res.ok) {
        const data = await res.json()
        setChannels(data.channels || [])
      }
    } catch (e) {
      console.error('Failed to fetch channels:', e)
    }
  }

  const fetchClaudeStatus = async () => {
    try {
      const res = await apiFetch('/v1/claude/oauth/status', {}, accountId)
      if (!res.ok) return
      const data = await res.json()
      setClaudeConfigured(!!data.configured)
      setClaudeConnected(!!data.connected)
    } catch {
      // ignore
    }
  }

  useEffect(() => {
    if (accountId) {
      fetchChannels()
      fetchClaudeStatus()
    }
  }, [accountId])

  const apiKeyChannels = channels.filter((c) => (c.auth_mode || 'api_key') !== 'oauth')
  // API Key 兼容提供商再按协议拆分为 OpenAI 兼容（type!=2）与 Claude 兼容（type==2）
  const openaiCompatibleChannels = apiKeyChannels.filter((c) => c.type !== 2)
  const claudeCompatibleChannels = apiKeyChannels.filter((c) => c.type === 2)
  const oauthChannels = channels.filter((c) => (c.auth_mode || '') === 'oauth')

  const syncAutoName = (next: typeof form, touched = nameTouchedRef.current) => {
    if (touched) return next
    return { ...next, name: defaultChannelName(next.type, next.models) }
  }

  const resetForm = (defaultType: number = 1) => {
    nameTouchedRef.current = false
    setNameTouched(false)
    setForm(syncAutoName({
      name: '',
      type: defaultType,
      key: '',
      base_url: defaultBaseURLs[defaultType],
      models: '',
      weight: 1,
      priority: 0,
      is_default: 0,
      auth_mode: modalMode,
    }, false))
  }

  const openAdd = (mode: ProviderMode, defaultType?: number) => {
    setModalMode(mode)
    setEditingId(null)
    resetForm(defaultType)
    setOauthPaste('')
    setOauthHint('')
    setOauthBusy(false)
    // 新增订阅 OAuth 提供商时，若未连接则先走 Claude 订阅连接步骤
    setOauthStep(mode === 'oauth' && !claudeConnected ? 'connect' : 'form')
    setShowModal(true)
  }

  const openEdit = (channel: Channel) => {
    const mode: ProviderMode = channel.auth_mode === 'oauth' ? 'oauth' : 'api_key'
    setModalMode(mode)
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
      auth_mode: mode,
    })
    setOauthStep('form')
    setOauthPaste('')
    setOauthHint('')
    setEditingId(channel.id)
    setShowModal(true)
  }

  const oauthStartConnect = async () => {
    setOauthBusy(true)
    setOauthHint('')
    try {
      const res = await apiFetch('/v1/claude/oauth/authorize?mode=paste', {}, accountId)
      const data = await res.json()
      if (!res.ok) {
        setOauthHint(data.error || '无法开始授权')
        return
      }
      window.open(data.authorize_url, '_blank', 'noopener,noreferrer')
      setOauthHint('已在新标签页打开授权。请复制地址栏中的 code#state 粘贴到下方并提交。')
    } catch {
      setOauthHint('网络错误')
    } finally {
      setOauthBusy(false)
    }
  }

  const oauthDoConnect = async () => {
    setOauthHint('')
    setOauthBusy(true)
    try {
      const res = await apiFetch('/v1/claude/oauth/exchange', {
        method: 'POST',
        body: JSON.stringify({ code: oauthPaste }),
      }, accountId)
      const data = await res.json()
      if (!res.ok) {
        setOauthHint(data.error || '换取 token 失败')
        return
      }
      setOauthPaste('')
      setOauthHint('')
      setClaudeConnected(true)
      setOauthStep('form')
      fetchClaudeStatus()
    } catch {
      setOauthHint('网络错误')
    } finally {
      setOauthBusy(false)
    }
  }

  const handleSubmit = async () => {
    let payload = { ...form }
    if (!payload.name.trim()) payload.name = defaultChannelName(payload.type, payload.models)
    payload.name = payload.name.trim().toLowerCase()

    if (modalMode === 'api_key' && !payload.key.trim() && !editingId) {
      alert('API Key 模式需要填写 Key')
      return
    }
    if (modalMode === 'oauth' && payload.type !== 2) {
      alert('订阅（OAuth）目前仅支持 Claude')
      return
    }
    payload.auth_mode = modalMode

    try {
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
      const res = await apiFetch(url, { method, body: JSON.stringify(payload) }, accountId)
      if (res.ok) {
        if (payload.is_default === 1 && targetId) {
          await apiFetch(`/v1/admin/channels/${targetId}/set-default`, { method: 'PUT' }, accountId)
        } else if (payload.is_default === 1 && !targetId) {
          const data = await res.json()
          if (data.id) await apiFetch(`/v1/admin/channels/${data.id}/set-default`, { method: 'PUT' }, accountId)
        }
        setShowModal(false)
        setEditingId(null)
        fetchChannels()
      } else {
        const data = await res.json().catch(() => ({}))
        alert(data.error || '保存失败')
      }
    } catch (e) {
      console.error('Failed to save channel:', e)
    }
  }

  const handleDelete = async (id: number) => {
    if (!confirm('确认删除该提供商通道？')) return
    try {
      await apiFetch(`/v1/admin/channels/${id}`, { method: 'DELETE' }, accountId)
      fetchChannels()
    } catch (e) {
      console.error('Failed to delete channel:', e)
    }
  }

  const handleSetDefault = async (id: number) => {
    try {
      await apiFetch(`/v1/admin/channels/${id}/set-default`, { method: 'PUT' }, accountId)
      fetchChannels()
    } catch (e) {
      console.error('Failed to set default channel:', e)
    }
  }

  const handleFetchModels = async () => {
    if (!editingId) return
    setFetchingModels(true)
    try {
      const res = await apiFetch(`/v1/admin/channels/${editingId}/fetch-models`, { method: 'POST' }, accountId)
      if (res.ok) {
        const data = await res.json()
        const modelStr = (data.models || []).join(', ')
        setForm((prev) => syncAutoName({ ...prev, models: modelStr }))
      }
    } catch (e) {
      console.error('Failed to fetch models:', e)
    } finally {
      setFetchingModels(false)
    }
  }

  const connectClaude = async () => {
    setClaudeHint('')
    setClaudeBusy(true)
    try {
      const res = await apiFetch('/v1/claude/oauth/authorize?mode=paste', {}, accountId)
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
      }, accountId)
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

  const canSubmit = Boolean(form.name.trim() || firstModel(form.models)) &&
    (modalMode === 'oauth' || Boolean(form.key) || editingId !== null)

  return (
    <div className="space-y-6">
      {/* ---------- Subscription OAuth providers ---------- */}
      <div className="space-y-3">
        {claudeConfigured && (
          <div className="bg-card border border-border rounded-xl p-4 flex flex-col sm:flex-row sm:items-center gap-3 justify-between">
            <div className="min-w-0">
              <p className="text-[13px] font-medium text-foreground">Claude 订阅 OAuth</p>
              <p className="text-[12px] text-muted-foreground mt-0.5">
                {claudeConnected
                  ? '已连接 — 可新增 Claude 订阅提供商'
                  : '打开授权页 → 粘贴 code#state 以连接订阅'}
              </p>
              {claudeHint && <p className="text-[12px] text-amber-500 mt-1">{claudeHint}</p>}
            </div>
            <div className="flex flex-wrap items-center gap-2 shrink-0">
              {claudeConfigured && !claudeConnected && (
                <button type="button" disabled={claudeBusy} onClick={connectClaude}
                  className="h-8 px-3 bg-primary text-primary-foreground rounded-lg text-[12px] font-medium hover:bg-primary/90 disabled:opacity-50">
                  打开授权页
                </button>
              )}
              {claudeConnected && (
                <span className="inline-flex items-center px-2 py-0.5 rounded-md text-[12px] font-medium text-green-400 bg-green-500/10">Connected</span>
              )}
              <Link to="/settings" className="h-8 px-3 inline-flex items-center border border-border rounded-lg text-[12px] text-muted-foreground hover:text-foreground hover:bg-accent">
                在 Settings 管理
              </Link>
            </div>
            {claudeConfigured && !claudeConnected && (
              <div className="flex gap-2 sm:w-full">
                <input value={claudePaste} onChange={(e) => setClaudePaste(e.target.value)} placeholder="粘贴 code#state"
                  className="flex-1 h-8 px-3 bg-background border border-border rounded-lg text-[12px] focus:outline-none focus:ring-2 focus:ring-ring" />
                <button type="button" disabled={claudeBusy || !claudePaste.trim()} onClick={submitClaudePaste}
                  className="h-8 px-3 border border-primary/30 text-primary rounded-lg text-[12px] hover:bg-primary/10 disabled:opacity-50">
                  提交
                </button>
              </div>
            )}
          </div>
        )}
        <ProviderSection
          title="订阅 OAuth 提供商"
          desc="通过订阅授权（OAuth）接入的供应商，目前支持 Claude。"
          onAdd={() => openAdd('oauth')}
          addLabel="新增订阅 OAuth 提供商"
          emptyHint="暂无订阅 OAuth 提供商，点击右上角新增并完成 Claude 订阅连接。"
          channels={oauthChannels}
          onEdit={openEdit}
          onDelete={handleDelete}
          onSetDefault={handleSetDefault}
        />
      </div>

      {/* ---------- API Key compatible providers (split by protocol) ---------- */}
      <ProviderSection
        title="OpenAI 兼容提供商"
        desc="遵循 OpenAI 协议的供应商，使用 API Key 鉴权，如 OpenAI / Gemini / DeepSeek / Ollama 等。"
        onAdd={() => openAdd('api_key', 1)}
        addLabel="新增 OpenAI 兼容提供商"
        emptyHint="暂无 OpenAI 兼容提供商，点击右上角新增。"
        channels={openaiCompatibleChannels}
        onEdit={openEdit}
        onDelete={handleDelete}
        onSetDefault={handleSetDefault}
      />

      <ProviderSection
        title="Claude 兼容提供商"
        desc="遵循 Anthropic Claude 协议的供应商，使用 API Key 鉴权。"
        onAdd={() => openAdd('api_key', 2)}
        addLabel="新增 Claude 兼容提供商"
        emptyHint="暂无 Claude 兼容提供商，点击右上角新增。"
        channels={claudeCompatibleChannels}
        onEdit={openEdit}
        onDelete={handleDelete}
        onSetDefault={handleSetDefault}
      />

      {/* ---------- Add / Edit Modal ---------- */}
      {showModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-card border border-border rounded-xl p-6 w-full max-w-md">
            <h3 className="text-base font-semibold text-foreground mb-4">
              {editingId ? '编辑提供商' : (modalMode === 'oauth' ? '新增订阅 OAuth 提供商' : '新增 API Key 提供商')}
            </h3>

            {modalMode === 'oauth' && oauthStep === 'connect' ? (
              <div className="space-y-4">
                <p className="text-[13px] text-muted-foreground">
                  新增订阅 OAuth 提供商需要先连接 Claude 订阅。点击下方按钮打开授权页，登录后复制地址栏中的 <span className="font-mono text-foreground">code#state</span> 并粘贴到下方完成连接。
                </p>
                {oauthHint && (
                  <p className="text-[12px] text-amber-500 bg-amber-500/10 rounded-lg px-3 py-2">{oauthHint}</p>
                )}
                <div className="flex gap-2">
                  <input
                    value={oauthPaste}
                    onChange={(e) => setOauthPaste(e.target.value)}
                    placeholder="粘贴 code#state"
                    className="flex-1 h-9 px-3 bg-background border border-border rounded-lg text-[13px] focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                  <button
                    type="button"
                    disabled={oauthBusy || !oauthPaste.trim()}
                    onClick={oauthDoConnect}
                    className="h-9 px-3 border border-primary/30 text-primary rounded-lg text-[12px] hover:bg-primary/10 disabled:opacity-50"
                  >
                    连接
                  </button>
                </div>
                <button
                  type="button"
                  disabled={oauthBusy}
                  onClick={oauthStartConnect}
                  className="h-9 px-4 bg-primary text-primary-foreground rounded-lg text-[13px] font-medium hover:bg-primary/90 disabled:opacity-50"
                >
                  打开 Claude 授权页
                </button>
              </div>
            ) : (
            <div className="space-y-4">
              <div>
                <label className="block text-[13px] font-medium text-foreground mb-1.5">供应商类型</label>
                <select
                  value={form.type}
                  onChange={(e) => {
                    const type = parseInt(e.target.value)
                    setForm((prev) => syncAutoName({
                      ...prev, type, base_url: defaultBaseURLs[type] || prev.base_url,
                    }))
                  }}
                  className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px]"
                >
                  {channelTypeOptions.map((o) => (
                    <option key={o.value} value={o.value}>{o.label}</option>
                  ))}
                </select>
              </div>

              {modalMode === 'api_key' ? (
                <div>
                  <label className="block text-[13px] font-medium text-foreground mb-1.5">
                    API Key {editingId && <span className="font-normal text-muted-foreground">（留空则不修改）</span>}
                  </label>
                  <input
                    type="password"
                    value={form.key}
                    onChange={(e) => setForm({ ...form, key: e.target.value })}
                    placeholder="sk-..."
                    className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px]"
                  />
                </div>
              ) : (
                <div className="text-[12px] text-muted-foreground bg-muted/50 rounded-lg p-3">
                  订阅模式将使用已连接的 OAuth 凭据，无需填写 Key。
                  {!claudeConnected && (
                    <p className="text-amber-500 mt-1">尚未连接 Claude 订阅，请先连接后再新增。</p>
                  )}
                </div>
              )}

              <div>
                <label className="block text-[13px] font-medium text-foreground mb-1.5">Base URL</label>
                <input
                  value={form.base_url}
                  onChange={(e) => setForm({ ...form, base_url: e.target.value })}
                  placeholder="https://api.openai.com/v1"
                  className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px]"
                />
              </div>

              <div>
                <label className="block text-[13px] font-medium text-foreground mb-1.5">Models (逗号分隔，留空表示全部)</label>
                <div className="flex gap-2">
                  <input
                    value={form.models}
                    onChange={(e) => setForm((prev) => syncAutoName({ ...prev, models: e.target.value }))}
                    placeholder="gpt-4o, gpt-3.5-turbo"
                    className="flex-1 h-9 px-3 bg-background border border-border rounded-lg text-[13px]"
                  />
                  {editingId && (
                    <button type="button" onClick={handleFetchModels} disabled={fetchingModels}
                      className="h-9 px-3 text-[12px] text-primary border border-primary/30 rounded-lg hover:bg-primary/10 disabled:opacity-50 whitespace-nowrap">
                      {fetchingModels ? '...' : 'Fetch Models'}
                    </button>
                  )}
                </div>
              </div>

              <div className="flex items-center gap-2">
                <input type="checkbox" id="is_default" checked={form.is_default === 1}
                  onChange={(e) => setForm({ ...form, is_default: e.target.checked ? 1 : 0 })} className="w-4 h-4 rounded border-border" />
                <label htmlFor="is_default" className="text-[13px] font-medium text-foreground">设为默认提供商</label>
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-[13px] font-medium text-foreground mb-1.5">Weight</label>
                  <input type="number" value={form.weight} onChange={(e) => setForm({ ...form, weight: parseInt(e.target.value) })}
                    className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px]" />
                </div>
                <div>
                  <label className="block text-[13px] font-medium text-foreground mb-1.5">Priority</label>
                  <input type="number" value={form.priority} onChange={(e) => setForm({ ...form, priority: parseInt(e.target.value) })}
                    className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px]" />
                </div>
              </div>

              <div>
                <label className="block text-[13px] font-medium text-foreground mb-1.5">
                  名称 {!nameTouched && <span className="ml-2 font-normal text-muted-foreground">默认 type/model</span>}
                </label>
                <input
                  value={form.name}
                  onChange={(e) => {
                    nameTouchedRef.current = true
                    setNameTouched(true)
                    setForm({ ...form, name: e.target.value.toLowerCase() })
                  }}
                  placeholder={defaultChannelName(form.type, form.models) || 'openai/gpt-4o'}
                  className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px]"
                />
              </div>
            </div>
            )}

            <div className="flex justify-end gap-2 mt-6">
              <button onClick={() => { setShowModal(false); setEditingId(null); }}
                className="h-9 px-4 text-[13px] text-muted-foreground hover:text-foreground border border-border rounded-md hover:bg-accent">
                取消
              </button>
              {!(modalMode === 'oauth' && oauthStep === 'connect') && (
              <button onClick={handleSubmit} disabled={!canSubmit}
                className="h-9 px-4 bg-primary text-primary-foreground rounded-lg text-[13px] font-medium hover:bg-primary/90 disabled:opacity-50">
                {editingId ? '更新' : '创建'}
              </button>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function ProviderSection({
  title, desc, onAdd, addLabel, emptyHint, channels, onEdit, onDelete, onSetDefault,
  disabledAdd, disabledAddHint,
}: {
  title: string
  desc: string
  onAdd: () => void
  addLabel: string
  emptyHint: string
  channels: Channel[]
  onEdit: (c: Channel) => void
  onDelete: (id: number) => void
  onSetDefault: (id: number) => void
  disabledAdd?: boolean
  disabledAddHint?: string
}) {
  return (
    <div className="bg-card border border-border rounded-xl">
      <div className="flex items-start justify-between px-5 py-4 border-b border-border">
        <div>
          <p className="text-[14px] font-semibold text-foreground">{title}</p>
          <p className="text-[12px] text-muted-foreground mt-0.5">{desc}</p>
        </div>
        <button
          onClick={onAdd}
          disabled={disabledAdd}
          className="h-9 px-4 bg-primary text-primary-foreground rounded-lg text-[13px] font-medium hover:bg-primary/90 disabled:opacity-50 flex items-center gap-2 shrink-0"
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
            <line x1="12" y1="5" x2="12" y2="19" />
            <line x1="5" y1="12" x2="19" y2="12" />
          </svg>
          {addLabel}{disabledAddHint}
        </button>
      </div>

      {channels.length === 0 ? (
        <p className="px-5 py-10 text-center text-[13px] text-muted-foreground">{emptyHint}</p>
      ) : (
        <table className="w-full">
          <thead>
            <tr className="border-b border-border">
              <th className="px-5 py-3 text-left text-[12px] font-medium text-muted-foreground">ID</th>
              <th className="px-5 py-3 text-left text-[12px] font-medium text-muted-foreground">名称</th>
              <th className="px-5 py-3 text-left text-[12px] font-medium text-muted-foreground">类型</th>
              <th className="px-5 py-3 text-left text-[12px] font-medium text-muted-foreground">Base URL</th>
              <th className="px-5 py-3 text-left text-[12px] font-medium text-muted-foreground">状态</th>
              <th className="px-5 py-3 text-left text-[12px] font-medium text-muted-foreground">默认</th>
              <th className="px-5 py-3 text-left text-[12px] font-medium text-muted-foreground">操作</th>
            </tr>
          </thead>
          <tbody>
            {channels.map((channel) => {
              const typeInfo = channelTypes[channel.type] || { name: 'Unknown', color: 'text-gray-400 bg-gray-500/10' }
              return (
                <tr key={channel.id} className="border-b border-border hover:bg-accent/50">
                  <td className="px-5 py-3 text-[13px] text-muted-foreground tabular-nums">{channel.id}</td>
                  <td className="px-5 py-3 text-[13px] text-foreground font-medium">{channel.name}</td>
                  <td className="px-5 py-3">
                    <span className={`inline-flex items-center px-2 py-0.5 rounded-md text-[12px] font-medium ${typeInfo.color}`}>
                      {typeInfo.name}
                    </span>
                  </td>
                  <td className="px-5 py-3 text-[13px] text-muted-foreground max-w-[200px] truncate">{channel.base_url}</td>
                  <td className="px-5 py-3">
                    <span className={`inline-flex items-center gap-1.5 text-[12px] font-medium ${channel.status === 1 ? 'text-success' : 'text-destructive'}`}>
                      <span className={`w-1.5 h-1.5 rounded-full ${channel.status === 1 ? 'bg-success' : 'bg-destructive'}`}></span>
                      {channel.status === 1 ? '启用' : '停用'}
                    </span>
                  </td>
                  <td className="px-5 py-3">
                    {channel.is_default === 1 ? (
                      <span className="inline-flex items-center gap-1 text-[12px] font-medium text-yellow-400">
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" stroke="none">
                          <polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26" />
                        </svg>
                        Default
                      </span>
                    ) : (
                      <button onClick={() => onSetDefault(channel.id)} className="text-[12px] text-muted-foreground hover:text-yellow-400">
                        设为默认
                      </button>
                    )}
                  </td>
                  <td className="px-5 py-3">
                    <div className="flex items-center gap-2">
                      <button onClick={() => onEdit(channel)} className="text-[13px] text-primary hover:text-primary/80 font-medium">编辑</button>
                      <button onClick={() => onDelete(channel.id)} className="text-[13px] text-destructive hover:text-destructive/80 font-medium">删除</button>
                    </div>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}
    </div>
  )
}

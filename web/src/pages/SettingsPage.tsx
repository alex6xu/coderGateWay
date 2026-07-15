import { FormEvent, useState } from 'react'
import { useAuth } from '../context/AuthContext'

export default function SettingsPage() {
  const { user, changePassword } = useAuth()
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const onChangePassword = async (e: FormEvent) => {
    e.preventDefault()
    setMessage('')
    setError('')
    if (newPassword !== confirmPassword) {
      setError('两次输入的新密码不一致')
      return
    }
    if (newPassword.length < 6) {
      setError('新密码至少 6 位')
      return
    }
    setSaving(true)
    const err = await changePassword(currentPassword, newPassword)
    setSaving(false)
    if (err) {
      setError(err)
      return
    }
    setCurrentPassword('')
    setNewPassword('')
    setConfirmPassword('')
    setMessage('密码已更新')
  }

  return (
    <div className="p-6">
      <div className="mb-6">
        <h2 className="text-base font-semibold text-foreground">Settings</h2>
        <p className="text-[13px] text-muted-foreground mt-0.5">账号与实例配置</p>
      </div>

      <div className="space-y-4 max-w-2xl">
        <div className="bg-card border border-border rounded-xl p-5">
          <h3 className="text-sm font-semibold text-foreground mb-4">当前账号</h3>
          <div className="space-y-2 text-[13px]">
            <p>
              <span className="text-muted-foreground">用户名：</span>
              {user?.username}
            </p>
            <p>
              <span className="text-muted-foreground">角色：</span>
              {user?.role}
            </p>
            <p>
              <span className="text-muted-foreground">邮箱：</span>
              {user?.email || '—'}
            </p>
          </div>
        </div>

        <div className="bg-card border border-border rounded-xl p-5">
          <h3 className="text-sm font-semibold text-foreground mb-2">语音输入</h3>
          <p className="text-[13px] text-muted-foreground leading-relaxed">
            Chat / Code 页支持麦克风口述。默认使用浏览器 Web Speech（推荐 Chrome）。若需完全开源离线识别，可配置
            <code className="mx-1 text-[12px]">asr.base_url</code>
            指向 Whisper 兼容服务（如 speaches / faster-whisper），或设置环境变量
            <code className="mx-1 text-[12px]">ASR_BASE_URL</code>。
          </p>
        </div>

        <div className="bg-card border border-border rounded-xl p-5">
          <h3 className="text-sm font-semibold text-foreground mb-2">GitHub 仓库接入</h3>
          <p className="text-[13px] text-muted-foreground leading-relaxed">
            在 Code 页可授权 GitHub 并导入仓库为云端工作区。服务端需配置 OAuth App：
            <code className="mx-1 text-[12px]">github.client_id</code>/
            <code className="mx-1 text-[12px]">client_secret</code>
            （或环境变量 <code className="text-[12px]">GITHUB_CLIENT_ID</code> /
            <code className="text-[12px]">GITHUB_CLIENT_SECRET</code>），回调地址为
            <code className="mx-1 text-[12px]">/v1/github/callback</code>。
          </p>
        </div>

        <form onSubmit={onChangePassword} className="bg-card border border-border rounded-xl p-5 space-y-4">
          <h3 className="text-sm font-semibold text-foreground">修改密码</h3>
          <div>
            <label className="block text-[13px] font-medium text-foreground mb-1.5">当前密码</label>
            <input
              type="password"
              value={currentPassword}
              onChange={(e) => setCurrentPassword(e.target.value)}
              className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] focus:outline-none focus:ring-2 focus:ring-ring"
              required
            />
          </div>
          <div>
            <label className="block text-[13px] font-medium text-foreground mb-1.5">新密码</label>
            <input
              type="password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] focus:outline-none focus:ring-2 focus:ring-ring"
              required
              minLength={6}
            />
          </div>
          <div>
            <label className="block text-[13px] font-medium text-foreground mb-1.5">确认新密码</label>
            <input
              type="password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              className="w-full h-9 px-3 bg-background border border-border rounded-lg text-[13px] focus:outline-none focus:ring-2 focus:ring-ring"
              required
              minLength={6}
            />
          </div>
          {error && <p className="text-[12px] text-red-500">{error}</p>}
          {message && <p className="text-[12px] text-green-500">{message}</p>}
          <div className="flex justify-end">
            <button
              type="submit"
              disabled={saving}
              className="h-9 px-5 bg-primary text-primary-foreground rounded-lg text-[13px] font-medium hover:bg-primary/90 disabled:opacity-50"
            >
              {saving ? '保存中…' : '更新密码'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

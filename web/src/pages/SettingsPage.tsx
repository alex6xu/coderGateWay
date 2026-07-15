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

import { createContext, useContext, useEffect, useState, ReactNode } from 'react'

export interface Account {
  id: number
  username: string
  email: string
  role: string
  quota: number
  used_quota: number
  created_at: string
  updated_at: string
}

interface AccountContextValue {
  accounts: Account[]
  currentAccount: Account | null
  loading: boolean
  setCurrentAccountId: (id: number) => void
  refreshAccounts: () => Promise<void>
  createAccount: (data: { username: string; email?: string; role?: string }) => Promise<Account | null>
  deleteAccount: (id: number) => Promise<boolean>
}

const STORAGE_KEY = 'codegateway_account_id'

const AccountContext = createContext<AccountContextValue | null>(null)

export function accountHeaders(accountId?: number | null): HeadersInit {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  if (accountId) {
    headers['X-Account-ID'] = String(accountId)
  } else {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored) {
      headers['X-Account-ID'] = stored
    }
  }
  return headers
}

export async function apiFetch(input: string, init: RequestInit = {}, accountId?: number | null): Promise<Response> {
  const headers = {
    ...accountHeaders(accountId),
    ...(init.headers || {}),
  }
  return fetch(input, { ...init, headers })
}

export function AccountProvider({ children }: { children: ReactNode }) {
  const [accounts, setAccounts] = useState<Account[]>([])
  const [currentAccount, setCurrentAccount] = useState<Account | null>(null)
  const [loading, setLoading] = useState(true)

  const refreshAccounts = async () => {
    try {
      const response = await fetch('/v1/admin/accounts')
      if (!response.ok) return
      const data = await response.json()
      const list: Account[] = data.accounts || []
      setAccounts(list)

      const storedId = Number(localStorage.getItem(STORAGE_KEY) || '0')
      const selected =
        list.find((a) => a.id === storedId) ||
        list.find((a) => a.username === 'admin') ||
        list[0] ||
        null

      if (selected) {
        localStorage.setItem(STORAGE_KEY, String(selected.id))
        setCurrentAccount(selected)
      } else {
        setCurrentAccount(null)
      }
    } catch (error) {
      console.error('Failed to load accounts:', error)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    refreshAccounts()
  }, [])

  const setCurrentAccountId = (id: number) => {
    const account = accounts.find((a) => a.id === id)
    if (!account) return
    localStorage.setItem(STORAGE_KEY, String(id))
    setCurrentAccount(account)
  }

  const createAccount = async (data: { username: string; email?: string; role?: string }) => {
    try {
      const response = await fetch('/v1/admin/accounts', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
      })
      if (!response.ok) {
        const err = await response.json().catch(() => ({}))
        throw new Error(err.error || 'failed to create account')
      }
      const result = await response.json()
      await refreshAccounts()
      return result.account as Account
    } catch (error) {
      console.error('Failed to create account:', error)
      return null
    }
  }

  const deleteAccount = async (id: number) => {
    try {
      const response = await fetch(`/v1/admin/accounts/${id}`, { method: 'DELETE' })
      if (!response.ok) {
        const err = await response.json().catch(() => ({}))
        throw new Error(err.error || 'failed to delete account')
      }
      if (currentAccount?.id === id) {
        localStorage.removeItem(STORAGE_KEY)
      }
      await refreshAccounts()
      return true
    } catch (error) {
      console.error('Failed to delete account:', error)
      return false
    }
  }

  return (
    <AccountContext.Provider
      value={{
        accounts,
        currentAccount,
        loading,
        setCurrentAccountId,
        refreshAccounts,
        createAccount,
        deleteAccount,
      }}
    >
      {children}
    </AccountContext.Provider>
  )
}

export function useAccount() {
  const ctx = useContext(AccountContext)
  if (!ctx) {
    throw new Error('useAccount must be used within AccountProvider')
  }
  return ctx
}

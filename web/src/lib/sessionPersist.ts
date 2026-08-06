/** Browser keys for restoring Code / Chat sessions across route changes. */

export function coderWorkspaceKey(accountId: number) {
  return `cg_coder_workspace_${accountId}`
}

export function coderSessionKey(accountId: number, workspaceId: string) {
  return `cg_coder_session_${accountId}_${workspaceId || 'none'}`
}

export function chatSessionKey(accountId: number) {
  return `cg_chat_session_${accountId}`
}

export function readLocal(key: string): string {
  try {
    return localStorage.getItem(key) || ''
  } catch {
    return ''
  }
}

export function writeLocal(key: string, value: string) {
  try {
    if (value) localStorage.setItem(key, value)
    else localStorage.removeItem(key)
  } catch {
    /* ignore quota / private mode */
  }
}

export function clearLocal(key: string) {
  writeLocal(key, '')
}

export type RestoredSessionMessage = {
  id: string
  role: string
  content: string
  model?: string
  created_at?: string
}

export type ActiveRunInfo = {
  id: string
  status: string
  last_seq: number
  model?: string
}

export type SessionRestorePayload = {
  messages: RestoredSessionMessage[]
  active_run?: ActiveRunInfo
  active_run_tool_steps?: { tool: string; args: string; result: string }[]
  latest_run?: ActiveRunInfo
  latest_run_tool_steps?: { tool: string; args: string; result: string }[]
}

/** Attach tool steps from a finished/active run onto the last assistant message when missing. */
export function attachToolStepsToMessages<T extends { id: string; role: string; toolSteps?: { tool: string; args: string; result: string }[] }>(
  messages: T[],
  steps?: { tool: string; args: string; result: string }[],
): T[] {
  if (!steps?.length) return messages
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].role === 'assistant') {
      const next = [...messages]
      next[i] = { ...next[i], toolSteps: steps }
      return next
    }
  }
  return messages
}

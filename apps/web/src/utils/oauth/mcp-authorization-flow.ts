interface AuthorizationStatus {
  has_token?: boolean
  expired?: boolean
  authorization?: { id?: string, status?: string }
}

interface Options {
  authorizationId: string
  popup: { readonly closed?: boolean, close?: () => void } | null
  isCurrent: () => boolean
  pollStatus: (signal: AbortSignal) => Promise<AuthorizationStatus | null>
  checkConnection: (signal: AbortSignal) => Promise<boolean>
  onSuccess: () => void
  onFailure: (reason: 'failed' | 'unconfirmed' | 'connection') => void
  pollIntervalMs?: number
  timeoutMs?: number
}

// MCP success requires a receipt for this exact exchange plus a live connection
// check. Old tokens, expiry changes and untrusted postMessages are not receipts.
export function startMCPAuthorizationFlow(options: Options) {
  let completed = false
  let pollTimer: ReturnType<typeof setTimeout> | undefined
  const controller = new AbortController()
  const active = () => !completed && options.isCurrent()
  const dispose = () => {
    completed = true
    clearTimeout(pollTimer)
    clearTimeout(deadline)
    controller.abort()
  }
  const finish = (success: boolean, reason: 'failed' | 'unconfirmed' | 'connection' = 'unconfirmed') => {
    if (!active()) return dispose()
    dispose()
    if (success) {
      options.popup?.close?.()
      options.onSuccess()
    } else {
      options.onFailure(reason)
    }
  }
  const deadline = setTimeout(() => finish(false), options.timeoutMs ?? 120_000)
  const poll = async () => {
    if (!active()) return dispose()
    try {
      const status = await options.pollStatus(controller.signal)
      if (!active()) return dispose()
      const receipt = status?.authorization
      if (receipt?.id === options.authorizationId && receipt.status === 'succeeded') {
        if (!status?.has_token || status.expired) return finish(false, 'connection')
        const connected = await options.checkConnection(controller.signal)
        if (!active()) return dispose()
        return finish(connected, 'connection')
      }
      if (receipt?.id !== options.authorizationId || receipt.status !== 'pending' || options.popup?.closed) {
        return finish(false, receipt?.id === options.authorizationId && receipt.status === 'failed' ? 'failed' : 'unconfirmed')
      }
    } catch {
      if (!active()) return dispose()
      if (options.popup?.closed) return finish(false)
    }
    pollTimer = setTimeout(() => { void poll() }, options.pollIntervalMs ?? 2000)
  }
  pollTimer = setTimeout(() => { void poll() }, options.pollIntervalMs ?? 2000)
  return { dispose }
}

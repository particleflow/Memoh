import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { startMCPAuthorizationFlow } from './mcp-authorization-flow'

describe('MCP authorization transaction', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  function fixture() {
    const status = { has_token: true, expired: false, authorization: { id: 'current', status: 'pending' } }
    const options = {
      authorizationId: 'current',
      popup: { closed: false, close: vi.fn() },
      isCurrent: vi.fn(() => true),
      pollStatus: vi.fn(async () => status),
      checkConnection: vi.fn(async () => true),
      onSuccess: vi.fn(),
      onFailure: vi.fn(),
      pollIntervalMs: 10,
      timeoutMs: 100,
    }
    return { status, options }
  }

  it.each([false, true])('does not complete from an old token (expired=%s)', async (expired) => {
    const { status, options } = fixture()
    status.expired = expired
    const flow = startMCPAuthorizationFlow(options)
    await vi.advanceTimersByTimeAsync(40)
    expect(options.onSuccess).not.toHaveBeenCalled()
    expect(options.checkConnection).not.toHaveBeenCalled()
    expect(options.popup.close).not.toHaveBeenCalled()
    flow.dispose()
  })

  it('requires the exact successful receipt and a successful live probe', async () => {
    const { status, options } = fixture()
    status.authorization.status = 'succeeded'
    startMCPAuthorizationFlow(options)
    await vi.advanceTimersByTimeAsync(40)
    expect(options.checkConnection).toHaveBeenCalledTimes(1)
    expect(options.onSuccess).toHaveBeenCalledTimes(1)
    expect(options.popup.close).toHaveBeenCalledTimes(1)
  })

  it.each(['failed', 'unknown'])('rejects %s even with a valid old token', async (result) => {
    const { status, options } = fixture()
    status.authorization.status = result
    startMCPAuthorizationFlow(options)
    await vi.advanceTimersByTimeAsync(20)
    expect(options.onSuccess).not.toHaveBeenCalled()
    expect(options.checkConnection).not.toHaveBeenCalled()
    expect(options.onFailure).toHaveBeenCalledTimes(1)
    expect(options.onFailure).toHaveBeenCalledWith(result === 'failed' ? 'failed' : 'unconfirmed')
  })

  it('rejects another transaction and a failed connection probe', async () => {
    const first = fixture()
    first.status.authorization = { id: 'other', status: 'succeeded' }
    startMCPAuthorizationFlow(first.options)
    await vi.advanceTimersByTimeAsync(20)
    expect(first.options.checkConnection).not.toHaveBeenCalled()
    expect(first.options.onSuccess).not.toHaveBeenCalled()
    const second = fixture()
    second.status.authorization.status = 'succeeded'
    second.options.checkConnection.mockResolvedValue(false)
    startMCPAuthorizationFlow(second.options)
    await vi.advanceTimersByTimeAsync(20)
    expect(second.options.onSuccess).not.toHaveBeenCalled()
    expect(second.options.onFailure).toHaveBeenCalledTimes(1)
    expect(second.options.onFailure).toHaveBeenCalledWith('connection')
  })

  it('treats a closed popup with a pending exchange as cancelled', async () => {
    const { options } = fixture()
    options.popup.closed = true
    startMCPAuthorizationFlow(options)
    await vi.advanceTimersByTimeAsync(20)
    expect(options.onFailure).toHaveBeenCalledTimes(1)
    expect(options.onFailure).toHaveBeenCalledWith('unconfirmed')
    expect(options.onSuccess).not.toHaveBeenCalled()
  })

  it.each(['dispose', 'bot-switch', 'timeout'])('ignores late status after %s', async (reason) => {
    const { status, options } = fixture()
    let resolve!: (value: typeof status) => void
    options.pollStatus.mockImplementation(() => new Promise(r => { resolve = r }))
    const flow = startMCPAuthorizationFlow(options)
    await vi.advanceTimersByTimeAsync(10)
    if (reason === 'dispose') flow.dispose()
    if (reason === 'bot-switch') options.isCurrent.mockReturnValue(false)
    if (reason === 'timeout') await vi.advanceTimersByTimeAsync(100)
    status.authorization.status = 'succeeded'
    resolve(status)
    await vi.advanceTimersByTimeAsync(10)
    expect(options.checkConnection).not.toHaveBeenCalled()
    expect(options.onSuccess).not.toHaveBeenCalled()
    expect(options.popup.close).not.toHaveBeenCalled()
  })

  it('ignores a late probe after leaving the Bot and supports desktop polling', async () => {
    const { status, options } = fixture()
    status.authorization.status = 'succeeded'
    let resolve!: (value: boolean) => void
    options.checkConnection.mockImplementation(() => new Promise(r => { resolve = r }))
    const flow = startMCPAuthorizationFlow({ ...options, popup: null })
    await vi.advanceTimersByTimeAsync(10)
    flow.dispose()
    resolve(true)
    await vi.advanceTimersByTimeAsync(10)
    expect(options.onSuccess).not.toHaveBeenCalled()
  })
})

import { effectScope, ref, shallowRef } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { BotdelegationView } from '@memohai/sdk'
import { useBotDelegationDelivery } from './useBotDelegationDelivery'
import { useBotDelegations } from './useBotDelegations'
import type { TaskScope } from './useSessionTasks'

const api = vi.hoisted(() => ({ retry: vi.fn(), list: vi.fn(), detail: vi.fn() }))
vi.mock('@memohai/sdk', () => ({
  postBotsByBotIdSessionsBySessionIdDelegationsByDelegationIdDelivery: api.retry,
  getBotsByBotIdSessionsBySessionIdDelegations: api.list,
  getBotsByBotIdSessionsBySessionIdDelegationsByDelegationId: api.detail,
}))
const task: BotdelegationView = {
  delegation_id: 'handoff-a', request_id: 'request-a', source_session_id: 'session-a', target_bot_id: 'specialist-a',
  state: 'completed', delivery_state: 'unknown', attachments: [],
  created_at: '2026-10-22T00:00:00Z', updated_at: '2026-10-22T00:00:00Z',
}
const accepted = { ...task, delivery_state: 'accepted' as const }
const owners: ReturnType<typeof effectScope>[] = []
function setup(realReader = false) {
  const owner = effectScope(); owners.push(owner)
  const scope = ref<TaskScope | null>({ bot: 'supervisor-a', session: 'session-a', selection: 'pc-a', identity: 'synthetic-a' })
  const visible = ref(true)
  const reader = realReader ? owner.run(() => useBotDelegations(scope, visible))! : {
    detail: shallowRef<BotdelegationView | undefined>(task), selected: ref<string | undefined>(task.delegation_id),
    error: shallowRef<unknown>(), reload: vi.fn().mockResolvedValue(undefined),
  }
  const action = owner.run(() => useBotDelegationDelivery(scope, visible, reader))!
  return { owner, scope, visible, reader, action }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (cause: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
beforeEach(() => {
  vi.useFakeTimers()
  api.retry.mockReset().mockResolvedValue({ data: accepted })
  api.list.mockReset().mockResolvedValue({ data: { items: [task] } })
  api.detail.mockReset().mockResolvedValue({ data: task })
})
afterEach(() => { owners.splice(0).forEach(owner => owner.stop()); vi.useRealTimers() })

describe('delivery-only mutation identity and acknowledgement', () => {
  it('coalesces repeated clicks into one POST for the existing delegation, then reads state', async () => {
    const request = deferred<{ data: BotdelegationView }>()
    api.retry.mockReturnValueOnce(request.promise)
    const { action, reader } = setup()
    const first = action.value.retry()
    action.value.retry(); action.value.retry()
    expect(api.retry).toHaveBeenCalledTimes(1)
    const options = api.retry.mock.calls[0]![0]
    expect(options.path).toEqual({ bot_id: 'supervisor-a', session_id: 'session-a', delegation_id: 'handoff-a' })
    expect(options).not.toHaveProperty('body')
    expect(options).not.toHaveProperty('headers')
    expect(options.cache).toBe('no-store')
    expect(options.throwOnError).toBe(true)
    expect(action.value.pending).toBe(true)
    vi.mocked(reader.reload).mockImplementationOnce(async () => { reader.detail.value = accepted })
    request.resolve({ data: accepted }); await first
    expect(reader.reload).toHaveBeenCalledTimes(1)
    expect(action.value.pending).toBe(false)
    expect(action.value.response).toEqual(accepted)
    expect(action.value.disabled).toBe(true)
    action.value.retry()
    expect(api.retry).toHaveBeenCalledTimes(1)
  })

  it.each(['accepted', 'running', 'unknown'] as const)('never retries specialist execution state %s', async (state) => {
    const { action, reader } = setup()
    reader.detail.value = { ...task, state }
    await action.value.retry()
    expect(action.value.disabled).toBe(true)
    expect(api.retry).not.toHaveBeenCalled()
  })

  it('uses the fresh server observation after a prior accepted response', async () => {
    const { action, reader } = setup()
    await action.value.retry()
    expect(action.value.response?.delivery_state).toBe('accepted')
    // A subsequent authoritative read remains unknown; do not replace it with
    // the older POST acknowledgement or treat it as supervisor completion.
    expect(reader.detail.value?.delivery_state).toBe('unknown')
    expect(action.value.disabled).toBe(false)
  })

  it('rejects a detail that belongs to a different source session', async () => {
    const { action, reader } = setup()
    reader.detail.value = { ...task, source_session_id: 'another-session' }
    await action.value.retry()
    expect(api.retry).not.toHaveBeenCalled()
  })

  it('does not issue a POST for an already accepted delivery', async () => {
    const { action, reader } = setup()
    reader.detail.value = accepted
    await action.value.retry()
    expect(api.retry).not.toHaveBeenCalled()
    expect(action.value.response).toBeUndefined()
  })

  it.each(['pending', 'unknown', 'unavailable'] as const)('preserves server %s response for a failed business task', async (delivery_state) => {
    const { action, reader } = setup()
    reader.detail.value = { ...task, state: 'failed' }
    const response = { ...task, state: 'failed' as const, delivery_state }
    api.retry.mockResolvedValue({ data: response })
    await action.value.retry()
    expect(action.value.response).toEqual(response)
    expect(action.value.response?.state).toBe('failed')
    expect(action.value.disabled).toBe(false)
  })

  it.each([401, 403, 404])('retains a %s denial, refreshes authority and blocks repeated mutation', async (status) => {
    const { action, reader } = setup()
    const denied = { status, code: status === 404 ? 'session.not_found' : `http.${status === 401 ? 'unauthorized' : 'forbidden'}`, detail: 'PRIVATE diagnostic' }
    api.retry.mockRejectedValueOnce(denied)
    await action.value.retry()
    expect(action.value.error).toEqual(denied)
    expect(action.value.response).toBeUndefined()
    expect(reader.reload).toHaveBeenCalledTimes(1)
    expect(action.value.disabled).toBe(true)
    await action.value.retry()
    expect(api.retry).toHaveBeenCalledTimes(1)
  })

  it('treats a 409 execution conflict as a failed request, never synthesizing completion', async () => {
    const { action, reader } = setup()
    api.retry.mockRejectedValueOnce({ status: 409, code: 'http.conflict' })
    await action.value.retry()
    expect(action.value.error).toMatchObject({ status: 409 })
    expect(action.value.response).toBeUndefined()
    expect(reader.reload).toHaveBeenCalledTimes(1)
  })

  it('keeps a network failure unconfirmed and never retries automatically', async () => {
    const { action } = setup()
    api.retry.mockRejectedValueOnce(new TypeError('synthetic disconnect'))
    await action.value.retry()
    await vi.advanceTimersByTimeAsync(60000)
    expect(action.value.response).toBeUndefined()
    expect(action.value.error).toBeInstanceOf(TypeError)
    expect(api.retry).toHaveBeenCalledTimes(1)
  })
})

describe('late mutation response isolation', () => {
  it.each(['bot', 'session', 'selection', 'identity', 'hide', 'task', 'dispose'] as const)('discards a late success after %s changes', async (change) => {
    const request = deferred<{ data: BotdelegationView }>()
    api.retry.mockReturnValueOnce(request.promise)
    const { action, reader, scope, visible, owner } = setup()
    const pending = action.value.retry()
    const signal: AbortSignal = api.retry.mock.calls[0]![0].signal
    if (change === 'hide') visible.value = false
    else if (change === 'task') reader.selected.value = 'handoff-b'
    else if (change === 'dispose') owner.stop()
    else scope.value = { ...scope.value!, [change]: 'another-scope' }
    expect(signal.aborted).toBe(true)
    request.resolve({ data: accepted }); await pending
    expect(action.value.pending).toBe(false)
    expect(action.value.response).toBeUndefined()
    expect(action.value.error).toBeUndefined()
    expect(reader.reload).not.toHaveBeenCalled()
  })

  it('ignores an old denial while a new scope mutation is still pending', async () => {
    const old = deferred<{ data: BotdelegationView }>()
    const current = deferred<{ data: BotdelegationView }>()
    api.retry.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const { action, reader, scope } = setup()
    const first = action.value.retry()
    scope.value = { ...scope.value!, bot: 'supervisor-b', session: 'session-b' }
    reader.detail.value = { ...task, source_session_id: 'session-b' }
    const second = action.value.retry()
    old.reject({ status: 403, code: 'http.forbidden' }); await first
    expect(action.value.pending).toBe(true)
    expect(action.value.error).toBeUndefined()
    current.resolve({ data: { ...accepted, source_session_id: 'session-b' } }); await second
    expect(action.value.response?.source_session_id).toBe('session-b')
    expect(reader.reload).toHaveBeenCalledTimes(1)
  })

  it('retires a pre-action GET so its late response cannot replace the accepted observation', async () => {
    const { action, reader } = setup(true)
    if (!('openTask' in reader)) throw new Error('real reader required')
    await Promise.resolve(); reader.openTask(task); await Promise.resolve()
    const oldRead = deferred<{ data: BotdelegationView }>()
    api.detail.mockReturnValueOnce(oldRead.promise).mockResolvedValueOnce({ data: accepted })
    const stale = reader.refresh()
    await action.value.retry()
    expect(reader.detail.value?.delivery_state).toBe('accepted')
    oldRead.resolve({ data: task }); await stale
    expect(reader.detail.value?.delivery_state).toBe('accepted')
    expect(action.value.response?.delivery_state).toBe('accepted')
    expect(api.retry).toHaveBeenCalledTimes(1)
  })
})

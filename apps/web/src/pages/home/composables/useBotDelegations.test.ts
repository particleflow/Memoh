import { effectScope, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { BotdelegationView } from '@memohai/sdk'
import { delegationAPIUnsupported, useBotDelegations } from './useBotDelegations'
import type { TaskScope } from './useSessionTasks'

const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn() }))
vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdSessionsBySessionIdDelegations: api.list,
  getBotsByBotIdSessionsBySessionIdDelegationsByDelegationId: api.detail,
}))
const task: BotdelegationView = {
  delegation_id: 'delegation-a', request_id: 'request-a', source_session_id: 'session-a',
  target_bot_id: 'specialist-a', state: 'completed', delivery_state: 'accepted',
  created_at: '2026-10-22T00:00:00Z', updated_at: '2026-10-22T00:00:00Z',
  attachments: [], summary: { text: 'Final visible reply', truncated: false, source: 'assistant_final' },
}
const owners: ReturnType<typeof effectScope>[] = []
function setup(shown = true) {
  const owner = effectScope(); owners.push(owner)
  const scope = ref<TaskScope | null>({ bot: 'supervisor-a', session: 'session-a', selection: 'native', identity: 'synthetic' })
  const visible = ref(shown)
  const reader = owner.run(() => useBotDelegations(scope, visible))!
  return { reader, scope, visible }
}
async function settle() { await Promise.resolve(); await Promise.resolve() }
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (cause: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

beforeEach(() => {
  vi.useFakeTimers()
  api.list.mockReset().mockResolvedValue({ data: { items: [task] } })
  api.detail.mockReset().mockResolvedValue({ data: task })
})
afterEach(() => { owners.splice(0).forEach(owner => owner.stop()); vi.useRealTimers() })

describe('authorized Bot handoff observation', () => {
  it('makes no hidden requests, polls visible GETs and aborts when closed', async () => {
    const { reader, visible } = setup(false)
    expect(api.list).not.toHaveBeenCalled()
    visible.value = true
    await settle()
    expect(api.list).toHaveBeenCalledWith(expect.objectContaining({
      path: { bot_id: 'supervisor-a', session_id: 'session-a' }, query: { limit: 20, cursor: undefined }, cache: 'no-store',
    }))
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.list).toHaveBeenCalledTimes(2)
    const late = deferred<{ data: BotdelegationView }>()
    api.detail.mockReturnValueOnce(late.promise)
    reader.openTask(task)
    const signal = api.detail.mock.lastCall![0].signal as AbortSignal
    visible.value = false
    expect(signal.aborted).toBe(true)
    late.resolve({ data: task }); await settle()
    expect(reader.detail.value).toBeUndefined()
    expect(reader.items.value).toEqual([])
    await vi.advanceTimersByTimeAsync(15000)
    expect(api.list).toHaveBeenCalledTimes(2)
  })

  it('coalesces repeated opens and reload clicks while one read is pending', async () => {
    const { reader } = setup(); await settle()
    const late = deferred<{ data: BotdelegationView }>()
    api.detail.mockReturnValueOnce(late.promise)
    reader.openTask(task); reader.openTask(task); void reader.refresh(); void reader.refresh()
    expect(api.detail).toHaveBeenCalledTimes(1)
    late.resolve({ data: task }); await settle()
    expect(reader.detail.value).toEqual(task)
    expect(api.detail).toHaveBeenCalledWith(expect.objectContaining({
      path: { bot_id: 'supervisor-a', session_id: 'session-a', delegation_id: 'delegation-a' }, cache: 'no-store',
    }))
  })

  it('preserves an opaque cursor on an empty filtered page and resets to newest', async () => {
    api.list.mockResolvedValueOnce({ data: { items: [], next_cursor: 'delegations-only-cursor' } })
    const { reader } = setup(); await settle()
    reader.page(reader.nextCursor.value); await settle()
    expect(api.list).toHaveBeenLastCalledWith(expect.objectContaining({ query: { limit: 20, cursor: 'delegations-only-cursor' } }))
    reader.page(); await settle()
    expect(reader.cursor.value).toBeUndefined()
  })

  it.each(['bot', 'session', 'identity', 'selection'] as const)('rejects old detail and list responses after %s changes', async (field) => {
    const { reader, scope } = setup(); await settle()
    const old = deferred<{ data: BotdelegationView }>()
    api.detail.mockReturnValueOnce(old.promise); reader.openTask(task)
    const oldSignal = api.detail.mock.lastCall![0].signal as AbortSignal
    const fresh = { ...task, delegation_id: 'new-task', summary: { text: 'New scope reply' } }
    api.list.mockResolvedValue({ data: { items: [fresh] } })
    scope.value = { ...scope.value!, [field]: 'new-scope' }
    expect(oldSignal.aborted).toBe(true)
    expect(reader.items.value).toEqual([])
    expect(reader.selected.value).toBeUndefined()
    old.resolve({ data: task }); await settle()
    expect(reader.detail.value).toBeUndefined()
    expect(reader.items.value).toEqual([fresh])

    const oldList = deferred<{ data: { items: BotdelegationView[], next_cursor: string } }>()
    api.list.mockReturnValueOnce(oldList.promise); void reader.refresh()
    scope.value = { ...scope.value!, [field]: 'another-scope' }; await settle()
    oldList.resolve({ data: { items: [task], next_cursor: 'old-cursor' } }); await settle()
    expect(reader.items.value).toEqual([fresh])
    expect(reader.nextCursor.value).toBeUndefined()
  })

  it.each([
    { code: 'http.unauthorized', status: 401 },
    { code: 'http.forbidden', status: 403 },
    { code: 'session.not_found', status: 404 },
    { code: 'http.not_found', status: 404 },
  ])('clears denied content and pauses automatic reads for $code', async (failure) => {
    const { reader } = setup(); await settle()
    reader.openTask(task); await settle()
    api.detail.mockRejectedValueOnce(failure); await reader.refresh()
    expect(reader.detail.value).toBeUndefined()
    expect(reader.items.value).toEqual([])
    expect(reader.error.value).toEqual(failure)
    const count = api.detail.mock.calls.length
    await vi.advanceTimersByTimeAsync(15000)
    expect(api.detail).toHaveBeenCalledTimes(count)
    await reader.refresh()
    expect(reader.detail.value).toEqual(task)
  })

  it('clears a stale success on a network failure and can read again without mutation', async () => {
    const { reader } = setup(); await settle()
    api.list.mockRejectedValueOnce(new TypeError('private network diagnostic'))
    await reader.refresh()
    expect(reader.items.value).toEqual([])
    await vi.advanceTimersByTimeAsync(5000)
    expect(reader.items.value).toEqual([task])
  })

  it('bounds a hung read to 15 seconds', async () => {
    api.list.mockImplementationOnce(({ signal }: { signal: AbortSignal }) => new Promise((_, reject) => {
      signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
    }))
    const { reader, visible } = setup(false)
    await vi.advanceTimersByTimeAsync(100)
    visible.value = true
    await vi.advanceTimersByTimeAsync(15000)
    expect(reader.loading.value).toBe(false)
    expect(reader.error.value).toBeInstanceOf(DOMException)
    expect(reader.items.value).toEqual([])
  })

  it('distinguishes unsupported route from a concealed authorization denial', () => {
    expect(delegationAPIUnsupported({ code: 'http.not_found', status: 404 })).toBe(true)
    expect(delegationAPIUnsupported({ status: 404 })).toBe(true)
    expect(delegationAPIUnsupported({ code: 'session.not_found', status: 404 })).toBe(false)
    expect(delegationAPIUnsupported({ code: 'http.forbidden', status: 403 })).toBe(false)
  })
})

import { effectScope, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { BotdelegationView } from '@memohai/sdk'
import { useBotDelegationDownload } from './useBotDelegationDownload'
import type { TaskScope } from './useSessionTasks'
const api = vi.hoisted(() => ({ head: vi.fn(), url: vi.fn(), click: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ headBotsByBotIdSessionsBySessionIdDelegationsByDelegationIdAttachmentsByContentHash: api.head }))
vi.mock('@/lib/api-client', () => ({ sdkApiUrl: api.url, sdkAuthQuery: () => ({ token: 'synthetic' }) }))
const hash = 'a'.repeat(64)
const task: BotdelegationView = {
  delegation_id: 'handoff', source_session_id: 'session', target_bot_id: 'specialist', request_id: 'request', state: 'completed', delivery_state: 'accepted',
  created_at: '2026-10-23', updated_at: '2026-10-23', attachments: [{ content_hash: hash, url: `/bots/bot/sessions/session/delegations/handoff/attachments/${hash}` }],
}
const owners: ReturnType<typeof effectScope>[] = []
function setup() {
  const owner = effectScope(); owners.push(owner)
  const scope = ref<TaskScope | null>({ bot: 'bot', session: 'session', selection: 'native', identity: 'user' })
  const visible = ref(true)
  const reader = { detail: ref<BotdelegationView | undefined>(structuredClone(task)), selected: ref('handoff'), error: ref<unknown>() }
  const action = owner.run(() => useBotDelegationDownload(scope, visible, reader))!
  return { scope, visible, reader, action }
}
beforeEach(() => {
  api.head.mockReset().mockResolvedValue({})
  api.url.mockReset().mockImplementation(({ url }) => url)
  api.click.mockReset()
  vi.stubGlobal('document', { createElement: () => ({ click: api.click }) })
})
afterEach(() => { owners.splice(0).forEach(o => o.stop()); vi.unstubAllGlobals() })
describe('scope-bound delegation downloads', () => {
  it('checks the original route once, coalesces double click, streams via browser without Blob fallback', async () => {
    let finish!: () => void
    api.head.mockReturnValue(new Promise<void>(resolve => { finish = resolve }))
    const { action } = setup()
    const first = action.value.download(task.attachments[0]!)
    await action.value.download(task.attachments[0]!)
    expect(api.head).toHaveBeenCalledTimes(1)
    expect(api.head.mock.calls[0]![0]).toMatchObject({ path: { bot_id: 'bot', session_id: 'session', delegation_id: 'handoff', content_hash: hash }, cache: 'no-store', throwOnError: true })
    finish(); await first
    expect(api.click).toHaveBeenCalledTimes(1)
    expect(api.url).toHaveBeenCalledWith({ url: task.attachments[0]!.url, query: { token: 'synthetic' } })
  })
  it.each(['https://example.invalid/file', '/bots/bot/media/'+hash, `/bots/other/sessions/session/delegations/handoff/attachments/${hash}`, `/bots/bot/sessions/session/delegations/foreign/attachments/${hash}`])('rejects a mismatching returned URL %s', async url => {
    const { action, reader } = setup()
    const a = { content_hash: hash, url }; reader.detail.value!.attachments = [a]
    expect(action.value.available(a)).toBe(false)
    await action.value.download(a)
    expect(api.head).not.toHaveBeenCalled(); expect(api.click).not.toHaveBeenCalled()
  })
  it.each([401, 403, 404])('stops after %i and never falls back or auto retries', async status => {
    api.head.mockRejectedValue({ status, detail: 'PRIVATE' })
    const { action } = setup()
    await action.value.download(task.attachments[0]!); await action.value.download(task.attachments[0]!)
    expect(api.head).toHaveBeenCalledTimes(1); expect(api.click).not.toHaveBeenCalled()
    expect(action.value.available(task.attachments[0]!)).toBe(false)
  })
  it.each(['scope', 'hidden', 'denial', 'removed'])('ignores a late HEAD after %s', async change => {
    let finish!: () => void
    api.head.mockReturnValue(new Promise<void>(resolve => { finish = resolve }))
    const { action, reader, scope, visible } = setup()
    const first = action.value.download(task.attachments[0]!)
    if (change === 'scope') scope.value = { ...scope.value!, bot: 'other' }
    if (change === 'hidden') visible.value = false
    if (change === 'denial') reader.error.value = { status: 403 }
    if (change === 'removed') reader.detail.value!.attachments = []
    finish(); await first
    expect(api.click).not.toHaveBeenCalled()
  })
})

// @vitest-environment jsdom
import { effectScope, nextTick, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SessiontaskActivity, SessiontaskActivityPage } from '@memohai/sdk'
import { useBotActivity } from './useBotActivity'

const api = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ getBotsByBotIdActivity: api.get }))
let scope: ReturnType<typeof effectScope>
const bots = ref(['first', 'second']), identity = ref('account'), visible = ref(true)
const item = (bot: string, id: string): SessiontaskActivity => ({ id, bot_id: bot, session_id: 'original-' + id, kind: 'agent', name: 'Original name', state: 'running' })
async function settle() { await nextTick(); await vi.advanceTimersByTimeAsync(20); await nextTick() }
function start() { scope = effectScope(); return scope.run(() => useBotActivity(bots, identity, visible))! }
beforeEach(() => { vi.useFakeTimers(); bots.value = ['first', 'second']; identity.value = 'account'; visible.value = true; api.get.mockReset(); api.get.mockResolvedValue({ data: { items: [] } }) })
afterEach(() => { scope?.stop(); vi.useRealTimers() })
describe('accessible Bot activity discovery', () => {
  it('reads every accessible Bot including an unopened Bot and follows its original continuation', async () => {
    api.get.mockImplementation(async ({ path, query }: { path: { bot_id: string }, query: { cursor?: string } }) => ({ data: path.bot_id === 'first' ? { items: [] } : query.cursor ? { items: [item('second', 'later'), item('second', 'later')] } : { items: [], next_cursor: 'original-cursor' } }))
    const reader = start(); await settle()
    expect(api.get).toHaveBeenCalledTimes(3)
    expect(reader.items.value.map(x => x.id)).toEqual(['later'])
  })
  it('rejects a repeated cursor without retaining stale rows for that Bot', async () => {
    api.get.mockResolvedValue({ data: { items: [], next_cursor: 'loop' } })
    const reader = start(); await settle(); expect(reader.items.value).toEqual([])
    expect(api.get).toHaveBeenCalledTimes(4)
  })
  it('does not allow one denied Bot to hide another readable Bot or keep its prior success', async () => {
    api.get.mockImplementation(async ({ path }: { path: { bot_id: string } }) => ({ data: { items: [item(path.bot_id, path.bot_id)] } }))
    const reader = start(); await settle(); expect(reader.items.value).toHaveLength(2)
    api.get.mockImplementation(async ({ path }: { path: { bot_id: string } }) => { if (path.bot_id === 'first') throw { status: 403 }; return { data: { items: [item('second', 'second')] } } })
    await vi.advanceTimersByTimeAsync(5000); expect(reader.items.value.map(x => x.bot_id)).toEqual(['second'])
    const deniedReads = api.get.mock.calls.filter(([x]) => x.path.bot_id === 'first').length
    await vi.advanceTimersByTimeAsync(5000)
    expect(api.get.mock.calls.filter(([x]) => x.path.bot_id === 'first')).toHaveLength(deniedReads)
  })
  it('recovers after a transient read failure without submitting any task', async () => {
    bots.value = ['first']; api.get.mockResolvedValueOnce({ data: { items: [item('first', 'same')] } }).mockRejectedValueOnce({ status: 503 }).mockResolvedValue({ data: { items: [item('first', 'same')] } })
    const reader = start(); await settle(); await vi.advanceTimersByTimeAsync(5000); expect(reader.items.value).toEqual([])
    await vi.advanceTimersByTimeAsync(5000); expect(reader.items.value[0]?.id).toBe('same')
  })
  it('drops late success after account A to B to A and accepts only the new generation', async () => {
    bots.value = ['first']; let finish!: (value: { data: SessiontaskActivityPage }) => void
    api.get.mockReturnValueOnce(new Promise(resolve => { finish = resolve })).mockResolvedValue({ data: { items: [item('first', 'new')] } })
    const reader = start(); await settle(); identity.value = 'other'; await settle(); identity.value = 'account'; await settle()
    finish({ data: { items: [item('first', 'old')] } }); await settle(); expect(reader.items.value.map(x => x.id)).toEqual(['new'])
  })
  it('keeps a late rejection from clearing a newer account result', async () => {
    bots.value = ['first']; let fail!: (value: unknown) => void
    api.get.mockReturnValueOnce(new Promise((_resolve, reject) => { fail = reject })).mockResolvedValue({ data: { items: [item('first', 'new')] } })
    const reader = start(); await settle(); identity.value = 'other'; await settle(); identity.value = 'account'; await settle()
    fail({ status: 401 }); await settle(); expect(reader.items.value[0]?.id).toBe('new')
  })
  it('clears inaccessible/foreign Bot rows and stops hidden reads', async () => {
    bots.value = ['first']; api.get.mockResolvedValue({ data: { items: [item('second', 'foreign')] } })
    const reader = start(); await settle(); expect(reader.items.value).toEqual([])
    visible.value = false; await settle(); const count = api.get.mock.calls.length
    await vi.advanceTimersByTimeAsync(10000); expect(api.get).toHaveBeenCalledTimes(count)
  })
  it('only enables old-server fallback when all Bot endpoints are missing', async () => {
    api.get.mockRejectedValue({ status: 404 }); const reader = start(); await settle(); expect(reader.unsupported.value).toBe(true)
    api.get.mockRejectedValue({ status: 403 }); bots.value = ['replacement']; await settle(); expect(reader.unsupported.value).toBe(false)
  })
})

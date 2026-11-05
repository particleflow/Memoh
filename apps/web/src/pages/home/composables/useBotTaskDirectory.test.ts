import { effectScope, ref } from 'vue'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { useBotTaskDirectory } from './useBotTaskDirectory'
const api = vi.hoisted(() => ({ list: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ getBotsByBotIdTasks: api.list }))
let owner: ReturnType<typeof effectScope> | undefined
const row = (id: string, state = 'failed') => ({ task_type: 'native_agent', task_id: 'bg_original', record_id: id, name: 'Same name', state, created_at: '2026-01-01', destination: { bot_id: 'bot', session_id: 'execution' } })
const batch = (id: string) => ({ task_type: 'computer_batch' as const, task_id: id, record_id: id, name: 'Album batch', state: 'running', progress: '1/3', created_at: '2026-01-01', destination: { bot_id: 'bot', session_id: 'execution' } })
const settle = async () => { for (let i = 0; i < 20; i++) await Promise.resolve() }
function setup() {
  owner = effectScope(); const bot = ref('bot'), identity = ref('account'), visible = ref(true)
  const reader = owner.run(() => useBotTaskDirectory(bot, identity, visible))!
  return { bot, identity, visible, reader }
}
beforeEach(() => { vi.useFakeTimers(); api.list.mockReset() })
afterEach(() => { owner?.stop(); vi.useRealTimers() })
it('appends the older page on scroll instead of replacing the view', async () => {
  api.list.mockResolvedValueOnce({ data: { items: [row('run-1'), row('run-2')], next_cursor: 'more' } }).mockResolvedValueOnce({ data: { items: [row('run-3'), batch('batch-1')] } })
  const { reader } = setup(); await settle()
  expect(reader.items.value.map(r => r.record_id)).toEqual(['run-1', 'run-2'])
  reader.loadOlder(); await settle()
  expect(reader.items.value.map(r => r.record_id)).toEqual(['run-1', 'run-2', 'run-3', 'batch-1'])
  expect(api.list.mock.calls[1]![0]).toMatchObject({ path: { bot_id: 'bot' }, query: { cursor: 'more' }, cache: 'no-store' })
})
it('merges the live window over the accumulated rows without paging controls', async () => {
  api.list.mockResolvedValueOnce({ data: { items: [row('run-1'), row('run-2')], next_cursor: 'more' } }).mockResolvedValueOnce({ data: { items: [row('run-3')] } }).mockResolvedValueOnce({ data: { items: [row('run-3', 'running')], next_cursor: 'more' } }).mockResolvedValueOnce({ data: { items: [row('run-3', 'running'), batch('batch-1')], next_cursor: 'more' } })
  const { reader } = setup(); await settle()
  reader.loadOlder(); await settle()
  expect(reader.items.value.map(r => r.record_id)).toEqual(['run-1', 'run-2', 'run-3'])
  // The newest read replaces the live window on top: rows that slid out of it
  // disappear until scrolled to again, and a state change arrives with it.
  await reader.refresh()
  expect(reader.items.value.map(r => r.record_id)).toEqual(['run-3'])
  expect(reader.items.value[0]).toMatchObject({ state: 'running' })
  await reader.refresh()
  expect(reader.items.value.map(r => r.record_id)).toEqual(['run-3', 'batch-1'])
})
it('rebuilds the whole list on restart', async () => {
  api.list.mockResolvedValueOnce({ data: { items: [row('run-1')], next_cursor: 'more' } }).mockResolvedValueOnce({ data: { items: [row('run-2')] } }).mockResolvedValueOnce({ data: { items: [row('run-2')] } })
  const { reader } = setup(); await settle()
  reader.loadOlder(); await settle()
  expect(reader.items.value.map(r => r.record_id)).toEqual(['run-1', 'run-2'])
  await reader.restart()
  expect(reader.items.value.map(r => r.record_id)).toEqual(['run-2'])
})
it('does not publish an older account or Bot result after scope changes', async () => {
  let finish!: (value: unknown) => void
  api.list.mockImplementationOnce(() => new Promise(resolve => { finish = resolve })).mockResolvedValue({ data: { items: [row('new-account')] } })
  const { reader, identity } = setup(); identity.value = 'new'; await settle()
  finish({ data: { items: [row('old-account')] } }); await settle()
  expect(reader.items.value.map(r => r.record_id)).toEqual(['new-account'])
})
it('clears revoked results and never falls back to sessions or activity', async () => {
  api.list.mockResolvedValueOnce({ data: { items: [row('old')] } }).mockRejectedValue({ status: 403 })
  const { reader } = setup(); await settle(); await reader.refresh()
  expect(reader.items.value).toEqual([]); expect(reader.error.value).toEqual({ status: 403 })
})

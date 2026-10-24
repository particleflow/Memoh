// @vitest-environment jsdom
import { effectScope, nextTick, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SessiontaskTask } from '@memohai/sdk'
import { useNativeTask } from './useNativeTask'
import type { TaskScope } from './useSessionTasks'
const api = vi.hoisted(() => ({ read: vi.fn(), send: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdContent: api.read, postBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdFollowup: api.send }))
let owner: ReturnType<typeof effectScope>
let nextID = 0
const reply = (id = 'op-a') => ({ data: { operation_id: id, supported: true, followup_available: true, observation: { output: 'synthetic sample', output_fidelity: 'terminal_snapshot_unverified', task_success_verified: false }, followups: [] as { request_id: string; state: string }[] } })
async function settle() { await nextTick(); await vi.advanceTimersByTimeAsync(0); await nextTick() }
function setup() {
 const scope = ref<TaskScope | null>({ bot: 'bot-a', session: 'root-a', selection: 'native', identity: 'synthetic-login' })
 const selected = ref<Pick<SessiontaskTask, 'task_type' | 'task_id'> | undefined>({ task_type: 'computer_operation', task_id: 'op-a' })
 const enabled = ref(true)
 owner = effectScope()
 return { scope, enabled, view: owner.run(() => useNativeTask(scope, selected, enabled))! }
}
beforeEach(() => {
 vi.useFakeTimers(); sessionStorage.clear(); nextID = 0
 vi.stubGlobal('crypto', { subtle: { digest: vi.fn(async (_: string, bytes: Uint8Array) => bytes.buffer) }, randomUUID: vi.fn(() => `00000000-0000-4000-8000-${String(++nextID).padStart(12, '0')}`) })
 api.read.mockReset().mockResolvedValue(reply()); api.send.mockReset()
})
afterEach(() => { owner?.stop(); vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.useRealTimers() })
describe('native terminal sample and retained follow-up request', () => {
 it('reads only when explicitly opened and visible, without claiming terminal success', async () => {
  const { view, enabled } = setup(); await vi.advanceTimersByTimeAsync(10000)
  expect(api.read).not.toHaveBeenCalled()
  await view.value.open(); expect(view.value.data?.observation?.task_success_verified).toBe(false)
  await vi.advanceTimersByTimeAsync(5000); expect(api.read).toHaveBeenCalledTimes(2)
  enabled.value = false; await settle(); expect(view.value.data).toBeUndefined()
  await vi.advanceTimersByTimeAsync(10000); expect(api.read).toHaveBeenCalledTimes(2)
 })
 it('persists only UUID before one POST; unknown survives reload and blocks another ID', async () => {
  let { view } = setup(); await view.value.open()
  api.send.mockImplementation(async (input: { body: { request_id: string } }) => {
   expect(Object.values(sessionStorage)).toContain(input.body.request_id)
   expect(JSON.stringify(sessionStorage)).not.toContain('synthetic instruction')
   throw new TypeError('lost response')
  })
  await view.value.send('synthetic instruction', 'bound_session')
  const id = view.value.requestId
  expect(id).toBeTruthy(); await view.value.send('second instruction', 'current_agent'); expect(api.send).toHaveBeenCalledTimes(1)
  owner.stop(); ({ view } = setup()); await view.value.open()
  expect(view.value.requestId).toBe(id)
  await view.value.send('third instruction', 'bound_session'); expect(api.send).toHaveBeenCalledTimes(1)
  const read = reply(); read.data.followups = [{ request_id: id!, state: 'accepted' }]
  api.read.mockResolvedValue(read); await view.value.refresh()
  expect(view.value.requestId).toBeUndefined(); expect(view.value.response?.state).toBe('accepted')
 })
 it('does not send if UUID cannot be persisted or batch capability is absent', async () => {
  const { view } = setup(); await view.value.open()
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('storage full') })
  await view.value.send('synthetic', 'bound_session'); expect(api.send).not.toHaveBeenCalled()
  api.read.mockResolvedValue({ data: { operation_id: 'op-a', supported: false, followup_available: false } }); await view.value.refresh()
  await view.value.send('synthetic', 'current_agent'); expect(api.send).not.toHaveBeenCalled()
 })
 it('keeps read-only content available when browser storage is unavailable', async () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('storage denied') })
  const { view } = setup(); await view.value.open()
  expect(view.value.data?.observation?.output).toBe('synthetic sample')
  await view.value.send('synthetic', 'bound_session'); expect(api.send).not.toHaveBeenCalled()
 })
 it.each([401, 403, 404])('clears content after %s, stops auto polling, and allows explicit refresh', async status => {
  const { view } = setup(); await view.value.open()
  api.read.mockRejectedValue({ status }); await vi.advanceTimersByTimeAsync(5000)
  expect(view.value.data).toBeUndefined()
  await vi.advanceTimersByTimeAsync(10000); expect(api.read).toHaveBeenCalledTimes(2)
  api.read.mockResolvedValue(reply()); await view.value.refresh(); expect(view.value.data).toBeTruthy()
 })
 it('recovers a disconnected read and retires a late response after scope changes', async () => {
  const { view, scope } = setup(); await view.value.open()
  api.read.mockRejectedValueOnce(new TypeError('offline')); await vi.advanceTimersByTimeAsync(5000)
  expect(view.value.data).toBeUndefined(); await vi.advanceTimersByTimeAsync(5000); expect(view.value.data).toBeTruthy()
  let resolve!: (value: ReturnType<typeof reply>) => void
  api.read.mockReturnValueOnce(new Promise<ReturnType<typeof reply>>(yes => { resolve = yes }))
  const pending = view.value.refresh(); await settle()
  scope.value = { bot: 'bot-b', session: 'root-b', selection: 'native', identity: 'new-user' }; await settle()
  resolve(reply()); await pending; expect(view.value.data).toBeUndefined(); expect(view.value.opened).toBe(false)
 })
})

// @vitest-environment jsdom
import { createApp, h, nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { SessiontaskTask } from '@memohai/sdk'
import en from '@/i18n/locales/en.json'
import { useSessionTasks, type TaskScope } from '../composables/useSessionTasks'
import SessionTaskResults from './session-task-results.vue'

const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn(), observation: vi.fn(), deliveries: vi.fn(), content: vi.fn(), send: vi.fn() }))
vi.mock('@/store/chat-list', () => ({ useChatStore: () => ({ currentBotId: 'synthetic-bot' }) }))
vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdSessionsBySessionIdTasks: api.list,
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskId: api.detail,
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdObservation: api.observation,
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdDeliveries: api.deliveries,
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdContent: api.content,
  postBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdFollowup: api.send,
}))
const task: SessiontaskTask = { task_type: 'computer_operation', task_id: 'original-operation', session_id: 'child', parent_session_id: 'root', direct_parent_session_id: 'root', created_at: '', updated_at: '', status: 'unknown', summary: 'Persisted receipt', content_available: true, attachments_available: false, result_summary: { text: 'Persisted final text', source: 'pi_final_assistant', truncated: false } }
const observation = (availability = 'online') => ({ data: { operation_id: task.task_id, native: { availability, agent_status: 'working' } } })
let app: ReturnType<typeof createApp>, root: HTMLDivElement, reader: ReturnType<typeof useSessionTasks>
async function settle() { await nextTick(); await vi.advanceTimersByTimeAsync(0); await nextTick() }
async function mount() {
  const scope = ref<TaskScope | null>({ bot: 'synthetic-bot', session: 'root', selection: 'native', identity: 'synthetic-login' })
  const visible = ref(true), paused = ref(false)
  root = document.createElement('div'); document.body.append(root)
  app = createApp({ setup() {
    reader = useSessionTasks(scope, visible, paused)
    return () => h(SessionTaskResults, { items: reader.items.value, selected: reader.selected.value, detail: reader.detail.value, loading: reader.loading.value, observation: reader.observation.value, deliveries: reader.deliveries.value, nativeTask: reader.nativeTask.value })
  } })
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en } })); app.mount(root)
  await settle(); reader.openTask(task); await settle()
  return { scope, visible, paused }
}
function clickObservation() {
  const panel = root.querySelector('section[aria-label="Current computer observation"]')!
  const button = panel.querySelector('button')!
  button.click()
}
beforeEach(() => {
  vi.useFakeTimers(); sessionStorage.clear()
  api.list.mockReset().mockResolvedValue({ data: { items: [task] } }); api.detail.mockReset().mockResolvedValue({ data: task })
  api.observation.mockReset().mockResolvedValue(observation())
  api.deliveries.mockReset().mockResolvedValue({ data: { version: 1, operation_id: task.task_id, files: [], has_more: false } })
  api.content.mockReset(); api.send.mockReset()
})
afterEach(() => { app?.unmount(); root?.remove(); vi.useRealTimers() })
describe('shared task summary and explicit live observation', () => {
  it('reads persisted summary, relationships and attachments without any native call', async () => {
    await mount(); await vi.advanceTimersByTimeAsync(15000)
    expect(root.textContent).toContain('Persisted final text')
    expect(root.textContent).toContain('Session’s direct parentroot')
    expect(api.deliveries).toHaveBeenCalled()
    expect(api.observation).not.toHaveBeenCalled(); expect(api.content).not.toHaveBeenCalled(); expect(api.send).not.toHaveBeenCalled()
  })
  it('starts visible polling only from the existing button and reconnects with GET for the same task', async () => {
    await mount(); clickObservation(); await settle()
    expect(api.observation).toHaveBeenCalledTimes(1)
    api.observation.mockRejectedValueOnce(new TypeError('offline'))
    await vi.advanceTimersByTimeAsync(5000); expect(reader.observation.value.data).toBeUndefined()
    await vi.advanceTimersByTimeAsync(5000); expect(reader.observation.value.data?.native?.availability).toBe('online')
    expect(api.observation).toHaveBeenCalledTimes(3)
    expect(api.observation.mock.calls.every(([input]) => input.path.task_id === task.task_id && input.path.session_id === 'root')).toBe(true)
    expect(api.content).not.toHaveBeenCalled(); expect(api.send).not.toHaveBeenCalled()
  })
  it.each(['authentication_unavailable', 401, 403, 404] as const)('pauses automatic reads after %s including parent revalidation', async failure => {
    const { paused } = await mount()
    if (typeof failure === 'string') api.observation.mockResolvedValue(observation(failure))
    else api.observation.mockRejectedValue({ status: failure })
    clickObservation(); await settle()
    expect(api.observation).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(20000)
    paused.value = true; await settle(); paused.value = false; await settle()
    await vi.advanceTimersByTimeAsync(10000)
    expect(api.observation).toHaveBeenCalledTimes(1)
    expect(root.textContent).toContain('Persisted final text')
    api.observation.mockResolvedValue(observation()); clickObservation(); await settle()
    expect(api.observation).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(5000); expect(api.observation).toHaveBeenCalledTimes(3)
    expect(api.send).not.toHaveBeenCalled()
  })
  it('does not carry observation opt-in across task or identity changes', async () => {
    const { scope } = await mount(); clickObservation(); await settle()
    scope.value = { ...scope.value!, identity: 'new-login' }; await settle()
    reader.openTask(task); await settle(); await vi.advanceTimersByTimeAsync(10000)
    expect(api.observation).toHaveBeenCalledTimes(1)
    expect(reader.observation.value.data).toBeUndefined()
  })
})

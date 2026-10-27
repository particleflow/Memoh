// @vitest-environment jsdom
import { effectScope, nextTick, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ToolCallBlock } from '@/store/chat-list'
import { notifyAuthSessionCleared } from '@/lib/auth-session'
import en from '@/i18n/locales/en.json'
import { originalNativeTaskResume, useNativeTaskResume, type NativeTaskResumeScope } from './useNativeTaskResume'

const api = vi.hoisted(() => ({ send: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ postBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskIdResume: api.send }))
const owners: ReturnType<typeof effectScope>[] = []
const args = { kind: 'herdr_pi_task', task: '  original synthetic instruction\n', target_id: 'target-a', work_dir: 'C:\\synthetic', artifact: { source_ref: 'one.txt', mime: 'text/plain' }, artifacts: [{ source_ref: 'two.txt' }] }
function tool(): ToolCallBlock {
  return { id: 1, type: 'tool', name: 'computer_task_submit', tool_call_id: 'call-a', running: false, toolCallId: 'call-a', toolName: 'computer_task_submit', done: true, input: structuredClone(args), result: { structuredContent: { operation_id: 'op-a', kind: 'herdr_pi_task', state: 'unknown', phase: 'batch_workspace_adopted', target_id: 'target-a' } } }
}
const reply = (outcome = 'submitted', operation = 'op-a') => ({ data: { operation_id: operation, outcome, automatic_retry: false, business_outcome: 'unknown' } })
async function settle() { await nextTick(); await vi.advanceTimersByTimeAsync(0); await nextTick() }
function setup(blockValue = tool()) {
  const scope = ref<NativeTaskResumeScope | null>({ bot: 'bot-a', session: 'session-a', identity: 'synthetic-user-a' })
  const block = ref(blockValue), token = ref('synthetic-token-a'), owner = effectScope()
  owners.push(owner)
  return { scope, block, token, owner, view: owner.run(() => useNativeTaskResume(scope, block, token))! }
}
beforeEach(() => {
  vi.useFakeTimers(); sessionStorage.clear(); api.send.mockReset().mockResolvedValue(reply())
  vi.stubGlobal('crypto', { subtle: { digest: vi.fn(async (_: string, bytes: Uint8Array) => bytes.buffer) } })
})
afterEach(() => { owners.splice(0).forEach(owner => owner.stop()); vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.useRealTimers() })

describe('explicit original native batch continuation', () => {
  it('preserves original input hashes through the existing resume request', async () => {
    const inputs = ['a'.repeat(64), 'b'.repeat(64)]
    const block = tool(); block.input = { ...args, inputs }
    const { view } = setup(block); await settle(); await view.send()
    expect(api.send).toHaveBeenCalledTimes(1)
    expect(api.send.mock.calls[0]![0].body).toEqual({ ...args, inputs })
    for (const invalid of ['hash', [7], ['not-a-hash']]) {
      block.input = { ...args, inputs: invalid }
      expect(originalNativeTaskResume(block)).toBeUndefined()
    }
  })
  it('sends nothing on mount or elapsed time, then uses the exact original scope, operation and arguments once', async () => {
    const { view } = setup(); await settle(); await vi.advanceTimersByTimeAsync(120000)
    expect(api.send).not.toHaveBeenCalled(); expect(view.canSend.value).toBe(true)
    api.send.mockImplementation(async () => {
      expect(Object.values(sessionStorage)).toEqual(['unknown'])
      expect(JSON.stringify(sessionStorage)).not.toMatch(/synthetic-user|synthetic-token|instruction|one.txt/)
      return reply()
    })
    await Promise.all([view.send(), view.send()])
    expect(api.send).toHaveBeenCalledTimes(1)
    expect(api.send).toHaveBeenCalledWith(expect.objectContaining({
      path: { bot_id: 'bot-a', session_id: 'session-a', task_type: 'computer_operation', task_id: 'op-a' },
      body: args, cache: 'no-store', throwOnError: true, signal: expect.any(AbortSignal),
    }))
    expect(Object.keys(api.send.mock.calls[0]![0].body)).toEqual(Object.keys(args))
    expect(view.status.value).toBe('submitted'); expect(view.canSend.value).toBe(false)
    expect(en.chat.tasks.resume.submitted).toContain('Task success is still unknown')
  })
  it.each(['unknown', 'submitted', 'unchanged', 'lost'])('retains %s through reload and never replays or creates a new identity', async outcome => {
    const first = setup(); await settle()
    if (outcome === 'lost') api.send.mockRejectedValueOnce(new TypeError('private transport detail'))
    else api.send.mockResolvedValueOnce(reply(outcome))
    await first.view.send(); const marker = JSON.stringify(sessionStorage)
    first.owner.stop(); const second = setup(); await settle()
    await second.view.send(); await vi.advanceTimersByTimeAsync(120000)
    expect(api.send).toHaveBeenCalledTimes(1); expect(JSON.stringify(sessionStorage)).toBe(marker)
    expect(second.view.status.value).toBe(outcome === 'lost' ? 'unknown' : outcome)
  })
  it('coordinates duplicate views of the same operation without another POST', async () => {
    const first = setup(), second = setup(); await settle()
    await Promise.all([first.view.send(), second.view.send()])
    expect(api.send).toHaveBeenCalledTimes(1); expect(second.view.canSend.value).toBe(false)
  })
  it.each(['unavailable', 'blocked'])('shows %s and permits only an explicit recheck', async outcome => {
    const { view } = setup(); await settle(); api.send.mockResolvedValue(reply(outcome))
    await view.send(); expect(view.status.value).toBe(outcome); expect(view.canSend.value).toBe(true)
    await vi.advanceTimersByTimeAsync(120000); expect(api.send).toHaveBeenCalledTimes(1)
    await view.send(); expect(api.send).toHaveBeenCalledTimes(2)
  })
  it.each([[401, 'unauthorized'], [403, 'forbidden'], [404, 'unsupported'], [400, 'blocked'], [409, 'blocked']] as const)('renders HTTP %s safely with no automatic retries', async (code, expected) => {
    const { view } = setup(); await settle(); api.send.mockRejectedValue({ status: code, message: 'SECRET native output' })
    await view.send(); expect(view.status.value).toBe(expected)
    expect(view.canSend.value).toBe(code !== 404)
    await vi.advanceTimersByTimeAsync(120000); expect(api.send).toHaveBeenCalledTimes(1)
  })
  it('keeps server failures and mismatched response receipts uncertain', async () => {
    const first = setup(); await settle(); api.send.mockResolvedValueOnce(reply('submitted', 'other-operation'))
    await first.view.send(); expect(first.view.status.value).toBe('unknown'); expect(first.view.canSend.value).toBe(false)
    sessionStorage.clear(); const second = setup(); await settle(); api.send.mockRejectedValueOnce({ status: 500 })
    await second.view.send(); expect(second.view.status.value).toBe('unknown'); expect(Object.values(sessionStorage)).toEqual(['unknown'])
  })
  it('aborts and discards a late response after scope A → B → A', async () => {
    const { view, scope } = setup(); await settle()
    let resolve!: (value: ReturnType<typeof reply>) => void
    api.send.mockReturnValueOnce(new Promise(yes => { resolve = yes }))
    const pending = view.send(); await settle(); const signal = api.send.mock.calls[0]![0].signal
    scope.value = { bot: 'bot-b', session: 'session-b', identity: 'synthetic-user-a' }; await settle()
    scope.value = { bot: 'bot-a', session: 'session-a', identity: 'synthetic-user-a' }; await settle()
    expect(signal.aborted).toBe(true); resolve(reply()); await pending
    expect(view.status.value).toBe('unknown'); expect(view.canSend.value).toBe(false)
  })
  it.each(['token', 'login'] as const)('discards a late result after %s changes', async change => {
    const { view, token } = setup(); await settle()
    let resolve!: (value: ReturnType<typeof reply>) => void
    api.send.mockReturnValueOnce(new Promise(yes => { resolve = yes }))
    const pending = view.send(); await settle()
    if (change === 'token') { token.value = 'synthetic-token-b'; await settle(); token.value = 'synthetic-token-a'; await settle() }
    else notifyAuthSessionCleared('login')
    resolve(reply()); await pending
    expect(view.status.value).not.toBe('submitted'); expect(view.canSend.value).toBe(false)
  })
  it('cannot POST when request tracking cannot be persisted', async () => {
    const { view } = setup(); await settle()
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('storage full') })
    await view.send(); expect(api.send).not.toHaveBeenCalled(); expect(view.status.value).toBe('storageUnavailable')
  })
  it('revalidates a same-token login without losing an uncertain operation marker', async () => {
    const { view } = setup(); await settle()
    notifyAuthSessionCleared('login'); await settle()
    expect(view.canSend.value).toBe(true)
    api.send.mockRejectedValueOnce(new TypeError('lost response'))
    await view.send(); expect(view.status.value).toBe('unknown')
    notifyAuthSessionCleared('login'); await settle(); await view.send()
    expect(view.status.value).toBe('unknown'); expect(api.send).toHaveBeenCalledTimes(1)
  })
  it('does not reset or abort an in-flight request when unrelated tool display data changes', async () => {
    const { view, block } = setup(); await settle()
    let resolve!: (value: ReturnType<typeof reply>) => void
    api.send.mockReturnValueOnce(new Promise(yes => { resolve = yes }))
    const pending = view.send(); await settle()
    block.value.result = { ...(block.value.result as Record<string, unknown>), content: [{ type: 'text', text: 'same-operation display update' }] }
    await settle()
    expect(api.send.mock.calls[0]![0].signal.aborted).toBe(false)
    resolve(reply()); await pending; expect(view.status.value).toBe('submitted')
  })
  it.each([
    (block: ToolCallBlock) => { block.input = null },
    (block: ToolCallBlock) => { block.input = JSON.stringify(args) },
    (block: ToolCallBlock) => { block.input = { ...args, resume_operation_id: 'op-a' } },
    (block: ToolCallBlock) => { block.input = { ...args, task: undefined } },
    (block: ToolCallBlock) => { block.input = { ...args, target_id: 'another-target' } },
    (block: ToolCallBlock) => { block.input = { ...args, artifacts: [null] } },
    (block: ToolCallBlock) => { block.result = null },
    (block: ToolCallBlock) => { block.result = { content: [{ type: 'text', text: JSON.stringify({ operation_id: 'op-a' }) }] } },
    (block: ToolCallBlock) => { block.result = { ...args, operation_id: 'op-a', state: 'unknown', phase: 'batch_prompt_dispatching' } },
    (block: ToolCallBlock) => { block.input = { ...args, kind: 'legacy' } },
  ])('keeps missing, ambiguous, legacy or ineligible originals read only (%#)', async mutate => {
    const block = tool(); mutate(block)
    expect(originalNativeTaskResume(block)).toBeUndefined()
    const { view } = setup(block); await settle(); await view.send()
    expect(view.status.value).toBe('unavailable'); expect(api.send).not.toHaveBeenCalled()
  })
  it('accepts all eligible pre-prompt phases and the normalized MCP receipt wrappers', () => {
    for (const phase of ['batch_creating_workspace', 'batch_workspace_adopted', 'batch_tab_adopted']) {
      const block = tool(); block.result = { structured_content: { operation_id: 'op-a', target_id: 'target-a', kind: 'herdr_pi_task', state: 'unknown', phase } }
      expect(originalNativeTaskResume(block)?.body).toEqual(args)
    }
  })
})

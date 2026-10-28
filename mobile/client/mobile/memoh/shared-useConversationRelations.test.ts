// @vitest-environment jsdom
import { effectScope, nextTick, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useConversationRelations } from './useConversationRelations'
import type { TaskScope } from './useSessionTasks'

const api = vi.hoisted(() => ({ session: vi.fn(), tasks: vi.fn(), delegations: vi.fn() }))
vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdSessionsBySessionId: api.session,
  getBotsByBotIdSessionsBySessionIdTasks: api.tasks,
  getBotsByBotIdSessionsBySessionIdDelegations: api.delegations,
}))
const scopes: ReturnType<typeof effectScope>[] = []
const sessions = new Map<string, { id: string, bot_id: string, title: string, parent_session_id?: string, type?: string, runtime_type?: string }>()
function add(bot: string, id: string, parent?: string, title = id, descriptor: { type?: string, runtime_type?: string } = {}) { sessions.set(`${bot}/${id}`, { bot_id: bot, id, title, parent_session_id: parent, ...descriptor }) }
const task = (id: string, parent: string, state = 'running') => ({ task_id: id, task_type: 'subagent', session_id: id, parent_session_id: 'root', direct_parent_session_id: parent, status: state, summary: 'DO_NOT_SHOW_PROMPT' })
const computer = (id: string, owner = 'root', status = 'submitted') => ({ task_id: id, task_type: 'computer_operation', session_id: owner, parent_session_id: 'root', status, summary: 'DO_NOT_SHOW_PROMPT' })
const link = (source = 'root', bot = 'expert', target = 'expert-session') => ({ source_session_id: source, target_bot_id: bot, target_session_id: target, state: 'running', target_run_state: 'waiting_decision', delegation_id: 'delegation' })
async function settle() { for (let i = 0; i < 60; i++) await Promise.resolve(); await nextTick() }
function mount() {
  const scope = ref<TaskScope | null>({ bot: 'bot', session: 'root', identity: 'account', selection: '' }), visible = ref(true)
  const owner = effectScope(); scopes.push(owner)
  const reader = owner.run(() => useConversationRelations(scope, visible, id => ({ bot: 'Supervisor', expert: 'Expert', unrelated: 'Unrelated Bot' })[id] || '', () => 'completed'))!
  return { reader, scope, visible }
}
beforeEach(() => {
  vi.useFakeTimers(); sessions.clear(); add('bot', 'root')
  api.session.mockReset().mockImplementation(({ path }) => {
    const value = sessions.get(`${path.bot_id}/${path.session_id}`)
    return value ? Promise.resolve({ data: value }) : Promise.reject({ status: 404 })
  })
  api.tasks.mockReset().mockResolvedValue({ data: { items: [] } })
  api.delegations.mockReset().mockResolvedValue({ data: { items: [] } })
})
afterEach(() => { for (const scope of scopes.splice(0)) scope.stop(); vi.useRealTimers() })

describe('conversation-owned relations', () => {
  it('retains computer-only current and historical tasks without claiming their Bot owner is an Agent', async () => {
    api.tasks.mockResolvedValue({ data: { items: [computer('pending'), computer('old', 'root', 'completed'), computer('uncertain', 'root', 'unknown')] } })
    const { reader } = mount(); await settle()
    expect(reader.items.value.filter(row => row.kind === 'task').map(row => [row.key, row.state, row.task])).toEqual([
      ['task:bot:pending', 'submitted', { id: 'pending', root: 'root', agentSession: undefined }],
      ['task:bot:old', 'completed', { id: 'old', root: 'root', agentSession: undefined }],
      ['task:bot:uncertain', 'unknown', { id: 'uncertain', root: 'root', agentSession: undefined }],
    ])
    expect(reader.items.value.filter(row => row.kind === 'agent')).toEqual([])
    expect(JSON.stringify(reader.items.value)).not.toContain('DO_NOT_SHOW_PROMPT')
  })
  it.each([
    { type: 'subagent' }, { type: 'acp_agent' },
    { type: 'chat', runtime_type: 'codex' }, { type: 'chat', runtime_type: 'claude-code' },
    { type: 'chat', runtime_type: 'grok-build' },
  ])('uses the persisted owning Agent session $type/$runtime_type without a separate subagent row', async descriptor => {
    const owner = descriptor.type === 'subagent' ? 'owner' : 'root'
    add('bot', owner, owner === 'root' ? undefined : 'root', 'Actual Agent', descriptor)
    api.tasks.mockResolvedValue({ data: { items: [computer('pc-task', owner)] } })
    const { reader } = mount(); await settle()
    expect(reader.items.value.find(row => row.kind === 'task')).toMatchObject({
      session: owner, name: 'Actual Agent', state: 'submitted', parent: 'session:bot:root',
      task: { id: 'pc-task', root: 'root', agentSession: owner },
    })
  })
  it('preserves task nesting independently of receipt/subagent list order', async () => {
    add('bot', 'a', 'root', 'Actual Agent', { type: 'subagent' })
    api.tasks.mockResolvedValue({ data: { items: [computer('pc-task', 'a'), task('a', 'root')] } })
    const { reader } = mount(); await settle()
    expect(reader.items.value.find(row => row.kind === 'task')?.parent).toBe('session:bot:a')
  })
  it('retains missing and mismatched old owner receipts without borrowing another Agent destination', async () => {
    add('bot', 'a', 'root', 'Other Agent', { type: 'subagent' }); add('bot', 'moved', 'elsewhere', 'Moved Agent', { type: 'subagent' })
    api.tasks.mockResolvedValue({ data: { items: [task('a', 'root'), computer('missing', 'gone'), { ...computer('moved-task', 'moved'), direct_parent_session_id: 'root' }, computer('no-owner', '')] } })
    const { reader } = mount(); await settle()
    const receipts = reader.items.value.filter(row => row.kind === 'task')
    expect(receipts).toHaveLength(3)
    expect(receipts.every(row => row.task?.agentSession === undefined && row.name === '')).toBe(true)
    expect(receipts.every(row => row.parent === 'session:bot:root')).toBe(true)
  })
  it('keeps the current associated Bot without turning an inactive run into an active Agent', async () => {
    const { reader } = mount(); await settle()
    expect(reader.items.value.filter(row => row.kind === 'agent')).toEqual([])
    expect(reader.bots.value.map(row => [row.bot, row.state])).toEqual([['bot', 'completed']])
  })
  it('keeps a real three-level chain and distinct same-name Agents without exposing task text', async () => {
    add('bot', 'a', 'root', 'Review'); add('bot', 'b', 'a', 'Review'); add('bot', 'old', 'root')
    api.tasks.mockResolvedValue({ data: { items: [task('a', 'root'), task('b', 'a'), task('old', 'root', 'aborted')] } })
    const { reader } = mount(); await settle()
    expect(reader.items.value.map(row => [row.session, row.state, row.parent])).toEqual([
      ['root', 'completed', undefined], ['a', 'running', 'session:bot:root'], ['b', 'running', 'session:bot:a'], ['old', 'aborted', 'session:bot:root'],
    ])
    expect(reader.items.value.filter(row => row.name === 'Review')).toHaveLength(2)
    expect(JSON.stringify(reader.items.value)).not.toContain('DO_NOT_SHOW_PROMPT')
  })
  it('reads only linked Bot conversations and their child Agents, not all accessible Bots', async () => {
    add('expert', 'expert-session'); add('expert', 'expert-child', 'expert-session')
    api.delegations.mockImplementation(({ path }) => Promise.resolve({ data: { items: path.bot_id === 'bot' && path.session_id === 'root' ? [link()] : [] } }))
    api.tasks.mockImplementation(({ path }) => Promise.resolve({ data: { items: path.bot_id === 'expert' ? [{ ...task('expert-child', 'expert-session'), parent_session_id: 'expert-session' }] : [] } }))
    const { reader } = mount(); await settle()
    expect(reader.items.value.map(row => row.session)).toEqual(['root', 'expert-session', 'expert-child'])
    expect(reader.items.value.find(row => row.bot === 'expert')?.state).toBe('waiting_decision')
    expect(api.session.mock.calls.every(([args]) => args.path.bot_id !== 'unrelated')).toBe(true)
  })
  it('keeps unknown as unknown; rejects orphan, mismatched and foreign ancestry', async () => {
    add('bot', 'uncertain', 'root'); add('bot', 'orphan', 'missing'); add('bot', 'mismatch', 'other'); add('bot', 'foreign', 'root')
    api.tasks.mockResolvedValue({ data: { items: [task('uncertain', 'root', 'unknown'), task('orphan', 'missing'), task('mismatch', 'root'), { ...task('foreign', 'root'), parent_session_id: 'foreign-root' }] } })
    const { reader } = mount(); await settle()
    expect(reader.items.value.map(row => row.session)).toEqual(['root', 'uncertain'])
    expect(reader.items.value[1]?.state).toBe('unknown')
    expect(api.session.mock.calls.some(([args]) => args.path.session_id === 'foreign')).toBe(false)
  })
  it('consumes original cursors and deduplicates the same persisted node', async () => {
    add('bot', 'a', 'root'); add('bot', 'b', 'a')
    api.tasks.mockImplementation(({ query }) => Promise.resolve({ data: query.cursor ? { items: [task('a', 'root'), task('b', 'a')] } : { items: [task('a', 'root')], next_cursor: 'next' } }))
    const { reader } = mount(); await settle()
    expect(reader.items.value.map(row => row.session)).toEqual(['root', 'a', 'b'])
    expect(api.tasks.mock.calls.map(([args]) => args.query.cursor)).toEqual([undefined, 'next'])
  })
  it('does not turn a repeated cursor into a successful empty read', async () => {
    api.tasks.mockResolvedValue({ data: { items: [], next_cursor: 'same' } })
    const { reader } = mount(); await settle()
    expect(reader.error.value).toBeTruthy(); expect(reader.items.value).toEqual([])
  })
  it('clears revoked root data and pauses reads until the view is normally reopened', async () => {
    add('bot', 'a', 'root'); api.tasks.mockResolvedValue({ data: { items: [task('a', 'root')] } })
    const { reader, visible } = mount(); await settle(); expect(reader.items.value).toHaveLength(2)
    api.tasks.mockRejectedValue({ status: 403 }); await vi.advanceTimersByTimeAsync(5000); await settle()
    expect(reader.items.value).toEqual([]); const calls = api.tasks.mock.calls.length
    await vi.advanceTimersByTimeAsync(10000); expect(api.tasks).toHaveBeenCalledTimes(calls)
    api.tasks.mockResolvedValue({ data: { items: [task('a', 'root')] } })
    visible.value = false; await settle(); visible.value = true; await settle()
    expect(reader.items.value.map(row => row.session)).toEqual(['root', 'a'])
  })
  it('reconnects by GET with the same IDs, without creating a replacement task', async () => {
    add('bot', 'a', 'root'); api.tasks.mockRejectedValueOnce({ status: 503 }).mockResolvedValue({ data: { items: [task('a', 'root')] } })
    const { reader } = mount(); await settle(); expect(reader.error.value).toBeTruthy()
    await vi.advanceTimersByTimeAsync(5000); await settle()
    expect(reader.items.value.map(row => row.session)).toEqual(['root', 'a'])
    expect(api.tasks.mock.calls.every(([args]) => args.path.bot_id === 'bot' && args.path.session_id === 'root')).toBe(true)
  })
  it('ignores a late old-session response after switching conversations', async () => {
    let resolve!: (value: unknown) => void
    api.tasks.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const { reader, scope } = mount(); await settle(); add('bot', 'other')
    scope.value = { bot: 'bot', session: 'other', identity: 'account', selection: '' }; await settle()
    resolve({ data: { items: [task('late', 'root')] } }); await settle()
    expect(reader.items.value.map(row => row.session)).toEqual(['other'])
  })
})

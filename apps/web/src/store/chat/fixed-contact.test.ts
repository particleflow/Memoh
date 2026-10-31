import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { createChatCommands, type ChatCommandDeps } from './commands'
import { createChatTargets } from './targets'

vi.mock('@/composables/api/useChat', () => ({ executeQuickAction: vi.fn() }))
describe('Fixed contact commands and history', () => {
  it.each(['/new', '/new chat', '/new discuss codex', '/reset', '/clear', '/NEW'])('refuses %s without creating or staging', async text => {
    const stage = vi.fn()
    const deps = { requestDraftView: stage, beginDraftCommand: stage, commandErrorMessage: (code: string) => code } as unknown as ChatCommandDeps
    const result = await createChatCommands(deps).handleWebNewCommand(text, undefined, { viewId: 'view', botId: 'bot', sessionId: 'fixed' })
    expect(result.kind).toBe('error'); expect(stage).not.toHaveBeenCalled()
  })
  it('keeps ordinary words out of the reset command guard', async () => {
    const result = await createChatCommands({} as ChatCommandDeps).handleWebNewCommand('new task please', undefined, { viewId: 'view', botId: 'bot', sessionId: 'fixed' })
    expect(result.kind).toBe('none')
  })
  it('allows the resolved contact while historical execution views remain read-only', () => {
    const target = { viewId: 'view', botId: 'bot', sessionId: 'fixed' }
    const targets = createChatTargets({ currentBotId: ref('bot'), focusedViewId: ref('view'), explicitSessionSelection: ref(false), normalizeTarget: t => ({ ...target, ...t }), knownSession: id => ({ id, bot_id: 'bot', title: '', type: 'chat', channel_type: 'local' }), contactSessionFor: () => 'fixed', pendingExternalAgentState: () => null })
    expect(targets.readOnlyFor(target)).toBe(false)
    expect(targets.readOnlyFor({ ...target, sessionId: 'old-75' })).toBe(true)
    expect(targets.canForkFor(target)).toBe(false)
  })
})

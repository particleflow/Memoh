// @vitest-environment jsdom
import { beforeEach, expect, it } from 'vitest'
import { readPendingBotSession, writePendingBotSession } from './pending-bot-session'

beforeEach(() => sessionStorage.clear())

it('scopes a pending request and never saves Agent metadata', () => {
  expect(writePendingBotSession('server-a/user-a', 'key-1', { name: 'cat' }, {
    agent: { name: 'Codex', provider: 'codex', metadata: { provider: 'codex', auth: 'api_key' }, authorizationId: 'auth-1' },
  })).toBe(true)
  const raw = sessionStorage.getItem(sessionStorage.key(0)!)!
  expect(raw).not.toContain('api_key')
  expect(readPendingBotSession('server-b/user-a')).toBeNull()
  expect(readPendingBotSession('server-a/user-b')).toBeNull()
  expect(readPendingBotSession('server-a/user-a')?.requestKey).toBe('key-1')
})

it('does not persist arbitrary request metadata', () => {
  expect(writePendingBotSession('scope', 'key', { metadata: { api_key: 'secret' } }, {})).toBe(false)
  expect(sessionStorage.length).toBe(0)
})

// @vitest-environment jsdom
import { expect, it } from 'vitest'
import { clearPersistedUserScopedState } from './auth-session'
it('clears only creation/account state and preserves the existing local service binding', () => {
  const key = 'memoh:harmony:service-config:v1'
  const binding = JSON.stringify({ version: 1, apiOrigin: 'https://example.test/api', hapDownloadUrl: '', resetPending: false })
  localStorage.setItem(key, binding)
  sessionStorage.setItem('memoh:new-bot:pending:scope-a', 'synthetic pending')
  sessionStorage.setItem('memoh:onboarding:pending:scope-b', 'synthetic pending')
  sessionStorage.setItem('memoh:new-bot:authorization', 'synthetic target')
  sessionStorage.setItem('unrelated', 'preserved')
  clearPersistedUserScopedState()
  expect(sessionStorage.getItem('memoh:new-bot:pending:scope-a')).toBeNull()
  expect(sessionStorage.getItem('memoh:onboarding:pending:scope-b')).toBeNull()
  expect(sessionStorage.getItem('memoh:new-bot:authorization')).toBeNull()
  expect(localStorage.getItem(key)).toBe(binding)
  expect(sessionStorage.getItem('unrelated')).toBe('preserved')
})

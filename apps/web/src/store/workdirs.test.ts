// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useWorkdirsStore } from './workdirs'

const api = vi.hoisted(() => ({ fetch: vi.fn() }))
vi.mock('@/composables/api/useWorkdirs', () => ({ fetchWorkdirs: api.fetch }))

describe('workdir reads without workspace permission', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
    api.fetch.mockReset()
  })

  it('handles one forbidden read shared by three background consumers and retries after a grant', async () => {
    const store = useWorkdirsStore()
    const denied = { code: 'http.forbidden', status: 403 }
    api.fetch.mockRejectedValueOnce(denied)
    const results = await Promise.allSettled([
      store.ensureWorkdirs('bot'), store.ensureWorkdirs('bot'), store.ensureWorkdirs('bot'),
    ])
    expect(results.map(result => result.status)).toEqual(['fulfilled', 'fulfilled', 'fulfilled'])
    expect(api.fetch).toHaveBeenCalledTimes(1)
    expect(store.workdirsFor('bot')).toEqual([])

    api.fetch.mockResolvedValueOnce([{ id: 'existing', bot_id: 'bot', name: 'Existing folder' }])
    await store.ensureWorkdirs('bot')
    expect(api.fetch).toHaveBeenCalledTimes(2)
    expect(store.workdirById('bot', 'existing')?.name).toBe('Existing folder')
    await store.ensureWorkdirs('bot')
    expect(api.fetch).toHaveBeenCalledTimes(2)
  })

  it('removes previously read paths on denial without deleting the saved workdir choice or another Bot', async () => {
    const store = useWorkdirsStore()
    api.fetch.mockResolvedValueOnce([{ id: 'original', path: '/original', bot_id: 'bot' }])
      .mockResolvedValueOnce([{ id: 'other-folder', path: '/other', bot_id: 'other' }])
    await store.ensureWorkdirs('bot')
    await store.ensureWorkdirs('other')
    store.setWorkingWorkdir('bot', 'original')
    api.fetch.mockRejectedValueOnce({ code: 'http.forbidden', status: 403 })
    await store.refreshWorkdirs('bot')
    expect(store.workdirsFor('bot')).toEqual([])
    expect(store.workingWorkdirId('bot')).toBe('original')
    expect(store.workdirsFor('other')).toHaveLength(1)
    api.fetch.mockResolvedValueOnce([{ id: 'original', path: '/original', bot_id: 'bot' }])
    await store.ensureWorkdirs('bot')
    expect(store.workingWorkdirFor('bot')?.id).toBe('original')
  })

  it.each([
    new TypeError('Network unavailable'),
    { code: 'internal', status: 500 },
    { code: 'http.unauthorized', status: 401 },
    { code: 'internal', status: 403 },
  ])('preserves unexpected failures and leaves the read retryable: %j', async cause => {
    const store = useWorkdirsStore()
    api.fetch.mockRejectedValueOnce(cause).mockResolvedValueOnce([])
    await expect(store.ensureWorkdirs('bot')).rejects.toBe(cause)
    await store.ensureWorkdirs('bot')
    expect(api.fetch).toHaveBeenCalledTimes(2)
  })
})

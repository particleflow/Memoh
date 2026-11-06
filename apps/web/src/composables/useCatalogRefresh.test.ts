import { effectScope, nextTick, ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { CATALOG_REFRESH_INTERVAL_MS, useCatalogRefresh } from './useCatalogRefresh'

const invalidateQueries = vi.hoisted(() => vi.fn().mockResolvedValue(undefined))
vi.mock('@pinia/colada', () => ({ useQueryCache: () => ({ invalidateQueries }) }))

afterEach(() => { vi.useRealTimers(); invalidateQueries.mockClear(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('useCatalogRefresh', () => {
  it('refreshes local catalog keys only while active and visible, and stops on disposal', async () => {
    vi.useFakeTimers()
    const fakeDocument = { visibilityState: 'visible' }
    vi.stubGlobal('document', fakeDocument)
    const active = ref(false)
    const scope = effectScope()
    scope.run(() => useCatalogRefresh(active))
    vi.advanceTimersByTime(CATALOG_REFRESH_INTERVAL_MS)
    expect(invalidateQueries).not.toHaveBeenCalled()
    active.value = true
    await nextTick()
    vi.advanceTimersByTime(CATALOG_REFRESH_INTERVAL_MS)
    expect(invalidateQueries.mock.calls).toEqual([[{ key: ['models'] }], [{ key: ['provider-models'] }]])
    fakeDocument.visibilityState = 'hidden'
    vi.advanceTimersByTime(CATALOG_REFRESH_INTERVAL_MS)
    expect(invalidateQueries).toHaveBeenCalledTimes(2)
    scope.stop()
    vi.advanceTimersByTime(CATALOG_REFRESH_INTERVAL_MS)
    expect(invalidateQueries).toHaveBeenCalledTimes(2)
  })
})

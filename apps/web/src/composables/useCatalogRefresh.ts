import { onScopeDispose, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { useQueryCache } from '@pinia/colada'

// The server may reconcile an upstream catalog while the app stays open.
// Poll only Memoh's local catalog, never the provider's inference endpoints.
export const CATALOG_REFRESH_INTERVAL_MS = 2 * 60 * 1000

export function useCatalogRefresh(active: MaybeRefOrGetter<boolean>) {
  const cache = useQueryCache()
  let timer: ReturnType<typeof setInterval> | undefined
  function stop() {
    if (timer !== undefined) clearInterval(timer)
    timer = undefined
  }
  watch(() => toValue(active), enabled => {
    stop()
    if (!enabled) return
    timer = setInterval(() => {
      if (document.visibilityState !== 'visible') return
      void cache.invalidateQueries({ key: ['models'] }).catch(() => undefined)
      void cache.invalidateQueries({ key: ['provider-models'] }).catch(() => undefined)
    }, CATALOG_REFRESH_INTERVAL_MS)
  }, { immediate: true })
  onScopeDispose(stop)
}

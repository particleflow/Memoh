import type { Router } from 'vue-router'
import { CHAT_ROUTE_NAMES } from '@/constants/chat-routes'

// Web creates workspace/chat stores before installing Router; native hosts can
// do the same. Home supplies its real router once mounted. Keep this context
// live and unpersisted so a later initialize retry sees the current entry.
export function createChatRouteContext(initialRouter: Router | null) {
  let router = initialRouter
  return {
    bindRouteContext(nextRouter: Router) { router = nextRouter },
    hasHistoryEntry: () => !!router?.currentRoute.value.query.session,
    hasContactEntry: () => !!router && CHAT_ROUTE_NAMES.has(router.currentRoute.value.name as string) && !router.currentRoute.value.query.session,
  }
}

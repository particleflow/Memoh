import router from '../router'
import uiTheme from './harmony-theme.json'
import { sdkAuthQuery } from '@/lib/api-client'

// Login, sessions and theme preferences stay in the official router/store.
const host = window as Window & {
  begoniaNative?: { request: (request: string) => Promise<string> }
}
export function publishTheme(mode: 'light' | 'dark' | 'warm') {
  const themeId = mode === 'warm' ? 'warm-theme' : mode === 'dark' ? uiTheme.defaultDark : uiTheme.defaultLight
  void host.begoniaNative?.request(JSON.stringify({ version: 1, command: 'client.theme', themeId })).catch(() => {})
}
function publish() {
  const state = router.currentRoute.value.name === 'Login' ? 'login-page' : 'ready'
  const token = sdkAuthQuery().token
  void host.begoniaNative?.request(JSON.stringify({ version: 1, command: 'client.state', state,
    authorization: state === 'ready' && token ? 'Bearer ' + token : undefined })).catch(() => {})
}
router.afterEach(publish)
void router.isReady().then(publish)

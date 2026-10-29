import router from '../router'
import uiTheme from './harmony-theme.json'
import { resumeDiagnostics, suspendDiagnostics } from './harmony-diagnostics'

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
  void host.begoniaNative?.request(JSON.stringify({ version: 1, command: 'client.state', state })).then(() => {
    if (state !== 'ready') { suspendDiagnostics(); return }
    let bot = localStorage.getItem('chat-bot-id') ?? ''
    try { const parsed: unknown = JSON.parse(bot); if (typeof parsed === 'string') bot = parsed } catch { /* Existing plain string key. */ }
    void resumeDiagnostics(bot).catch(() => {})
  }).catch(() => {})
}
router.afterEach(publish)
void router.isReady().then(publish)

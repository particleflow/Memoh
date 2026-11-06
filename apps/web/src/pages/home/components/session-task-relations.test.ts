// @vitest-environment jsdom
import { createApp, h, nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { SessiontaskTask } from '@memohai/sdk'
import en from '@/i18n/locales/en.json'
import SessionTaskResults from './session-task-results.vue'

vi.mock('@/store/chat-list', () => ({ useChatStore: () => ({ currentBotId: 'unrelated-bot' }) }))
let app: ReturnType<typeof createApp>
let root: HTMLDivElement
afterEach(() => { app?.unmount(); root?.remove() })

function task(id: string, session: string, parent?: string): SessiontaskTask {
  return { task_type: 'subagent', task_id: id, session_id: session, direct_parent_session_id: parent,
    parent_session_id: 'query-root', created_at: '', updated_at: '', status: 'unknown',
    summary: 'Same name', content_available: false, attachments_available: false }
}

describe.each(['web', 'native'])('persisted session references in %s host', host => {
  it('shows three levels and ownership across pages without inventing missing nodes', async () => {
    const child = task('child-id', 'child-session', 'query-root')
    const grandchild = task('grandchild-id', 'grandchild-session', 'child-session')
    const operation = { ...task('operation-id', 'grandchild-session', 'child-session'), task_type: 'computer_operation' as const }
    const items = ref([child, grandchild, operation])
    const detail = ref<SessiontaskTask>()
    root = document.createElement('div'); document.body.append(root)
    app = createApp({ render: () => h(host === 'native' ? 'dialog' : 'section', { open: true }, [h(SessionTaskResults, {
      items: items.value, detail: detail.value, selected: detail.value, loading: false,
    })]) })
    app.use(createI18n({ legacy: false, locale: 'en', messages: { en } })); app.mount(root)
    const rows = root.querySelectorAll('button')
    expect(rows).toHaveLength(3)
    expect(rows[0]?.textContent).toContain('Owning session: child-session')
    expect(rows[0]?.textContent).toContain('Session’s direct parent: query-root')
    expect(rows[1]?.textContent).toContain('Session’s direct parent: child-session')
    expect(rows[2]?.textContent).toContain('Owning session: grandchild-session')
    items.value = [grandchild]; await nextTick()
    expect(root.querySelectorAll('button')).toHaveLength(1)
    expect(root.textContent).toContain('Session’s direct parent: child-session')
    expect(root.querySelectorAll('a,[role=treeitem]')).toHaveLength(0)
    detail.value = operation; await nextTick()
    const relationship = root.querySelector('dl[aria-label="Session relationship"]')!
    expect(relationship.textContent).toContain('Owning sessiongrandchild-session')
    expect(relationship.textContent).toContain('Session’s direct parentchild-session')
    expect(relationship.textContent).toContain('Requested conversation rootquery-root')
    detail.value = { ...operation, direct_parent_session_id: undefined }; await nextTick()
    expect(relationship.textContent).toContain('Session’s direct parentNo reference provided')
    expect(relationship.querySelectorAll('dd')[1]?.textContent).toBe('No reference provided')
    detail.value = { ...operation, direct_parent_session_id: '<img src=x onerror=alert(1)>' }; await nextTick()
    expect(relationship.textContent).toContain('<img src=x onerror=alert(1)>')
    expect(relationship.querySelectorAll('img,script,a')).toHaveLength(0)
  })
})

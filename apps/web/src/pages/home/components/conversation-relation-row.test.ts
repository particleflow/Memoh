// @vitest-environment jsdom
import { createApp, h } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import en from '@/i18n/locales/en.json'
import zh from '@/i18n/locales/zh.json'
import ja from '@/i18n/locales/ja.json'
import type { ConversationRelation } from '../composables/useConversationRelations'
import ConversationRelationRow from './conversation-relation-row.vue'

let app: ReturnType<typeof createApp> | undefined
let root: HTMLUListElement | undefined
afterEach(() => { app?.unmount(); root?.remove() })

describe.each([{ locale: 'en', messages: en }, { locale: 'zh', messages: zh }, { locale: 'ja', messages: ja }])('conversation status in $locale', ({ locale, messages }) => {
  it.each([
    ['errored', 'failed'], ['admitting', 'accepted'], ['running', 'running'],
    ['aborted', 'aborted'], ['lost', 'lost'], ['', 'unknown'],
  ] as const)('renders native %s without leaking a translation key or changing navigation', (state, translated) => {
    const row: ConversationRelation = { key: 'session:bot:original', kind: 'bot', bot: 'bot', session: 'original', name: '红玉', state }
    const open = vi.fn()
    root = document.createElement('ul'); document.body.append(root)
    app = createApp({ render: () => h(ConversationRelationRow, { row, items: [row], disabled: false, onOpen: open }) })
    app.use(createI18n({ legacy: false, locale, messages: { [locale]: messages } })); app.mount(root)
    expect(root.textContent).toBe(`红玉${messages.chat.tasks.statuses[translated]}`)
    expect(root.textContent).not.toContain('chat.tasks.statuses.')
    root.querySelector('button')!.click()
    expect(open).toHaveBeenCalledExactlyOnceWith(row)
    expect(row.state).toBe(state)
  })
})

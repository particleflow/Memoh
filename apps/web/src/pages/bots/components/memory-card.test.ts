// @vitest-environment jsdom
import { createApp, nextTick, type App } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, expect, it, vi } from 'vitest'
import type { AdaptersMemoryItem } from '@memohai/sdk'
import MemoryCard from './memory-card.vue'

let app: App | undefined
let root: HTMLDivElement | undefined
afterEach(() => { app?.unmount(); root?.remove() })

async function mount(item: AdaptersMemoryItem & { memory: string }) {
  const onOpenFile = vi.fn()
  const onEdit = vi.fn()
  root = document.createElement('div')
  document.body.append(root)
  app = createApp(MemoryCard, { item, locale: 'en', onOpenFile, onEdit })
  app.use(createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false, messages: { en: {} } }))
  app.mount(root)
  await nextTick()
  return { onOpenFile, onEdit }
}

it('opens only the server-validated projection and preserves the edit action', async () => {
  const file = { status: 'available' as const, content_hash: 'a'.repeat(64), name: `${'a'.repeat(64)}.txt`, mime: 'text/plain' }
  const { onOpenFile, onEdit } = await mount({ id: 'm1', memory: 'synthetic summary', file_refs: [file], metadata: { url: 'https://SECRET.invalid', name: 'SECRET', bot_id: 'SECRET' } })
  const buttons = [...root!.querySelectorAll('button')]
  buttons.find(b => b.textContent?.includes('bots.memory.openFile'))!.click()
  expect(onOpenFile).toHaveBeenCalledWith(file)
  buttons.find(b => b.getAttribute('aria-label') === 'bots.memory.editMemory')!.click()
  expect(onEdit).toHaveBeenCalledOnce()
  expect(root!.textContent).not.toContain('SECRET')
})

it('renders unavailable without an open action', async () => {
  await mount({ id: 'm2', memory: 'synthetic unavailable', file_refs: [{ status: 'unavailable' }], metadata: { content_hash: 'b'.repeat(64) } })
  expect(root!.textContent).toContain('bots.memory.fileUnavailable')
  expect(root!.textContent).not.toContain('bots.memory.openFile')
  expect(root!.textContent).not.toContain('b'.repeat(64))
})

it('does not promote arbitrary metadata or change plain memories', async () => {
  const { onEdit } = await mount({ id: 'm3', memory: 'plain memory', metadata: { content_hash: 'c'.repeat(64), url: 'https://SECRET.invalid', file_refs: [{ status: 'available' }] } })
  expect(root!.textContent).toContain('plain memory')
  expect(root!.textContent).not.toContain('bots.memory.openFile')
  expect(root!.textContent).not.toContain('bots.memory.fileUnavailable')
  root!.querySelector<HTMLButtonElement>('button')!.click()
  expect(onEdit).toHaveBeenCalledOnce()
})

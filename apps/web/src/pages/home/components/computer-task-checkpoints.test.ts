// @vitest-environment jsdom
import { createApp, h, nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it } from 'vitest'
import type { ComputertaskRecovery } from '@memohai/sdk'
import en from '@/i18n/locales/en.json'
import zh from '@/i18n/locales/zh.json'
import ja from '@/i18n/locales/ja.json'
import ComputerTaskCheckpoints from './computer-task-checkpoints.vue'

let app: ReturnType<typeof createApp>
let root: HTMLDivElement
afterEach(() => { app?.unmount(); root?.remove() })
function mount(data: ComputertaskRecovery, locale = 'en') {
  const value = ref(data)
  root = document.createElement('div'); document.body.append(root)
  app = createApp({ render: () => h(ComputerTaskCheckpoints, { data: value.value }) })
  app.use(createI18n({ legacy: false, locale, messages: { en, zh, ja } })); app.mount(root)
  return value
}
const revision = '9007199254740993123'
const hash = 'a'.repeat(64)
const fixture: ComputertaskRecovery = {
  target_id: 'synthetic-target', workspace_id: 'w-original', pane_id: 'p-original', pi_session_id: 'b'.repeat(64),
  attempt: 'c'.repeat(32), process_state: 'exited', pi_result: 'succeeded', result_reason: 'verified_persisted_result', business_outcome: 'unknown',
  native: { availability: 'offline', agent_status: 'done', reference_match: 'unknown' },
  reconciliation: { revision, generation: 'd'.repeat(32), receipt_state: 'validated', files: [
    { meta: { name: 'stdout', size: 128 }, state: 'recorded', confirmed_offset: 128, current_availability: 'not_rechecked', recorded_content_hash: hash },
    { meta: { name: 'stderr', size: 0 }, state: 'empty', confirmed_offset: 0, current_availability: 'not_rechecked' },
  ] },
  continuity: { status: 'unconfirmed', reason: 'history_not_rechecked', last_invocation: { before: 'verified', after: 'verified' } },
}
describe('read-only computer checkpoints', () => {
  it.each(['en', 'zh', 'ja'])('separates current connection and persisted evidence in %s', locale => {
    mount(fixture, locale)
    const copy = { en, zh, ja }[locale as 'en' | 'zh' | 'ja'].chat.tasks
    expect(root.querySelectorAll('section')).toHaveLength(4)
    for (const key of ['connection', 'receipt', 'history', 'collection'] as const) {
      expect(root.querySelector(`section[aria-label="${copy.observation.checkpoints[key]}"]`)).not.toBeNull()
    }
    expect(root.textContent).toContain(revision)
    expect(root.textContent).toContain(fixture.attempt)
    expect(root.textContent).toContain(fixture.reconciliation!.generation)
    expect(root.textContent).toContain(copy.observation.availability.offline)
    expect(root.textContent).toContain(copy.outcomes.succeeded)
    expect(root.textContent).toContain(copy.observation.checkpoints.continuityStates.unconfirmed)
    expect(root.textContent).toContain(copy.observation.checkpoints.historyNotice)
    expect(root.textContent).toContain(copy.observation.checkpoints.collectionNotice)
    expect(root.textContent).toContain('0 / 0')
    expect(root.textContent).toContain('128 / 128')
    expect(root.textContent).toContain(hash)
    expect(root.querySelectorAll('a,button,img,iframe,input,progress')).toHaveLength(0)
    expect(root.textContent).not.toContain('chat.tasks.')
    const business = [...root.querySelectorAll('dt')].find(node => node.textContent === copy.observation.checkpoints.fields.business_outcome)!
    expect(business.nextElementSibling?.textContent).toBe(copy.observation.states.unknown)
  })
  it('keeps omitted or unsupported fields unknown instead of inferring recovery', async () => {
    const value = mount({})
    const copy = en.chat.tasks.observation
    expect(root.textContent).toContain(copy.match.unknown)
    expect(root.textContent).not.toContain(copy.checkpoints.receiptStates.validated)
    expect(root.textContent).not.toContain(copy.checkpoints.collectionStates.empty)
    value.value = { reconciliation: { receipt_state: 'future', files: [{ state: 'future', current_availability: 'future', confirmed_offset: -1, meta: { size: Number.MAX_SAFE_INTEGER + 1 } }] }, continuity: { status: 'future' } }
    await nextTick()
    expect(root.textContent).toContain('Unknown / Unknown bytes')
    expect(root.textContent).not.toContain('future')
  })
  it('renders partial collection and untrusted identity as text, then replaces stale checkpoints', async () => {
    const value = mount({ ...fixture, workspace_id: '<img src=x onerror=alert(1)>', reconciliation: { files: [{ state: 'collecting', confirmed_offset: 7, meta: { name: '<script>bad</script>', size: 100 } }] } })
    expect(root.textContent).toContain('7 / 100 bytes')
    expect(root.textContent).toContain('Partially collected')
    expect(root.textContent).toContain('<script>bad</script>')
    expect(root.querySelectorAll('img,script')).toHaveLength(0)
    value.value = { native: { availability: 'connection_unavailable' } }; await nextTick()
    expect(root.textContent).not.toContain('7 / 100')
    expect(root.textContent).not.toContain('<script>bad</script>')
  })
})

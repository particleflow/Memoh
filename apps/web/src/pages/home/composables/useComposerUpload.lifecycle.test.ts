// @vitest-environment jsdom
import { createApp, defineComponent, h, KeepAlive, nextTick, onUnmounted, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { getBotsByBotIdMediaLimits, postBotsByBotIdMedia } from '@memohai/sdk'
import { useComposerUpload } from './useComposerUpload'

vi.mock('@memohai/sdk', () => ({
  getBotsByBotIdMediaLimits: vi.fn(),
  postBotsByBotIdMedia: vi.fn(),
}))

describe('cached chat tab upload lifecycle', () => {
  it('aborts when switching to an already-open tab and never sends a late upload result', async () => {
    vi.mocked(getBotsByBotIdMediaLimits).mockResolvedValue({ data: { max_upload_bytes: 1024 } } as never)
    let complete!: (value: never) => void
    const upload = vi.mocked(postBotsByBotIdMedia)
    upload.mockImplementation(() => new Promise<never>(resolve => { complete = resolve }))
    const sendMessage = vi.fn()
    const unmounted = vi.fn()
    const mounted: string[] = []
    let sending!: Promise<unknown>
    // eslint-disable-next-line vue/one-component-per-file -- cached panes and their tab host
    const Pane = defineComponent({
      props: { tab: { type: String, required: true } },
      setup(props) {
        const composer = useComposerUpload()
        const draft = ref(`draft-${props.tab}`)
        mounted.push(props.tab)
        onUnmounted(unmounted)
        return () => h('button', {
          'data-send': props.tab,
          onClick: () => {
            sending = composer.upload('bot-a', [new File(['x'], 'fixture.txt')]).then(sendMessage)
            // The component's error boundary consumes cancellation.
            void sending.catch(() => {})
          },
        }, draft.value)
      },
    })
    const shown = ref('b')
    const host = document.createElement('div')
    // Match panel-chat's separate KeepAlive + visible v-if for each open tab.
    // eslint-disable-next-line vue/one-component-per-file -- cached panes and their tab host
    const app = createApp({
      render: () => h('div', ['a', 'b'].map(tab =>
        h(KeepAlive, { key: tab }, { default: () => shown.value === tab ? h(Pane, { tab }) : null }))),
    })
    app.mount(host)
    try {
      shown.value = 'a'
      await nextTick()
      host.querySelector<HTMLButtonElement>('[data-send="a"]')!.click()
      await vi.waitFor(() => expect(upload).toHaveBeenCalledOnce())
      const signal = upload.mock.calls[0]![0].signal!
      expect(signal.aborted).toBe(false)
      shown.value = 'b'
      await nextTick()
      expect(host.textContent).toBe('draft-b')
      expect(unmounted).not.toHaveBeenCalled()
      expect(mounted).toEqual(['b', 'a'])
      expect(signal.aborted).toBe(true)
      complete({ data: { content_hash: 'a'.repeat(64), size_bytes: 1, mime: 'text/plain' } } as never)
      await expect(sending).rejects.toMatchObject({ name: 'AbortError' })
      expect(sendMessage).not.toHaveBeenCalled()
      shown.value = 'a'
      await nextTick()
      expect(host.textContent).toBe('draft-a')
      expect(mounted).toEqual(['b', 'a'])
    } finally {
      app.unmount()
    }
  })
})

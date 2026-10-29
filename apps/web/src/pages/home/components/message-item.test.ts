// @vitest-environment jsdom
/* eslint-disable vue/one-component-per-file */

import { createApp, defineComponent, h, nextTick, ref, type App, type PropType } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { handleBrowserKeyboardShortcut } from '@/lib/browser-keyboard-shortcuts'
import { keyboardBindings, resolveKeyboardBinding } from '@/lib/keyboard-bindings'
import { appKeyboardCommands, createKeyboardCommandRegistry } from '@/lib/keyboard-commands'
import type { ChatWebSocket, UIRuntimeEvent, UIStreamEvent } from '@/composables/api/useChat'
import { createRuntimeClient } from '@/store/chat/runtime-client'
import { createChatRealtimeController, type ChatRealtimeTransport } from '@/store/chat/realtime'
import { createTranscriptController } from '@/store/chat/transcript'
import streamFixtures from './fixtures/contact-runtime-stream.json'

const recoveryApi = vi.hoisted(() => ({ recover: vi.fn(), observe: vi.fn() }))
vi.mock('@memohai/sdk', async importOriginal => ({ ...(await importOriginal<typeof import('@memohai/sdk')>()), postBotsByBotIdSessionsBySessionIdDecisionsByDecisionIdRecover: recoveryApi.recover, getBotsByBotIdSessionsBySessionIdInvocationsByInvocationId: recoveryApi.observe }))

const Stub = defineComponent({ inheritAttrs: false, setup: () => () => h('div') })

vi.mock('markstream-vue', () => ({ default: defineComponent({ props: { content: { type: String, default: '' } }, setup: props => () => h('p', props.content) }), setCustomComponents: vi.fn(), enableKatex: vi.fn(), enableMermaid: vi.fn() }))
vi.mock('@/components/markdown', () => ({ registerSharedMarkdownComponents: vi.fn() }))
vi.mock('@/components/themed-mermaid-block/index.vue', () => ({ default: Stub }))
vi.mock('@/components/chat-list/channel-badge/index.vue', () => ({ default: Stub }))
for (const child of ['chat-code-block', 'attachment-block', 'dependency-missing-block']) {
  vi.doMock(`./${child}.vue`, () => ({ default: Stub }))
}
for (const child of ['tool-call-group', 'chat-answers-card', 'background-task-block']) {
  vi.doMock(`./${child}.vue`, () => ({ default: defineComponent({
    props: { items: { type: Array as PropType<Array<{ type: string; content?: string }>>, default: () => [] } },
    setup: props => () => h('div', { 'data-process-test': child },
      props.items.map(item => item.type === 'reasoning' ? item.content : JSON.stringify(item)).join('')),
  }) }))
}
vi.mock('./collapsible-user-text.vue', () => ({
  default: defineComponent({ props: { text: String }, setup: props => () => h('p', props.text) }),
}))
vi.mock('./message-actions.vue', () => ({
  default: defineComponent({
    props: { onEdit: Function, onReplay: { type: Function, default: undefined } },
    setup: props => () => h('div', [h('button', { 'data-edit': '', onClick: () => props.onEdit?.() }), props.onReplay ? h('button', { 'data-replay': '', onClick: () => props.onReplay?.() }) : null]),
  }),
}))
vi.mock('@felinic/ui', async importOriginal => ({
  Avatar: Stub, AvatarImage: Stub, AvatarFallback: Stub, Button: Stub, Spinner: Stub, CalloutBanner: (await importOriginal<typeof import('@felinic/ui')>()).CalloutBanner,
  Textarea: defineComponent({
    inheritAttrs: false,
    props: { modelValue: String },
    emits: ['update:modelValue'],
    setup: (props, { attrs, emit }) => () => h('textarea', {
      ...attrs,
      value: props.modelValue,
      onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLTextAreaElement).value),
    }),
  }),
}))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string, args?: Record<string, string>) => key === 'chat.tools.userInputRecoveryState' ? `Recovery: ${args?.state}` : key, te: (key: string) => key.startsWith('chat.runtimeStatus.'), locale: ref('en') }),
}))
vi.mock('@/store/settings', () => ({ useSettingsStore: () => ({}) }))
vi.mock('@/store/user', () => ({ useUserStore: () => ({ userInfo: {} }) }))
vi.mock('@vueuse/core', async importOriginal => ({
  ...await importOriginal<typeof import('@vueuse/core')>(),
  useElementVisibility: () => ref(false),
}))

let app: App | undefined
let root: HTMLDivElement | undefined
afterEach(() => {
  app?.unmount()
  root?.remove()
})

describe('message-item edit', () => {
  it.each(['mac', 'win', 'linux'] as const)('leaves the %s focus-input shortcut to the workbench instead of resubmitting the edit', async (platform) => {
    const registry = createKeyboardCommandRegistry()
    const focusChatInput = vi.fn(() => true)
    registry.register(appKeyboardCommands.focusChatInput, focusChatInput)
    const bindings = keyboardBindings.map(binding => resolveKeyboardBinding(binding, platform))
    const dispatch = (event: KeyboardEvent) => { handleBrowserKeyboardShortcut(event, registry, bindings, platform) }
    window.addEventListener('keydown', dispatch)
    try {
      const editMessage = vi.fn()
      const MessageItem = (await import('./message-item.vue')).default
      root = document.createElement('div')
      document.body.append(root)
      app = createApp(MessageItem, {
        message: { id: 'm1', turnId: 't1', role: 'user', text: 'hello', attachments: [], messages: [], timestamp: new Date() },
        canEditLatestUser: true,
        isScrolling: false,
        onEditMessage: editMessage,
      })
      app.config.globalProperties.$t = (key: string) => key
      app.mount(root)
      await nextTick()
      root.querySelector<HTMLButtonElement>('[data-edit]')!.click()
      await nextTick()
      const textarea = root.querySelector('textarea')!

      const chord = bindings.find(binding => binding.command === appKeyboardCommands.focusChatInput)!
      textarea.dispatchEvent(new KeyboardEvent('keydown', {
        key: chord.key,
        metaKey: platform === 'mac' && !!chord.mod,
        ctrlKey: platform !== 'mac' && !!chord.mod,
        altKey: !!chord.alt,
        shiftKey: !!chord.shift,
        bubbles: true,
        cancelable: true,
      }))
      expect(editMessage).not.toHaveBeenCalled()
      expect(focusChatInput).toHaveBeenCalledOnce()

      const submit = new KeyboardEvent('keydown', { key: 'Enter', metaKey: platform === 'mac', ctrlKey: platform !== 'mac', bubbles: true, cancelable: true })
      textarea.dispatchEvent(submit)
      expect(editMessage).toHaveBeenCalledOnce()
      expect(editMessage.mock.calls[0]!.slice(0, 2)).toEqual(['t1', 'hello'])
    } finally {
      window.removeEventListener('keydown', dispatch)
    }
  })
})


describe('accepted-answer failure hint', () => {
  it.each(['submitted', 'pending'])('uses the existing single hint for %s input, never the whole-turn retry action', async status => {
    localStorage.setItem('token', 'synthetic-account')
    recoveryApi.recover.mockReset().mockResolvedValue({ data: { original_run_id: 'original-run', decision_id: 'original-decision', recovery_run_id: 'recovery-run', invocation_id: 'recovery-invocation', state: 'accepted', started: true } })
    recoveryApi.observe.mockReset().mockImplementation(() => new Promise(() => {}))
    const retry = vi.fn(), MessageItem = (await import('./message-item.vue')).default
    root = document.createElement('div'); document.body.append(root)
    app = createApp(MessageItem, {
      botId: 'bot', sessionId: 'session', recoveryEnabled: true, recoveryVisible: true, hideProcessActivity: true, isScrolling: false, onRetryMessage: retry,
      message: { id: 'original-message', role: 'assistant', timestamp: '2026-10-27T07:00:00Z', streaming: false, turnId: 'original-turn', runtimeRunId: 'original-run', messages: [
        { id: 1, type: 'tool', name: 'ask_user', input: {}, tool_call_id: 'call', toolCallId: 'call', toolName: 'ask_user', result: null, done: true, running: false, userInput: { user_input_id: 'original-decision', status, answers: [] } },
        { id: 2, type: 'error', code: 'runtime_run_failed', content: '' },
      ] },
    })
    app.config.globalProperties.$t = (key: string) => key; app.mount(root); await nextTick()
    const hints = root.querySelectorAll<HTMLElement>('[data-slot="callout-banner"]'); expect(hints).toHaveLength(1)
    expect(hints[0]!.tagName).toBe(status === 'submitted' ? 'BUTTON' : 'DIV')
    hints[0]!.click(); hints[0]!.click(); await nextTick(); for (let i = 0; i < 10; i++) await Promise.resolve(); await nextTick()
    expect(retry).not.toHaveBeenCalled(); expect(recoveryApi.recover).toHaveBeenCalledTimes(status === 'submitted' ? 1 : 0)
    if (status === 'submitted') {
      expect(recoveryApi.recover).toHaveBeenCalledWith(expect.objectContaining({ body: { original_run_id: 'original-run' }, path: { bot_id: 'bot', session_id: 'session', decision_id: 'original-decision' } }))
      expect(root.textContent).toContain('Recovery: chat.tasks.statuses.accepted')
      expect(root.querySelectorAll('[data-slot="callout-banner"]')).toHaveLength(1)
    }
    localStorage.removeItem('token')
  })
})


describe('message speech state', () => {
  it('scopes the frame and accessible status to actual playing and offers keyboard replay', async () => {
    const MessageItem = (await import('./message-item.vue')).default
    const status = ref<'playing' | 'paused' | 'error' | 'idle'>('playing')
    const replay = vi.fn()
    root = document.createElement('div'); document.body.append(root)
    app = createApp(defineComponent({ setup: () => () => h(MessageItem, {
      message: { id: 'speech', role: 'assistant', timestamp: '', streaming: false, messages: [{ id: 1, type: 'text', content: 'reply' }] },
      isScrolling: false, speechEnabled: true, speechStatus: status.value, onReplaySpeech: replay,
    }) }))
    app.config.globalProperties.$t = (key: string) => key
    app.mount(root); await nextTick()
    expect(root.querySelectorAll('[data-speech-state="playing"]')).toHaveLength(1)
    expect(root.querySelector('[role="status"]')?.textContent).toBe('chat.readAloud.playing')
    expect(root.querySelectorAll('audio')).toHaveLength(0)
    root.querySelector<HTMLButtonElement>('[data-replay]')!.click(); expect(replay).toHaveBeenCalledOnce()
    for (const next of ['paused', 'error', 'idle'] as const) {
      status.value = next; await nextTick()
      expect(root.querySelectorAll('[data-speech-state="playing"]')).toHaveLength(0)
      expect(root.querySelector('[role="status"]')).toBeNull()
    }
  })
})

describe('contact transcript projection', () => {
  it('keeps parent public deltas isolated from child, cross-Bot and internal-return traffic', async () => {
    const scheduled: Array<FrameRequestCallback> = []
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { scheduled.push(callback); return 1 })
    try {
      const parentBot = 'stream-bot', targetBot = 'target-bot', parentSession = 'parent-session', childSession = 'child-session', targetSession = 'target-session'
      const transcripts = new Map<string, ReturnType<typeof createTranscriptController>>()
      for (const [bot, sid] of [[parentBot, parentSession], [parentBot, childSession], [targetBot, targetSession]]) {
        transcripts.set(`${bot}/${sid}`, createTranscriptController({
          currentBotId: ref(bot!), sessionId: ref(sid!), rememberBackgroundTask: task => task,
          applyPendingBackgroundEventsToTool: () => {}, bumpFsChangedAtIfFsMutation: () => {},
          fetchMessages: async () => [], locateMessage: async () => ({ items: [], target_id: '', target_external_message_id: '' }),
        }))
      }
      const sockets: Array<{ bot: string; receive: (event: UIStreamEvent) => void }> = []
      const transport: ChatRealtimeTransport = {
        connectWebSocket: (bot, receive) => {
          sockets.push({ bot, receive })
          return { connected: true, send: () => {}, abort: () => {}, forget: () => {}, bindSession: () => {}, close: () => {}, onOpen: null, onClose: null } satisfies ChatWebSocket
        },
        streamBotSessionsActivityEvents: async () => {},
        createRetryingStream: () => ({ start: () => {}, stop: () => {} }),
      }
      const controller = () => createChatRealtimeController({
        onWebSocketEvent: () => {}, onBotSessionsActivityEvent: () => {},
        prepareSessionRuntime: async (_bot, _sid, commit) => { await commit(() => {}) },
        onRuntimeProjection: (bot, sid, change) => transcripts.get(`${bot}/${sid}`)!.applyRuntimeTranscript(change.current.transcript),
      }, transport)
      const parent = controller(), target = controller()
      parent.startWebSocket(parentBot); parent.startSessionRuntime(parentBot, parentSession); parent.startSessionRuntime(parentBot, childSession)
      target.startWebSocket(targetBot); target.startSessionRuntime(targetBot, targetSession)
      const parentSocket = sockets[0]!, targetSocket = sockets[1]!
      const fixture = (kind: 'direct' | 'result', bot: string, sid: string, run: string, prefix: string): UIRuntimeEvent[] =>
        JSON.parse(JSON.stringify(streamFixtures[kind])
          .replaceAll(`stream-session-${kind}`, sid).replaceAll(`stream-run-${kind}`, run).replaceAll('stream-bot', bot)
          .replaceAll('Show progress', `${prefix} request`).replaceAll('Provider reasoning summary', `${prefix} reasoning`)
          .replaceAll('First streamed fragment', `${prefix} first`).replaceAll('; second fragment', `; ${prefix} second`))
      const own = fixture('direct', parentBot, parentSession, 'parent-run', 'OWN delegate_to_bot')
      const child = fixture('direct', parentBot, childSession, 'child-run', 'PRIVATE_ALPHA')
      const other = fixture('direct', targetBot, targetSession, 'target-run', 'PRIVATE_BETA')
      const returned = fixture('result', parentBot, parentSession, 'summary-run', 'OWN natural outcome')
      for (const stream of [child, other]) {
        const status = stream[1]!
        if (status.type === 'runtime_delta') status.delta.message_upserts = [{ id: 0, type: 'status', name: 'api_retry', args: { attempt: '1', max_retries: '3', seconds: '2' } }]
      }
      const settle = async () => {
        scheduled.splice(0).forEach(callback => callback(0))
        for (let i = 0; i < 8; i++) await Promise.resolve()
        await nextTick()
      }
      const release = async (socket: typeof parentSocket, frame: UIRuntimeEvent) => { socket.receive(frame); await settle() }
      const MessageItem = (await import('./message-item.vue')).default
      root = document.createElement('div'); document.body.append(root)
      const parentTranscript = transcripts.get(`${parentBot}/${parentSession}`)!
      app = createApp(defineComponent({ setup: () => () => h('div', [
        h('section', { 'data-boundary-contact': '' }, parentTranscript.messages.map(message => h(MessageItem, { key: message.id, message, hideProcessActivity: true, isScrolling: false }))),
        h('section', { 'data-boundary-debug': '' }, [...transcripts.values()].flatMap(transcript => transcript.messages.map(message => h(MessageItem, { key: message.id, message, hideProcessActivity: false, isScrolling: false })))),
      ]) }))
      app.config.globalProperties.$t = (key: string) => key; app.mount(root)
      const contact = () => root!.querySelector('[data-boundary-contact]')!.textContent!
      const debug = () => root!.querySelector('[data-boundary-debug]')!.textContent!
      const noInternalDialogue = () => {
        for (const text of ['PRIVATE_ALPHA', 'PRIVATE_BETA', 'PRIVATE_TOOL', 'MACHINE_CONTEXT_MUST_NOT_RENDER']) expect(contact()).not.toContain(text)
        expect(contact()).not.toContain('chat.runtimeStatus.api_retry')
      }
      await release(parentSocket, own[0]!); await release(parentSocket, child[0]!); await release(targetSocket, other[0]!)
      for (let i = 1; i <= 3; i++) {
        await release(parentSocket, child[i]!); await release(targetSocket, other[i]!); await release(parentSocket, own[i]!)
        noInternalDialogue()
      }
      expect(contact()).toContain('OWN delegate_to_bot reasoning')
      expect(contact()).toContain('chat.runtimeStatus.compacting')
      expect(contact()).toContain('OWN delegate_to_bot first')
      expect(contact()).not.toContain('; OWN delegate_to_bot second')
      const toolFrame = JSON.parse(JSON.stringify(own[3])) as Extract<UIRuntimeEvent, { type: 'runtime_delta' }>
      toolFrame.seq += 1
      toolFrame.delta = { message_upserts: ['spawn_agent', 'send_message', 'delegate_to_bot'].map((name, i) => ({
        id: 20 + i, type: 'tool', name, tool_call_id: `call-${i}`, running: i !== 1,
        input: { message: `PRIVATE_TOOL_INPUT_${i}` }, output: { text: `PRIVATE_TOOL_OUTPUT_${i}` }, progress: [{ text: `PRIVATE_TOOL_PROGRESS_${i}` }],
      })) }
      await release(parentSocket, toolFrame)
      noInternalDialogue()
      expect(debug()).toContain('PRIVATE_TOOL_INPUT_0')
      expect(debug()).toContain('PRIVATE_TOOL_OUTPUT_1')
      expect(debug()).toContain('PRIVATE_TOOL_PROGRESS_2')
      expect(debug()).toContain('PRIVATE_ALPHA reasoning')
      expect(debug()).toContain('PRIVATE_BETA first')
      expect(debug()).toContain('chat.runtimeStatus.api_retry')
      expect(parentTranscript.messages.filter(message => message.role === 'user')).toHaveLength(1)
      for (let i = 4; i < child.length; i++) {
        await release(parentSocket, child[i]!); await release(targetSocket, other[i]!); noInternalDialogue()
      }
      for (let i = 4; i < own.length; i++) {
        const frame = JSON.parse(JSON.stringify(own[i])) as UIRuntimeEvent; frame.seq += 1
        await release(parentSocket, frame); noInternalDialogue()
      }
      // Same source Bot/session, distinct host summary run. Its actual captured
      // contract has no request user turn; target text remains in its own view.
      for (const frame of returned) { frame.seq += 100; await release(parentSocket, frame); noInternalDialogue() }
      expect(contact()).toContain('OWN natural outcome first; OWN natural outcome second')
      expect(parentTranscript.messages.filter(message => message.role === 'user')).toHaveLength(1)
      // A late, correctly scoped target snapshot from the old socket cannot
      // mutate its cached projection after switching the connection's Bot.
      const targetSeq = target.runtimeProjection(targetSession)!.seq
      target.startWebSocket(parentBot); target.startSessionRuntime(parentBot, parentSession)
      const before = contact()
      const lateTarget = JSON.parse(JSON.stringify(other[0])) as Extract<UIRuntimeEvent, { type: 'runtime_snapshot' }>
      lateTarget.seq = targetSeq + 1; lateTarget.snapshot.seq = lateTarget.seq
      await release(targetSocket, lateTarget)
      expect(contact()).toBe(before)
      expect(target.runtimeProjection(targetSession)!.seq).toBe(targetSeq)
      noInternalDialogue()
      parent.stopStreams(); target.stopStreams()
    } finally { vi.unstubAllGlobals() }
  })

  it.each(['direct', 'result'] as const)('renders captured %s runtime deltas before the next fragment or terminal event', async kind => {
    // Captured from the actual session manager + UI converter at 5a42d013.
    // Each frame is released separately: no completed answer or typewriter simulation.
    const frames = streamFixtures[kind] as unknown as UIRuntimeEvent[]
    const sid = frames[0]!.session_id
    const transcript = createTranscriptController({
      currentBotId: ref('stream-bot'), sessionId: ref(sid),
      rememberBackgroundTask: task => task,
      applyPendingBackgroundEventsToTool: () => {}, bumpFsChangedAtIfFsMutation: () => {},
      fetchMessages: async () => [], locateMessage: async () => ({ items: [], target_id: '', target_external_message_id: '' }),
    })
    const callbacks: Array<() => void> = []
    const client = createRuntimeClient({
      send: () => {}, scheduleFrame: callback => callbacks.push(callback),
      onProjection: (_sid, change) => transcript.applyRuntimeTranscript(change.current.transcript),
    })
    client.subscribe(sid); client.onConnected()
    const MessageItem = (await import('./message-item.vue')).default
    root = document.createElement('div'); document.body.append(root)
    app = createApp(defineComponent({ setup: () => () => h('div', transcript.messages.map(message =>
      h(MessageItem, { key: message.id, message, hideProcessActivity: true, isScrolling: false }),
    )) }))
    app.config.globalProperties.$t = (key: string) => key; app.mount(root)
    const release = async (index: number) => {
      client.handleEvent(frames[index]!); callbacks.splice(0).forEach(callback => callback()); await nextTick()
    }
    await release(0)
    await release(1)
    expect(root.textContent).toContain('chat.runtimeStatus.compacting')
    await release(2)
    expect(root.textContent).toContain('Provider reasoning summary')
    await release(3)
    expect(root.textContent).toContain('First streamed fragment')
    expect(root.textContent).not.toContain('; second fragment')
    expect(transcript.messages.some(message => message.role === 'assistant' && message.streaming)).toBe(true)
    await release(4)
    expect(root.textContent).toContain('First streamed fragment; second fragment')
    expect(root.textContent).not.toContain('MACHINE_CONTEXT_MUST_NOT_RENDER')
    expect(transcript.messages.filter(message => message.role === 'user')).toHaveLength(kind === 'direct' ? 1 : 0)
    for (let index = 5; index < frames.length; index++) await release(index)
    expect(transcript.messages.some(message => message.streaming)).toBe(false)
    expect(root.textContent).not.toContain('chat.runtimeStatus.compacting')
    expect(root.textContent).toContain('First streamed fragment; second fragment')
  })

  it.each([true, false])('keeps public reasoning, ordinary text and completed Q&A while hiding raw tools (streaming=%s)', async streaming => {
    const MessageItem = (await import('./message-item.vue')).default
    const hide = ref(true)
    const text = 'Spawn agent in background; execution state: unknown'
    const tool = { id: 1, type: 'tool' as const, name: 'exec', input: {}, tool_call_id: 'call', toolCallId: 'call', toolName: 'exec', result: 'raw result', done: true, running: false }
    const message = { id: 'projection', role: 'assistant' as const, timestamp: '', streaming, messages: [
      tool,
      { id: 2, type: 'reasoning' as const, content: 'Provider reasoning summary' },
      { id: 3, type: 'text' as const, content: text },
      { ...tool, id: 4, toolName: 'ask_user', result: { answers: [] } },
    ] }
    root = document.createElement('div'); document.body.append(root)
    app = createApp(defineComponent({ setup: () => () => h(MessageItem, { message, hideProcessActivity: hide.value, isScrolling: false }) }))
    app.config.globalProperties.$t = (key: string) => key; app.mount(root); await nextTick()
    expect(root.textContent).toContain(text)
    expect(root.querySelectorAll('[data-process-test="tool-call-group"]')).toHaveLength(1)
    expect(root.textContent).toContain('Provider reasoning summary')
    expect(root.querySelectorAll('[data-process-test="chat-answers-card"]')).toHaveLength(1)
    expect(message.messages[0]).toBe(tool)
    expect(tool.result).toBe('raw result')
    hide.value = false; await nextTick()
    expect(root.querySelectorAll('[data-process-test="tool-call-group"]')).toHaveLength(1)
    expect(root.textContent).toContain(text)
  })

  it.each(['tool-only', 'background', 'user'] as const)('projects %s by structured role and kind, never by wording', async kind => {
    const MessageItem = (await import('./message-item.vue')).default
    const hide = ref(true)
    const text = 'Spawn agent in background; execution state: unknown'
    const message = kind === 'user'
      ? { id: 'user', role: 'user', text, attachments: [], messages: [], timestamp: '' }
      : kind === 'background'
        ? { id: 'background', role: 'system', kind: 'background_task', backgroundTask: {}, timestamp: '' }
        : { id: 'tool-only', role: 'assistant', timestamp: '', streaming: false, messages: [{ id: 1, type: 'tool', toolName: 'exec', done: true }] }
    root = document.createElement('div'); document.body.append(root)
    app = createApp(defineComponent({ setup: () => () => h(MessageItem, { message: message as never, hideProcessActivity: hide.value, isScrolling: false }) }))
    app.config.globalProperties.$t = (key: string) => key; app.mount(root); await nextTick()
    if (kind === 'user') expect(root.textContent).toContain(text)
    else expect(root.children).toHaveLength(0)
    hide.value = false; await nextTick()
    expect(root.children.length).toBeGreaterThan(0)
  })
})

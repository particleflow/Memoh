import { afterEach, beforeEach, describe, expect, it, vi, type MockInstance } from 'vitest'
import { createSSRApp, ref } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { createI18n } from 'vue-i18n'
import type {
  RuntimeCurrentRunView,
  UIRuntimeDeltaEvent,
  UIRuntimeSnapshotEvent,
  UIToolMessage,
  UITurn,
} from '@/composables/api/useChat.types'
import en from '@/i18n/locales/en.json'
import ImageDetail from '@/pages/home/components/tool-call-detail-image.vue'
import { structuredToolResult } from '../chat-list.normalize'
import { createBackgroundTaskTracker } from './background-tasks'
import {
  createEmptyRuntimeProjection,
  projectRuntimeTranscript,
  reduceRuntimeProjection,
  type RuntimeTranscriptSlice,
} from './runtime-projection'
import { createTranscriptController } from './transcript'
import { createTranscriptHistory } from './transcript-history'
import type { ChatMessage, ToolCallBlock } from './types'

vi.mock('@/store/user', () => ({
  useUserStore: () => ({ userInfo: { id: 'user-1' } }),
}))

let fetchSpy: MockInstance<typeof globalThis.fetch>
beforeEach(() => {
  fetchSpy = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('Unexpected network request'))
})
afterEach(() => {
  // Recovery changes presentation only. It must never submit another image request.
  expect(fetchSpy).not.toHaveBeenCalled()
  fetchSpy.mockRestore()
})

const timestamp = '2026-10-25T00:00:00.000Z'
const contentHash = 'a'.repeat(64)
const terminalStatuses = ['aborted', 'errored', 'lost', 'completed'] as const
const activeStatuses = ['admitting', 'running', 'waiting_decision', 'aborting', 'finishing'] as const

function imageTool(overrides: Partial<UIToolMessage> = {}): UIToolMessage {
  return {
    id: 0,
    type: 'tool',
    name: 'generate_image',
    input: { prompt: 'Synthetic image request' },
    tool_call_id: 'image-call-1',
    running: true,
    ...overrides,
  }
}

function runView(overrides: Partial<RuntimeCurrentRunView> = {}): RuntimeCurrentRunView {
  return {
    run_id: 'run-1',
    turn_id: 'turn-1',
    generation: 'generation-1',
    status: 'running',
    started_at: timestamp,
    updated_at: timestamp,
    messages: [imageTool()],
    ...overrides,
  }
}

function snapshot(currentRunView: RuntimeCurrentRunView, seq = 1): UIRuntimeSnapshotEvent {
  return {
    type: 'runtime_snapshot',
    session_id: 'session-1',
    epoch: 'epoch-1',
    seq,
    snapshot: {
      bot_id: 'bot-1',
      session_id: 'session-1',
      epoch: 'epoch-1',
      seq,
      current_run_view: currentRunView,
      updated_at: timestamp,
    },
  }
}

function delta(value: UIRuntimeDeltaEvent['delta'], seq = 2): UIRuntimeDeltaEvent {
  return { type: 'runtime_delta', session_id: 'session-1', epoch: 'epoch-1', seq, delta: value }
}

function findTool(turns: readonly ChatMessage[], callId = 'image-call-1'): ToolCallBlock {
  for (const turn of turns) {
    if (turn.role !== 'assistant') continue
    const block = turn.messages.find(message => message.type === 'tool' && message.toolCallId === callId)
    if (block?.type === 'tool') return block
  }
  throw new Error(`Missing tool block: ${callId}`)
}

function projectedTool(slice: RuntimeTranscriptSlice): ToolCallBlock {
  const backgroundTasks = createBackgroundTaskTracker()
  const history = createTranscriptHistory({
    messages: [],
    rememberBackgroundTask: backgroundTasks.rememberBackgroundTask,
    applyPendingBackgroundEventsToTool: backgroundTasks.applyPendingBackgroundEventsToTool,
  })
  return findTool(slice.turns.map(history.normalizeTurn))
}

function makeTranscript() {
  const backgroundTasks = createBackgroundTaskTracker()
  return createTranscriptController({
    currentBotId: ref<string | null>('bot-1'),
    sessionId: ref<string | null>('session-1'),
    rememberBackgroundTask: backgroundTasks.rememberBackgroundTask,
    applyPendingBackgroundEventsToTool: backgroundTasks.applyPendingBackgroundEventsToTool,
    bumpFsChangedAtIfFsMutation: vi.fn(),
    fetchMessages: vi.fn().mockResolvedValue([]),
    locateMessage: vi.fn(),
    isTurnLive: () => false,
  })
}

function storedImage(output: unknown): UITurn {
  return {
    id: 'stored-assistant',
    turn_id: 'turn-1',
    turn_position: 1,
    role: 'assistant',
    timestamp,
    messages: [imageTool({ output, running: false })],
  }
}

async function imageDetail(block: ToolCallBlock): Promise<string> {
  const app = createSSRApp(ImageDetail, { block })
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en } }))
  return renderToString(app)
}

async function expectUnconfirmed(block: ToolCallBlock) {
  expect(block).toMatchObject({ done: true, running: false, result: null })
  const html = await imageDetail(block)
  expect(html).toContain(en.chat.tools.detail.imageUnconfirmed)
  expect(html).not.toContain(en.chat.tools.detail.waitingOutput)
  expect(html).not.toContain(en.chat.tools.detail.imageSaved)
  expect(html).not.toContain('<img')
}

const receipts = [
  {
    name: 'completed',
    output: { state: 'completed', content_hash: contentHash, delivered: 'current_conversation' },
    text: en.chat.tools.detail.imageReturned,
  },
  {
    name: 'unknown',
    output: { structuredContent: { state: 'unknown', retryable: false } },
    text: en.chat.tools.detail.imageUnconfirmed,
  },
  {
    name: 'storage unconfirmed',
    output: { structured_content: { state: 'generated', storage_state: 'unconfirmed', retryable: false } },
    text: en.chat.tools.detail.imageStorageUnconfirmed,
  },
]

describe('terminal image receipt recovery', () => {
  it.each(terminalStatuses)('stops waiting after %s and after reconnecting to the same snapshot', async (status) => {
    const initial = reduceRuntimeProjection(createEmptyRuntimeProjection(), snapshot(runView()))
    const ended = reduceRuntimeProjection(initial, delta({ run: { run_id: 'run-1', status } }))

    expect(ended.transcript.streaming).toBe(false)
    await expectUnconfirmed(projectedTool(ended.transcript))

    expect(ended.currentRunView).not.toBeNull()
    const replay = reduceRuntimeProjection(createEmptyRuntimeProjection(), snapshot(ended.currentRunView!, 3))
    await expectUnconfirmed(projectedTool(replay.transcript))
  })

  it('ends an interrupted image edit without changing its input image references', async () => {
    const input = { prompt: 'Synthetic edit request', images: [contentHash] }
    const block = projectedTool(projectRuntimeTranscript(runView({
      status: 'aborted',
      messages: [imageTool({ name: 'edit_image', input })],
    })))

    expect(block.input).toEqual(input)
    await expectUnconfirmed(block)
  })

  it.each(receipts)('keeps a normal $name tool result on a live delta and snapshot', async ({ output, text }) => {
    const initial = reduceRuntimeProjection(createEmptyRuntimeProjection(), snapshot(runView()))
    const ended = reduceRuntimeProjection(initial, delta({
      message_upserts: [imageTool({ output, running: false })],
    }))
    const block = projectedTool(ended.transcript)
    expect(block).toMatchObject({ done: true, running: false, result: output })
    expect(await imageDetail(block)).toContain(text)

    const replay = reduceRuntimeProjection(createEmptyRuntimeProjection(), snapshot(ended.currentRunView!, 3))
    expect(projectedTool(replay.transcript).result).toEqual(output)
    expect(await imageDetail(projectedTool(replay.transcript))).toContain(text)
  })

  it.each(receipts)('does not erase a persisted $name receipt with a stale terminal snapshot', async ({ output, text }) => {
    const transcript = makeTranscript()
    transcript.replaceMessages([storedImage(output)], 'session-1')
    expect(await imageDetail(findTool(transcript.messages))).toContain(text)

    transcript.applyRuntimeTranscript(projectRuntimeTranscript(runView({ status: 'lost' })))

    const block = findTool(transcript.messages)
    expect(block).toMatchObject({ done: true, running: false, result: output })
    expect(await imageDetail(block)).toContain(text)
  })

  it('keeps the persisted image turn when a restarted ledger has no streamed messages or error', async () => {
    const transcript = makeTranscript()
    const requestUserTurn = {
      turn_id: 'turn-1',
      role: 'user' as const,
      text: 'Synthetic image request',
      timestamp,
    }
    const output = receipts[0]!.output
    transcript.replaceMessages([
      { ...requestUserTurn, id: 'stored-user', turn_position: 1 },
      storedImage(output),
    ], 'session-1')
    const existingAssistant = transcript.messages.find(turn => turn.role === 'assistant')
    const restored = reduceRuntimeProjection(createEmptyRuntimeProjection(), snapshot(runView({
      status: 'aborted',
      messages: [],
      error_code: '',
      request_user_turn: requestUserTurn,
    })))

    expect(restored.transcript.turns.some(turn => turn.role === 'assistant')).toBe(false)
    transcript.applyRuntimeTranscript(restored.transcript)

    expect(transcript.messages.find(turn => turn.role === 'assistant')).toBe(existingAssistant)
    const block = findTool(transcript.messages)
    expect(block).toMatchObject({ done: true, running: false, result: output })
    expect(await imageDetail(block)).toContain(en.chat.tools.detail.imageReturned)
  })

  it('does not borrow a completed image result from a different tool call', async () => {
    const transcript = makeTranscript()
    transcript.replaceMessages([storedImage(receipts[0]!.output)], 'session-1')
    transcript.applyRuntimeTranscript(projectRuntimeTranscript(runView({
      status: 'aborted',
      messages: [imageTool({ tool_call_id: 'image-call-2' })],
    })))

    const block = findTool(transcript.messages, 'image-call-2')
    await expectUnconfirmed(block)
    expect(structuredToolResult(block.result).content_hash).toBeUndefined()
  })

  it.each(activeStatuses)('keeps an image waiting while the parent run is %s', async (status) => {
    const block = projectedTool(projectRuntimeTranscript(runView({ status })))
    expect(block).toMatchObject({ done: false, running: true, result: null })
    expect(await imageDetail(block)).toContain(en.chat.tools.detail.waitingOutput)
  })

  it.each(['running', 'stalled'])('keeps an independently %s background task active', (status) => {
    const block = projectedTool(projectRuntimeTranscript(runView({
      status: 'aborted',
      messages: [imageTool({ background_task: { task_id: 'background-1', status } })],
    })))
    expect(block).toMatchObject({ done: false, running: true, result: null })
    expect(block.backgroundTask).toMatchObject({ taskId: 'background-1', status })
  })

  it('replaces an unknown outcome when the real tool result arrives later', async () => {
    const initial = reduceRuntimeProjection(createEmptyRuntimeProjection(), snapshot(runView()))
    const ended = reduceRuntimeProjection(initial, delta({ run: { run_id: 'run-1', status: 'aborted' } }))
    await expectUnconfirmed(projectedTool(ended.transcript))

    const output = receipts[0]!.output
    const received = reduceRuntimeProjection(ended, delta({
      message_upserts: [imageTool({ output, running: false })],
    }, 3))
    const block = projectedTool(received.transcript)
    expect(block).toMatchObject({ done: true, running: false, result: output })
    expect(await imageDetail(block)).toContain(en.chat.tools.detail.imageReturned)
  })
})

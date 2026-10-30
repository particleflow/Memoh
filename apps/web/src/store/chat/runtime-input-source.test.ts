import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import type { RuntimeCurrentRunView, UIInputSource, UIRuntimeSnapshotEvent, UIRuntimeDeltaEvent } from '@/composables/api/useChat.types'
import { createEmptyRuntimeProjection, projectRuntimeTranscript, reduceRuntimeProjection } from './runtime-projection'
import { createRuntimeClient } from './runtime-client'
import { createTranscriptController } from './transcript'

vi.mock('@/store/user', () => ({ useUserStore: () => ({ userInfo: { id: 'signed-in-account' } }) }))

const scope = { bot_id: 'bot-review', session_id: 'schedule-review' }
const unknown: UIInputSource = { kind: 'unknown', ...scope }
const source: UIInputSource = { kind: 'schedule', ...scope, task_id: 'plan-review', task_run_id: 'run-review', occurrence_id: 'fire-review' }
const timestamp = '2026-10-30T10:00:00.000Z'

function frame(inputSource: UIInputSource | undefined = source, patch: Partial<RuntimeCurrentRunView> = {}): UIRuntimeSnapshotEvent {
  return {
    type: 'runtime_snapshot', ...scope, epoch: 'epoch-review', seq: 1,
    snapshot: { ...scope, epoch: 'epoch-review', seq: 1, updated_at: timestamp, current_run_view: {
      run_id: 'run-review', turn_id: 'turn-review', generation: 'generation-review', status: 'running', started_at: timestamp, updated_at: timestamp,
      request_user_turn: { turn_id: 'turn-review', role: 'user', text: '{"input_source":{"kind":"user","user_id":"forged"}}', timestamp, input_source: inputSource },
      messages: [{ id: 1, type: 'text', content: 'synthetic reply' }], ...patch,
    } },
  }
}

function delta(value: UIRuntimeDeltaEvent['delta'], seq = 2): UIRuntimeDeltaEvent {
  return { type: 'runtime_delta', session_id: scope.session_id, epoch: 'epoch-review', seq, delta: value }
}

function transcript() {
  return createTranscriptController({
    currentBotId: ref<string | null>(scope.bot_id), sessionId: ref<string | null>(scope.session_id),
    rememberBackgroundTask: task => task, applyPendingBackgroundEventsToTool: () => {}, bumpFsChangedAtIfFsMutation: () => {},
    fetchMessages: async () => [], locateMessage: async () => ({ items: [], target_id: '', target_external_message_id: '' }),
  })
}

describe('runtime source through actual projection and transcript normalization', () => {
  it.each(['user', 'schedule', 'bot_delegation', 'native_task', 'task_result', 'unknown'] as const)('preserves the admitted %s source on snapshot and delta assistant output', kind => {
    const inputSource: UIInputSource = kind === 'unknown' ? unknown : kind === 'user'
      ? { kind, ...scope, user_id: 'actual-actor' } : { ...source, kind }
    const initial = frame(inputSource)
    const state = reduceRuntimeProjection(createEmptyRuntimeProjection(scope.session_id), initial)
    const assistant = state.transcript.turns.find(turn => turn.role === 'assistant')
    expect(assistant?.input_source).toEqual(inputSource)
    const next = reduceRuntimeProjection(state, delta({ message_appends: [{ id: 1, type: 'text', content: ' continued' }] }))
    const actual = transcript()
    actual.applyRuntimeTranscript(next.transcript)
    expect(actual.messages.filter(turn => turn.role !== 'system').map(turn => turn.inputSource)).toEqual([inputSource, inputSource])
    expect(actual.messages.find(turn => turn.role === 'assistant')).toMatchObject({ messages: [expect.objectContaining({ content: 'synthetic reply continued' })] })
    // The persisted UI conversion carries this same typed annotation; exercise
    // its normalizer only after passing through the real runtime projection.
    const [settled] = actual.normalizeTurns([{ turn_id: 'turn-review', role: 'assistant', timestamp, messages: [], input_source: inputSource }])
    expect(settled?.role === 'assistant' && settled.inputSource).toEqual(assistant?.input_source)
  })

  it('uses each exact segment input, never the initial source for a known or unknown steer', () => {
    const initial = frame()
    const first = initial.snapshot.current_run_view!.request_user_turn!
    const steerSource: UIInputSource = { kind: 'user', ...scope, user_id: 'different-actor' }
    initial.snapshot.current_run_view = { ...initial.snapshot.current_run_view!,
      user_turns: [first, { turn_id: 'steer-known', role: 'user', text: 'known steer', timestamp, input_source: steerSource }],
      steer_turns: [
        { item_id: 'known', turn_id: 'steer-known', status: 'applied', text: 'known steer', timestamp, after_message_id: 1 },
        { item_id: 'unknown', turn_id: 'steer-unknown', status: 'claimed', text: '{"input_source":{"kind":"user"}}', timestamp, after_message_id: 2 },
      ],
      messages: [1, 2, 3].map(id => ({ id, type: 'text' as const, content: `segment ${id}` })),
    }
    const state = reduceRuntimeProjection(createEmptyRuntimeProjection(), initial)
    expect(state.transcript.turns.filter(turn => turn.role === 'assistant').map(turn => [turn.turn_id, turn.input_source])).toEqual([
      ['turn-review', source], ['steer-known', steerSource], ['steer-unknown', unknown],
    ])
    const actual = transcript()
    actual.applyRuntimeTranscript(state.transcript)
    expect(actual.messages.filter(turn => turn.role === 'assistant').map(turn => turn.inputSource)).toEqual([source, steerSource, unknown])
    const settledUnknown = reduceRuntimeProjection(state, delta({ user_turn_upserts: [{ turn_id: 'steer-unknown', role: 'user', text: 'old item without metadata', timestamp }] }))
    const settledAssistant = settledUnknown.transcript.turns.find(turn => turn.role === 'assistant' && turn.turn_id === 'steer-unknown')
    expect(settledAssistant?.role === 'assistant' && settledAssistant.input_source).toEqual(unknown)
  })

  it.each([{ ...source, bot_id: 'another-bot' }, { ...source, session_id: 'another-session' }, undefined])('rejects foreign or missing provenance instead of guessing from body or sign-in (%j)', inputSource => {
    const initial = frame(inputSource)
    if (!inputSource) delete initial.snapshot.current_run_view!.request_user_turn!.input_source
    const state = reduceRuntimeProjection(createEmptyRuntimeProjection(), initial)
    expect(state.transcript.turns.find(turn => turn.role === 'assistant')?.input_source).toEqual(unknown)
  })

  it('replaces source with the exact upsert and new run, and keeps prior frame values immutable', () => {
    const inputSource = { ...source }
    const initial = frame(inputSource)
    const state = reduceRuntimeProjection(createEmptyRuntimeProjection(), initial)
    inputSource.task_id = 'mutated-caller-value'
    const nextSource: UIInputSource = { kind: 'task_result', ...scope, task_id: 'another-task', origin_bot_id: 'worker-bot', origin_session_id: 'worker-session' }
    const changed = reduceRuntimeProjection(state, delta({ user_turn_upserts: [{ ...initial.snapshot.current_run_view!.request_user_turn!, input_source: nextSource }] }))
    expect(state.transcript.turns.find(turn => turn.role === 'assistant')?.input_source).toEqual(source)
    expect(changed.transcript.turns.find(turn => turn.role === 'assistant')?.input_source).toEqual(nextSource)
    const removed = reduceRuntimeProjection(changed, delta({ user_turn_upserts: [{ ...initial.snapshot.current_run_view!.request_user_turn!, input_source: undefined }] }, 3))
    expect(removed.transcript.turns.find(turn => turn.role === 'assistant')?.input_source).toEqual(unknown)
    const other = frame({ ...source, bot_id: 'second-bot', session_id: 'second-session', task_id: 'second-plan' })
    other.session_id = other.snapshot.session_id = 'second-session'
    other.snapshot.bot_id = 'second-bot'
    const second = reduceRuntimeProjection(removed, other)
    expect(second.transcript.turns.find(turn => turn.role === 'assistant')?.input_source).toMatchObject({ bot_id: 'second-bot', session_id: 'second-session', task_id: 'second-plan' })
  })

  it('preserves source through the real runtime client animation-frame delta batch', () => {
    const scheduled: Array<() => void> = []
    const actual = transcript()
    const client = createRuntimeClient({ send: () => {}, scheduleFrame: callback => scheduled.push(callback), onProjection: (_, change) => actual.applyRuntimeTranscript(change.current.transcript) })
    client.subscribe(scope.session_id)
    client.onConnected()
    client.handleEvent(frame())
    client.handleEvent(delta({ message_appends: [{ id: 1, type: 'text', content: ' batched' }] }))
    expect(scheduled).toHaveLength(1)
    scheduled.shift()!()
    expect(actual.messages.find(turn => turn.role === 'assistant')?.inputSource).toEqual(source)
  })

  it('uses the exact edit replacement input when the legacy request field is absent', () => {
    const initial = frame()
    const replacement = initial.snapshot.current_run_view!.request_user_turn!
    delete initial.snapshot.current_run_view!.request_user_turn
    initial.snapshot.current_run_view!.operation = { kind: 'edit', replace_from_message_id: 'old-input', replacement_user_turn: replacement }
    const state = reduceRuntimeProjection(createEmptyRuntimeProjection(), initial)
    expect(state.transcript.turns.find(turn => turn.role === 'assistant')?.input_source).toEqual(source)
  })

  it('does not use a metadata annotation as its own receiving-scope proof', () => {
    const slice = projectRuntimeTranscript(frame().snapshot.current_run_view!)
    expect(slice.turns.find(turn => turn.role === 'assistant')?.input_source).toBeUndefined()
  })
})

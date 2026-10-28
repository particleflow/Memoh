import { describe, expect, it, vi } from 'vitest'
import type { ChatAssistantTurn } from '@/store/chat/types'
import { ChatReadAloud, LiveSpeechReplies, speechChunks, speechKey, speechText, type SpeechState } from './chat-read-aloud'

class Player extends EventTarget {
  src = ''
  play = vi.fn(async () => {})
  pause = vi.fn()
  load = vi.fn()
  emit(type: string) { this.dispatchEvent(new Event(type)) }
}
const tick = async () => { for (let i = 0; i < 10; i++) await Promise.resolve() }
const blob = () => new Blob(['synthetic audio'], { type: 'audio/wav' })
function fixture(synthesize = vi.fn(async (_text: string, _signal: AbortSignal) => blob())) {
  const player = new Player(), states: SpeechState[] = []
  const revokeURL = vi.fn(), createURL = vi.fn(() => 'blob:synthetic')
  const speech = new ChatReadAloud({ synthesize, createPlayer: () => player, createURL, revokeURL, onState: state => states.push(state) })
  return { speech, player, states, synthesize, revokeURL, createURL, latest: () => states.at(-1) }
}
function reply(id: string, streaming = false, text = '你好'): ChatAssistantTurn {
  return { id, turnId: `turn:${id}`, role: 'assistant', streaming, timestamp: '', messages: [{ id: 1, type: 'text', content: text }] }
}

describe('chat read aloud media lifecycle', () => {
  it('reads in order, highlights only actual playing, pauses and ends cleanly', async () => {
    const f = fixture()
    f.speech.enqueue({ key: 'a', text: 'one' }); f.speech.enqueue({ key: 'b', text: 'two' })
    await tick()
    expect(f.latest()).toMatchObject({ key: 'a', status: 'loading' })
    f.player.emit('playing'); expect(f.latest()).toMatchObject({ key: 'a', status: 'playing' })
    f.player.emit('pause'); expect(f.latest()?.status).toBe('paused')
    f.speech.resume(); expect(f.player.play).toHaveBeenCalledTimes(2)
    f.player.emit('playing'); f.player.emit('waiting'); expect(f.latest()?.status).toBe('loading')
    f.player.emit('playing'); f.player.emit('ended'); await tick()
    expect(f.latest()).toMatchObject({ key: 'b', status: 'loading' })
    f.player.emit('playing'); expect(f.latest()).toMatchObject({ key: 'b', status: 'playing' })
    f.player.emit('ended'); expect(f.latest()?.status).toBe('idle')
    expect(f.synthesize.mock.calls.map(call => call[0])).toEqual(['one', 'two'])
    expect(f.revokeURL).toHaveBeenCalledTimes(2)
  })

  it('cancels synthesis and all queued chunks; a late success cannot play', async () => {
    let finish!: (value: Blob) => void
    const f = fixture(vi.fn((_text: string, _signal: AbortSignal) => new Promise<Blob>(resolve => { finish = resolve })))
    f.speech.enqueue({ key: 'old', text: '中'.repeat(1001) })
    const signal = f.synthesize.mock.calls[0]![1]
    f.speech.stop(); expect(signal.aborted).toBe(true)
    finish(blob()); await tick()
    expect(f.latest()?.status).toBe('idle')
    expect(f.player.play).not.toHaveBeenCalled()
    expect(f.synthesize).toHaveBeenCalledOnce()
    expect(f.createURL).not.toHaveBeenCalled()
  })

  it('ignores old play promise rejection after interruption and a new replay', async () => {
    const f = fixture(); let rejectOld!: (value: Error) => void
    f.player.play.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectOld = reject }))
    f.speech.enqueue({ key: 'old', text: 'old' }); await tick()
    f.speech.stop(); f.speech.replay({ key: 'new', text: 'new' }); await tick()
    f.player.emit('playing'); rejectOld(new Error('late')); await tick()
    expect(f.latest()).toMatchObject({ key: 'new', status: 'playing' })
    f.speech.stop(); f.player.emit('playing'); f.player.emit('ended'); f.player.emit('error')
    expect(f.latest()?.status).toBe('idle')
    expect(f.player.src).toBe('')
  })

  it('offers supported gesture resume after autoplay rejection without resynthesizing', async () => {
    const f = fixture()
    f.player.play.mockRejectedValueOnce(Object.assign(new Error('gesture required'), { name: 'NotAllowedError' }))
    f.speech.enqueue({ key: 'a', text: 'text' }); await tick()
    expect(f.latest()?.status).toBe('blocked')
    f.speech.resume()
    // play() is invoked synchronously in the user gesture, before microtasks.
    expect(f.player.play).toHaveBeenCalledTimes(2)
    expect(f.synthesize).toHaveBeenCalledOnce()
    f.player.emit('playing'); expect(f.latest()?.status).toBe('playing')
    f.speech.stop()
  })

  it('clears the queue on decode or synthesis error and never retries automatically', async () => {
    const f = fixture()
    f.speech.enqueue({ key: 'a', text: 'text' }); f.speech.enqueue({ key: 'b', text: 'queued' }); await tick()
    f.player.emit('error'); await tick()
    expect(f.latest()?.status).toBe('error'); expect(f.synthesize).toHaveBeenCalledOnce()
    expect(f.player.src).toBe(''); expect(f.revokeURL).toHaveBeenCalledOnce()
    f.speech.replay({ key: 'a', text: 'text' }); await tick()
    expect(f.synthesize).toHaveBeenCalledTimes(2)
    f.speech.stop()
    const failed = fixture(vi.fn(async () => { throw new Error('lost response') }))
    failed.speech.enqueue({ key: 'lost', text: 'text' }); await tick()
    expect(failed.latest()?.status).toBe('error'); expect(failed.synthesize).toHaveBeenCalledOnce()
  })

  it('chunks by Unicode code points and preserves all non-whitespace text', () => {
    const text = '🙂'.repeat(1203)
    const chunks = speechChunks(text)
    expect(chunks.map(chunk => Array.from(chunk).length)).toEqual([500, 500, 203])
    expect(chunks.join('')).toBe(text)
    expect(speechChunks('')).toEqual([])
  })
})

describe('live reply eligibility', () => {
  it('does not read history, but reads an observed live reply only once despite identity replacement', () => {
    const replies = new LiveSpeechReplies()
    expect(replies.update([reply('history')], true, false)).toEqual([])
    expect(replies.update([{ ...reply('live', true), runtimeRunId: 'live-run' }], true, false)).toEqual([])
    const final = { ...reply('live'), id: 'persisted-render-id' }
    expect(replies.update([final], true, false)).toEqual([{ key: speechKey(final), text: '你好' }])
    expect(replies.update([final], true, false)).toEqual([])
  })
  it('consumes interrupted streams, disabled/hidden panes and history refreshes', () => {
    const replies = new LiveSpeechReplies(), stream = reply('old', true)
    replies.update([stream], true, false); replies.interrupt([stream])
    expect(replies.update([reply('old')], true, false)).toEqual([])
    replies.update([reply('hidden', true)], false, false)
    expect(replies.update([reply('hidden')], true, false)).toEqual([])
    expect(replies.update([reply('history')], true, true)).toEqual([])
    expect(replies.update([reply('history')], true, false)).toEqual([])
    replies.update([reply('fresh', true)], true, false)
    expect(replies.update([reply('fresh')], true, false)).toHaveLength(1)
  })
  it('allows a new retry run on the same turn and silences later segments of an interrupted run', () => {
    const replies = new LiveSpeechReplies()
    const original = { ...reply('one', true), runtimeRunId: 'old-run', invocationId: 'invocation' }
    replies.update([original], true, false); replies.interrupt([original])
    const later = { ...reply('steer', true), runtimeRunId: 'old-run' }
    replies.update([later], true, false)
    expect(replies.update([{ ...later, streaming: false }], true, false)).toEqual([])
    const retry = { ...original, runtimeRunId: 'retry-run', invocationId: 'retry-invocation' }
    replies.update([retry], true, false)
    expect(replies.update([{ ...retry, streaming: false }], true, false)).toHaveLength(1)
  })
  it('keeps explicit audio attachments and excludes tools, reasoning and failures', () => {
    const audio = reply('file')
    if (audio.role !== 'assistant') throw new Error('fixture')
    audio.messages.push({ id: 2, type: 'attachments', attachments: [{ type: 'voice', url: '/fixture.wav', mime: 'audio/wav' }] })
    expect(speechText(audio)).toBe('')
    const replies = new LiveSpeechReplies(); replies.update([reply('error', true)], true, false)
    const error = reply('error')
    if (error.role !== 'assistant') throw new Error('fixture')
    error.messages.push({ id: 2, type: 'error', content: 'failed' })
    expect(replies.update([error], true, false)).toEqual([])
  })
})

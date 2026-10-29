// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ChatAssistantTurn } from '@/store/chat/types'
import { ChatReadAloud, LiveSpeechReplies, speechChunks, speechText } from './chat-read-aloud'
import { openSpeechStream } from './speech-audio-stream'
import { visibleSpeechText } from './speech-visible-text'

function reply(text: string, streaming = true): ChatAssistantTurn {
  return { id: 'render', turnId: 'turn', runtimeRunId: 'run', role: 'assistant', timestamp: '', streaming, messages: [{ id: 0, type: 'text', content: text }] }
}
const tick = async () => { for (let i = 0; i < 20; i++) await Promise.resolve() }
afterEach(() => vi.unstubAllGlobals())

describe('visible speech projection', () => {
  it('reads rich labels and reference labels while retaining visible naked URLs, code, tables and currency', () => {
    const text = visibleSpeechText('# 标题\n请看[**显示文字**](https://hidden.test/secret "hidden title") 与 [引用][ref]。\n\n[ref]: https://hidden.test/ref\n\n可见 https://visible.test/path\n\n代码 `x < 2`\n\n```js\nconst x = 3\n```\n\n|甲|乙|\n|-|-|\n|1|2|\n\nUS$3–6/月')
    for (const part of ['标题', '显示文字', '引用', 'https://visible.test/path', 'x < 2', 'const x = 3', '甲 乙', '1 2', 'US$3–6/月']) expect(text).toContain(part)
    expect(text).not.toMatch(/hidden|\*\*|```|\[ref\]/u)
  })
  it('does not read hidden HTML, attributes, image destinations or math source annotations', () => {
    const text = visibleSpeechText('<span title="secret">可见</span><span hidden>隐藏</span><span aria-hidden="true">视觉正文</span>\n\n![图](https://secret/image)\n\n\\(x^2\\)')
    expect(text).toContain('可见'); expect(text).toContain('x'); expect(text).toContain('2')
    expect(text).toContain('视觉正文')
    expect(text).not.toMatch(/secret|隐藏|x\^2/u)
  })
  it('never leaks a partial inline link target across token boundaries', () => {
    expect(visibleSpeechText('开头 [显', true)).toBe('开头')
    expect(visibleSpeechText('开头 [显示](https://hid', true)).toBe('开头 显示')
    expect(visibleSpeechText('开头 [显示](https://hidden.test/)', true)).toBe('开头 显示')
    expect(visibleSpeechText('最终可见的 [普通括号')).toContain('[普通括号')
  })
})
describe('incremental reply scheduling', () => {
  it('starts before completion and emits only unseen visible suffixes', () => {
    const live = new LiveSpeechReplies()
    expect(live.update([reply('第一句。请看')], true, false)).toEqual([{ key: 'turn', text: '第一句。' }])
    expect(live.update([reply('第一句。请看[文档](https://secret.test)')], true, false, true)).toEqual([{ key: 'turn', text: '请看文档' }])
    expect(live.update([reply('第一句。请看[文档](https://secret.test)，结束。', false)], true, false)).toEqual([{ key: 'turn', text: '，结束。' }])
    expect(live.update([reply('第一句。请看[文档](https://secret.test)，结束。', false)], true, false)).toEqual([])
  })
  it('flushes a short opening without waiting for end; history and interrupted runs stay silent', () => {
    const live = new LiveSpeechReplies(), message = reply('短开头')
    expect(live.update([message], true, false)).toEqual([])
    expect(live.update([message], true, false, true)).toEqual([{ key: 'turn', text: '短开头' }])
    live.interrupt([message])
    expect(live.update([reply('短开头后续。')], true, false, true)).toEqual([])
    expect(new LiveSpeechReplies().update([reply('历史。', false)], true, false)).toEqual([])
  })
  it('preserves assistant-only and explicit-audio exclusions', () => {
    const message = reply('字幕。')
    message.messages.push({ id: 1, type: 'attachments', attachments: [{ type: 'audio', url: '/fixture', mime: 'audio/mpeg' }] })
    expect(speechText(message)).toBe('')
  })
})
class BufferFixture extends EventTarget {
  chunks: Uint8Array[] = []
  appendBuffer(bytes: ArrayBuffer) { this.chunks.push(new Uint8Array(bytes)); queueMicrotask(() => this.dispatchEvent(new Event('updateend'))) }
}
class MediaFixture extends EventTarget {
  static instance: MediaFixture
  static isTypeSupported(type: string) { return type === 'audio/mpeg' }
  readyState = 'open'
  buffer = new BufferFixture()
  ended = false
  constructor() { super(); MediaFixture.instance = this }
  addSourceBuffer() { return this.buffer }
  endOfStream() { this.ended = true }
}
function streamFixture() {
  vi.stubGlobal('MediaSource', MediaFixture)
  let feed!: ReadableStreamDefaultController<Uint8Array>
  const cancel = vi.fn()
  const stream = new ReadableStream<Uint8Array>({ start(controller) { feed = controller }, cancel })
  const abort = new AbortController()
  const source = openSpeechStream({ stream, contentType: 'audio/mpeg' }, abort.signal, () => 'blob:stream')
  MediaFixture.instance.dispatchEvent(new Event('sourceopen'))
  return { source, abort, feed, cancel, media: MediaFixture.instance }
}
describe('incremental native decoder input', () => {
  it('appends the first provider bytes before EOF and only ends after the remaining bytes', async () => {
    const f = streamFixture()
    f.feed.enqueue(new Uint8Array([1, 2])); await tick()
    expect(f.media.buffer.chunks.map(chunk => [...chunk])).toEqual([[1, 2]])
    expect(f.media.ended).toBe(false)
    f.feed.enqueue(new Uint8Array([3, 4])); f.feed.close(); await f.source.done
    expect(f.media.buffer.chunks.map(chunk => [...chunk])).toEqual([[1, 2], [3, 4]])
    expect(f.media.ended).toBe(true)
  })
  it('cancels an unfinished upstream and rejects without feeding more bytes', async () => {
    const f = streamFixture(), settled = f.source.done.catch(error => error)
    f.feed.enqueue(new Uint8Array([1, 2])); await tick(); f.abort.abort()
    expect(await settled).toBeDefined(); expect(f.cancel).toHaveBeenCalledOnce()
    expect(f.media.ended).toBe(false)
  })
  it('fails explicitly for unsupported codecs without collecting a full Blob', async () => {
    vi.stubGlobal('MediaSource', MediaFixture)
    const cancel = vi.fn(), stream = new ReadableStream<Uint8Array>({ cancel })
    expect(() => openSpeechStream({ stream, contentType: 'audio/wav' }, new AbortController().signal, () => '')).toThrow(/unsupported/)
    await tick(); expect(cancel).toHaveBeenCalledOnce()
  })
  it('manual replay uses bounded requests and cancels a pending streaming request synchronously', async () => {
    expect(speechChunks('中'.repeat(1100), 80).map(chunk => Array.from(chunk).length)).toEqual([80, 500, 500, 20])
    let resolve!: (value: Blob) => void
    const synthesize = vi.fn((_text: string, _signal: AbortSignal) => new Promise<Blob>(done => { resolve = done }))
    const player = Object.assign(new EventTarget(), { src: '', play: vi.fn(async () => {}), pause: vi.fn(), load: vi.fn() })
    const speech = new ChatReadAloud({ synthesize, createPlayer: () => player, createURL: () => 'blob:test', revokeURL: vi.fn(), onState: vi.fn() })
    speech.replay({ key: 'manual', text: '正文'.repeat(300) })
    expect(Array.from(synthesize.mock.calls[0]![0]).length).toBeLessThanOrEqual(80)
    speech.stop(); expect(synthesize.mock.calls[0]![1].aborted).toBe(true)
    resolve(new Blob(['late'], { type: 'audio/mpeg' })); await tick()
    expect(player.play).not.toHaveBeenCalled(); expect(synthesize).toHaveBeenCalledOnce()
  })
})

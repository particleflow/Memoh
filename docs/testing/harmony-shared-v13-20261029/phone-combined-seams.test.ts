// @vitest-environment jsdom
import { createApp, h, nextTick, provide, ref, type App } from 'vue'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { useChatReadAloud } from '@candidate/useChatReadAloud'
import { chatWorkspaceVisibleKey } from '@candidate/chat-workspace-visibility'
import { renderedSpeechText } from '@candidate/speech-rendered-text'
import type { ChatMessage } from '@/store/chat/types'

const api = vi.hoisted(() => ({ playback: vi.fn() }))
vi.mock('@memohai/sdk', () => ({ postBotsByBotIdTtsPlayback: api.playback }))
let app: App | undefined, root: HTMLElement, gesture: boolean
let feeds: Array<{ feed: ReadableStreamDefaultController<Uint8Array>, signal: AbortSignal, cancelled: boolean }>
let players: Player[], urls: Blob[]
class Player extends EventTarget {
  src = ''; calls = 0
  async play() {
    this.calls++
    if (!gesture) throw Object.assign(new Error('controlled gesture required'), { name: 'NotAllowedError' })
    this.dispatchEvent(new Event('playing'))
  }
  pause() { this.dispatchEvent(new Event('pause')) }
  load() {}
  constructor() { super(); players.push(this) }
}
const settle = async () => { for (let i=0;i<35;i++) { await Promise.resolve(); await nextTick() } }
const msg = (id: string, streaming = true): ChatMessage => ({id,turnId:'turn-'+id,runtimeRunId:'run-'+id,role:'assistant',streaming,timestamp:'',messages:[{id:0,type:'text',content:'Visible response.'}]})
beforeEach(() => {
  feeds=[];players=[];urls=[];gesture=true
  vi.stubGlobal('MediaSource',undefined);vi.stubGlobal('Audio',Player)
  vi.stubGlobal('URL',{createObjectURL:(blob:Blob)=>{urls.push(blob);return 'blob:combined-'+urls.length},revokeObjectURL:vi.fn()})
  api.playback.mockReset().mockImplementation(async (options) => {
    let item!:typeof feeds[number]
    const stream=new ReadableStream<Uint8Array>({start(feed){item={feed,signal:options.signal,cancelled:false};feeds.push(item)},cancel(){item.cancelled=true}})
    return {data:stream,response:new Response(null,{headers:{'Content-Type':'audio/wav'}})}
  })
})
afterEach(()=>{app?.unmount();app=undefined;root?.remove();vi.unstubAllGlobals()})
function mount(initial:ChatMessage[]=[]) {
  const messages=ref<ChatMessage[]>(initial),visible=ref(true),enabled=ref(true),draft=ref('')
  let speech!:ReturnType<typeof useChatReadAloud>
  root=document.createElement('div');document.body.append(root)
  const child={setup(){speech=useChatReadAloud({botId:ref('bot'),scope:ref('bot:fixed'),enabled,loading:ref(false),draft,messages,messageElement:id=>root.querySelector(`[data-message="${id}"]`),bodyRoot:()=>root.querySelector('[data-root]')});return()=>h('div',{'data-root':''},messages.value.map(message=>h('div',{'data-message':message.id},[h('div',{'data-speech-body':'','data-speech-block-id':'0'},message.messages[0]?.type==='text'?message.messages[0].content:'')])))} }
  app=createApp({setup(){provide(chatWorkspaceVisibleKey,visible);return()=>h(child)}})
  app.mount(root)
  return {messages,visible,enabled,draft,get speech(){return speech}}
}
it('retains the current visibility/task-location stop gates across pending WAV and post-DOM streaming collection',async()=>{
  const f=mount([msg('history',false)]);await settle();expect(api.playback).not.toHaveBeenCalled()
  f.messages.value.push(msg('live'));await settle();expect(api.playback).toHaveBeenCalledOnce()
  feeds[0]!.feed.enqueue(new Uint8Array([1,2]));await settle();expect(urls).toHaveLength(0)
  f.visible.value=false;await settle();expect(feeds[0]!.signal.aborted).toBe(true);expect(feeds[0]!.cancelled).toBe(true);expect(f.speech.state.value.status).toBe('idle')
  f.visible.value=true;await settle();expect(api.playback).toHaveBeenCalledOnce()
  f.messages.value.push(msg('fresh'));await settle();expect(api.playback).toHaveBeenCalledTimes(2)
  feeds[1]!.feed.enqueue(new Uint8Array([3]));feeds[1]!.feed.close();await settle();expect(urls).toHaveLength(1);expect(f.speech.state.value.status).toBe('playing')
  f.enabled.value=false;expect(f.speech.state.value.status).toBe('idle');expect(players[0]!.src).toBe('')
  f.enabled.value=true;await settle();expect(api.playback).toHaveBeenCalledTimes(2)
})
it('keeps the same-message replay gesture synchronous after one-response WAV fallback, without resynthesis',async()=>{
  const message=msg('manual',false),f=mount([message]);await settle();expect(renderedSpeechText(message,root.querySelector('[data-message="manual"]')),root.outerHTML).toBe('Visible response.');gesture=false;f.speech.replay(message);await settle()
  feeds[0]!.feed.enqueue(new Uint8Array([1]));feeds[0]!.feed.close();await settle();expect(f.speech.state.value.status).toBe('blocked')
  const source=players[0]!.src;gesture=true;f.speech.replay(message);expect(f.speech.state.value.status).toBe('playing');expect(players[0]!.src).toBe(source);expect(players[0]!.calls).toBe(2);expect(api.playback).toHaveBeenCalledOnce();expect(urls).toHaveLength(1)
  f.draft.value='input interrupts';expect(f.speech.state.value.status).toBe('idle');players[0]!.dispatchEvent(new Event('playing'));await settle();expect(f.speech.state.value.status).toBe('idle')
})

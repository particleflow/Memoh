import { afterEach, expect, it, vi } from 'vitest'
import { client } from '@memohai/sdk/client'
import {
  getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskId,
  getUsersMe,
  postBotsByBotIdContainerFsUpload,
  postBotsByBotIdTtsPlayback,
} from '@memohai/sdk'
import { setupApiClient } from './api-client'
import { serverFirstAudioMS } from '@/pages/home/composables/speech-timing'

const base = 'https://logical.example.invalid/api'
const physical = 'https://physical.example.invalid'
const bot = '10000000-0000-4000-8000-000000000001'
const session = '20000000-0000-4000-8000-000000000001'
const operation = '30000000-0000-4000-8000-000000000001'
const token = 'synthetic-no-authority'

afterEach(() => {
  client.interceptors.request.clear()
  vi.unstubAllGlobals()
})

it('file policy leaves timing playback on its original authenticated cancellable stream', async () => {
  const requests: Request[] = []
  const audio = new Uint8Array([73, 68, 51, 0, 255])
  vi.stubGlobal('localStorage', { getItem: () => token })
  client.interceptors.request.clear()
  setupApiClient({ baseUrl: base, fetch: async input => {
    const request = input as Request
    requests.push(request)
    if (request.url.endsWith('/users/me')) return Response.json({ id: 'synthetic-owner', network_policy: { version: 2, endpoint: { logical_origin: new URL(base).origin, physical_origin: physical } } })
    if (request.url.endsWith('/tts/playback')) return new Response(audio, { headers: { 'Content-Type': 'audio/mpeg', 'Server-Timing': 'speech_first_audio;dur=18.5' } })
    return Response.json({ path: '/data/original.bin' })
  } })
  await getUsersMe({ throwOnError: true })
  await postBotsByBotIdContainerFsUpload({ path: { bot_id: bot }, body: { path: '/data/original.bin', file: new File(['unchanged'], 'original.bin') } as never, throwOnError: true })
  const controller = new AbortController()
  const playback = await postBotsByBotIdTtsPlayback({ path: { bot_id: bot }, body: { text: 'controlled text' }, signal: controller.signal, headers: { 'X-Request-ID': 'speech-timing-synthetic' }, parseAs: 'stream', throwOnError: true })
  expect(requests.map(request => request.url)).toEqual([base + '/users/me', physical + `/api/bots/${bot}/container/fs/upload`, base + `/bots/${bot}/tts/playback`])
  const spoken = requests[2]!
  expect(spoken.headers.get('Authorization')).toBe('Bearer ' + token)
  expect(spoken.headers.get('X-Request-ID')).toBe('speech-timing-synthetic')
  expect(await spoken.json()).toEqual({ text: 'controlled text' })
  expect(serverFirstAudioMS(playback.response.headers.get('Server-Timing'))).toBe(18.5)
  if (!(playback.data instanceof ReadableStream)) throw new Error('Expected playback stream')
  expect(new Uint8Array(await new Response(playback.data).arrayBuffer())).toEqual(audio)
  controller.abort()
  expect(spoken.signal.aborted).toBe(true)
  expect(requests).toHaveLength(3)
})

it('mapped Files failure does not replay a write or reroute PR93 history and denied control reads', async () => {
  const requests: Request[] = []
  let denied = false
  const original = { task_id: operation, task_type: 'computer_operation', status: 'unknown', session_id: session }
  vi.stubGlobal('localStorage', { getItem: () => token })
  client.interceptors.request.clear()
  setupApiClient({ baseUrl: base, fetch: async input => {
    const request = input as Request
    requests.push(request)
    if (request.url.endsWith('/users/me')) return Response.json({ id: 'synthetic-owner', network_policy: { version: 2, endpoint: { logical_origin: new URL(base).origin, physical_origin: physical } } })
    if (request.url.endsWith('/container/fs/upload')) throw new TypeError('controlled transport loss')
    return Response.json(denied ? { error: 'denied' } : original, { status: denied ? 403 : 200 })
  } })
  await getUsersMe({ throwOnError: true })
  await expect(postBotsByBotIdContainerFsUpload({ path: { bot_id: bot }, body: { path: '/data/original.bin', file: new File(['unchanged'], 'original.bin') } as never, throwOnError: true })).rejects.toThrow('controlled transport loss')
  const path = { bot_id: bot, session_id: session, task_type: 'computer_operation' as const, task_id: operation }
  const result = await getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskId({ path, throwOnError: true })
  expect(result.data).toEqual(original)
  denied = true
  const refused = await getBotsByBotIdSessionsBySessionIdTasksByTaskTypeByTaskId({ path })
  expect(refused.response?.status).toBe(403)
  expect(refused.error).toEqual({ error: 'denied' })
  expect(requests.slice(2).map(request => request.url)).toEqual(Array(2).fill(base + `/bots/${bot}/sessions/${session}/tasks/computer_operation/${operation}`))
  expect(requests.every(request => request.headers.get('Authorization') === 'Bearer ' + token)).toBe(true)
  expect(requests.filter(request => request.method === 'POST')).toHaveLength(1)
  expect(requests).toHaveLength(4)
})

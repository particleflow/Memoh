// Scripted local service for the phone acceptance harness.
//
// Serves the packaged web bundle verbatim, answers the Memoh HTTP surface that
// bundle actually calls, and replays a recorded stream over its WebSocket.
// It never contacts a real NAS and never synthesizes audio through a provider:
// audio bytes are a local silent MP3 so the client's decoder path is exercised.
import { createServer } from 'node:http'
import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync, statSync, mkdirSync } from 'node:fs'
import { extname, join, normalize, sep } from 'node:path'
import { tmpdir } from 'node:os'

const MIME = {
  '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8', '.css': 'text/css; charset=utf-8',
  '.svg': 'image/svg+xml', '.png': 'image/png', '.jpg': 'image/jpeg', '.jpeg': 'image/jpeg', '.webp': 'image/webp',
  '.ico': 'image/x-icon', '.json': 'application/json', '.webmanifest': 'application/manifest+json',
  '.wasm': 'application/wasm', '.woff': 'font/woff', '.woff2': 'font/woff2', '.ttf': 'font/ttf', '.gif': 'image/gif',
}

export const BOT_ID = 'b0000000-0000-4000-8000-000000000001'
export const SESSION_ID = 'a0000000-0000-4000-8000-000000000001'
export const OTHER_SESSION_ID = 'a0000000-0000-4000-8000-000000000002'
const SPEECH_IDENTITY = 'stub-speech-identity-v1'
// Thrown inside a replay script when the harness has moved on to another
// scenario; it is a cancellation, never a script error.
const SCENARIO_RESET = Symbol('scenario-reset')
const ACK_TEXT = '收到，我来处理。'
const REASONING = ['先看一下附件，', '然后给出结论。']
const BODY = ['第一句已经说完。', '第二句跟上来了。', '第三句也来了。', '最后一句收尾。']
// Gate 2 body: long enough that the rolled-back text of a failed task and the
// text that reads after it are both observable as distinct speech requests.
export const FAIL_TASK_BODY = [
  '第一段话详细说明了今天上午在路上遇到的全部情况。',
  '第二段话完整列出了三个需要后续跟进的要点事项。',
  '第三段话给出了目前可以确认的结论与处理意见。',
  '第四段话是最后需要补充说明的风险与提醒内容。',
  '第五段话重申了下一步的时间安排与负责人。',
  '第六段话是整段回复最后的收尾与致谢。',
]
const HISTORY_BODY = '这是历史回复，加载时不应朗读。'
// The post-steer continuation of the steer scenario: its own sentences, so the
// harness can tell the text only the claimed follow-up produced apart from the
// first reply the user interrupted by sending the follow-up.
export const STEER_BODY = [
  '追问后的第一段回答说明了新的安排。',
  '追问后的第二段给出了进一步的解释。',
  '追问后的第三段是这轮回答的收尾。',
]
// A receipt round is the turn the server opens from a finished task: every row
// of that turn carries the task_result input source, and presence never posts
// an acknowledgement for it (task_result input is ineligible).
function receiptSource(sessionId) {
  return { kind: 'task_result', bot_id: BOT_ID, session_id: sessionId, task_kind: 'native_task', task_id: 'task-receipt-0001' }
}

export function nowIso() { return new Date().toISOString() }

// Extract the single file part of a multipart/form-data body. Returns the raw
// bytes and the client-declared mime type; falls back to the whole body when
// the framing is unexpected, which the hash check downstream will reject.
function multipartFile(body, contentType) {
  const match = /boundary=(?:"([^"]+)"|([^;]+))/i.exec(contentType)
  if (!match) return { bytes: body, mime: contentType || '' }
  const boundary = Buffer.from('--' + (match[1] ?? match[2]).trim())
  let at = body.indexOf(boundary)
  while (at !== -1) {
    const start = body.indexOf(Buffer.from('\r\n\r\n'), at)
    const next = body.indexOf(boundary, start)
    if (start === -1 || next === -1) break
    const header = body.subarray(at, start).toString('utf8')
    if (/filename=/i.test(header)) {
      const mime = /content-type:\s*([^\r\n]+)/i.exec(header)?.[1]?.trim()
      const from = start + 4
      return { bytes: body.subarray(from, next - 2), mime: mime || '' }
    }
    at = next
  }
  return { bytes: body, mime: '' }
}

// Deterministic pseudo-image bytes (PNG signature + a fixed LCG stream): the
// same size always yields the same sha256, so scenario evidence is reproducible
// across runs, reports and the emulator level.
function botImageBytes(size) {
  const bytes = Buffer.alloc(size)
  Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]).copy(bytes, 0)
  let state = 0x2d6ef1 >>> 0
  for (let at = 8; at < size; at++) {
    state = (Math.imul(state, 0x41c64e6d) + 0x3039) >>> 0
    bytes[at] = (state >>> 16) & 0xff
  }
  return bytes
}

function silentMp3(seconds = 30) {
  const dir = join(tmpdir(), 'memoh-acceptance-audio')
  const file = join(dir, `silence-${seconds}.mp3`)
  if (existsSync(file) && statSync(file).size > 0) return readFileSync(file)
  mkdirSync(dir, { recursive: true })
  execFileSync('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-f', 'lavfi', '-i', 'anullsrc=r=24000:cl=mono', '-t', String(seconds), '-c:a', 'libmp3lame', '-b:a', '16k', '-y', file], { stdio: 'ignore' })
  return readFileSync(file)
}

function wsAccept(key) {
  return createHash('sha1').update(key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64')
}

function encodeFrame(text) {
  const payload = Buffer.from(text, 'utf8')
  let header
  if (payload.length < 126) header = Buffer.from([0x81, payload.length])
  else if (payload.length < 65536) { header = Buffer.alloc(4); header[0] = 0x81; header[1] = 126; header.writeUInt16BE(payload.length, 2) }
  else { header = Buffer.alloc(10); header[0] = 0x81; header[1] = 127; header.writeBigUInt64BE(BigInt(payload.length), 2) }
  return Buffer.concat([header, payload])
}

function decodeFrames(buffer) {
  const messages = []
  let offset = 0
  while (offset + 2 <= buffer.length) {
    const first = buffer[offset], second = buffer[offset + 1]
    const opcode = first & 0x0f
    const masked = (second & 0x80) !== 0
    let length = second & 0x7f
    let cursor = offset + 2
    if (length === 126) { if (cursor + 2 > buffer.length) break; length = buffer.readUInt16BE(cursor); cursor += 2 }
    else if (length === 127) { if (cursor + 8 > buffer.length) break; length = Number(buffer.readBigUInt64BE(cursor)); cursor += 8 }
    const mask = masked ? buffer.subarray(cursor, cursor + 4) : null
    if (masked) cursor += 4
    if (cursor + length > buffer.length) break
    const payload = Buffer.from(buffer.subarray(cursor, cursor + length))
    if (mask) for (let i = 0; i < payload.length; i++) payload[i] ^= mask[i % 4]
    messages.push({ opcode, text: payload.toString('utf8') })
    offset = cursor + length
  }
  return { messages, rest: buffer.subarray(offset) }
}

export class StubService {
  constructor({ bundleDir, apiOrigin = '', port = 0 }) {
    this.bundleDir = bundleDir
    this.apiOrigin = apiOrigin
    this.port = port
    this.speech = []
    this.apiLog = []
    this.clientMessages = []
    this.scriptErrors = []
    this.frames = []
    this.sockets = new Set()
    this.runtime = new Map()
    this.history = new Map()
    this.generation = 0
    this.mode = 'stream'
    this.delay = 700
    // Upload pacing: bytes per paced segment (0 = legacy per-chunk delay only)
    // and the per-segment sleep; the staged Bot attachment of a bot-image reply.
    this.uploadPaceBytes = 0
    this.uploadChunkDelayMs = 0
    this.downloadChunkDelayMs = 0
    this.botAttachment = null
    // Gap between the last sentence frame and the completed frame; null keeps
    // the ordinary per-step delay. A long settle separates "read on the first
    // sentence" from "read when the reply settles" by whole seconds.
    this.settleDelay = null
    this.startDelay = 800
    this.t0 = 0
    this.mp3 = silentMp3(30)
    this.shortMp3 = silentMp3(1)
    this.postCount = 0
    this.bodyPostCount = 0
    this.firstSentenceAt = null
    this.lastSentenceAt = null
    this.settledAt = null
    // Steer scenario: the queued follow-up claimed mid-run, and the timing of
    // the continuation it produced (same clock base as the speech entries).
    // A message the composer sends while a run streams does not travel as a
    // websocket message at all: the client enqueues it through the session
    // input queues over REST, and the running turn claims it as a steer.
    this.queueItems = []
    this.scriptActive = false
    this.steerDone = false
    this.steerClaimedAt = null
    this.steerFirstSentenceAt = null
    this.steerSettledAt = null
    this.server = createServer((req, res) => this.handle(req, res).catch(error => {
      res.writeHead(500, { 'content-type': 'application/json' })
      res.end(JSON.stringify({ message: String(error && error.message || error) }))
    }))
    this.server.on('upgrade', (req, socket) => this.upgrade(req, socket))
  }

  async start() {
    await new Promise(resolve => this.server.listen(this.port, '127.0.0.1', resolve))
    this.port = this.server.address().port
    // The bundled URL validator rejects loopback endpoints, exactly as it does
    // in the product. Chromium maps this name onto 127.0.0.1 for the run.
    this.apiOrigin = `http://stub.test:${this.port}`
    return this.apiOrigin
  }

  async stop() {
    for (const socket of this.sockets) { try { socket.destroy() } catch {} }
    await new Promise(resolve => this.server.close(resolve))
  }

  control(path, body, method = 'POST') {
    return fetch(`http://127.0.0.1:${this.port}${path}`, method === 'GET' ? { method } : { method, body: JSON.stringify(body ?? {}) }).then(response => response.json())
  }

  mark() { this.t0 = Date.now(); this.speech.length = 0; this.apiLog.length = 0; this.postCount = 0; this.bodyPostCount = 0; this.firstSentenceAt = null; this.lastSentenceAt = null; this.settledAt = null; this.steerClaimedAt = null; this.steerFirstSentenceAt = null; this.steerSettledAt = null }

  async handle(req, res) {
    const url = new URL(req.url, this.apiOrigin)
    const path = url.pathname
    if (req.method === 'OPTIONS') {
      res.writeHead(204, { 'access-control-allow-origin': '*', 'access-control-allow-headers': '*', 'access-control-allow-methods': '*' })
      res.end()
      return
    }
    if (path === '/__control/scenario') {
      const body = JSON.parse(await this.readBody(req) || '{}')
      if (body.scenario) this.mode = body.scenario
      if (body.delay !== undefined) this.delay = body.delay
      this.settleDelay = body.settleDelay ?? null
      this.queueItems = []
      this.steerDone = false
      this.mark()
      this.json(res, 200, { ok: true })
      return
    }
    if (path === '/__control/speech') {
      this.json(res, 200, { t0: this.t0, firstSentenceAt: this.firstSentenceAt, lastSentenceAt: this.lastSentenceAt, settledAt: this.settledAt, steerClaimedAt: this.steerClaimedAt, steerFirstSentenceAt: this.steerFirstSentenceAt, steerSettledAt: this.steerSettledAt, entries: this.speech })
      return
    }
    if (path === '/__control/mark') { this.mark(); this.json(res, 200, { ok: true }); return }
    if (path === '/__control/requests') { this.json(res, 200, { items: this.apiLog }); return }
    if (path === '/__control/messages') { this.json(res, 200, { items: this.clientMessages }); return }
    if (path === '/__control/frames') { this.json(res, 200, { items: this.frames, speech: this.speech }); return }
    if (path === '/__control/reset') {
      // A scenario boundary: the script's own history must not be read back as
      // the next scenario's baseline, so it is dropped together with the
      // projections. Scenario configuration survives.
      this.generation += 1
      this.history.clear(); this.runtime.clear(); this.queueItems = []; this.steerDone = false; this.mark()
      this.json(res, 200, { ok: true, generation: this.generation })
      return
    }
    if (path === '/__control/history') {
      this.history.set(SESSION_ID, this.historyTurns())
      this.json(res, 200, { ok: true })
      return
    }
    if (path === '/__control/upload-config') {
      const body = JSON.parse(await this.readBody(req) || '{}')
      if (body.chunkDelayMs !== undefined) this.uploadChunkDelayMs = body.chunkDelayMs
      if (body.paceBytes !== undefined) this.uploadPaceBytes = body.paceBytes
      if (body.downloadChunkDelayMs !== undefined) this.downloadChunkDelayMs = body.downloadChunkDelayMs
      this.uploadEvents = []
      this.json(res, 200, { ok: true })
      return
    }
    // Stage the original a scripted Bot reply will attach: deterministic bytes
    // (stable sha256 across runs), registered in the media store so the client
    // verified-read path can fetch it by content hash. failAfterChunks breaks
    // the paced download body part-way to drive the transfer-failure state.
    if (path === '/__control/bot-attachment') {
      const body = JSON.parse(await this.readBody(req) || '{}')
      const size = Number(body.sizeBytes)
      if (!Number.isSafeInteger(size) || size <= 0) {
        this.botAttachment = null
        this.json(res, 200, { ok: true, cleared: true })
        return
      }
      const bytes = botImageBytes(size)
      const hash = createHash('sha256').update(bytes).digest('hex')
      ;(this.mediaFiles ??= new Map()).set(hash, { mime: 'image/png', bytes })
      this.botAttachment = { hash, size,
        failAfterChunks: Number.isSafeInteger(body.failAfterChunks) && body.failAfterChunks > 0 ? body.failAfterChunks : null }
      this.json(res, 200, { ok: true, content_hash: hash, size_bytes: size, failAfterChunks: this.botAttachment.failAfterChunks })
      return
    }
    if (path === '/__control/uploads') {
      this.json(res, 200, { events: this.uploadEvents ?? [] })
      return
    }
    if (path.startsWith('/api/')) return this.api(req, res, path.slice(4), url)
    // The packaged build derives its base URL from the deployment origin, so
    // SDK calls arrive without an /api prefix. Anything that is not a bundle
    // file belongs to the scripted service.
    if (!extname(path)) return this.api(req, res, path, url)
    return this.static(res, path)
  }

  json(res, code, value) {
    res.writeHead(code, { 'content-type': 'application/json', 'cache-control': 'no-store', 'access-control-allow-origin': '*' })
    res.end(JSON.stringify(value))
  }

  readBody(req) {
    return new Promise(resolve => {
      const chunks = []
      req.on('data', chunk => chunks.push(chunk))
      req.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')))
    })
  }

  static(res, path) {
    const relative = decodeURIComponent(path === '/' ? '/index.html' : path)
    const root = normalize(this.bundleDir)
    const target = normalize(join(root, relative))
    if (target !== root && !target.startsWith(root + sep)) { res.writeHead(403); res.end(); return }
    if (!existsSync(target) || statSync(target).isDirectory()) { res.writeHead(404); res.end('not found'); return }
    let data = readFileSync(target)
    // The signed package carries the deployment config filled by the Windows
    // build; inject the local origin at serve time, never on disk.
    if (relative === '/memoh_client.json') {
      const config = JSON.parse(data.toString('utf8'))
      config.apiOrigin = this.apiOrigin
      data = Buffer.from(JSON.stringify(config))
    }
    res.writeHead(200, { 'content-type': MIME[extname(target).toLowerCase()] ?? 'application/octet-stream', 'cache-control': 'no-store' })
    res.end(data)
  }

  async api(req, res, path, url) {
    this.apiLog.push({ method: req.method, path, at: nowIso() })
    if (path === '/users/me') {
      this.json(res, 200, { id: 'user-stub', username: 'acceptance', role: 'admin', display_name: 'Acceptance', avatar_url: '', timezone: 'UTC', metadata: { onboarding_completed: true } })
      return
    }
    if (path === '/ping') { this.json(res, 200, { status: 'ok' }); return }
    if (path === '/users/me/channel-identities') { this.json(res, 200, []); return }
    if (path === '/bots' && req.method === 'GET') {
      this.json(res, 200, { items: [{ id: BOT_ID, name: 'stub-bot', display_name: 'Stub Bot', status: 'ready', is_active: true, current_user_permissions: ['chat', 'manage', 'workspace_exec', 'read'], metadata: {}, timezone: 'UTC' }] })
      return
    }
    if (path === '/models') { this.json(res, 200, [{ id: 'stub-model', model_id: 'stub-model', name: 'Stub Model', provider_id: 'stub-provider' }]); return }
    if (path === '/providers' || path === '/speech-models' || path === '/agent-authorizations' || path === '/users/me/runtimes' || path === '/channels') { this.json(res, 200, []); return }
    if (path === `/bots/${BOT_ID}/settings`) {
      this.json(res, 200, { bot_id: BOT_ID, chat_model_id: 'stub-model', tts_model_id: 'stub-tts', tts_voice_id: 'stub-voice', transcription_model_id: '' })
      return
    }
    if (path === `/bots/${BOT_ID}/container`) { this.json(res, 200, { id: '', status: 'stopped' }); return }
    // Session input queues: a message the composer sends while a run streams
    // is enqueued here (never as a websocket message). The pending item is
    // the follow-up the steer scenario's running turn claims; a claimed item
    // leaves the queue, exactly as the promoted input stops being pending.
    const queueFollowUp = new RegExp(`^/bots/${BOT_ID}/sessions/([0-9a-f-]+)/follow-up-queue$`).exec(path)
    const queueSteer = new RegExp(`^/bots/${BOT_ID}/sessions/([0-9a-f-]+)/steer-queue$`).exec(path)
    if ((queueFollowUp || queueSteer) && req.method === 'POST') {
      const body = JSON.parse(await this.readBody(req) || '{}')
      const kind = queueFollowUp ? 'follow_up' : 'steer'
      const item = {
        item_id: `qi-${kind}-${Date.now()}-${this.queueItems.length}`,
        status: 'pending', position: this.queueItems.length + 1, text: body.text ?? '',
        ...(kind === 'steer' ? { target_run_id: '' } : { enqueued_during_run_id: '' }),
      }
      this.queueItems.push({ ...item, invocationId: body.invocation_id ?? '', kind, claimed: false })
      this.json(res, 200, item)
      return
    }
    const sessionQueue = new RegExp(`^/bots/${BOT_ID}/sessions/([0-9a-f-]+)/queue$`).exec(path)
    if (sessionQueue && req.method === 'GET') {
      const pending = this.queueItems.filter(item => !item.claimed)
      this.json(res, 200, {
        steer_supported: true,
        steer: pending.filter(item => item.kind === 'steer').map(({ claimed, invocationId, kind, ...item }) => item),
        follow_up: pending.filter(item => item.kind === 'follow_up').map(({ claimed, invocationId, kind, ...item }) => item),
      })
      return
    }
    if ((queueFollowUp || queueSteer) && req.method === 'GET') {
      const kind = queueFollowUp ? 'follow_up' : 'steer'
      this.json(res, 200, { items: this.queueItems.filter(item => !item.claimed && item.kind === kind).map(({ claimed, invocationId, kind: _kind, ...item }) => item) })
      return
    }
    const queueItem = new RegExp(`^/bots/${BOT_ID}/sessions/([0-9a-f-]+)/(follow-up-queue|steer-queue)/([^/]+)$`).exec(path)
    if (queueItem && (req.method === 'PATCH' || req.method === 'DELETE')) {
      const target = this.queueItems.find(item => item.item_id === queueItem[3])
      if (target && req.method === 'PATCH') {
        const body = JSON.parse(await this.readBody(req) || '{}')
        target.text = body.text ?? target.text
        this.json(res, 200, { item_id: target.item_id, status: target.status, position: target.position, text: target.text })
        return
      }
      if (target) this.queueItems = this.queueItems.filter(item => item !== target)
      this.json(res, 200, { ok: true })
      return
    }
    if (path === `/bots/${BOT_ID}/sessions` && req.method === 'GET') {
      this.json(res, 200, {
        items: [
          { id: SESSION_ID, bot_id: BOT_ID, type: 'chat', session_mode: 'chat', runtime_type: 'model', title: '主会话', created_at: nowIso(), updated_at: nowIso() },
          { id: OTHER_SESSION_ID, bot_id: BOT_ID, type: 'chat', session_mode: 'chat', runtime_type: 'model', title: '历史会话', created_at: nowIso(), updated_at: nowIso() },
        ],
        next_cursor: '',
      })
      return
    }
    if (path === `/bots/${BOT_ID}/sessions/contact`) {
      this.json(res, 200, { id: SESSION_ID, bot_id: BOT_ID, type: 'chat', session_mode: 'chat', runtime_type: 'model', title: '主会话', created_at: nowIso(), updated_at: nowIso() })
      return
    }
    const sessionMatch = new RegExp(`^/bots/${BOT_ID}/sessions/([0-9a-f-]+)$`).exec(path)
    if (sessionMatch) {
      const id = sessionMatch[1]
      this.json(res, 200, { id, bot_id: BOT_ID, type: 'chat', session_mode: 'chat', runtime_type: 'model', title: id === OTHER_SESSION_ID ? '历史会话' : '主会话', created_at: nowIso(), updated_at: nowIso() })
      return
    }
    if (path === `/bots/${BOT_ID}/messages`) {
      const sessionId = url.searchParams.get('session_id') ?? ''
      const items = sessionId === OTHER_SESSION_ID ? this.historyTurns() : (this.history.get(sessionId) ?? [])
      this.json(res, 200, { items, next_cursor: '' })
      return
    }
    if (path === `/bots/${BOT_ID}/sessions/events`) {
      res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-store', connection: 'keep-alive', 'access-control-allow-origin': '*' })
      res.write(`data: ${JSON.stringify({ type: 'activity_ready', cache_invalidation: false })}\n\n`)
      const timer = setInterval(() => { try { res.write(': keepalive\n\n') } catch { clearInterval(timer) } }, 15000)
      req.on('close', () => clearInterval(timer))
      return
    }
    if (path === `/bots/${BOT_ID}/tts/playback/config`) {
      if (this.mode === 'no-tts') { this.json(res, 404, { title: 'not configured' }); return }
      this.json(res, 200, { identity: SPEECH_IDENTITY, input_content_type: 'audio/mpeg' })
      return
    }
    if (path === `/bots/${BOT_ID}/tts/playback/input` && req.method === 'POST') {
      const body = JSON.parse(await this.readBody(req) || '{}')
      this.postCount += 1
      // The ack line reads as its own short task ahead of the body (the client
      // never caches it). Its audio is a short clip, as a real synthesized ack
      // would be, so the body task is not parked behind a 30 s silence.
      const ack = body.text === ACK_TEXT
      if (!ack) this.bodyPostCount += 1
      const entry = { kind: 'post', id: body.id, sequence: body.sequence, offset: body.offset, text: body.text, final: body.final, ms: Date.now() - this.t0, status: 200 }
      this.speech.push(entry)
      // Scenario 2: the first body task fails transport-style. The client must
      // end only that task and read the remaining text through the next one.
      if (this.mode === 'fail-task' && !ack && this.bodyPostCount === 1) {
        entry.status = 503
        res.writeHead(503, { 'content-type': 'application/json', 'x-speech-playback-identity': SPEECH_IDENTITY })
        res.end(JSON.stringify({ title: 'provider unavailable' }))
        this.rejectedFirstPost?.resolve?.()
        return
      }
      res.writeHead(200, { 'content-type': 'audio/mpeg', 'x-speech-playback-identity': SPEECH_IDENTITY, 'cache-control': 'private, no-store' })
      res.end(ack || this.mode === 'early-ended' ? this.shortMp3 : this.mp3)
      return
    }
    if (path === `/bots/${BOT_ID}/tts/playback/input` && req.method === 'PUT') {
      const body = JSON.parse(await this.readBody(req) || '{}')
      this.speech.push({ kind: 'put', id: body.id, sequence: body.sequence, offset: body.offset, text: body.text, final: body.final, ms: Date.now() - this.t0 })
      res.writeHead(204, { 'cache-control': 'no-store' })
      res.end()
      return
    }
    // E8 upload wire: the browser path confirms the upload limit, probes by
    // content hash with HEAD (200 with the ETag/Content-Length identity, 404
    // for a new original), POSTs the multipart body through
    // the XHR seam whose wire bytes drive the transfer ring, then re-reads the
    // stored original over a paced body so the streamed read - and the ring it
    // feeds - stays observable. Only the file part of the multipart body is the
    // original; the rest is transport framing.
    if (path === `/bots/${BOT_ID}/media/limits` && req.method === 'GET') {
      this.json(res, 200, { max_upload_bytes: 1024 * 1024 * 1024 })
      return
    }
    const mediaMatch = new RegExp(`^/bots/${BOT_ID}/media/([0-9a-f]{64})$`).exec(path)
    if (mediaMatch && (req.method === 'GET' || req.method === 'HEAD')) {
      const stored = this.mediaFiles?.get(mediaMatch[1])
      if (!stored) { this.json(res, 404, { title: 'not found' }); return }
      // The production handler serves HEAD through the same headers as GET:
      // the ETag is the content hash and Content-Length the stored size.
      const identity = { 'content-type': stored.mime, 'content-length': String(stored.bytes.length), 'etag': `"${mediaMatch[1]}"`, 'cache-control': 'no-store', 'access-control-allow-origin': '*' }
      if (req.method === 'HEAD') { res.writeHead(200, identity); res.end(); return }
      res.writeHead(200, identity)
      // A scripted mid-body break: the paced response dies after failAfterChunks
      // writes so the client's verified read rejects part-way through the
      // transfer, exactly like a connection lost mid-download.
      const failAfter = this.botAttachment?.hash === mediaMatch[1] ? this.botAttachment.failAfterChunks : null
      if (!this.downloadChunkDelayMs && failAfter == null) { res.end(stored.bytes); return }
      const step = Math.max(1, Math.ceil(stored.bytes.length / 64))
      for (let at = 0, written = 0; at < stored.bytes.length; at += step, written += 1) {
        if (failAfter != null && written >= failAfter) { res.destroy(); return }
        res.write(stored.bytes.subarray(at, at + step))
        if (this.downloadChunkDelayMs) await new Promise(resolve => setTimeout(resolve, this.downloadChunkDelayMs))
      }
      res.end()
      return
    }
    if (path === `/bots/${BOT_ID}/media` && req.method === 'POST') {
      ;(this.uploadEvents ??= []).push({ at: nowIso(), bytes: 0, done: false })
      const event = this.uploadEvents[this.uploadEvents.length - 1]
      const chunks = []
      let received = 0
      let pacedAt = 0
      for await (const chunk of req) {
        chunks.push(chunk)
        received += chunk.length
        event.bytes = received
        // Paced byte segments: with uploadPaceBytes set, the read sleeps once
        // per segment (independent of TCP chunk sizing), so a large body keeps
        // the wire window open for seconds. Without it, the legacy per-chunk
        // delay applies.
        const boundary = this.uploadPaceBytes > 0 && received - pacedAt >= this.uploadPaceBytes
        if (boundary || (!this.uploadPaceBytes && this.uploadChunkDelayMs)) {
          if (boundary) pacedAt = received
          this.uploadEvents.push({ at: nowIso(), bytes: received, done: false })
          if (this.uploadChunkDelayMs) await new Promise(resolve => setTimeout(resolve, this.uploadChunkDelayMs))
        }
      }
      event.done = true
      const file = multipartFile(Buffer.concat(chunks), req.headers['content-type'] || '')
      const hash = createHash('sha256').update(file.bytes).digest('hex')
      ;(this.mediaFiles ??= new Map()).set(hash, { mime: file.mime || 'application/octet-stream', bytes: file.bytes })
      this.json(res, 201, { content_hash: hash, size_bytes: file.bytes.length, mime: file.mime || 'application/octet-stream' })
      return
    }
    this.json(res, 200, { items: [] })
  }

  historyTurns() {
    const stamp = nowIso()
    return [
      { turn_id: 'turn-history-user', role: 'user', text: '历史问题', timestamp: stamp, turn_position: 1, id: 'h-u1' },
      { turn_id: 'turn-history-assistant', role: 'assistant', timestamp: stamp, turn_position: 2, id: 'h-a1', messages: [{ id: 1, type: 'text', content: HISTORY_BODY }] },
    ]
  }

  upgrade(req, socket) {
    const key = req.headers['sec-websocket-key']
    if (!key) { socket.destroy(); return }
    socket.write(['HTTP/1.1 101 Switching Protocols', 'Upgrade: websocket', 'Connection: Upgrade', `Sec-WebSocket-Accept: ${wsAccept(key)}`, '\r\n'].join('\r\n'))
    this.sockets.add(socket)
    let buffer = Buffer.alloc(0)
    socket.on('data', chunk => {
      buffer = Buffer.concat([buffer, chunk])
      const { messages, rest } = decodeFrames(buffer)
      buffer = rest
      for (const frame of messages) {
        if (frame.opcode === 0x8) { socket.destroy(); return }
        if (frame.opcode === 0x9) { socket.write(Buffer.from([0x8a, 0x00])); continue }
        if (frame.opcode !== 0x1) continue
        let message
        try { message = JSON.parse(frame.text) } catch { continue }
        this.onClientMessage(message, socket)
      }
    })
    socket.on('error', () => this.sockets.delete(socket))
    socket.on('close', () => this.sockets.delete(socket))
  }

  send(socket, payload) {
    this.frames.push({ at: Date.now(), type: payload.type, seq: payload.seq, status: payload.delta?.run?.status, payload: JSON.stringify(payload).slice(0, 1200) })
    try { socket.write(encodeFrame(JSON.stringify(payload))) } catch {}
  }

  onClientMessage(message, socket) {
    this.clientMessages.push({ type: message.type, at: nowIso(), payload: JSON.stringify(message).slice(0, 2000) })
    if (message.type === 'runtime_subscribe') {
      const state = this.runtimeFor(message.session_id)
      this.send(socket, {
        type: 'runtime_snapshot', session_id: message.session_id, epoch: state.epoch, seq: state.seq,
        snapshot: {
          bot_id: BOT_ID, session_id: message.session_id, epoch: state.epoch, seq: state.seq,
          ...(state.runView ? { current_run_view: state.runView } : {}),
          updated_at: nowIso(),
        },
      })
      return
    }
    if (message.type === 'message' || message.type === 'retry_message') {
      this.runScript(message, socket).catch(error => {
        if (error !== SCENARIO_RESET) this.scriptErrors.push(String((error && error.message) || error))
      })
    }
  }

  // The steer script waits inside its held step for the queued follow-up (the
  // harness submits it there). A wait that times out without one settles the
  // reply normally instead of hanging the scenario.
  async awaitSteerQueue(pause, timeoutMs = 30000) {
    const deadline = Date.now() + timeoutMs
    let pending = this.pendingQueueItems()
    while (!pending.length && Date.now() < deadline) {
      await pause(120)
      pending = this.pendingQueueItems()
    }
    if (!pending.length) return null
    pending[0].claimed = true
    return pending[0]
  }

  pendingQueueItems() {
    return this.queueItems.filter(item => !item.claimed)
  }

  runtimeFor(sessionId) {
    if (!this.runtime.has(sessionId)) this.runtime.set(sessionId, { epoch: 'epoch-1', seq: 0, runView: null })
    return this.runtime.get(sessionId)
  }

  // One recorded event sequence per send:
  //   run_accepted -> ack status -> reasoning -> body sentences -> completed.
  // The timing of each step is scripted so the harness can measure
  // "first completed sentence -> first speech request" without guesswork.
  // A scenario reset abandons the script at its next pause: a run started by
  // the previous scenario must never settle into the next scenario's
  // transcript, or that page shows a second reply it never read.
  async runScript(message, socket) {
    const generation = this.generation
    this.scriptActive = true
    try {
      await this.replayScript(message, socket, generation)
    } finally {
      this.scriptActive = false
    }
  }

  async replayScript(message, socket, generation) {
    const pause = ms => new Promise(r => setTimeout(r, ms)).then(() => { if (this.generation !== generation) throw SCENARIO_RESET })
    const sessionId = message.session_id || SESSION_ID
    const invocation = message.invocation_id
    const runId = `run-${invocation}`
    const turnId = `turn-${invocation}`
    const state = this.runtimeFor(sessionId)
    if (!message.session_id) this.send(socket, { type: 'session_created', invocation_id: invocation, session_id: sessionId })
    // Model time-to-first-token: the phone's history load finishes before any
    // model output exists. Scripting the wait keeps the case out of the
    // "reply arrived inside the initial load" absorption window, which is a
    // different (already covered) behaviour.
    await pause(this.startDelay)
    this.send(socket, { type: 'run_accepted', invocation_id: invocation, run_id: runId, session_id: sessionId, turn_id: turnId, turn_position: 10, epoch: state.epoch, seq: state.seq })

    const receipt = this.mode === 'receipt' ? receiptSource(sessionId) : null
    const userTurn = { turn_id: turnId, role: 'user', text: message.text ?? '', timestamp: nowIso(), turn_position: 10, ...(receipt ? { input_source: receipt } : {}) }
    const view = {
      run_id: runId, turn_id: turnId, turn_position: 10, invocation_id: invocation,
      generation: 'gen-1', status: 'running', started_at: nowIso(), updated_at: nowIso(),
      messages: [], user_turns: [userTurn],
    }
    state.runView = view
    const ackBlock = { id: 0, type: 'status', name: 'conversation_ack', args: { text: ACK_TEXT } }
    const reasoning = { id: 1, type: 'reasoning', content: '' }
    const body = { id: 2, type: 'text', content: '' }
    // The bot-image reply carries the staged original as an image attachment
    // instead of a text body, so the rendered card runs the verified-read
    // onProgress path against the paced (or breaking) media body.
    const botImage = this.mode === 'bot-image' && this.botAttachment ? {
      id: 3, type: 'attachments',
      attachments: [{
        type: 'image', name: 'bot-image.png', mime: 'image/png',
        size: this.botAttachment.size, content_hash: this.botAttachment.hash,
        metadata: { bot_id: BOT_ID },
      }],
    } : null
    const blocks = receipt ? [] : [ackBlock]
    const step = async (deltaFor, delay = this.delay) => {
      state.seq += 1
      view.messages = blocks.map(block => ({ ...block }))
      view.updated_at = nowIso()
      state.runView = { ...view, messages: view.messages.map(block => ({ ...block })) }
      const delta = typeof deltaFor === 'function' ? deltaFor() : deltaFor
      this.send(socket, { type: 'runtime_delta', session_id: sessionId, epoch: state.epoch, seq: state.seq, delta })
      await pause(delay)
    }
    await step(() => ({ current_run_view: { ...view, messages: view.messages.map(block => ({ ...block })) } }))
    // The acknowledgement stays alone on screen long enough to be observed:
    // on the phone it is the only visible output until the first body text.
    await pause(1200)
    if (botImage) {
      blocks.push(botImage)
      // The settle delay holds the live run open (as it does for the last body
      // sentence), so the paced verified read finishes inside the live turn
      // before the persisted transcript replaces it.
      await step({ message_upserts: [{ id: botImage.id, type: 'attachments', attachments: botImage.attachments.map(attachment => ({ ...attachment })) }] }, this.settleDelay ?? this.delay)
    } else {
      if (this.mode !== 'no-reasoning') {
        reasoning.content = REASONING[0]
        blocks.push(reasoning)
        await step({ message_upserts: [{ ...reasoning }] })
        reasoning.content = REASONING.join('')
        await step({ message_appends: [{ id: 1, type: 'reasoning', content: REASONING[1] }] })
      }
      blocks.push(body)
      // Gate 2 replays the long body: the first task is rejected by the service
      // and the client must roll its text back into the next task. Gate 1 replays
      // the same long body so "read on the first sentence" is separated from
      // "read when the reply settles" by several sentences and a long settle.
      // The steer scenario streams the same long body only until the second
      // sentence, then holds the run open for the queued follow-up to be
      // claimed (the harness submits it inside that window).
      const steerScript = this.mode === 'steer' && !this.steerDone
      const sentences = this.mode === 'fail-task' || this.mode === 'long' || steerScript ? FAIL_TASK_BODY : BODY
      const settle = this.settleDelay ?? this.delay
      const streamCount = steerScript ? 2 : sentences.length
      for (const [index, sentence] of sentences.entries()) {
        if (index >= streamCount) break
        body.content += sentence
        const last = index === streamCount - 1
        if (last && !steerScript) this.lastSentenceAt = Date.now() - this.t0
        if (index === 0) {
          this.firstSentenceAt = Date.now() - this.t0
          await step({ message_upserts: [{ ...body }] }, this.delay)
        } else await step({ message_appends: [{ id: 2, type: 'text', content: sentence }] }, this.delay)
      }
      if (steerScript) {
        // The server claims the queued follow-up at the next step boundary and
        // publishes it before the step's output: the steer names the turn it
        // opens (after the committed text block), and the claimed input itself
        // arrives as a user turn upsert (PR #131's claim frame).
        const claim = await this.awaitSteerQueue(pause)
        if (claim) {
          const steerTurnId = `turn-${claim.invocationId}`
          state.seq += 1
          this.steerClaimedAt = Date.now() - this.t0
          this.send(socket, {
            type: 'runtime_delta', session_id: sessionId, epoch: state.epoch, seq: state.seq,
            delta: {
              steer_turn_upserts: [{
                item_id: `steer-${claim.invocationId}`, status: 'applied', text: claim.text ?? '',
                turn_id: steerTurnId, turn_position: 11, after_message_id: 2, timestamp: nowIso(),
              }],
              user_turn_upserts: [{
                turn_id: steerTurnId, role: 'user', text: claim.text ?? '', timestamp: nowIso(),
                turn_position: 11,
                input_source: { kind: 'user', user_id: 'user-stub', bot_id: BOT_ID, session_id: sessionId },
              }],
            },
          })
          await pause(this.delay)
          // The continuation is the model's answer to the claimed input: a new
          // text block after the steer boundary, its own sentences on the same
          // paced cadence, and its own settle gap after the last one.
          const continuation = { id: 3, type: 'text', content: '' }
          blocks.push(continuation)
          for (const [index, sentence] of STEER_BODY.entries()) {
            continuation.content += sentence
            const last = index === STEER_BODY.length - 1
            if (index === 0) {
              this.steerFirstSentenceAt = Date.now() - this.t0
              await step({ message_upserts: [{ ...continuation }] }, this.delay)
            } else await step({ message_appends: [{ id: 3, type: 'text', content: sentence }] }, this.delay)
            if (last) await pause(settle)
          }
          this.steerSettledAt = Date.now() - this.t0
          this.settledAt = this.steerSettledAt
          const existing = this.history.get(sessionId) ?? []
          this.history.set(sessionId, [...existing,
            { turn_id: turnId, role: 'user', text: message.text ?? '', timestamp: nowIso(), turn_position: 10, id: `t-${invocation}-u` },
            {
              turn_id: turnId, role: 'assistant', timestamp: nowIso(), turn_position: 10, id: `t-${invocation}-a`,
              messages: [
                ...(this.mode === 'no-reasoning' ? [] : [{ id: 1, type: 'reasoning', content: REASONING.join('') }]),
                { id: 2, type: 'text', content: FAIL_TASK_BODY.slice(0, 2).join('') },
              ],
            },
            { turn_id: steerTurnId, role: 'user', text: claim.text ?? '', timestamp: nowIso(), turn_position: 11, id: `t-${claim.invocationId}-u` },
            {
              turn_id: steerTurnId, role: 'assistant', timestamp: nowIso(), turn_position: 11, id: `t-${claim.invocationId}-a`,
              messages: [{ id: 3, type: 'text', content: STEER_BODY.join('') }],
            },
          ])
          state.seq += 1
          this.steerDone = true
          this.send(socket, {
            type: 'runtime_delta', session_id: sessionId, epoch: state.epoch, seq: state.seq,
            delta: { run: { run_id: runId, status: 'completed', updated_at: nowIso() } },
          })
          return
        }
      }
      for (let index = streamCount; index < sentences.length; index++) {
        const sentence = sentences[index]
        body.content += sentence
        const last = index === sentences.length - 1
        if (last) this.lastSentenceAt = Date.now() - this.t0
        await step({ message_appends: [{ id: 2, type: 'text', content: sentence }] }, last ? settle : this.delay)
      }
    }
    // Settle: the persisted transcript replaces the live one on refresh.
    state.seq += 1
    this.settledAt = Date.now() - this.t0
    const finalTurns = [
      { turn_id: turnId, role: 'user', text: message.text ?? '', timestamp: nowIso(), turn_position: 10, id: `t-${invocation}-u`, ...(receipt ? { input_source: receipt } : {}) },
      {
        turn_id: turnId, role: 'assistant', timestamp: nowIso(), turn_position: 10, id: `t-${invocation}-a`,
        ...(receipt ? { input_source: receipt } : {}),
        messages: botImage
          ? [{ id: botImage.id, type: 'attachments', attachments: botImage.attachments.map(attachment => ({ ...attachment })) }]
          : [
              ...(this.mode === 'no-reasoning' ? [] : [{ id: 1, type: 'reasoning', content: REASONING.join('') }]),
              { id: 2, type: 'text', content: (this.mode === 'fail-task' || this.mode === 'long' ? FAIL_TASK_BODY : BODY).join('') },
            ],
      },
    ]
    const existing = this.history.get(sessionId) ?? []
    this.history.set(sessionId, [...existing, ...finalTurns])
    this.send(socket, {
      type: 'runtime_delta', session_id: sessionId, epoch: state.epoch, seq: state.seq,
      delta: { run: { run_id: runId, status: 'completed', updated_at: nowIso() } },
    })
  }
}

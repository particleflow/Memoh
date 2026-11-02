// Minimal CDP client over the built-in WebSocket (Node >= 22). No dependency.
import { spawn } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import http from 'node:http'

const sleep = ms => new Promise(r => setTimeout(r, ms))

export function getJson(url) {
  return new Promise((resolve, reject) => {
    http.get(url, res => {
      let body = ''
      res.setEncoding('utf8')
      res.on('data', chunk => { body += chunk })
      res.on('end', () => {
        try { resolve(JSON.parse(body)) } catch (error) { reject(new Error(body || String(error))) }
      })
    }).on('error', reject)
  })
}

export async function launchChrome({ executable, port = 0, userDataDir, args = [] }) {
  const dir = userDataDir ?? mkdtempSync(join(tmpdir(), 'acceptance-chrome-'))
  const chrome = spawn(executable, [
    '--headless=new',
    '--disable-gpu',
    '--no-first-run',
    '--no-default-browser-check',
    '--disable-background-networking',
    '--disable-features=Translate,MediaRouter',
    '--autoplay-policy=no-user-gesture-required',
    `--remote-debugging-port=${port}`,
    `--user-data-dir=${dir}`,
    '--host-resolver-rules=MAP stub.test 127.0.0.1',
    ...args,
    'about:blank',
  ], { stdio: 'ignore' })
  const active = join(dir, 'DevToolsActivePort')
  let file = ''
  for (let i = 0; i < 100; i++) {
    try { file = readFileSync(active, 'utf8') } catch { await sleep(100); continue }
    if (file.trim().split('\n').length >= 2) break
    await sleep(100)
  }
  const [devtoolsPort, browserPath] = file.trim().split('\n')
  if (!devtoolsPort || !browserPath) throw new Error('Chrome DevTools endpoint did not start')
  const version = await getJson(`http://127.0.0.1:${devtoolsPort}/json/version`)
  return {
    process: chrome,
    userDataDir: dir,
    wsUrl: version.webSocketDebuggerUrl,
    browserPath,
    async close() {
      try { chrome.kill() } catch {}
      await sleep(300)
      try { rmSync(dir, { recursive: true, force: true }) } catch {}
    },
  }
}

export class CDP {
  constructor(ws) { this.ws = ws; this.id = 0; this.pending = new Map(); this.listeners = new Map() }
  static async connect(url) {
    const socket = new WebSocket(url)
    const cdp = new CDP(socket)
    await new Promise((resolve, reject) => { socket.addEventListener('open', resolve, { once: true }); socket.addEventListener('error', reject, { once: true }) })
    socket.addEventListener('message', event => {
      const message = JSON.parse(event.data)
      if (message.id !== undefined) {
        const pending = cdp.pending.get(message.id)
        if (pending) {
          cdp.pending.delete(message.id)
          if (message.error) pending.reject(new Error(message.error.message))
          else pending.resolve(message.result)
        }
        return
      }
      for (const listener of cdp.listeners.get(message.method) ?? []) listener(message.params, message.sessionId)
    })
    return cdp
  }
  send(method, params = {}, sessionId) {
    const id = ++this.id
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject })
      this.ws.send(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) }))
    })
  }
  on(method, listener) {
    if (!this.listeners.has(method)) this.listeners.set(method, new Set())
    this.listeners.get(method).add(listener)
    return () => this.listeners.get(method)?.delete(listener)
  }
  close() { try { this.ws.close() } catch {} }
}

export async function openPage(cdp, browserContextId) {
  const { targetId } = await cdp.send('Target.createTarget', { url: 'about:blank', ...(browserContextId ? { browserContextId } : {}) })
  const { sessionId } = await cdp.send('Target.attachToTarget', { targetId, flatten: true })
  const page = {
    targetId,
    sessionId,
    async eval(expression) {
      const result = await cdp.send('Runtime.evaluate', {
        expression, awaitPromise: true, returnByValue: true, userGesture: true,
      }, sessionId)
      if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description ?? result.exceptionDetails.text)
      return result.result?.value
    },
    async waitFor(expression, timeout = 15000, interval = 100) {
      const start = Date.now()
      let last
      while (Date.now() - start < timeout) {
        try {
          const value = await page.eval(expression)
          if (value) return value
        } catch (error) { last = error }
        await sleep(interval)
      }
      throw new Error(`waitFor timeout: ${expression}${last ? ' (' + last.message + ')' : ''}`)
    },
    async goto(url) { await cdp.send('Page.navigate', { url }, sessionId) },
    async screenshot(path) {
      const { data } = await cdp.send('Page.captureScreenshot', { format: 'png' }, sessionId)
      writeFileSync(path, Buffer.from(data, 'base64'))
    },
    close() { return cdp.send('Target.closeTarget', { targetId }) },
  }
  await cdp.send('Page.enable', {}, sessionId)
  await cdp.send('Runtime.enable', {}, sessionId)
  await cdp.send('Network.enable', {}, sessionId)
  return page
}

import fs from 'node:fs'
import assert from 'node:assert/strict'
import { chromium } from '/opt/codex/runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright/index.mjs'

const out = '/tmp/shared-auth-session-guard'
const origin = 'http://127.0.0.1:18402'
const token = fs.readFileSync('/tmp/web-observation-reconnect/private-token', 'utf8').trim()
const meResponse = await fetch('http://127.0.0.1:18400/users/me', { headers: { Authorization: `Bearer ${token}` } })
assert.equal(meResponse.status, 200, 'existing synthetic Server identity must be usable')
const me = await meResponse.json()
assert(me.id)
const proof = { realWeb: true, existingSyntheticServerAndPG: true, loginResponseIntercepted: true, loginRequests: 0, actualWrites: 0, taskSubmissions: 0, late401Cases: [], current401Cleared: false, pageErrors: [], readStatuses: {} }
const browser = await chromium.launch({ headless: true, executablePath: '/usr/bin/chromium', args: ['--no-sandbox'] })
try {
  const context = await browser.newContext({ viewport: { width: 1200, height: 900 } })
  await context.addInitScript(() => localStorage.setItem('language', 'en'))
  const page = await context.newPage()
  page.setDefaultTimeout(20000)
  page.on('pageerror', error => proof.pageErrors.push(error.message))
  page.on('response', response => {
    const path = new URL(response.url()).pathname
    if (path.startsWith('/api/') && !path.startsWith('/api/audit/')) {
      const key = String(response.status())
      proof.readStatuses[key] = (proof.readStatuses[key] || 0) + 1
    }
  })
  await page.route('**/api/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (request.method() === 'POST' && path === '/api/auth/login') {
      proof.loginRequests++
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ access_token: token, user_id: me.id, username: me.username, display_name: me.display_name, role: me.role, avatar_url: me.avatar_url, timezone: 'UTC' }) })
    }
    if (request.method() === 'PUT' && path === '/api/users/me') return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(me) })
    if (!['GET', 'HEAD', 'OPTIONS'].includes(request.method())) {
      proof.actualWrites++
      if (path.includes('/tasks') || path.includes('/chat') || path.includes('/messages')) proof.taskSubmissions++
      return route.abort()
    }
    return route.continue()
  })
  await page.goto(`${origin}/login`)
  await page.locator('#username').waitFor()
  async function hold401(name) {
    let arrived
    const ready = new Promise(resolve => { arrived = resolve })
    let held
    await page.route(`**/api/audit/${name}`, async route => { held = route; arrived() })
    await page.evaluate(async name => {
      const source = await (await fetch('/src/lib/api-client.ts')).text()
      const sdkURL = source.match(/import \{ client \} from "([^"]+)"/)[1]
      const { client } = await import(sdkURL)
      if (client.getConfig().baseUrl !== '/api') throw new Error('not the bootstrapped application SDK')
      window.auditPending = client.get({ url: `/audit/${name}` }).then(() => { window.auditSettled = name })
    }, name)
    await ready
    return async () => {
      await held.fulfill({ status: 401, contentType: 'application/json', body: '{"message":"synthetic unauthorized"}' })
      await page.waitForFunction(name => window.auditSettled === name, name)
    }
  }
  const anonymous = await hold401('late-anonymous')
  await page.locator('#username').fill('synthetic-login')
  await page.locator('#password').fill('synthetic-not-a-real-password')
  await page.locator('form button[type=submit]').click()
  await page.waitForURL(url => url.pathname !== '/login')
  await page.waitForFunction(() => !!localStorage.getItem('token'))
  await anonymous()
  assert.equal(await page.evaluate(() => localStorage.getItem('token')), token)
  assert.notEqual(new URL(page.url()).pathname, '/login')
  proof.late401Cases.push('anonymous request before login: new identity retained')
  console.log('PASS actual login form/store + existing authenticated Server reads: late anonymous 401 retains the new login')

  const same = await hold401('late-same-token')
  await page.evaluate(() => {
    const store = document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('user')
    store.login({ ...store.userInfo }, localStorage.getItem('token'))
    localStorage.setItem('chat-bot-id', 'synthetic-new-login-selection')
  })
  await same()
  assert.equal(await page.evaluate(() => localStorage.getItem('token')), token)
  assert.equal(await page.evaluate(() => localStorage.getItem('chat-bot-id')), 'synthetic-new-login-selection')
  assert.notEqual(new URL(page.url()).pathname, '/login')
  proof.late401Cases.push('same-token new login generation: identity and fresh selection retained')
  console.log('PASS real user-store login with the same token: prior 401 cannot clear the new generation or selection')
  // Avoid leaving a fake navigation selection for the subsequent normal page load.
  await page.evaluate(() => localStorage.removeItem('chat-bot-id'))
  await page.reload()
  await page.waitForTimeout(1500)
  assert.equal(await page.evaluate(() => localStorage.getItem('token')), token)
  assert.notEqual(new URL(page.url()).pathname, '/login')
  await page.screenshot({ path: `${out}/login-retained.png` })
  console.log('PASS normal reload retains authenticated application access after both stale responses')

  const current = await hold401('current-session')
  await current()
  await page.locator('#username').waitFor()
  assert(!await page.evaluate(() => localStorage.getItem('token')))
  assert.equal(new URL(page.url()).pathname, '/login')
  proof.current401Cleared = true
  await page.screenshot({ path: `${out}/current-session-cleared.png` })
  console.log('PASS current-session 401 still clears identity and routes to the real login page')
  assert.equal(proof.loginRequests, 1)
  assert.equal(proof.actualWrites, 0)
  assert.equal(proof.taskSubmissions, 0)
  assert.deepEqual(proof.pageErrors, [])
  fs.writeFileSync(`${out}/browser-proof.json`, JSON.stringify(proof, null, 2) + '\n')
} finally { await browser.close() }

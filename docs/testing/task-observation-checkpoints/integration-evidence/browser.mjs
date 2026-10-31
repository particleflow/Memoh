import fs from 'node:fs'
import assert from 'node:assert/strict'
import { chromium } from '/opt/codex/runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright/index.mjs'

const root = '/tmp/task-checkpoint-integration'
const fixture = JSON.parse(fs.readFileSync('/tmp/task-native-observation/private-observation-fixture.json', 'utf8'))
const en = JSON.parse(fs.readFileSync('apps/web/src/i18n/locales/en.json', 'utf8'))
const revision = '9007199254740993123'
const hash = 'a'.repeat(64)
const task = { task_type: 'computer_operation', task_id: fixture.operation, session_id: fixture.session, created_at: '', updated_at: '', status: 'submitted', summary: 'Synthetic persisted checkpoint', content_available: false, attachments_available: false }
const observation = { operation_id: fixture.operation, target_id: 'synthetic-target', workspace_id: 'synthetic-workspace', pane_id: 'synthetic-pane', pi_session_id: 'b'.repeat(64), attempt: 'c'.repeat(32), process_state: 'exited', pi_result: 'succeeded', result_reason: 'verified_persisted_result', business_outcome: 'unknown', native: { availability: 'offline', agent_status: 'done', reference_match: 'unknown' }, reconciliation: { revision, generation: 'd'.repeat(32), receipt_state: 'validated', files: [{ meta: { name: 'stdout', size: 128 }, state: 'recorded', confirmed_offset: 128, current_availability: 'not_rechecked', recorded_content_hash: hash }, { meta: { name: 'stderr', size: 0 }, state: 'empty', confirmed_offset: 0, current_availability: 'not_rechecked' }] }, continuity: { status: 'unconfirmed', reason: 'history_not_rechecked', last_invocation: { before: 'verified', after: 'verified' } } }
let mode = 'success'
let disconnected = false
const proof = { realWeb: true, existingIsolatedLogin: true, syntheticTaskResponses: true, taskMethods: [], observationIDs: [], pageErrors: [], mutations: 0, deviceRequests: 0 }
const browser = await chromium.launch({ headless: true, executablePath: '/usr/bin/chromium', args: ['--no-sandbox'] })
try {
  const context = await browser.newContext({ viewport: { width: 1200, height: 1000 } })
  await context.addInitScript(token => { localStorage.setItem('token', token); if (!localStorage.getItem('language')) localStorage.setItem('language', 'en'); if (!localStorage.getItem('theme')) localStorage.setItem('theme', 'light') }, fs.readFileSync('/tmp/web-observation-reconnect/private-token', 'utf8').trim())
  const page = await context.newPage(); page.setDefaultTimeout(20000)
  page.on('pageerror', error => proof.pageErrors.push(error.message))
  await page.route('**/api/**', async route => {
    const req = route.request(), path = new URL(req.url()).pathname
    const reply = (data, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(data) })
    if (path.includes('/tasks')) {
      proof.taskMethods.push(req.method())
      assert.equal(req.method(), 'GET')
      if (disconnected) return route.abort('internetdisconnected')
      if (path.endsWith('/observation')) {
        proof.observationIDs.push(path.split('/').at(-2))
        if (mode === 'denied') return reply({ status: 403, code: 'http.forbidden' }, 403)
        if (mode === 'unknown') return reply({ operation_id: fixture.operation, native: {} })
        return reply(observation)
      }
      if (path.endsWith('/deliveries')) return reply({ version: 1, operation_id: fixture.operation, files: [], has_more: false })
      if (path.endsWith('/content')) return reply({ operation_id: fixture.operation, available: false, followup_available: false })
      if (path.endsWith('/tasks')) return reply({ items: [task] })
      return reply(task)
    }
    // No observation can reach a device. Avoid unrelated connection refresh
    // while opening the existing synthetic conversation.
    if (path.endsWith('/workspace-targets')) return reply({ items: [] })
    if (/\/oauth\/|\/mcp\//.test(path)) { proof.deviceRequests++; return reply({}, 503) }
    if (!['GET', 'HEAD'].includes(req.method())) { proof.mutations++; return reply({}, 405) }
    return route.continue()
  })
  await page.goto(`http://127.0.0.1:18402/bot/${fixture.bot}`)
  await page.getByText('Task file observation synthetic', { exact: true }).first().click()
  await page.getByRole('button', { name: 'Tasks', exact: true }).click()
  await page.getByRole('button').filter({ hasText: fixture.operation.slice(0, 8) }).click()
  const panel = page.getByRole('region', { name: 'Current computer observation', exact: true })
  await panel.getByText(revision, { exact: true }).waitFor()
  assert((await panel.innerText()).includes('Computer reported offline'))
  assert((await panel.innerText()).includes(en.chat.tasks.outcomes.succeeded))
  assert((await panel.innerText()).includes('Business outcome'))
  for (const name of ['Current connection', 'Persisted receipt and Pi result', 'Historical continuity', 'Collection checkpoints']) assert.equal(await panel.getByRole('region', { name, exact: true }).count(), 1)
  assert.equal(await panel.getByRole('region', { name: 'Collection checkpoints' }).locator('a,button,img').count(), 0)
  await panel.getByRole('region', { name: 'Persisted receipt and Pi result' }).scrollIntoViewIfNeeded()
  await page.screenshot({ path: `${root}/receipt.png` })
  await panel.getByRole('region', { name: 'Collection checkpoints' }).scrollIntoViewIfNeeded()
  assert((await panel.innerText()).includes('128 / 128 bytes')); assert((await panel.innerText()).includes('0 / 0 bytes'))
  await page.screenshot({ path: `${root}/collection.png` })
  console.log('PASS actual combined Web: four groups, exact string revision, offline separated from persisted success, zero/recorded bytes without download URLs')
  mode = 'denied'
  await panel.getByRole('button', { name: 'Refresh computer', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('[role=dialog]')?.textContent.includes('9007199254740993123'))
  const deniedReads = proof.observationIDs.length
  await page.waitForTimeout(5500)
  assert.equal(proof.observationIDs.length, deniedReads)
  assert.equal(await panel.getByText('synthetic-workspace', { exact: true }).count(), 0)
  await page.screenshot({ path: `${root}/denied.png` })
  mode = 'success'
  await panel.getByRole('button', { name: 'Refresh computer', exact: true }).click()
  await panel.getByText(revision, { exact: true }).waitFor()
  disconnected = true
  await context.setOffline(true)
  await panel.getByRole('button', { name: 'Refresh computer', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('[role=dialog]')?.textContent.includes('9007199254740993123'))
  disconnected = false
  await context.setOffline(false)
  await page.getByText(revision, { exact: true }).waitFor()
  assert(proof.observationIDs.every(id => id === fixture.operation))
  console.log('PASS 403 clears checkpoints and stops polling; explicit read and browser reconnection restore the same operation without mutations')
  mode = 'unknown'
  await panel.getByRole('button', { name: 'Refresh computer', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('[role=dialog]')?.textContent.includes('9007199254740993123'))
  assert.equal(await panel.getByText('Persisted receipt validated', { exact: true }).count(), 0)
  mode = 'success'
  await page.setViewportSize({ width: 390, height: 844 })
  await page.evaluate(() => { localStorage.setItem('language', 'zh'); localStorage.setItem('theme', 'dark') })
  await page.reload()
  await page.getByRole('button', { name: '任务', exact: true }).click()
  await page.getByRole('button').filter({ hasText: fixture.operation.slice(0, 8) }).click()
  await page.getByText(revision, { exact: true }).waitFor()
  await page.getByText(hash, { exact: true }).scrollIntoViewIfNeeded()
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
  assert.equal(await page.evaluate(() => document.documentElement.classList.contains('dark')), true)
  await page.screenshot({ path: `${root}/narrow-zh.png` })
  console.log('PASS omitted evidence stays unknown; reloaded same identity, 390px Chinese dark mode has no horizontal overflow')
  assert.deepEqual(proof.pageErrors, []); assert.equal(proof.mutations, 0); assert.equal(proof.deviceRequests, 0)
  proof.passed = true
} finally {
  fs.writeFileSync(`${root}/browser-proof.json`, JSON.stringify(proof, null, 2) + '\n')
  await browser.close()
}

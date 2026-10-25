import fs from 'node:fs'
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { chromium } from '/opt/codex/runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright/index.mjs'

const root = '/tmp/task-management-workload'
const f = JSON.parse(fs.readFileSync(`${root}/private-fixture.json`, 'utf8'))
const token = fs.readFileSync('/tmp/web-observation-reconnect/private-token', 'utf8').trim()
const proof = { realServerPG: true, transport: 'synthetic MCP official method shapes, NOT native Runtime Exec or real Herdr/Pi', actualLocalWorkloads: [], posts: [], errors: [], restartedServer: false }
const count = () => JSON.parse(fs.readFileSync(`${root}/calls.json`)).filter(x => x === 'agent.prompt').length
const browser = await chromium.launch({ headless: true, executablePath: '/usr/bin/chromium', args: ['--no-sandbox'] })
let observedPage
const responseReads = []
const onResponse = response => {
  if (new URL(response.url()).pathname.endsWith('/followup')) responseReads.push(response.json().then(body => proof.posts.push({ http: response.status(), state: body.state })))
}
try {
  const context = await browser.newContext({ viewport: { width: 1200, height: 1000 } })
  await context.addInitScript(value => { localStorage.setItem('token', value); localStorage.setItem('language', 'en') }, token)
  const page = await context.newPage(); page.setDefaultTimeout(18000)
  observedPage = page
  page.on('pageerror', e => proof.errors.push(e.message))
  page.on('response', onResponse)
  // Unrelated workspace selector is not an acceptance subject. All task HTTP
  // calls go to the real Server, including access failures and management POST.
  await page.route('**/workspace-targets', route => route.fulfill({ status: 200, contentType: 'application/json', body: '{"items":[]}' }))
  await page.goto(`http://127.0.0.1:18402/bot/${f.bot}`)
  await page.getByText(f.title, { exact: true }).first().click()
  await page.getByRole('button', { name: 'Tasks', exact: true }).click()
  await page.getByRole('button').filter({ hasText: f.operation.slice(0, 8) }).click()
  const panel = page.getByRole('region', { name: 'Native Agent content and follow-up', exact: true })
  const dialog = page.getByRole('dialog')
  assert.equal(await dialog.getByRole('button', { name: /^(Cancel task|Stop|Kill|Pause|Resume)$/ }).count(), 0)
  await panel.getByRole('button', { name: 'Open terminal sample', exact: true }).click()
  await panel.getByText('Synthetic Agent ready; no workload has been executed.', { exact: true }).waitFor()
  const input = panel.getByRole('textbox', { name: 'New instruction', exact: true })
  await input.fill('Synthetic accepted workload')
  const send = panel.getByRole('button', { name: 'Send one instruction', exact: true })
  await send.evaluate(button => { button.click(); button.click() })
  await panel.getByText('Input accepted. Work completion and output ownership remain unverified.', { exact: true }).waitFor()
  assert.equal(count(), 1)
  await panel.getByRole('button', { name: 'Refresh content and request status', exact: true }).click()
  await panel.locator('pre').filter({ hasText: 'WORKLOAD accepted START' }).waitFor()
  proof.actualLocalWorkloads.push('accepted')
  await page.screenshot({ path: `${root}/accepted-workload.png` })
  console.log('PASS real authenticated follow-up + PG CAS + two browser clicks: one local process; accepted remains input-only; actual output sample visible')
  await page.waitForFunction(() => document.querySelector('[aria-label="Native Agent content and follow-up"] pre')?.textContent?.includes('WORKLOAD accepted EXIT 0'), { timeout: 12000 })
  assert.equal(count(), 1)

  fs.writeFileSync(`${root}/mode.txt`, 'unknown')
  await input.fill('Synthetic unknown workload')
  await send.click()
  await panel.getByText('Input delivery is unknown. Check the same request; do not resend with a new ID.', { exact: true }).waitFor()
  const pending = await page.evaluate(() => Object.entries(sessionStorage).filter(([key]) => key.startsWith('memoh:native-followup:')))
  assert.equal(pending.length, 1); assert.match(pending[0][1], /^[a-f0-9-]{36}$/)
  assert.equal(count(), 2)
  const workloadPID = Number(fs.readFileSync(`${root}/unknown.pid`, 'utf8'))
  process.kill(workloadPID, 0)
  proof.actualLocalWorkloads.push('unknown')
  console.log('PASS deliberately lost peer confirmation follows real second process start: PG/browser retain unknown and original request UUID')

  await context.setOffline(true)
  console.log(execFileSync('python3', [`${root}/restart.py`], { encoding: 'utf8' }).trim())
  proof.restartedServer = true
  process.kill(workloadPID, 0)
  await context.setOffline(false)
  await page.reload()
  await page.getByRole('button', { name: 'Tasks', exact: true }).click()
  await page.getByRole('button').filter({ hasText: f.operation.slice(0, 8) }).click()
  await panel.getByRole('button', { name: 'Open terminal sample', exact: true }).click()
  await panel.getByText('Input delivery is unknown. Check the same request; do not resend with a new ID.', { exact: true }).waitFor()
  await panel.locator('pre').filter({ hasText: 'WORKLOAD unknown START' }).waitFor()
  assert.equal(await send.isDisabled(), true)
  assert.deepEqual(await page.evaluate(() => Object.entries(sessionStorage).filter(([key]) => key.startsWith('memoh:native-followup:'))), pending)
  assert.equal(count(), 2); process.kill(workloadPID, 0)
  await page.screenshot({ path: `${root}/same-task-after-server-restart.png` })
  console.log('PASS actual Server process restart with same PG: same operation/request/pane output, original local PID still alive, no third prompt')
  fs.writeFileSync(`${root}/release-unknown`, 'release finite fixture only')
  await panel.getByRole('button', { name: 'Refresh content and request status', exact: true }).click()
  await page.waitForFunction(() => document.querySelector('[aria-label="Native Agent content and follow-up"] pre')?.textContent?.includes('WORKLOAD unknown EXIT 0'), { timeout: 12000 })
  await panel.getByText('Input delivery is unknown. Check the same request; do not resend with a new ID.', { exact: true }).waitFor()
  assert.equal(count(), 2)
  console.log('PASS real workload exit and sampled output do not promote unknown receipt or task to completed')

  console.log(execFileSync('python3', [`${root}/permission.py`, 'revoke'], { encoding: 'utf8' }).trim())
  await panel.getByRole('button', { name: 'Refresh content and request status', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('[aria-label="Native Agent content and follow-up"] pre'))
  const callsBefore = JSON.parse(fs.readFileSync(`${root}/calls.json`)).length
  // True handler permission failure, not a mocked browser response.
  const denied = await page.evaluate(async ({ f, token }) => {
    const response = await fetch(`/api/bots/${f.bot}/sessions/${f.root}/tasks/computer_operation/${f.operation}/followup`, { method: 'POST', headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }, body: JSON.stringify({ request_id: crypto.randomUUID(), mode: 'bound_session', task: 'Must not execute' }) })
    return { status: response.status, cache: response.headers.get('Cache-Control') }
  }, { f, token })
  assert.equal(denied.status, 404); assert.equal(denied.cache, 'no-store')
  await page.waitForTimeout(5500)
  assert.equal(JSON.parse(fs.readFileSync(`${root}/calls.json`)).length, callsBefore)
  assert.equal(count(), 2)
  proof.revocation = denied
  console.log('PASS actual Runtime revocation: content cleared; unauthorized follow-up 404/no-store; automatic content reads stop; zero peer calls or new workload')
  console.log(execFileSync('python3', [`${root}/permission.py`, 'restore'], { encoding: 'utf8' }).trim())
  await page.reload()
  await page.getByRole('button', { name: 'Tasks', exact: true }).click()
  await page.getByRole('button').filter({ hasText: f.operation.slice(0, 8) }).click()
  await panel.getByRole('button', { name: 'Open terminal sample', exact: true }).click()
  await panel.getByText('Input delivery is unknown. Check the same request; do not resend with a new ID.', { exact: true }).waitFor()
  assert.equal(count(), 2)
  await page.setViewportSize({ width: 390, height: 844 })
  await panel.scrollIntoViewIfNeeded()
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
  await page.screenshot({ path: `${root}/unknown-narrow.png` })
  assert.deepEqual(proof.errors, [])
  proof.passed = true
  console.log('PASS permission restored, same task/request read only; narrow shared Web layout; no business replay')
} finally {
  // Complete receipt reads before closing the page: teardown must not race
  // a late, denied follow-up response. This is test-harness-only.
  observedPage?.off('response', onResponse)
  await Promise.all(responseReads)
  fs.writeFileSync(`${root}/browser-proof.json`, JSON.stringify(proof, null, 2) + '\n')
  await browser.close()
}

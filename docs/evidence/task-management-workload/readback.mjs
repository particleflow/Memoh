// Read-only closure of the first browser run. No prompt or workload is repeated.
import fs from 'node:fs'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { chromium } from '/opt/codex/runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright/index.mjs'
const root = '/tmp/task-management-workload'
const f = JSON.parse(fs.readFileSync(`${root}/private-fixture.json`))
const saved = JSON.parse(fs.readFileSync(`${root}/private-readback.json`))
const token = fs.readFileSync('/tmp/web-observation-reconnect/private-token', 'utf8').trim()
const key = `memoh:native-followup:${createHash('sha256').update(token).digest('hex')}:${f.bot}:${f.root}:${f.operation}`
const browser = await chromium.launch({ headless: true, executablePath: '/usr/bin/chromium', args: ['--no-sandbox'] })
let mutations = 0
try {
  const context = await browser.newContext({ viewport: { width: 1100, height: 950 } })
  // This continuation injects the already persisted request for a readback;
  // original same-tab storage survival was proven in browser-first-exit1.log.
  await context.addInitScript(({ token, key, id }) => { localStorage.setItem('token', token); localStorage.setItem('language', 'en'); sessionStorage.setItem(key, id) }, { token, key, id: saved.request_id })
  const page = await context.newPage(); page.setDefaultTimeout(18000)
  await page.route('**/api/**', route => {
    const request = route.request()
    if (!['GET', 'HEAD', 'OPTIONS'].includes(request.method())) { mutations++; return route.abort() }
    if (new URL(request.url()).pathname.endsWith('/workspace-targets')) return route.fulfill({ status: 200, contentType: 'application/json', body: '{"items":[]}' })
    return route.continue()
  })
  await page.goto(`http://127.0.0.1:18402/bot/${f.bot}`)
  await page.getByText(f.title, { exact: true }).first().click()
  await page.getByRole('button', { name: 'Tasks', exact: true }).click()
  await page.getByRole('button').filter({ hasText: f.operation.slice(0, 8) }).click()
  const panel = page.getByRole('region', { name: 'Native Agent content and follow-up', exact: true })
  await panel.getByRole('button', { name: 'Open terminal sample', exact: true }).click()
  await panel.getByText('Input delivery is unknown. Check the same request; do not resend with a new ID.', { exact: true }).waitFor()
  await panel.locator('pre').filter({ hasText: 'WORKLOAD unknown EXIT 0' }).waitFor()
  assert.equal(await panel.getByRole('button', { name: 'Send one instruction', exact: true }).isDisabled(), true)
  const observation = page.getByRole('region', { name: 'Current computer observation', exact: true })
  const button = observation.getByRole('button').first()
  await button.click()
  await observation.getByText('w-executable', { exact: true }).waitFor()
  await observation.getByText('p-executable', { exact: true }).waitFor()
  assert((await observation.innerText()).includes(f.target))
  await observation.getByText('w-executable', { exact: true }).scrollIntoViewIfNeeded()
  await page.screenshot({ path: `${root}/readback-with-native-references.png` })
  assert.equal(mutations, 0)
  assert.equal(JSON.parse(fs.readFileSync(`${root}/calls.json`)).filter(x => x === 'agent.prompt').length, 2)
  console.log('PASS read-only real shared Web continuation: unknown receipt, disabled resubmit, original target/workspace/pane; zero mutations or repeated workload')
} finally { await browser.close() }

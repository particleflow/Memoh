import fs from 'node:fs'
import assert from 'node:assert/strict'
import { chromium } from '/opt/codex/runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright/index.mjs'
const root = process.env.OBSERVATION_EVIDENCE_DIR
if (!root) throw new Error('OBSERVATION_EVIDENCE_DIR is required')
const state = JSON.parse(fs.readFileSync(`${root}/fixture-state.json`, 'utf8'))
const browser = await chromium.launch({ headless: true, executablePath: '/usr/bin/chromium', args: ['--no-sandbox'] })
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
  await context.addInitScript(token => {
    localStorage.setItem('token', token)
    localStorage.setItem('language', 'en')
    localStorage.setItem('theme', 'light')
  }, fs.readFileSync(`${root}/private-token`, 'utf8'))
  const page = await context.newPage()
  page.setDefaultTimeout(20000)
  const errors = [], requests = [], responses = []
  page.on('pageerror', error => errors.push(error.message))
  page.on('request', request => {
    const path = new URL(request.url()).pathname
    if (path.startsWith('/api/')) requests.push({ path, method: request.method() })
  })
  page.on('response', response => {
    const path = new URL(response.url()).pathname
    if (path.startsWith('/api/')) responses.push({ path, method: response.request().method(), status: response.status() })
  })
  const parentPath = `/api/bots/${state.source}/sessions/${state.session}/delegations/${state.delegation.delegation_id}`
  const childPrefix = `/api/bots/${state.delegation.target_bot_id}/sessions/${state.delegation.target_session_id}/tasks`
  const childPath = `${childPrefix}/${state.task.task_type}/${state.task.task_id}`
  const countChildren = () => requests.filter(r => r.path.startsWith(childPrefix)).length
  const dialog = page.getByRole('dialog')
  const title = dialog.getByText('Specialist Bot tasks', { exact: true })
  const text = dialog.getByText('DOWNSTREAM_VISIBLE_FINAL_42', { exact: true })
  const missing = locator => assert.equal(locator, 0)
  await page.goto('http://127.0.0.1:18102/bot/web-management-supervisor')
  await page.getByText('Web management actual delegation', { exact: true }).first().click()
  await page.getByRole('button', { name: 'Tasks', exact: true }).click()
  await page.getByRole('tab', { name: 'Specialist Bot handoffs', exact: true }).click()
  await page.getByRole('button').filter({ hasText: state.delegation.delegation_id.slice(0, 8) }).click()
  await page.getByRole('button', { name: 'View downstream tasks', exact: true }).click()
  await page.getByRole('button').filter({ hasText: state.task.task_id.slice(0, 8) }).click()
  await text.waitFor()
  await page.screenshot({ path: `${root}/browser-initial.png` })
  assert(responses.some(r => r.path === childPath && r.status === 200))
  console.log('PASS existing real PG delegation -> child task detail; no business dispatch')

  await context.setOffline(true)
  await dialog.getByText('Network connection failed. Check your connection and try again.', { exact: true }).waitFor()
  await title.waitFor()
  missing(await text.count())
  const pausedReads = countChildren()
  await page.waitForTimeout(5500)
  assert.equal(countChildren(), pausedReads)
  await page.screenshot({ path: `${root}/browser-offline.png` })
  await context.setOffline(false)
  await text.waitFor()
  await title.waitFor()
  assert.equal(requests.filter(r => r.path === childPrefix).length, 1)
  await page.screenshot({ path: `${root}/browser-recovered.png` })
  console.log('PASS real browser offline -> clear output/pause child reads -> automatic same-detail recovery')

  let injected = 0
  const unavailable = async route => {
    injected++
    await route.fulfill({ status: 503, contentType: 'application/problem+json', body: JSON.stringify({ status: 503, code: 'http.unavailable', detail: 'PRIVATE_SYNTHETIC_UPSTREAM' }) })
  }
  await page.route(`**${parentPath}`, unavailable)
  await page.waitForFunction(() => document.querySelector('[role=dialog]')?.textContent.includes('Reload view') && !document.querySelector('[role=dialog]')?.textContent.includes('DOWNSTREAM_VISIBLE_FINAL_42'))
  await title.waitFor()
  const paused503 = countChildren()
  await page.waitForTimeout(5500)
  assert.equal(countChildren(), paused503)
  assert(injected >= 1)
  missing(await page.getByText('PRIVATE_SYNTHETIC_UPSTREAM').count())
  await page.screenshot({ path: `${root}/browser-503.png` })
  await page.unroute(`**${parentPath}`, unavailable)
  await page.getByRole('button', { name: 'Reload view', exact: true }).click()
  await text.waitFor()
  await title.waitFor()
  assert.equal(requests.filter(r => r.path === childPrefix).length, 1)
  console.log('PASS parent-only synthetic 503 -> child pause -> explicit read-only reload returns to same detail')

  const denied = route => route.fulfill({ status: 403, contentType: 'application/problem+json', body: JSON.stringify({ status: 403, code: 'http.forbidden', detail: 'PRIVATE_SYNTHETIC_DENIAL' }) })
  await page.route(`**${parentPath}`, denied)
  await page.waitForFunction(() => document.querySelector('[role=dialog]')?.textContent.includes('Reload view') && !document.querySelector('[role=dialog]')?.textContent.includes('Specialist Bot tasks') && !document.querySelector('[role=dialog]')?.textContent.includes('DOWNSTREAM_VISIBLE_FINAL_42'))
  const stopped = countChildren()
  await page.waitForTimeout(5500)
  assert.equal(countChildren(), stopped)
  missing(await page.getByText('PRIVATE_SYNTHETIC_DENIAL').count())
  await page.screenshot({ path: `${root}/browser-denied.png` })
  await page.unroute(`**${parentPath}`, denied)
  assert.deepEqual(errors, [])
  assert.deepEqual(requests.filter(r => r.method !== 'GET' && r.method !== 'HEAD'), [])
  const proof = { realBackend: true, realPG: true, reusedExistingSyntheticTask: true, newModelCalls: 0, businessDispatches: 0, realBrowserOffline: true, syntheticHTTPFailures: [503, 403], sameTaskResumed: true, pausedReadsHeld: true, denialRetiresNavigation: true, pageErrors: errors, requests, responses }
  fs.writeFileSync(`${root}/browser-proof.json`, JSON.stringify(proof, null, 2) + '\n')
  console.log('PASS denial retires navigation, no stale output, no observation POST, no page errors')
} finally {
  await browser.close()
}

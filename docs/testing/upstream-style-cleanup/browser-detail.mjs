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
  await page.screenshot({ path: `/tmp/upstream-style-cleanup/browser-detail.png` })
  assert(responses.some(r => r.path === childPath && r.status === 200))
  console.log('PASS existing real PG delegation -> child task detail; no business dispatch')

  assert.equal(errors.length, 0)
  assert(requests.every(r => r.method === 'GET'))
  fs.writeFileSync('/tmp/upstream-style-cleanup/browser-proof.json', JSON.stringify({page_errors: errors, only_get_requests: true, api_requests: requests.length, child_detail_200: responses.some(r => r.path === childPath && r.status === 200)}, null, 2))
} finally {
  await browser.close()
}

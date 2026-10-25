import fs from 'node:fs'
import assert from 'node:assert/strict'
import { chromium } from '/opt/codex/runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright/index.mjs'
const root = '/tmp/delegation-run-integration'
const f = JSON.parse(fs.readFileSync(`${root}/private-fixture.json`, 'utf8'))
const token = fs.readFileSync('/tmp/web-observation-reconnect/private-token', 'utf8').trim()
const path = `/api/bots/${f.source}/sessions/${f.source_session}/delegations/${f.id}`
const targetPath = `/api/bots/${f.target}/sessions/${f.target_session}`
const proof = { realServer: true, existingSyntheticPG: true, mutations: 0, detailHTTP: [], targetSessionHTTP: [], targetHistoryHTTP: [], otherErrorHTTP: {}, pageErrors: [] }
const response = await fetch('http://127.0.0.1:18400'+path.slice(4), { headers: { Authorization: `Bearer ${token}` } })
assert.equal(response.status, 200)
const detail = await response.json()
assert.equal(detail.target_run_id, f.run)
assert.equal(detail.target_run_state, f.native_state)
assert(detail.target_run_created_at && detail.target_run_updated_at)
assert.equal(detail.state, f.state)
assert.equal(detail.delivery_state, f.delivery)
const anonymous = await fetch('http://127.0.0.1:18400'+path.slice(4))
assert.equal(anonymous.status, 401)
assert.match(anonymous.headers.get('cache-control'), /no-store/)
console.log('PASS real JWT/Server/PG projection: exact existing native run and times; delegation/delivery unchanged; anonymous 401 no-store')
const browser = await chromium.launch({ headless: true, executablePath: '/usr/bin/chromium', args: ['--no-sandbox'] })
try {
  const context = await browser.newContext({ viewport: { width: 1200, height: 1000 } })
  await context.addInitScript(token => { localStorage.setItem('token', token); localStorage.setItem('language', 'en') }, token)
  const page = await context.newPage(); page.setDefaultTimeout(20000)
  let unavailable = false
  await page.route(`**${path}`, route => unavailable ? route.fulfill({ status: 503, contentType: 'application/json', body: '{"message":"synthetic connection interruption"}' }) : route.continue())
  page.on('pageerror', e => proof.pageErrors.push(e.message))
  page.on('request', request => { if (new URL(request.url()).pathname.startsWith('/api/') && !['GET', 'HEAD', 'OPTIONS'].includes(request.method())) proof.mutations++ })
  page.on('response', response => {
    const p = new URL(response.url()).pathname
    if (p === path) proof.detailHTTP.push(response.status())
    if (p === targetPath) proof.targetSessionHTTP.push(response.status())
    if (p === `/api/bots/${f.target}/messages` && new URL(response.url()).searchParams.get('session_id') === f.target_session) proof.targetHistoryHTTP.push(response.status())
    if (p.startsWith('/api/') && p !== path && response.status() >= 400) proof.otherErrorHTTP[response.status()] = (proof.otherErrorHTTP[response.status()] || 0) + 1
  })
  await page.goto(`http://127.0.0.1:18402/bot/${f.source}`)
  await page.getByText(f.source_title, { exact: true }).first().click()
  await page.getByRole('button', { name: 'Tasks', exact: true }).click()
  await page.getByRole('tab', { name: 'Specialist Bot handoffs', exact: true }).click()
  await page.getByRole('button').filter({ hasText: f.id.slice(0,8) }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByText(`Run ID · ${f.run}`, { exact: true }).waitFor()
  await dialog.getByText('Specialist session run', { exact: true }).waitFor()
  await page.screenshot({ path: `${root}/specialist-run.png` })
  console.log('PASS supervisor handoff exposes admitted root run, separately from execution/delivery state')
  await dialog.getByRole('button', { name: 'View downstream tasks', exact: true }).click()
  await dialog.getByText('Subagents and computer tasks', { exact: true }).waitFor()
  await dialog.getByText(`Run ID · ${f.run}`, { exact: true }).waitFor()
  await page.screenshot({ path: `${root}/root-and-children.png` })
  unavailable = true
  await page.waitForResponse(r => new URL(r.url()).pathname === path && r.status() === 503)
  await dialog.getByText(`Run ID · ${f.run}`, { exact: true }).waitFor({ state: 'hidden' })
  console.log('PASS parent read failure clears unconfirmed run observation')
  unavailable = false
  await dialog.getByRole('button', { name: 'Reload view', exact: true }).click()
  await dialog.getByText(`Run ID · ${f.run}`, { exact: true }).waitFor()
  console.log('PASS explicit reload recovers the same handoff and native run without dispatch')
  await dialog.getByRole('button', { name: 'Open specialist session history', exact: true }).click()
  await page.waitForURL(url => url.pathname === `/bot/${f.target}`)
  await dialog.waitFor({ state: 'hidden' })
  await page.waitForTimeout(1500)
  assert(proof.targetSessionHTTP.includes(200))
  assert(proof.targetHistoryHTTP.includes(200), 'native session message/history endpoint must be read')
  await page.screenshot({ path: `${root}/specialist-native-history.png` })
  assert.equal(proof.mutations, 0)
  assert.deepEqual(proof.pageErrors, [])
  console.log('PASS fresh source relation and target-session permission check -> ordinary native history; zero task/chat/delivery mutations')
  fs.writeFileSync(`${root}/browser-proof.json`, JSON.stringify(proof, null, 2)+'\n')
} finally { await browser.close() }

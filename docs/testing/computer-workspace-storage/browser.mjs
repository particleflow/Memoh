import fs from 'node:fs'
import assert from 'node:assert/strict'

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base = process.env.WEB_BASE_URL
const storage = process.env.TEST_STORAGE_URL
const runtime = process.env.TEST_RUNTIME_ID
const token = process.env.TEST_STORAGE_TOKEN
const output = process.env.WORKSPACE_STORAGE_EVIDENCE_DIR
assert.ok(base && storage && runtime && token && output)
const webToken = fs.readFileSync(process.env.WEB_AUTH_TOKEN_FILE, 'utf8').trim()
const proof = { realPGAndHTTP: true, simulatedWindowsShare: true, mkdir: 0, save: 0, modelCalls: 0, errors: [], requests: [] }
fs.mkdirSync(output, { recursive: true })
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM_BINARY, args: ['--no-sandbox', '--disable-dev-shm-usage'] })
let page
try {
  const context = await browser.newContext({ viewport: { width: 1100, height: 850 }, colorScheme: 'dark' })
  await context.addInitScript(value => { localStorage.setItem('token', value); localStorage.setItem('language', 'zh') }, webToken)
  page = await context.newPage()
  page.on('pageerror', error => proof.errors.push(error.message))
  await page.route('**/api/users/me/runtimes**', async route => {
    const request = route.request(), url = new URL(request.url())
    if (request.method() === 'POST') proof.mkdir++
    if (request.method() === 'PUT') proof.save++
    // The shared page uses its normal API base and login. This isolated route
    // forwards only the new module to the fresh-PG fixture's synthetic account.
    const response = await route.fetch({ url: storage + url.pathname.replace(/^\/api/, '') + url.search, headers: { ...request.headers(), authorization: `Bearer ${token}` } })
    proof.requests.push({ method: request.method(), status: response.status(), storageRead: url.pathname.endsWith('/workspace-storage') })
    return route.fulfill({ response })
  })
  page.on('request', request => { if (/\/messages$|\/turns$|\/completion/.test(new URL(request.url()).pathname) && request.method() === 'POST') proof.modelCalls++ })
  await page.goto(base + '/settings/workspace-storage')
  const choose = page.getByRole('button', { name: '选择 NAS 目录', exact: true })
  await choose.click()
  await page.getByLabel('新建目录', { exact: true }).fill('browser-project')
  assert.equal(proof.mkdir, 0); assert.equal(proof.save, 0)
  await page.getByRole('button', { name: '创建', exact: true }).click()
  await page.getByRole('button', { name: 'project', exact: true }).locator('svg').click()
  await page.getByText('browser-project', { exact: true }).waitFor()
  await page.getByRole('button', { name: 'browser-project', exact: true }).click()
  assert.equal(proof.mkdir, 1); assert.equal(proof.save, 0)
  await page.screenshot({ path: output + '/desktop.png', fullPage: true })
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await page.getByRole('dialog').waitFor({ state: 'hidden' })
  assert.equal(proof.save, 1)
  await page.reload(); await choose.click()
  await page.getByRole('button', { name: 'project', exact: true }).locator('svg').click()
  await page.getByText('browser-project', { exact: true }).waitFor()
  const check = await context.request.get(`${storage}/users/me/runtimes/${runtime}/workspace-storage`, { headers: { authorization: `Bearer ${token}` } })
  assert.equal(check.status(), 200); assert.equal((await check.json()).selected_path, 'project/browser-project')
  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: output + '/narrow-zh-dark.png', fullPage: true })
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
  await page.getByRole('button', { name: '取消', exact: true }).click()
  assert.equal(proof.save, 1)
  await choose.click()
  await page.getByLabel('新建目录', { exact: true }).fill('unavailable-project')
  fs.writeFileSync(process.env.TEST_STORAGE_FAILURE_FILE, 'synthetic share disconnected')
  await Promise.all([page.waitForResponse(response => response.url().endsWith('/directories') && response.status() === 503), page.getByRole('button', { name: '创建', exact: true }).click()])
  await page.getByLabel('新建目录', { exact: true }).waitFor({ state: 'hidden' })
  assert.equal(await page.getByRole('button', { name: '保存', exact: true }).isDisabled(), true)
  await page.getByRole('button', { name: '取消', exact: true }).click()
  await Promise.all([page.waitForResponse(response => response.url().includes('/workspace-storage') && response.status() === 503), choose.click()])
  await page.getByRole('dialog').waitFor()
  assert.equal(await page.getByRole('button', { name: '保存', exact: true }).isDisabled(), true)
  assert.equal(proof.save, 1); assert.equal(proof.modelCalls, 0)
  assert.deepEqual(proof.errors, [])
  fs.writeFileSync(output + '/browser-proof.json', JSON.stringify(proof, null, 2) + '\n')
  console.log('PASS actual shared settings page with fresh PostgreSQL/HTTP: browse, successful mkdir once, explicit save once, refresh preserved selection, cancel no write, narrow Chinese dark; actual HTTP503 from unavailable simulated share disables NAS writes; no model calls. Windows share is simulated.')
} catch (error) {
  if (page) {
    await page.screenshot({ path: output + '/failure.png', fullPage: true })
    fs.writeFileSync(output + '/failure-dialog.txt', await page.getByRole('dialog').innerText().catch(() => 'No open dialog'))
    fs.writeFileSync(output + '/failure-proof.json', JSON.stringify(proof, null, 2))
  }
  throw error
} finally { await browser.close() }

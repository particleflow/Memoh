import fs from 'node:fs'
import assert from 'node:assert/strict'
import { chromium } from '/opt/codex/runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright/index.mjs'
const root = '/tmp/media-owner-integration'
const token = fs.readFileSync('/tmp/web-observation-reconnect/private-token', 'utf8').trim()
const proof = { realServerPG: true, syntheticCatalog: true, modelCalls: 0, catalogRequests: 0, pageErrors: [] }
const browser = await chromium.launch({ headless: true, executablePath: '/usr/bin/chromium', args: ['--no-sandbox'] })
try {
  const context = await browser.newContext({ viewport: { width: 1200, height: 1000 } })
  await context.addInitScript(value => { localStorage.setItem('token', value); if (!localStorage.getItem('language')) localStorage.setItem('language', 'en') }, token)
  const page = await context.newPage(); page.setDefaultTimeout(20000)
  page.on('pageerror', error => proof.pageErrors.push(error.message))
  page.on('request', request => {
    const path = new URL(request.url()).pathname
    if (path.endsWith('/media-catalog')) proof.catalogRequests++
    if (request.method() === 'POST' && /\/(test|import-models|chat\/completions|images\/(generations|edits))$/.test(path)) proof.modelCalls++
  })
  await page.goto('http://127.0.0.1:18402/settings/providers')
  await page.getByText('Synthetic combined owner media catalog', { exact: true }).click()
  const title = page.getByRole('button', { name: 'Media catalog', exact: true })
  await title.waitFor(); await title.click()
  assert.equal(proof.catalogRequests, 0)
  async function read(mode, label = 'Read catalog') {
    fs.writeFileSync(`${root}/mode`, mode)
    const [response] = await Promise.all([
      page.waitForResponse(r => new URL(r.url()).pathname.endsWith('/media-catalog')),
      page.getByRole('button', { name: label, exact: true }).click(),
    ])
    return { status: response.status(), body: await response.json() }
  }
  const success = await read('success')
  assert.equal(success.status, 200); assert.equal(success.body.transcription_status, 'unverified')
  assert.equal(success.body.models[0].capability_source, 'catalog_kind_and_modalities')
  await page.getByText('synthetic/video-exact', { exact: true }).waitFor()
  await page.screenshot({ path: `${root}/catalog-success.png` })
  const denied = await read('forbidden')
  assert.equal(denied.status, 502); assert.equal(denied.body.code, 'agent.provider_permission_denied')
  assert(!JSON.stringify(denied.body).includes('synthetic-do-not-display'))
  await page.getByText('synthetic/video-exact', { exact: true }).waitFor({ state: 'hidden' })
  const empty = await read('empty'); assert.equal(empty.status, 200); assert.deepEqual(empty.body.models, [])
  await page.getByRole('button', { name: 'Close', exact: true }).filter({ hasText: /^Close$/ }).click()
  const name = page.getByRole('textbox', { name: 'Name', exact: true })
  await name.fill('Synthetic unsaved edit'); assert.equal(await title.isEnabled(), false)
  // Reload discards the unsaved edit; it must not cause remote catalog discovery.
  await page.evaluate(() => { localStorage.setItem('language', 'zh'); localStorage.setItem('theme', 'dark') })
  await page.setViewportSize({ width: 430, height: 900 }); await page.reload()
  const locale = JSON.parse(fs.readFileSync('/tmp/memoh-candidate-7bcb-review/apps/web/src/i18n/locales/zh.json', 'utf8')).provider.mediaCatalog
  await page.getByRole('button', { name: locale.title, exact: true }).click()
  assert.equal(proof.catalogRequests, 3)
  const recovered = await read('success', locale.read); assert.equal(recovered.status, 200)
  await page.getByText('synthetic/audio-exact', { exact: true }).waitFor()
  await page.screenshot({ path: `${root}/catalog-narrow.png` })
  assert.equal(proof.modelCalls, 0); assert.equal(proof.catalogRequests, 4); assert.deepEqual(proof.pageErrors, [])
  proof.passed = true
  console.log('PASS actual provider page: manual catalog read, exact metadata/unverified ASR, upstream error cleared old facts, valid empty list, unsaved edits disabled, reload did not discover, narrow localized recovery.')
} finally {
  fs.writeFileSync(`${root}/browser-proof.json`, JSON.stringify(proof, null, 2) + '\n')
  await browser.close()
}

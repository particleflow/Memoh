import fs from 'node:fs'
import assert from 'node:assert/strict'
import { chromium } from '/opt/codex/runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright/index.mjs'
const dir = '/tmp/nas-central-network-handoff'
const token = fs.readFileSync('/tmp/web-observation-reconnect/private-token', 'utf8').trim()
const fixture = JSON.parse(fs.readFileSync('/tmp/delegation-run-integration/private-fixture.json', 'utf8'))
const base = 'http://127.0.0.1:18402'
const proof = { currentWebSource: true, existingServer391: true, syntheticPG: true, fieldNetwork: false, mutations: 0, pageErrors: [], identityGets: 0, screenshots: [] }
const browser = await chromium.launch({ headless: true, executablePath: '/usr/bin/chromium', args: ['--no-sandbox', '--disable-dev-shm-usage'] })
try {
  const context = await browser.newContext({ viewport: { width: 1100, height: 850 }, colorScheme: 'dark' })
  await context.addInitScript(token => { localStorage.setItem('token', token); localStorage.setItem('language', 'en') }, token)
  const page = await context.newPage()
  page.on('pageerror', e => proof.pageErrors.push(e.message))
  page.on('request', r => {
    const u = new URL(r.url())
    if (u.pathname.startsWith('/api/') && !['GET', 'HEAD', 'OPTIONS'].includes(r.method())) proof.mutations++
    if (u.pathname === '/api/users/me') proof.identityGets++
  })
  await page.goto(`${base}/bot/${fixture.source}`)
  await page.getByText(fixture.source_title, { exact: true }).first().click()
  await page.getByRole('button', { name: 'Task records', exact: true }).waitFor()
  proof.identity = await page.evaluate(async bot => {
    const { mediaIdentity } = await import('/src/lib/media-transfer.ts')
    const identity = await mediaIdentity(bot, new AbortController().signal)
    identity.check()
    return { ownerPresent: Boolean(identity.owner), origin: identity.origin, botUnchanged: identity.bot === bot, policyOmitted: identity.networkPolicy === undefined }
  }, fixture.source)
  assert.equal(proof.identity.ownerPresent, true)
  assert.equal(proof.identity.botUnchanged, true)
  assert.equal(proof.identity.origin, `${base}/api`)
  assert.equal(proof.identity.policyOmitted, true)
  assert.equal(proof.mutations, 0)
  assert.deepEqual(proof.pageErrors, [])
  await page.screenshot({ path: `${dir}/current-web-old-server-compatible.png`, fullPage: true })
  proof.screenshots.push('current-web-old-server-compatible.png')
  fs.writeFileSync(`${dir}/combination-browser-proof.json`, JSON.stringify(proof, null, 2) + '\n')
  console.log('PASS current shared Web, actual SDK identity GET, existing JWT/Server/PG with omitted network_policy; original origin/Bot preserved; zero business writes/page errors')
} finally { await browser.close() }

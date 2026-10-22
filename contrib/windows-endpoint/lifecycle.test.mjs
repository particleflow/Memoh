import test from 'node:test'
import assert from 'node:assert/strict'
import * as fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { EndpointLifecycle, LifecycleError, StateStore, acquireLeases, connectorAttention, leaseKeys } from './lifecycle.mjs'
import { managedEnvironment } from './endpoint.mjs'

async function fixture(t, options = {}) {
  const directory = await fs.realpath(await fs.mkdtemp(path.join(os.tmpdir(), 'endpoint-life-')))
  const calls = [], alive = new Set()
  const driver = {
    status: { state: 'online', lastError: '' },
    validate: async () => { calls.push('validate') },
    hostStatus: () => ({ running: alive.has('herdr'), version: '0.9.5', endpoint_compatible: true }),
    alive: c => alive.has(c),
    connectorStatus: async () => driver.status,
    start: async c => { calls.push(`start:${c}`); alive.add(c) },
    waitReady: async c => { calls.push(`ready:${c}`) },
    stop: async c => { calls.push(`stop:${c}`); alive.delete(c) },
    recoverConnectorLock: async () => { calls.push('recover:lock') },
  }
  const store = new StateStore(directory, 'fixture-device')
  const create = () => new EndpointLifecycle({ driver, store, acquire: () => acquireLeases([directory], directory), clock: () => 1000000, delay: async () => {}, ...options })
  const lifecycle = create()
  t.after(async () => { if (lifecycle.release) await lifecycle.stop(); await fs.rm(directory, { recursive: true, force: true }) })
  return { lifecycle, create, driver, alive, calls, store, directory }
}

test('starts Herdr API before connector and normal stop reverses order', async t => {
  const { lifecycle, calls } = await fixture(t)
  const snapshot = await lifecycle.start()
  assert.deepEqual(calls, ['validate', 'start:herdr', 'ready:herdr', 'start:connector', 'ready:connector'])
  assert.equal(snapshot.ready, false)
  assert.equal(snapshot.transfer, 'lifecycle_interface_pending')
  await lifecycle.stop()
  assert.deepEqual(calls.slice(-2), ['stop:connector', 'stop:herdr'])
})

test('single instance protects shared identity even with another config location', async t => {
  const { lifecycle, create } = await fixture(t)
  await lifecycle.start()
  await assert.rejects(create().start(), /endpoint_already_owned/)
  const keys = leaseKeys({ deviceId: 'device', herdrConfigRoot: 'root', session: 'one', agenvoConfigDir: 'connector' })
  assert.equal(keys[0], 'device:device')
  await lifecycle.stop()
  const next = create(); await next.start(); await next.stop()
})

test('concurrent wake checks recover a crashed connector once without restarting host', async t => {
  const { lifecycle, alive, calls } = await fixture(t)
  await lifecycle.start(); alive.delete('connector')
  await Promise.all([lifecycle.tick(), lifecycle.tick(), lifecycle.tick()])
  assert.equal(calls.filter(c => c === 'start:connector').length, 2)
  assert.equal(calls.filter(c => c === 'start:herdr').length, 1)
  assert.equal(lifecycle.state.restarts.length, 1)
})

test('host crash stops connector then revalidates and recovers in dependency order', async t => {
  const { lifecycle, alive, calls } = await fixture(t)
  await lifecycle.start(); alive.delete('herdr'); calls.length = 0
  await lifecycle.tick()
  assert.deepEqual(calls, ['stop:connector', 'validate', 'start:herdr', 'ready:herdr', 'start:connector', 'ready:connector'])
})

test('offline live connector retains ownership of network reconnect without supervisor retries', async t => {
  const { lifecycle, driver, calls } = await fixture(t)
  await lifecycle.start(); driver.status = { state: 'offline', lastError: 'connection_error' }
  await lifecycle.tick(); await lifecycle.tick()
  assert.equal(lifecycle.state.phase, 'running')
  assert.equal(lifecycle.state.restarts.length, 0)
  assert.equal(calls.filter(c => c.startsWith('start:')).length, 2)
})

for (const lastError of ['handshake_401', 'handshake_403', 'handshake_426', 'config_changed_restart_required']) {
  test(`terminal ${lastError} persists attention and never restarts`, async t => {
    const { lifecycle, driver, alive, create, calls } = await fixture(t)
    await lifecycle.start(); driver.status = { state: 'needs_attention', lastError }; alive.delete('connector')
    await lifecycle.tick(); await lifecycle.tick()
    assert.equal(lifecycle.state.attention, connectorAttention(driver.status))
    assert.equal(lifecycle.state.restarts.length, 0)
    await lifecycle.stop()
    await assert.rejects(create().start(), new RegExp(connectorAttention(driver.status)))
    assert.equal(calls.filter(c => c.startsWith('start:')).length, 2)
  })
}

test('restart budget survives stop/start and explicit acknowledgement does not reset it', async t => {
  const { lifecycle, alive, create } = await fixture(t)
  await lifecycle.start()
  for (let i = 0; i < 4; i++) { alive.delete('connector'); await lifecycle.tick() }
  assert.equal(lifecycle.state.attention, 'restart_budget_exhausted')
  await lifecycle.stop()
  const next = create()
  await next.start({ acknowledgeAttention: true })
  alive.delete('connector'); await next.tick()
  assert.equal(next.state.attention, 'restart_budget_exhausted')
  assert.equal(next.state.restarts.length, 3)
  await next.stop()
})

test('unknown host observation stops recovery without guessing from unavailable', async t => {
  const { lifecycle, driver, calls } = await fixture(t)
  await lifecycle.start()
  driver.hostStatus = () => { throw new LifecycleError('herdr_observation_unknown') }
  await lifecycle.tick()
  assert.equal(lifecycle.state.attention, 'herdr_observation_unknown')
  assert.equal(calls.filter(c => c.startsWith('start:')).length, 2)
})

test('preexisting host is never adopted or started over', async t => {
  const { lifecycle, driver, calls } = await fixture(t)
  driver.hostStatus = () => ({ running: true })
  await assert.rejects(lifecycle.start(), /existing_herdr_requires_handoff/)
  assert.equal(calls.some(c => c.startsWith('start:')), false)
  // NativeDriver.stop only touches owned child handles; no PID/process lookup kill.
})

test('partial startup failure cleans up owned children and releases lease', async t => {
  const { lifecycle, driver, alive } = await fixture(t)
  driver.waitReady = async c => { if (c === 'connector') throw new LifecycleError('connector_startup_exited') }
  await assert.rejects(lifecycle.start(), /connector_startup_exited/)
  assert.equal(alive.size, 0); assert.equal(lifecycle.release, null)
})

test('stop during backoff prevents respawn', async t => {
  let resume, waiting
  const entered = new Promise(r => { waiting = r })
  const f = await fixture(t, { delay: () => new Promise(r => { resume = r; waiting() }) })
  await f.lifecycle.start(); f.alive.delete('connector')
  const tick = f.lifecycle.tick(); await entered
  const stop = f.lifecycle.stop(); resume(); await Promise.all([tick, stop])
  assert.equal(f.calls.filter(c => c === 'start:connector').length, 1)
  assert.equal(f.alive.size, 0)
})

test('lifecycle journal never alters original files, session or transfer journal', async t => {
  const { lifecycle, directory } = await fixture(t)
  const originals = ['original.bin', 'session.json', 'queue.jsonl', 'credentials.json']
  for (const file of originals) await fs.writeFile(path.join(directory, file), `unchanged:${file}`)
  await lifecycle.start(); await lifecycle.stop()
  for (const file of originals) assert.equal(await fs.readFile(path.join(directory, file), 'utf8'), `unchanged:${file}`)
})

test('shared managed environment removes all reserved pollution, not just overlay keys', () => {
  const config = { bundleRoot: '/bundle', herdrConfigRoot: '/config/herdr', herdrStateRoot: '/state/herdr', agenvoConfigDir: '/agenvo', piConfigDir: '/pi' }
  const env = managedEnvironment(config, { PATH: 'polluted', NODE_OPTIONS: '--require=bad', NODE_PATH: '/bad', BASH_ENV: '/bad', ENV: '/bad', HERDR_SESSION: 'bad', HERDR_SOCKET: '/bad', AGENVO_SECRET: 'bad', BASH_FUNC_bad: 'bad', SystemRoot: 'C:\\Windows' })
  for (const key of ['NODE_OPTIONS', 'NODE_PATH', 'BASH_ENV', 'ENV', 'HERDR_SESSION', 'HERDR_SOCKET', 'AGENVO_SECRET', 'BASH_FUNC_bad']) assert.equal(env[key], undefined)
  assert.equal(env.PATH.includes('polluted'), false)
  assert.equal(env.AGENVO_CONFIG_DIR, '/agenvo')
})


test('stop during startup waits for owned cleanup and never starts connector', async t => {
  const { lifecycle, driver, alive, calls } = await fixture(t)
  let resume, entered
  const ready = new Promise(r => { entered = r })
  driver.waitReady = async c => { if (c === 'herdr') await new Promise(r => { resume = r; entered() }) }
  const start = lifecycle.start()
  const rejected = assert.rejects(start, /startup_cancelled/)
  await ready
  const stops = [lifecycle.stop(), lifecycle.stop()]
  resume(); await Promise.all([rejected, ...stops])
  assert.equal(calls.includes('start:connector'), false)
  assert.equal(alive.size, 0)
  assert.equal(lifecycle.release, null)
  assert.equal(lifecycle.state.attention, null)
})

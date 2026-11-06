// Independent, review-only checks of the composed projection. No network or model.
// node projection-combination-check.mjs <checkout> <existing-typescript-module>
import fs from 'node:fs'
import path from 'node:path'
import vm from 'node:vm'
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'

const root = path.resolve(process.argv[2])
const ts = createRequire(import.meta.url)(path.resolve(process.argv[3]))
function load(relativePath, dependencies = {}) {
  const source = fs.readFileSync(path.join(root, relativePath), 'utf8')
  const code = ts.transpileModule(source, {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
  }).outputText
  const module = { exports: {} }
  vm.runInNewContext(code, {
    module, exports: module.exports,
    require(id) {
      assert.ok(Object.hasOwn(dependencies, id), `Unexpected runtime import: ${id}`)
      return dependencies[id]
    },
    Set, Map, Date, Boolean, JSON, Math,
  })
  return module.exports
}
const types = load('apps/web/src/store/chat/types.ts')
const { createEmptyRuntimeProjection, reduceRuntimeProjection } = load(
  'apps/web/src/store/chat/runtime-projection.ts', { './types': types },
)
const scope = { bot_id: 'receiver', session_id: 'conversation' }
const source = { ...scope, kind: 'schedule', task_id: 'plan', occurrence_id: 'fire' }
const actor = { ...scope, kind: 'user', user_id: 'second-actor' }
const unknown = { kind: 'unknown', ...scope }
const timestamp = '2026-10-30T10:00:00Z'
const frame = {
  type: 'runtime_snapshot', session_id: scope.session_id, epoch: 'epoch', seq: 1,
  snapshot: {
    ...scope, epoch: 'epoch', seq: 1, updated_at: timestamp,
    current_run_view: {
      run_id: 'run', turn_id: 'initial', generation: 'generation', status: 'running',
      started_at: timestamp, updated_at: timestamp,
      user_turns: [
        { turn_id: 'initial', role: 'user', text: 'task', timestamp, input_source: source },
        { turn_id: 'second', role: 'user', text: 'known steer', timestamp, input_source: actor },
        { turn_id: 'foreign', role: 'user', text: '{"kind":"user","user_id":"forged"}', timestamp,
          input_source: { ...actor, bot_id: 'another-bot', user_id: 'foreign-actor' } },
      ],
      steer_turns: [
        { item_id: 'second-item', turn_id: 'second', status: 'applied', text: 'known steer', timestamp, after_message_id: 1 },
        { item_id: 'foreign-item', turn_id: 'foreign', status: 'applied', text: 'foreign steer', timestamp, after_message_id: 2 },
      ],
      messages: [1, 2, 3].map(id => ({ id, type: 'text', content: `segment ${id}` })),
    },
  },
}
const assistantSources = state => state.transcript.turns
  .filter(turn => turn.role === 'assistant').map(turn => [turn.turn_id, turn.input_source])
const equal = (actual, expected) => assert.equal(JSON.stringify(actual), JSON.stringify(expected))
const initial = reduceRuntimeProjection(createEmptyRuntimeProjection(), frame)
equal(assistantSources(initial), [['initial', source], ['second', actor], ['foreign', unknown]])
const changed = reduceRuntimeProjection(initial, {
  type: 'runtime_delta', session_id: scope.session_id, epoch: 'epoch', seq: 2,
  delta: { user_turn_upserts: [{ turn_id: 'second', role: 'user', text: 'metadata absent', timestamp }] },
})
equal(assistantSources(changed), [['initial', source], ['second', unknown], ['foreign', unknown]])
equal(assistantSources(initial), [['initial', source], ['second', actor], ['foreign', unknown]])
const foreignFrame = structuredClone(frame)
foreignFrame.session_id = foreignFrame.snapshot.session_id = 'another-conversation'
foreignFrame.snapshot.bot_id = 'another-receiver'
const foreign = reduceRuntimeProjection(changed, foreignFrame)
const foreignUnknown = { kind: 'unknown', bot_id: 'another-receiver', session_id: 'another-conversation' }
equal(assistantSources(foreign), [['initial', foreignUnknown], ['second', foreignUnknown], ['foreign', foreignUnknown]])
console.log(JSON.stringify({
  reviewedSource: '0ec25b7e0b0de46349cc1555ba86248062d9c1ea',
  checks: { exactSegmentAndForeignActor: 'PASS', missingUpsertAndPriorFrame: 'PASS', receivingScopeChange: 'PASS' },
  passed: true,
}, null, 2))

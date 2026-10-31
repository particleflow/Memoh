// Review-only counterexample. Uses the candidate's actual projection and types;
// no application, network, provider, database or business operation is started.
// node runtime-source-repro.mjs <candidate-checkout> <existing-typescript-module>
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
    module,
    exports: module.exports,
    require(id) {
      assert.ok(Object.hasOwn(dependencies, id), `Unexpected runtime import: ${id}`)
      return dependencies[id]
    },
    Set, Map, Date, Boolean, JSON, Math,
  })
  return module.exports
}
const types = load('apps/web/src/store/chat/types.ts')
const projection = load('apps/web/src/store/chat/runtime-projection.ts', { './types': types })
const source = {
  kind: 'schedule', bot_id: 'bot-review', session_id: 'schedule-review',
  task_id: 'plan-review', task_run_id: 'run-review', occurrence_id: 'fire-review',
}
const state = projection.reduceRuntimeProjection(projection.createEmptyRuntimeProjection(), {
  type: 'runtime_snapshot', session_id: source.session_id, epoch: 'epoch-review', seq: 1,
  snapshot: {
    bot_id: source.bot_id, session_id: source.session_id, epoch: 'epoch-review', seq: 1,
    updated_at: '2026-10-30T10:00:00Z',
    current_run_view: {
      run_id: source.task_run_id, turn_id: 'turn-review', generation: 'generation-review',
      status: 'running', started_at: '2026-10-30T10:00:00Z', updated_at: '2026-10-30T10:00:00Z',
      request_user_turn: {
        turn_id: 'turn-review', role: 'user', text: 'synthetic scheduled input',
        timestamp: '2026-10-30T10:00:00Z', input_source: source,
      },
      messages: [{ id: 1, type: 'text', content: 'synthetic reply' }],
    },
  },
})
const turns = state.transcript.turns
const actualUser = turns.find(turn => turn.role === 'user')?.input_source
const actualAssistant = turns.find(turn => turn.role === 'assistant')?.input_source ?? null
assert.equal(JSON.stringify(actualUser), JSON.stringify(source), 'request source must survive the real projection')
const passed = JSON.stringify(actualAssistant) === JSON.stringify(source)
console.log(JSON.stringify({
  candidate: '65649801a58946993d2824f3963255750661eb2a',
  path: 'reduceRuntimeProjection(runtime_snapshot)',
  userSource: actualUser,
  assistantSource: actualAssistant,
  expectedAssistantSource: source,
  passed,
}, null, 2))
// A red result is intentional evidence, not a skipped assertion or a passing test.
if (!passed) process.exitCode = 1

import assert from 'node:assert/strict'
import { ESLint } from 'eslint'

const eslint = new ESLint()
const cases = [
  ...['node:fs', 'node:path', 'node:crypto', 'node:util'].map(module => ({
    file: 'internal/agenvo/pi_history.cjs', module, allowed: false,
  })),
  ...['fs', 'node:child_process', 'arbitrary-package'].map(module => ({
    file: 'internal/agenvo/pi_history.cjs', module, allowed: false,
  })),
  { file: 'internal/agenvo/pi_result.cjs', module: 'node:util', allowed: false },
  { file: 'docs/testing/unrelated.cjs', module: 'node:fs', allowed: false },
]
for (const { file, module, allowed } of cases) {
  const [result] = await eslint.lintText(`require(${JSON.stringify(module)})\n`, { filePath: file })
  const rejected = result.messages.some(message => message.ruleId === '@typescript-eslint/no-require-imports' && message.severity === 2)
  assert.equal(rejected, !allowed, `${file}: ${module}`)
}
for (const module of ['node:fs', 'node:path', 'node:crypto', 'node:util']) {
  const [result] = await eslint.lintText(`void import(${JSON.stringify(module)})\n`, { filePath: 'internal/agenvo/pi_history.cjs' })
  assert.equal(result.errorCount, 0, module)
}
console.log(`PASS ${cases.length} forbidden require probes and 4 permitted dynamic import probes; no lint policy changed`)

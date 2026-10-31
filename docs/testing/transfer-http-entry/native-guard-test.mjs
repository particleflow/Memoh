import fs from 'node:fs'
import assert from 'node:assert/strict'
import ts from 'typescript'
const fixture = JSON.parse(fs.readFileSync(new URL('./native-guard-source.json', import.meta.url)))
const source = fixture.source.replace('export function memohRequest', 'function memohRequest')
const js = ts.transpile(source, { target: ts.ScriptTarget.ES2022 })
const predicate = new Function('url', 'MEMOH_ORIGIN', js + '\nreturn memohRequest;')({ URL: { parseURL: value => new URL(value) } }, 'https://logical.fixture.test/api')
const hash = 'a'.repeat(64)
const rows = [
  { target: `https://logical.fixture.test/api/bots/synthetic/media/${hash}`, expected: true },
  { target: `http://logical.fixture.test/api/bots/synthetic/media/${hash}`, expected: false },
  { target: `http://fast.fixture.test:18683/api/bots/synthetic/media/${hash}`, expected: false },
  { target: `https://foreign.fixture.test/api/bots/synthetic/media/${hash}`, expected: false },
  { target: `https://logical.fixture.test/api/bots/synthetic/media/%252f${hash}`, expected: false },
]
for (const row of rows) { row.allowed = predicate(row.target); assert.equal(row.allowed, row.expected) }
const result = { sourceCommit: fixture.commit, sourceBlob: fixture.sha, sourcePath: fixture.path, scope: 'Exact SharedWebPolicy predicate using standard URL Kit adapter; not ArkWeb or RCP runtime', rows, cases: rows.length, passed: true, nativeProductChanged: false, rebuiltOrPackaged: false }
fs.writeFileSync(new URL('./native-guard-proof.json', import.meta.url), JSON.stringify(result, null, 2) + '\n')
console.log(JSON.stringify(result))

// Execute the real Python and TS validators against the pinned shared contract.
const assert = require('node:assert/strict')
const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm')
const { spawnSync } = require('node:child_process')
const { createHash } = require('node:crypto')
const ts = require(process.env.HARMONY_TEST_TYPESCRIPT || 'typescript')
const root = path.resolve(__dirname, '../..')
const fixtureBytes = fs.readFileSync(path.join(__dirname, 'fixtures/api-base-v1.json'))
assert.equal(createHash('sha256').update(fixtureBytes).digest('hex'),
  'ec9b39d5b16b2655e225cba9eb4e97483001e21b9fdd45ad095074b7e0a68c59')
const fixture = JSON.parse(fixtureBytes.toString('utf8'))
assert.equal(fixture.contract, 'memoh.file-api-base.v1')
assert.equal(fixture.cases.length, 30)
const moduleExports = {}
vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(root,
  'mobile/client/mobile/memoh/harmony-service-url.ts'), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText, { exports: moduleExports, URL, Error })

// Existing mobile policy, explicitly distinguished from transfer syntax:
// API bases require /api; public HTTP is retained for compatibility. Python's
// address allowlist additionally rejects the documentation-only IPv6 example.
const shared = fixture.cases.map(item => ({ ...item, field: 'apiOrigin',
  tsAccepted: item.name === 'https-root' ? false : item.name === 'http' ? true : item.accepted,
  pyAccepted: ['https-root', 'configured-ipv6'].includes(item.name) ? false : item.name === 'http' ? true : item.accepted,
}))
const local = [
  ['https://alpha.example/%61pi', null],
  ['https://alpha.example/foo/../api', null],
  ['https://alpha.example/foo/./api', null],
  ['https://alpha.example/not-api', null],
  ['https://alpha.example/api//', null],
  ['https://alpha.example:000/api', null],
  ['https://alpha.example:no/api', null],
  ['https://alpha.example:-1/api', null],
  ['https://alpha.example:1.2/api', null],
  ['https://alpha.example:65535/api', 'https://alpha.example:65535/api'],
  ['https://alpha.example:1/api', 'https://alpha.example:1/api'],
  ['https://ALPHA.example:443/gateway/api/', 'https://alpha.example/gateway/api'],
  ['http://alpha.example:80/api/', 'http://alpha.example/api'],
  ['https://nas.lan:08443/api', 'https://nas.lan:8443/api'],
  ['https://[2606:4700:0:0:0:0:0:1111]:443/api/', 'https://[2606:4700::1111]/api'],
  ['https://nas.lan/api\n', null],
  ['https://nas.lan/api\u007f', null],
  ['https://nas.lan/api\u00a0', null],
  ['https://download.example:443/releases/client.hap', 'https://download.example/releases/client.hap', 'hapDownloadUrl'],
  ['https://download.example/', 'https://download.example', 'hapDownloadUrl'],
  ['https://download.example:/releases/client.hap', null, 'hapDownloadUrl'],
  ['https://download.example/releases/../client.hap', null, 'hapDownloadUrl'],
].map(([input, normalized, field = 'apiOrigin'], index) => ({
  name: `mobile-${index}`, input, field, normalized,
  tsAccepted: normalized !== null, pyAccepted: normalized !== null,
}))
const cases = [...shared, ...local]
const python = spawnSync('python3', ['-c', `
import importlib.util,json,sys
spec=importlib.util.spec_from_file_location('config',sys.argv[1])
config=importlib.util.module_from_spec(spec)
spec.loader.exec_module(config)
results=[]
for case in json.load(sys.stdin):
    try:
        results.append({'accepted':True,'normalized':config.public_url(case['input'],case['field'])})
    except ValueError:
        results.append({'accepted':False})
print(json.dumps(results))
`, path.join(root, 'mobile/client/mobile/client_config.py')], {
  input: JSON.stringify(cases), encoding: 'utf8',
})
assert.equal(python.status, 0, python.stderr)
const pyResults = JSON.parse(python.stdout)
cases.forEach((item, index) => {
  let result
  try { result = { accepted: true, normalized: moduleExports.normalizeServiceUrl(item.input, item.field === 'apiOrigin') } }
  catch { result = { accepted: false } }
  assert.equal(result.accepted, item.tsAccepted, `${item.name}: TS acceptance`)
  assert.equal(pyResults[index].accepted, item.pyAccepted, `${item.name}: Python acceptance`)
  if (item.tsAccepted && item.pyAccepted) {
    assert.equal(result.normalized, pyResults[index].normalized, `${item.name}: normalization parity`)
    assert.equal(result.normalized, item.normalized ?? item.input.replace(/\/$/, ''), `${item.name}: expected URL`)
  }
})
console.log(JSON.stringify({ passed: true, sharedVectors: shared.length, additionalCases: local.length,
  sharedPolicyExceptions: ['https-root', 'http'], existingPythonAddressRestriction: 'configured-ipv6',
  scope: 'Real validator syntax/normalization only; no DNS, TLS, automatic route selection or credential migration' }))

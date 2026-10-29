// Execute the real document bootstrap and attachment component lifecycle with
// DOM/Vue boundaries. This is neither a rendered UI nor device qualification.
const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm'), assert = require('node:assert/strict')
const sourceRef = process.env.HARMONY_LIFETIME_SOURCE_REF
if (sourceRef) assert.match(sourceRef, /^[a-f0-9]{40}$/)
const root = path.resolve(__dirname, '../..'), ts = require(process.env.HARMONY_TEST_TYPESCRIPT || path.join(root, 'mobile/.build/memoh-web/checkout/node_modules/typescript'))
function execute(file, imports, globals = {}) {
  const m = { exports: {} }, source = sourceRef ? require('node:child_process').execFileSync('git', ['show', sourceRef + ':mobile/client/mobile/memoh/' + file], { cwd: root, encoding: 'utf8' }) : fs.readFileSync(path.join(root, 'mobile/memoh', file), 'utf8')
  vm.runInNewContext(ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText, {
    module: m, exports: m.exports, require: name => { assert.ok(name in imports, name); return imports[name] }, Promise, Error, JSON, Map, AbortController, ...globals,
  }); return m.exports
}
const settle = async () => { for (let i = 0; i < 12; i++) await new Promise(setImmediate) }
async function boot(mode) {
  const events = [], nodes = new Map(), config = { apiOrigin: 'https://nas.example/api', resetPending: mode === 'reset' }
  const node = id => { if (!nodes.has(id)) { const value = { dataset: {}, value: '', hidden: false, querySelector: node }; if (id === 'app') Object.defineProperty(value, 'innerHTML', { set() { events.push('form') } }); nodes.set(id, value) } return nodes.get(id) }
  const storage = new Map(mode === 'edit' ? [['edit', '1']] : []), router = { currentRoute: { value: { name: 'Home' } }, afterEach() {} }
  const imports = {
    './harmony-back': { installHarmonyBack() {} },
    './harmony-service-config': {
      SERVICE_CONFIG_KEY: 'config', EDIT_SERVICE_KEY: 'edit',
      async readDeploymentConfig() { if (mode === 'invalid') throw Error('invalid'); return {} }, async applyDeploymentConfig() { return config },
      deploymentNetworkPolicy() {}, inspectServiceSetup: () => ({ kind: 'bound' }), finishServiceTransition() {}, commitServiceTransition: v => ({ ...v, resetPending: false }),
    },
    vue: { effectScope: () => ({ run: f => f(), stop() {} }), watch() {} },
    '@vueuse/core': { useStorage: () => ({ value: 'system' }), useColorMode: () => ({ state: { value: 'auto' } }) },
    '../constants/color-schemes': {}, './harmony-theme.json': {}, '../style.css': {}, './harmony-service.css': {},
    './auth-session': { clearPersistedUserScopedState() { events.push('auth-clear') } }, '../pages/onboarding/constants': { ONBOARDING_KEYS: {} },
    '../main': {}, '../router': { __esModule: true, default: router },
  }
  execute('harmony-entry.ts', imports, {
    window: { begoniaNative: { async request(raw) { events.push(JSON.parse(raw).command); return '{"ok":true}' } }, addEventListener() {} },
    document: { documentElement: node('html'), getElementById: node, createElement: node, body: { append() { events.push('mounted') } } }, location: { protocol: 'begonia-client:' },
    sessionStorage: { getItem: key => storage.get(key), removeItem: key => storage.delete(key) }, localStorage: { removeItem() {} },
  })
  await settle(); return events
}
async function component() {
  const lifecycle = execute('harmony-transfer-lifetime.ts', {}), events = [], hooks = [], watch = [], props = { botId: 'bot-a', scope: 'session-a', disabled: false, modelValue: [] }
  let exposed, release, commandSignal
  const source = fs.readFileSync(path.join(root, 'mobile/memoh/HarmonyAttachments.vue'), 'utf8').split('<script setup lang="ts">')[1].split('</script>')[0]
  const m = { exports: {} }, imports = {
    vue: { ref: value => ({ value }), watch: (get, fn) => watch.push(fn), onBeforeUnmount: fn => hooks.push(fn), onMounted() {} },
    '@felinic/ui': {}, './harmony-transfer-lifetime': lifecycle,
    './harmony-resumable': { nativeAttachmentsAvailable: true, releaseNativeAttachmentView: async () => events.push('release-preview'), nativeUploadFailure() {}, validateNativeAttachments: v => v,
      nativeAttachmentCommand: (_kind, _bot, _scope, _fields, signal) => { commandSignal = signal; return new Promise(resolve => release = resolve) } },
    './harmony-upload-failures': { restoreNativeUploadFailures: async () => {} }, '@/lib/auth-session': { onAuthSessionCleared: () => () => {} },
  }
  vm.runInNewContext(ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText, {
    module: m, exports: m.exports, require: name => { assert.ok(name in imports, name); return imports[name] },
    defineProps: () => props, defineEmits: () => () => assert.fail('unmounted component emitted a result'), defineExpose: v => exposed = v, AbortController, Map, Error,
  })
  const pending = exposed.select(); assert.equal(commandSignal.aborted, false); hooks[0](); assert.equal(commandSignal.reason, lifecycle.NATIVE_VIEW_DETACHED)
  release([]); await pending; assert.deepEqual(events, ['release-preview'])
}
;(async () => {
  assert.deepEqual(await boot('normal'), ['client.configure', 'mounted'])
  assert.deepEqual(await boot('edit'), ['client.configuration', 'form'])
  assert.deepEqual(await boot('reset'), ['client.configuration', 'auth-clear', 'client.configure', 'mounted'])
  assert.deepEqual(await boot('invalid'), ['client.configuration', 'form'])
  await component()
  const calls = [], router = { currentRoute: { value: { name: 'Home' } }, afterEach() {}, isReady: async () => {} }
  execute('harmony.ts', { '../router': { __esModule: true, default: router }, './harmony-theme.json': {}, '@/lib/api-client': { sdkAuthQuery: () => ({ token: 'synthetic-current' }) } },
    { window: { begoniaNative: { async request(raw) { calls.push(JSON.parse(raw)); return '{"ok":true}' } } } })
  await settle(); assert.equal(calls[0].authorization, 'Bearer synthetic-current')
  console.log(JSON.stringify({ actualBootstrap: true, normalReloadRetainsTransfers: true, configurationEditStillRevokes: true, resetBeforeAuthCleanup: true, invalidConfigurationFailsClosed: true, actualComponentUnmountDetaches: true, noLateComponentEmit: true, currentSessionReadyEnvelope: true, renderedUI: false, nativeDevice: false }))
})().catch(e => { console.error(e); process.exitCode = 1 })

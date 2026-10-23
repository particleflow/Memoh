const fs = require('fs'), path = require('path'), vm = require('vm'), assert = require('assert'), crypto = require('crypto');
const ts = require(process.env.HARMONY_TEST_TYPESCRIPT || path.resolve('validation/harmony/.build/commandline-tools/command-line-tools/sdk/default/openharmony/ets/build-tools/ets-loader/node_modules/typescript'));
const workerPath = 'mobile/harmony/app/entry/src/main/ets/workers/VoiceRecognition.ets';
const contractPath = 'mobile/harmony/app/entry/src/main/ets/common/VoiceTextProtocol.ets';
const transpile = name => ts.transpileModule(fs.readFileSync(name, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.CommonJS } }).outputText;
const flush = () => new Promise(resolve => setImmediate(resolve));
async function scenario(completion) {
  const messages = []; let listener;
  const port = { postMessage: reply => messages.push(reply), close() {} };
  let busy = false;
  const engine = { setListener: value => { listener = value; }, isBusy: () => busy, startListening: value => { busy = true; listener.onStart(value.sessionId, ''); }, finish() {}, cancel() { busy = false; }, shutdown() {} };
  const contract = { exports: {} };
  vm.runInNewContext(transpile(contractPath), { module: contract, exports: contract.exports });
  vm.runInNewContext(transpile(workerPath), { exports: {}, require: name => name === '@kit.CoreSpeechKit' ? { speechRecognizer: { createEngine: async () => engine } } : name === '@kit.ArkTS' ? { worker: { workerPort: port } } : name.includes('VoiceTextProtocol') ? contract.exports : {}, setTimeout, clearTimeout, Date, Error, Promise, Uint8Array, console });
  const session = 'dictation-' + 'a'.repeat(32);
  port.onmessage({ data: { request: 1, command: 'start', session, language: 'zh-CN', durationMs: 20000 } });
  await flush(); assert(messages.some(reply => reply.request === 1 && !reply.error));
  // A last-sentence intermediate is genuine text but is not final completion.
  listener.onResult(session, { result: '你好', isFinal: false, isLast: true });
  port.onmessage({ data: { request: 2, command: 'finish', session } });
  await flush(); assert(!messages.some(reply => reply.request === 2), 'last partial must not complete dictation');
  if (completion === 'final') { busy = false; listener.onResult(session, { result: '你好。', isFinal: true, isLast: true }); }
  else {
    listener.onResult(session, { result: completion === 'empty' ? '' : '你好。', isFinal: true, isLast: false });
    busy = false; listener.onComplete(session, '');
  }
  await flush();
  const reply = messages.find(value => value.request === 2);
  assert(reply && !reply.error); assert.strictEqual(reply.value, completion === 'empty' ? '' : '你好。');
  port.onmessage({ data: { request: 0, command: 'close', session: '' } }); await flush();
  return { completion, actualFinal: reply.value, prematurePartialResponse: false };
}
async function warmingCancellation() {
  const messages = []; let completeCreation, listener, busy = false, creations = 0, shutdowns = 0, cancels = 0;
  const port = { postMessage: reply => messages.push(reply), close() {} };
  const engine = { isBusy: () => busy, setListener: value => { listener = value; }, startListening: value => { assert(!busy); busy = true; listener.onStart(value.sessionId, ''); }, cancel() { cancels++; busy = false; }, shutdown() { shutdowns++; } };
  const creation = new Promise(resolve => { completeCreation = () => resolve(engine); });
  const contract = { exports: {} };
  vm.runInNewContext(transpile(contractPath), { module: contract, exports: contract.exports });
  vm.runInNewContext(transpile(workerPath), { exports: {}, require: name => name === '@kit.CoreSpeechKit' ? { speechRecognizer: { createEngine: () => { creations++; return creation; } } } : name === '@kit.ArkTS' ? { worker: { workerPort: port } } : name.includes('VoiceTextProtocol') ? contract.exports : {}, setTimeout, clearTimeout, Date, Error, Promise, Uint8Array });
  const first = 'dictation-' + 'b'.repeat(32), second = 'dictation-' + 'c'.repeat(32);
  const send = (request, command, session = '') => port.onmessage({ data: { request, command, session, language: 'zh-CN', durationMs: 20000 } });
  send(1, 'prepare'); send(2, 'start', first); await flush();
  send(0, 'cancel', first); send(3, 'start', second); completeCreation(); await flush();
  assert.strictEqual(messages.find(reply => reply.request === 2)?.error, 'VOICE_TEXT_CANCELLED');
  assert(messages.some(reply => reply.request === 3 && !reply.error));
  assert.strictEqual(creations, 1); assert.strictEqual(shutdowns, 0); assert.strictEqual(cancels, 0);
  listener.onResult(first, { result: '旧内容', isFinal: true, isLast: true });
  assert(!messages.some(reply => reply.kind === 'partial' && reply.session === first), 'retired session leaked into replacement');
  send(0, 'cancel', second); await flush(); assert.strictEqual(cancels, 1); assert.strictEqual(shutdowns, 0);
  send(0, 'close'); await flush(); assert.strictEqual(shutdowns, 1);
  return { creationCancelledSessionDoesNotDestroyWarmEngine: true, staleResultIgnored: true, shutdownOnlyOnClose: true };
}
async function invalidEngineRecovery(code, phase) {
  const messages = []; let creations = 0, shutdowns = 0, oldListener;
  const port = { postMessage: reply => messages.push(reply), close() {} };
  const contract = { exports: {} };
  vm.runInNewContext(transpile(contractPath), { module: contract, exports: contract.exports });
  const createEngine = async () => {
    const first = ++creations === 1; let listener, busy = false;
    const invalid = where => { if (first && phase === where) { const error = new Error('SDK invalid engine'); error.code = code; throw error; } };
    return {
      isBusy: () => busy,
      setListener: value => { listener = value; if (first) oldListener = value; },
      startListening: value => { invalid('start'); busy = true; listener.onStart(value.sessionId, ''); },
      writeAudio() { invalid('write'); }, finish() { invalid('finish'); },
      cancel() { invalid('cancel'); busy = false; }, shutdown() { shutdowns++; busy = false; }
    };
  };
  vm.runInNewContext(transpile(workerPath), { exports: {}, require: name => name === '@kit.CoreSpeechKit' ? { speechRecognizer: { createEngine } } : name === '@kit.ArkTS' ? { worker: { workerPort: port } } : name.includes('VoiceTextProtocol') ? contract.exports : {}, setTimeout, clearTimeout, Date, Error, Promise, Uint8Array });
  const first = 'dictation-' + 'd'.repeat(32), second = 'dictation-' + 'e'.repeat(32);
  const send = (request, command, session = first) => port.onmessage({ data: { request, command, session, language: 'zh-CN', durationMs: 20000, bytes: new Uint8Array(640) } });
  send(1, 'start'); await flush();
  if (phase === 'callback') oldListener.onError(first, code, '');
  else if (phase !== 'start') { send(2, phase); await flush(); }
  assert.strictEqual(creations, 1, 'failed recording must not be automatically restarted');
  assert.strictEqual(shutdowns, 1, 'official invalid-engine code must discard the cache');
  assert(messages.some(reply => reply.phase === 'invalid-engine-discarded'));
  oldListener?.onStart(first, 'late start');
  oldListener?.onResult(first, { result: '失效后晚到', isFinal: false, isLast: false });
  oldListener?.onResult(first, { result: '失效后晚到', isFinal: true, isLast: true });
  oldListener?.onComplete(first, 'late complete');
  assert(!messages.some(reply => reply.kind === 'partial' && reply.session === first), 'retired engine callback updated cancelled result');
  send(0, 'cancel'); await flush();
  send(3, 'start', second); await flush();
  assert(messages.some(reply => reply.request === 3 && !reply.error), 'new user session must recover');
  assert.strictEqual(creations, 2);
  oldListener?.onResult(first, { result: '旧错误会话', isFinal: true, isLast: true });
  assert(!messages.some(reply => reply.kind === 'partial' && reply.session === first), 'invalid session leaked text');
  send(0, 'cancel', second); await flush();
  send(0, 'close', ''); await flush(); assert.strictEqual(shutdowns, 2);
  return { code, phase, retiredInvalidEngine: true, nextSessionRecovered: true, automaticRetry: false };
}
(async () => {
  const cases = []; for (const kind of ['final', 'onComplete', 'empty']) cases.push(await scenario(kind));
  const reuse = await warmingCancellation();
  const invalidEngines = [];
  for (const code of [1002200007, 1002200008]) for (const phase of ['start', 'callback', 'write', 'finish', 'cancel']) invalidEngines.push(await invalidEngineRecovery(code, phase));
  console.log(JSON.stringify({ sourceSha256: crypto.createHash('sha256').update(fs.readFileSync(workerPath)).digest('hex'), scope: 'Actual production worker with isolated SDK callbacks, no audio/device/provider; not a speed measurement', cases, reuse, invalidEngines }));
})().catch(error => { console.error(error.stack); process.exitCode = 1; });

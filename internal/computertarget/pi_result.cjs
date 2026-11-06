// Memoh-owned, finite post-invocation adapter. This is not a Pi extension or
// Agent patch. It reads official Pi v1.2 JSONL locally after Pi has exited.
// No network, provider calls, subprocesses or user-supplied code are evaluated.
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const {Worker, isMainThread, workerData} = require('node:worker_threads');

const MAX_INPUT = 64 * 1024 * 1024;
const MAX_RESULT = 8192;
const MAX_TEXT = 2048;
const decoder = new TextDecoder('utf-8', {fatal: true});

function preview(text) {
  let data = Buffer.from(text, 'utf8');
  if (data.length <= MAX_TEXT) return [text, false];
  data = data.subarray(0, MAX_TEXT);
  for (;;) {
    try { return [decoder.decode(data), true]; }
    catch { data = data.subarray(0, data.length - 1); }
  }
}

function verifier(expected) {
  const result = {outcome: 'invalid_protocol', truncated: false, records: 0};
  let valid = true, started = false, running = false, turn = false;
  let message = false, settled = false, aborted = false, final = false;
  const tools = new Set();
  function require(condition) { if (!condition) throw Error('invalid protocol'); }
  return {
    line(raw) {
      if (!valid) return;
      try {
        const event = JSON.parse(decoder.decode(raw));
        require(event && typeof event.type === 'string' && event.type !== '');
        result.records++;
        if (result.records === 1) {
          require(event.type === 'session' && event.version === 3 && event.id === expected.pi_session_id);
          return;
        }
        switch (event.type) {
          case 'session': throw Error('duplicate session');
          case 'agent_start':
            require(!running && !message && !turn && tools.size === 0);
            running = started = true; settled = final = false; break;
          case 'turn_start':
            require(running && !turn); turn = true; settled = final = false; break;
          case 'turn_end':
            require(turn && !message && tools.size === 0); turn = false; break;
          case 'agent_end':
            require(running && !turn && !message && tools.size === 0); running = false; break;
          case 'message_start':
            require(running && turn && !message); message = true; settled = false; break;
          case 'message_update': require(message); break;
          case 'message_end': {
            require(started && message); message = settled = false;
            const m = event.message;
            require(m && typeof m === 'object');
            if (m.role !== 'assistant') break;
            require(m.provider === expected.provider && m.model === expected.model && Array.isArray(m.content));
            let text = '';
            for (const part of m.content) {
              require(part && typeof part === 'object');
              if (part.type === 'text') { require(typeof part.text === 'string'); text += part.text; }
            }
            [result.output, result.truncated] = preview(text);
            require(typeof m.stopReason === 'string');
            result.stop_reason = m.stopReason; final = true; break;
          }
          case 'tool_execution_start':
            require(running && turn && typeof event.toolCallId === 'string' && event.toolCallId !== '' && !tools.has(event.toolCallId));
            tools.add(event.toolCallId); settled = false; break;
          case 'tool_execution_end': require(tools.has(event.toolCallId)); tools.delete(event.toolCallId); break;
          case 'tool_execution_update': require(tools.has(event.toolCallId)); break;
          case 'agent_settled':
            require(started && !running && !turn && !settled && typeof event.aborted === 'boolean' && !message && tools.size === 0);
            settled = true; aborted = event.aborted; break;
          case 'auto_retry_start': case 'compaction_start':
          case 'summarization_retry_scheduled': case 'summarization_retry_attempt_start':
            settled = false; break;
          case 'queue_update':
            require((event.steering === undefined || Array.isArray(event.steering)) && (event.followUp === undefined || Array.isArray(event.followUp)));
            if (event.steering?.length || event.followUp?.length) settled = false;
            break;
          case 'auto_retry_end': case 'compaction_end': case 'summarization_retry_finished':
          case 'entry_appended': case 'session_info_changed': case 'thinking_level_changed': break;
          default: throw Error('unrecognized event');
        }
      } catch { valid = false; }
    },
    finish(exitCode, complete, overLimit) {
      if (overLimit) result.outcome = 'output_limit';
      else if (valid && complete && settled && final && !running && !turn && !message && tools.size === 0) {
        if (aborted || result.stop_reason === 'aborted') result.outcome = 'aborted';
        else if (exitCode !== 0 || result.stop_reason === 'error') result.outcome = 'failed';
        else if (['length', 'toolUse'].includes(result.stop_reason)) result.outcome = 'incomplete';
        else if (result.stop_reason === 'stop') result.outcome = 'succeeded';
      }
      // Invalid/oversize streams must not present an earlier reply as a final
      // answer. Never export thinking blocks, tool bodies or error diagnostics.
      if (['invalid_protocol', 'output_limit'].includes(result.outcome)) {
        delete result.output; delete result.stop_reason; result.truncated = false;
      }
      return result;
    },
  };
}

function summarize(filename, expected, exitCode) {
  const verify = verifier(expected), hash = crypto.createHash('sha256');
  const fd = fs.openSync(filename, 'r');
  let size = 0, fragments = [], pendingBytes = 0, overLimit = false;
  try {
    if (!fs.fstatSync(fd).isFile()) throw Error('regular output file required');
    for (;;) {
      // Each retained fragment owns its buffer. Scan only the new chunk;
      // concatenate a split record once, when its LF arrives. A long line
      // therefore has linear scan/copy cost rather than quadratic prefixes.
      const buffer = Buffer.allocUnsafe(65536);
      const n = fs.readSync(fd, buffer, 0, buffer.length, null);
      if (n === 0) break;
      const chunk = buffer.subarray(0, n); hash.update(chunk); size += n;
      if (size > MAX_INPUT) { overLimit = true; fragments = []; pendingBytes = 0; continue; }
      let start = 0, end;
      while ((end = chunk.indexOf(10, start)) !== -1) {
        const part = chunk.subarray(start, end);
        if (fragments.length) {
          fragments.push(part);
          verify.line(Buffer.concat(fragments, pendingBytes + part.length));
          fragments = []; pendingBytes = 0;
        } else verify.line(part);
        start = end + 1;
      }
      if (start < n) {
        const part = chunk.subarray(start);
        fragments.push(part); pendingBytes += part.length;
      }
    }
  } finally { fs.closeSync(fd); }
  return {stdout: {name: 'stdout.jsonl', sha256: hash.digest('hex'), size},
    result: verify.finish(exitCode, pendingBytes === 0, overLimit)};
}

function main(code) {
  const dir = __dirname;
  const context = JSON.parse(fs.readFileSync(path.join(dir, 'result-context.json'), 'utf8'));
  if (!Number.isInteger(code) || code < 0 || code > 255) throw Error('invalid exit status');
  const {stdout, result} = summarize(path.join(dir, 'stdout.jsonl'), context, code);
  const record = {version: 1, attempt: context.attempt, generation: context.generation,
    request_hash: context.request_hash, pi_session_id: context.pi_session_id,
    workspace_id: context.workspace_id, pane_id: context.pane_id, terminal_id: context.terminal_id,
    stdout, result};
  let raw = JSON.stringify(record);
  // JSON escaping can expand a bounded UTF-8 preview. The wire result has its
  // own hard byte limit, independent of stdout/stderr size and terminal width.
  while (Buffer.byteLength(raw) > MAX_RESULT && result.output?.length) {
    result.output = Array.from(result.output).slice(0, -128).join('');
    result.truncated = true; raw = JSON.stringify(record);
  }
  if (Buffer.byteLength(raw) > MAX_RESULT) throw Error('result identity too large');
  const tmp = path.join(dir, 'result.tmp'), ready = path.join(dir, 'result.json');
  const fd = fs.openSync(tmp, 'wx', 0o600);
  try { fs.writeFileSync(fd, raw); fs.fsyncSync(fd); } finally { fs.closeSync(fd); }
  fs.renameSync(tmp, ready);
}

// The parser runs in an owned worker so synchronous file/JSON processing
// cannot starve the watchdog. Timeout terminates this helper process only:
// Pi has already exited. The envelope retains exit evidence without claiming
// semantic success or retrying the business invocation.
function runWithDeadline(exitCode, deadlineMs = 30000) {
  const worker = new Worker(__filename, {workerData: {exitCode}});
  const timer = setTimeout(() => process.exit(1), deadlineMs);
  worker.once('error', () => { clearTimeout(timer); process.exitCode = 1; });
  worker.once('exit', code => { clearTimeout(timer); if (code !== 0) process.exitCode = 1; });
}

module.exports = {summarize, runWithDeadline};
if (require.main === module) {
  try {
    if (isMainThread) runWithDeadline(Number(process.argv[2]));
    else main(workerData.exitCode);
  } catch { process.exitCode = 1; } // no raw paths or logs on stdout
}

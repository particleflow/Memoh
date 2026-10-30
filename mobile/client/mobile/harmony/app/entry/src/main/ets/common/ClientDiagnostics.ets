import { common } from '@kit.AbilityKit';
import { fileIo as fs } from '@kit.CoreFileKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { util } from '@kit.ArkTS';
import { BusinessError } from '@kit.BasicServicesKit';
import { cryptoFramework } from '@kit.CryptoArchitectureKit';

interface DiagnosticEvent { at: number; area: string; component: string; phase: string; detail: string; }
interface TraceEvent { seq: number; t: number; kind: string; g: number; request: number | null; method?: string; status?: number; loaded?: number; total?: number; lengthComputable?: boolean; check?: string; ok?: boolean; reason?: string; }
interface TraceBody { schema: number; document: string; timeUnit: string; events: TraceEvent[]; }
export interface DiagnosticBatch { id: string; document: string; through: number; body: string; writeAttempted?: boolean; }
interface QueueRecord { kind: string; at: number; scope: string; id: string; document?: string; through?: number; body?: string; writeAttempted?: boolean; }
interface QueueState { pending: Map<string, DiagnosticBatch>; acked: Set<string>; cursors: Map<string, number>; }

// Engineering state only: callers never pass messages, credentials or media content.
export class ClientDiagnostics {
  private static context?: common.Context;
  private static foregroundHook?: () => void;
  static bind(context: common.Context): void { ClientDiagnostics.context = context; }
  static onForeground(hook?: () => void): void { ClientDiagnostics.foregroundHook = hook; }
  static foreground(): void { try { ClientDiagnostics.foregroundHook?.(); } catch { hilog.error(0xB001, 'BegoniaClient', 'Diagnostic foreground callback failed'); } }
  private static shape(value: Object, fields: string[]): boolean { return Object.keys(value).every((key: string): boolean => fields.includes(key)); }
  private static number(value: number): boolean { return typeof value === 'number' && Number.isFinite(value) && value >= 0; }
  private static scope(value: string): void { if (!/^[a-f0-9]{64}$/.test(value)) { throw new Error('DIAGNOSTIC_SCOPE'); } }
  static validateBody(raw: string, document: string, through: number): void {
    const body = JSON.parse(raw) as TraceBody;
    if (!body || !ClientDiagnostics.shape(body, ['schema', 'document', 'timeUnit', 'events']) || body.schema !== 1 ||
      body.document !== document || !/^[a-f0-9]{32}$/.test(document) || body.timeUnit !== 'relative_monotonic_ms' ||
      !Array.isArray(body.events) || !body.events.length) { throw new Error('DIAGNOSTIC_BODY'); }
    let previous = 0;
    for (const event of body.events) {
      if (!ClientDiagnostics.shape(event, ['seq', 't', 'kind', 'g', 'request', 'method', 'status', 'loaded', 'total', 'lengthComputable', 'check', 'ok', 'reason']) ||
        !Number.isSafeInteger(event.seq) || event.seq <= previous || !ClientDiagnostics.number(event.t) || !Number.isSafeInteger(event.g) || event.g < 0 ||
        !['generation', 'stop_requested', 'run_failed', 'send', 'headers', 'progress', 'load', 'error', 'timeout', 'abort', 'loadend', 'validation'].includes(event.kind) ||
        (event.request !== null && (!Number.isSafeInteger(event.request) || event.request < 0)) ||
        (event.method !== undefined && !['HEAD', 'GET'].includes(event.method)) ||
        (event.status !== undefined && !ClientDiagnostics.number(event.status)) ||
        (event.loaded !== undefined && !ClientDiagnostics.number(event.loaded)) ||
        (event.total !== undefined && !ClientDiagnostics.number(event.total)) ||
        (event.lengthComputable !== undefined && typeof event.lengthComputable !== 'boolean') ||
        (event.check !== undefined && !['head', 'range_headers', 'body'].includes(event.check)) ||
        (event.ok !== undefined && typeof event.ok !== 'boolean') ||
        (event.reason !== undefined && event.reason !== 'user_stop')) { throw new Error('DIAGNOSTIC_BODY'); }
      previous = event.seq;
    }
    if (previous !== through || !Number.isSafeInteger(through)) { throw new Error('DIAGNOSTIC_CURSOR'); }
    // Reject hidden duplicate keys/noncanonical text before any persistence.
    if (JSON.stringify(body) !== raw) { throw new Error('DIAGNOSTIC_BODY'); }
  }
  private static path(): string {
    if (!ClientDiagnostics.context) { throw new Error('DIAGNOSTIC_NOT_BOUND'); }
    return ClientDiagnostics.context.filesDir + '/client-diagnostics.jsonl';
  }
  private static fold(scope: string): QueueState {
    ClientDiagnostics.scope(scope);
    const state: QueueState = { pending: new Map<string, DiagnosticBatch>(), acked: new Set<string>(), cursors: new Map<string, number>() };
    const path = ClientDiagnostics.path();
    for (const file of [path + '.previous', path]) {
      if (!fs.accessSync(file)) { continue; }
      const text = fs.readTextSync(file);
      // Only newline-committed records count. A torn tail cannot advance a cursor.
      const committed = text.slice(0, text.lastIndexOf('\n') + 1);
      for (const line of committed.split('\n')) {
        if (!line) { continue; }
        const record = JSON.parse(line) as QueueRecord;
        if (record.scope !== scope || !['diagnostic-put-v1', 'diagnostic-ack-v1'].includes(record.kind)) { continue; }
        if (!/^[a-f0-9]{64}$/.test(record.id)) { throw new Error('DIAGNOSTIC_JOURNAL_CORRUPT'); }
        if (record.kind === 'diagnostic-ack-v1') { state.acked.add(record.id); state.pending.delete(record.id); continue; }
        if (record.body === undefined || record.document === undefined || record.through === undefined) { throw new Error('DIAGNOSTIC_JOURNAL_CORRUPT'); }
        ClientDiagnostics.validateBody(record.body, record.document, record.through);
        state.cursors.set(record.document, Math.max(state.cursors.get(record.document) ?? 0, record.through));
        if (record.writeAttempted !== undefined && typeof record.writeAttempted !== 'boolean') { throw new Error('DIAGNOSTIC_JOURNAL_CORRUPT'); }
        if (!state.acked.has(record.id)) {
          const attempted = record.writeAttempted === true || state.pending.get(record.id)?.writeAttempted === true;
          state.pending.set(record.id, { id: record.id, document: record.document, through: record.through, body: record.body, writeAttempted: attempted });
        }
      }
    }
    return state;
  }
  static capturedThrough(scope: string, document: string): number {
    if (!/^[a-f0-9]{32}$/.test(document)) { throw new Error('DIAGNOSTIC_DOCUMENT'); }
    return ClientDiagnostics.fold(scope).cursors.get(document) ?? 0;
  }
  static pending(scope: string): DiagnosticBatch[] { return Array.from(ClientDiagnostics.fold(scope).pending.values()); }
  static async put(scope: string, batch: DiagnosticBatch): Promise<boolean> {
    ClientDiagnostics.scope(scope); ClientDiagnostics.validateBody(batch.body, batch.document, batch.through);
    const bytes = new util.TextEncoder().encodeInto(batch.body);
    const hash = cryptoFramework.createMd('SHA256'); await hash.update({ data: bytes });
    const id = Array.from((await hash.digest()).data).map((b: number): string => b.toString(16).padStart(2, '0')).join('');
    if (id !== batch.id) { throw new Error('DIAGNOSTIC_DIGEST'); }
    const state = ClientDiagnostics.fold(scope);
    if (state.acked.has(id)) { return false; }
    const pending = state.pending.get(id);
    if (pending) {
      if (batch.writeAttempted !== true || pending.writeAttempted) { return false; }
    } else if (batch.writeAttempted === true) { throw new Error('DIAGNOSTIC_NOT_PENDING'); }
    else if (batch.through <= (state.cursors.get(batch.document) ?? 0)) { throw new Error('DIAGNOSTIC_CURSOR'); }
    const record: QueueRecord = { kind: 'diagnostic-put-v1', at: Date.now(), scope, id, document: batch.document, through: batch.through, body: batch.body, writeAttempted: batch.writeAttempted === true };
    ClientDiagnostics.append(JSON.stringify(record));
    return true;
  }
  static ack(scope: string, id: string, verifiedHash: string, verifiedSize: number): void {
    if (id !== verifiedHash || !/^[a-f0-9]{64}$/.test(id)) { throw new Error('DIAGNOSTIC_ACK'); }
    const state = ClientDiagnostics.fold(scope);
    if (state.acked.has(id)) { return; }
    const pending = state.pending.get(id);
    if (!pending || new util.TextEncoder().encodeInto(pending.body).byteLength !== verifiedSize) { throw new Error('DIAGNOSTIC_ACK'); }
    const record: QueueRecord = { kind: 'diagnostic-ack-v1', at: Date.now(), scope, id };
    ClientDiagnostics.append(JSON.stringify(record));
  }
  private static append(line: string): void {
    const path = ClientDiagnostics.path();
    if (fs.accessSync(path) && fs.statSync(path).size > 1048576) {
      const previous = path + '.previous';
      if (fs.accessSync(previous)) { fs.unlinkSync(previous); }
      fs.renameSync(path, previous);
    }
    const file = fs.openSync(path, fs.OpenMode.CREATE | fs.OpenMode.READ_WRITE | fs.OpenMode.APPEND);
    try {
      const before = fs.statSync(file.fd).size;
      if (before) {
        const raw = new Uint8Array(before);
        if (fs.readSync(file.fd, raw.buffer, { offset: 0, length: before }) !== before) { throw new Error('DIAGNOSTIC_READ_INCOMPLETE'); }
        let end = before; while (end > 0 && raw[end - 1] !== 10) { end--; }
        if (end !== before) { fs.truncateSync(file.fd, end); fs.fsyncSync(file.fd); }
      }
      const bytes = new util.TextEncoder().encodeInto(line + '\n');
      let offset = 0;
      while (offset < bytes.byteLength) {
        const count = fs.writeSync(file.fd, bytes.buffer.slice(offset) as ArrayBuffer);
        if (count <= 0) { throw new Error('DIAGNOSTIC_WRITE_INCOMPLETE'); }
        offset += count;
      }
      fs.fsyncSync(file.fd);
    } finally { fs.closeSync(file); }
  }
  static failure(area: string, component: string, phase: string, error: Error, detail: string = ''): void {
    const code = (error as BusinessError).code;
    // SDK messages can contain source URIs, private paths or credentials. Retain only codes and our enums.
    const reason = /^(ASSET|NATIVE|MEDIA|VOICE_TEXT|UPLOAD|IMAGE|NETWORK_POLICY)_[A-Z0-9_]+$/.test(error.message) ? error.message : 'SDK_ERROR';
    ClientDiagnostics.event(area, component, phase, detail + '/code=' + (Number.isInteger(code) ? code : 0) + '/reason=' + reason);
  }
  static event(area: string, component: string, phase: string, detail: string): void {
    hilog.info(0xB001, 'BegoniaClient', '%{public}s %{public}s', component, phase);
    const context = ClientDiagnostics.context;
    if (!context) { return; }
    try {
      const record: DiagnosticEvent = { at: Date.now(), area, component, phase, detail };
      ClientDiagnostics.append(JSON.stringify(record));
    } catch { hilog.error(0xB001, 'BegoniaClient', 'Local diagnostic write failed'); }
  }
}

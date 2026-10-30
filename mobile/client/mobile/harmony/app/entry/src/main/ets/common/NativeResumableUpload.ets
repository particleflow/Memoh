import { common } from '@kit.AbilityKit';
import { fileIo as fs, picker, fileUri } from '@kit.CoreFileKit';
import { cryptoFramework } from '@kit.CryptoArchitectureKit';
import { uniformTypeDescriptor } from '@kit.ArkData';
import { util } from '@kit.ArkTS';
import { rcp } from '@kit.RemoteCommunicationKit';
import { MEMOH_ORIGIN, MEMOH_NETWORK_POLICY } from './MemohClientConfig';
import { NativeTransferNetwork, NativeTransferPolicy } from './NativeTransferNetwork';
import { copyMediaBytes, verifyMediaFd } from './NativeMediaFile';
import { NativePhotoSelection } from './NativePhotoSelection';
import { ClientDiagnostics } from './ClientDiagnostics';

export interface UploadScope { operation: string; botId: string; scope: string; authorization: string; handle?: string; photos?: boolean; recoverDrafts?: boolean; }
export interface UploadLocal { handle: string; name: string; type: string; size: number; hash: string; resumable: boolean; scope: string; }
interface Limits { max_upload_bytes: number; resumable_enabled?: boolean; chunk_max_bytes?: number; }
interface Identity { id: string; network_policy?: NativeTransferPolicy; }
interface Receipt { id: string; state: string; offset: number; size_bytes: number; expires_at: string; mime: string; content_hash?: string; }
export interface MediaReply { content_hash: string; size_bytes: number; mime: string; }
interface Reply { status: number; body: string; headers: rcp.ResponseHeaders; }
interface UploadSDKError extends Error { code: number; }
export interface UploadPreview { fd: number; mime: string; }
export interface UploadFailure { version: number; phase: string; sdkCode: number; at: number; }
export interface UploadStageFailure extends Error { uploadFailure?: UploadFailure; }
class StageError extends Error implements UploadStageFailure {
  private value?: UploadFailure;
  private allowed: () => boolean;
  constructor(error: Error, failure: UploadFailure | undefined, allowed: () => boolean) {
    super(error.message); this.value = failure; this.allowed = allowed;
  }
  get uploadFailure(): UploadFailure | undefined { return this.allowed() ? this.value : undefined; }
}
interface UploadStagingHandle { handle: string; }
interface UploadStagingOffset { offset: number; }
class Journal {
  version: number = 1; handle: string = ''; owner: string = ''; bot: string = ''; scope: string = '';
  name: string = ''; type: string = ''; size: number = 0; hash: string = ''; uploadId: string = '';
  offset: number = 0; expires: string = ''; retryAt?: number; retryDelay?: number; state: string = 'staged'; resumable: boolean = false; completePending: boolean = false;
}
class Running {
  apiOrigin: string = MEMOH_ORIGIN; epoch: number = 0; cancelled: boolean = false; staged: Journal[] = []; input: UploadScope; session: rcp.Session; journal?: Journal;
  phase: string = 'prepare'; accepted: boolean = false; retryWake?: () => void;
  failure?: StageError;
  copying: boolean = false; copyBusy: boolean = false; copyCommitted: boolean = false; copyOffset: number = 0; copyFile?: fs.File; ended: boolean = false;
  network: NativeTransferNetwork;
  constructor(input: UploadScope, epoch: number) { this.input = input; this.epoch = epoch; this.network = new NativeTransferNetwork(this.apiOrigin, MEMOH_NETWORK_POLICY); this.session = rcp.createSession(); }
}

// The existing app-owned immutable snapshot and atomic bounded journal
// survive process death. Tokens, arbitrary URIs and draft/model text never do.
export class NativeResumableUpload {
  private context: common.UIAbilityContext;
  private authorized: () => boolean;
  private progress: (operation: string, received: number, total: number) => void;
  private epoch: number = 0;
  private running?: Running;
  private root: string;
  private visible: Map<string, Journal> = new Map();
  private previews: Map<string, fs.File> = new Map();
  constructor(context: common.UIAbilityContext, authorized: () => boolean,
    progress: (operation: string, received: number, total: number) => void = (): void => {}) {
    this.progress = progress; this.context = context; this.authorized = authorized; this.root = context.filesDir + '/memoh-upload-journal-v1';
  }
  bindContext(context: common.UIAbilityContext): void { this.context = context; }
  private check(run: Running): void {
    if (!this.authorized() || run.epoch !== this.epoch || run.cancelled) { throw new Error('UPLOAD_CANCELLED'); }
  }
  private failed(run: Running, error: Error): Error {
    // Preserve the first staged failure when it crosses the outer select catch.
    if (run.failure) {
      if (error !== run.failure) { ClientDiagnostics.failure('SYSTEM', 'attachment-stage', 'cleanup', error); }
      return run.failure;
    }
    ClientDiagnostics.failure('SYSTEM', 'attachment-stage', run.phase, error);
    const code = (error as UploadSDKError).code;
    const failure: UploadFailure | undefined = this.authorized() && run.epoch === this.epoch ? {
      version: 1, phase: run.phase, sdkCode: Number.isSafeInteger(code) && code >= 0 && code <= 2147483647 ? code : 0,
      at: Date.now()
    } : undefined;
    const epoch = run.epoch;
    const result = new StageError(error, failure, () => this.authorized() && epoch === this.epoch);
    run.failure = result; return result;
  }
  private start(input: UploadScope): Running {
    if (!this.authorized() || !/^[a-f0-9]{32}$/.test(input.operation) || !/^[A-Za-z0-9_-]{1,128}$/.test(input.botId) ||
      !input.scope || input.scope.length > 512 || !/^Bearer [^\s\x00-\x1f]{1,8192}$/.test(input.authorization)) { throw new Error('UPLOAD_REQUEST'); }
    if (this.running) { throw new Error('UPLOAD_BUSY'); }
    if (!fs.accessSync(this.root)) { fs.mkdirSync(this.root); }
    // Incomplete local snapshot copies have no committed journal and cannot
    // be resumed. Reclaim only our generated names while no operation runs.
    for (const name of fs.listFileSync(this.root)) {
      const match = /^([a-f0-9]{32})(\.original|\.json\.tmp)$/.exec(name);
      if (match && (match[2] === '.json.tmp' || !fs.accessSync(this.path(match[1], '.json')))) { fs.unlinkSync(this.root + '/' + name); }
    }
    const run = new Running(input, this.epoch); this.running = run; return run;
  }
  private end(run: Running): void {
    if (run.ended) { return; }
    run.ended = true; run.retryWake?.();
    if (run.copyFile) { fs.closeSync(run.copyFile); run.copyFile = undefined; }
    if (run.copying && !run.copyCommitted && run.journal) { this.remove(run.journal); }
    run.session.close();
    if (run.cancelled) { for (const staged of run.staged) { this.remove(staged); } }
    if (run.cancelled && run.journal?.hash) { const path = this.path(run.journal.handle, '.original'); if (fs.accessSync(path)) { fs.unlinkSync(path); } }
    if (this.running === run) { this.running = undefined; }
  }
  private base(run: Running): string { return '/bots/' + encodeURIComponent(run.input.botId) + '/media'; }
  private path(handle: string, suffix: string): string {
    if (!/^[a-f0-9]{32}$/.test(handle)) { throw new Error('UPLOAD_IDENTITY'); }
    return this.root + '/' + handle + suffix;
  }
  private save(j: Journal, published?: () => void): void {
    const target = this.path(j.handle, '.json'), temporary = target + '.tmp';
    const file = fs.openSync(temporary, fs.OpenMode.CREATE | fs.OpenMode.TRUNC | fs.OpenMode.WRITE_ONLY | fs.OpenMode.NOFOLLOW);
    try {
      const bytes = new util.TextEncoder().encodeInto(JSON.stringify(j));
      if (bytes.byteLength > 8192) { throw new Error('UPLOAD_JOURNAL_SIZE'); }
      const n = fs.writeSync(file.fd, bytes.buffer);
      if (n !== bytes.byteLength) { throw new Error('UPLOAD_JOURNAL_WRITE'); }
      fs.fsyncSync(file.fd);
    } finally { fs.closeSync(file); }
    fs.renameSync(temporary, target);
    // Rename publishes a recoverable journal. A later directory sync failure
    // still fails the command, but must not reclaim its verified source.
    if (published) { published(); }
    const dir = fs.openSync(this.root, fs.OpenMode.READ_ONLY);
    try { fs.fsyncSync(dir.fd); } finally { fs.closeSync(dir); }
  }
  private read(handle: string): Journal {
    const path = this.path(handle, '.json');
    if (fs.statSync(path).size > 8192) { throw new Error('UPLOAD_JOURNAL_SIZE'); }
    const j = JSON.parse(fs.readTextSync(path)) as Journal;
    if (j.version !== 1 || j.handle !== handle || !/^[a-f0-9]{64}$/.test(j.hash) || !/^[a-f0-9]{64}$/.test(j.owner) ||
      !Number.isSafeInteger(j.size) || j.size <= 0 || !Number.isSafeInteger(j.offset) || j.offset < 0 || j.offset > j.size ||
      (j.uploadId !== '' && !/^[a-f0-9-]{36}$/.test(j.uploadId)) ||
      [j.retryAt, j.retryDelay].some((value: number | undefined): boolean => value !== undefined && (!Number.isSafeInteger(value) || value < 0))) { throw new Error('UPLOAD_JOURNAL_INVALID'); }
    return j;
  }
  private info(j: Journal): UploadLocal { return { handle: j.handle, name: j.name, type: j.type, size: j.size, hash: j.hash, resumable: j.resumable, scope: j.scope }; }
  private async hash(bytes: Uint8Array): Promise<string> {
    const md = cryptoFramework.createMd('SHA256'); await md.update({ data: bytes });
    return Array.from((await md.digest()).data).map((b: number) => b.toString(16).padStart(2, '0')).join('');
  }
  private async call(run: Running, path: string, method: rcp.HttpMethod = 'GET', body?: rcp.RequestContent,
    headers: rcp.RequestHeaders = {}): Promise<Reply> {
    this.check(run);
    const requestHeaders: rcp.RequestHeaders = { authorization: run.input.authorization, accept: 'application/json', 'cache-control': 'no-store' };
    for (const key of Object.keys(headers)) { requestHeaders[key] = headers[key]; }
    const target = run.apiOrigin.replace(/\/+$/, '') + path;
    const buffer = new Uint8Array(65536); let count = 0;
    const response = await run.network.fetch(run.session, target, method, (): rcp.Request => {
      this.check(run); count = 0;
      const request = new rcp.Request(target, method, requestHeaders, body);
      request.configuration = { transfer: { autoRedirect: false } };
      request.destination = (data: ArrayBuffer): void => {
        this.check(run);
        if (count + data.byteLength > buffer.length) { run.session.cancel(request); throw new Error('UPLOAD_RECEIPT_SIZE'); }
        buffer.set(new Uint8Array(data), count); count += data.byteLength;
      };
      return request;
    }); this.check(run);
    return { status: response.statusCode, body: new util.TextDecoder().decodeToString(buffer.subarray(0, count)), headers: response.headers };
  }
  private status(reply: Reply, expected: number = 200): void {
    if (reply.status !== expected) { throw new Error('UPLOAD_HTTP_' + reply.status); }
  }
  private async owner(run: Running): Promise<string> {
    run.phase = 'owner-read';
    const reply = await this.call(run, '/users/me'); this.status(reply);
    const identity = JSON.parse(reply.body) as Identity;
    const id = identity.id;
    if (typeof id !== 'string' || !id || id.length > 128) { throw new Error('UPLOAD_IDENTITY'); }
    this.check(run); if (identity.network_policy !== undefined && identity.network_policy !== null) { run.network.adopt(identity.network_policy); }
    return this.hash(new util.TextEncoder().encodeInto(run.apiOrigin + '\n' + id));
  }
  private async limits(run: Running): Promise<Limits> {
    run.phase = 'limits-read';
    const reply = await this.call(run, this.base(run) + '/limits');
    // Very old service: keep the old finite 200 MiB client ceiling, explicitly
    // single-request only. A lower server limit still rejects with 413.
    if (reply.status === 404) { return { max_upload_bytes: 209715200, resumable_enabled: false }; }
    this.status(reply); const limit = JSON.parse(reply.body) as Limits;
    if (!Number.isSafeInteger(limit.max_upload_bytes) || limit.max_upload_bytes <= 0 ||
      (limit.resumable_enabled && (!Number.isSafeInteger(limit.chunk_max_bytes) || (limit.chunk_max_bytes ?? 0) < 1))) { throw new Error('UPLOAD_LIMIT_INVALID'); }
    return limit;
  }
  private async owned(run: Running, owner: string): Promise<Journal> {
    const j = this.read(run.input.handle ?? '');
    if (j.owner !== owner || j.bot !== run.input.botId || j.scope !== run.input.scope || j.state === 'canceled') { throw new Error('UPLOAD_IDENTITY'); }
    run.journal = j; this.check(run); return j;
  }
  private async verify(run: Running, j: Journal): Promise<void> {
    const file = fs.openSync(this.path(j.handle, '.original'), fs.OpenMode.READ_ONLY | fs.OpenMode.NOFOLLOW);
    try { await verifyMediaFd(file.fd, j.size, j.hash, () => this.check(run)); } finally { fs.closeSync(file); }
  }
  private async stagePath(run: Running, owner: string, limits: Limits, uri: string, name: string, mime: string): Promise<UploadLocal> {
    run.phase = 'source-open';
    const input = fs.openSync(uri, fs.OpenMode.READ_ONLY), j = new Journal();
    j.handle = util.generateRandomUUID(true); j.owner = owner; j.bot = run.input.botId; j.scope = run.input.scope;
    j.name = name.replace(/[\\/\x00-\x1f]/g, '_').slice(0, 160) || 'file'; j.type = mime;
    j.resumable = limits.resumable_enabled === true; run.journal = j;
    let output: fs.File | undefined;
    let committed: boolean = false;
    try {
      run.phase = 'snapshot-create';
      j.size = fs.statSync(input.fd).size;
      if (!Number.isSafeInteger(j.size) || j.size <= 0 || j.size > limits.max_upload_bytes) { throw new Error('UPLOAD_HTTP_413'); }
      if (fs.accessSync(this.path(j.handle, '.original'))) { throw new Error('UPLOAD_IDENTITY'); }
      output = fs.openSync(this.path(j.handle, '.original'), fs.OpenMode.CREATE | fs.OpenMode.READ_WRITE | fs.OpenMode.NOFOLLOW);
      run.phase = 'snapshot-copy';
      await copyMediaBytes(input.fd, output.fd, j.size, () => this.check(run));
      run.phase = 'snapshot-hash';
      const md = cryptoFramework.createMd('SHA256'), buffer = new ArrayBuffer(262144); let at = 0;
      while (at < j.size) {
        this.check(run); fs.lseek(output.fd, at, fs.WhenceType.SEEK_SET);
        const n = await fs.read(output.fd, buffer, { length: Math.min(buffer.byteLength, j.size - at) }); this.check(run);
        if (n <= 0 || n > Math.min(buffer.byteLength, j.size - at)) { throw new Error('UPLOAD_SHORT_READ'); }
        await md.update({ data: new Uint8Array(buffer, 0, n) }); at += n;
      }
      j.hash = Array.from((await md.digest()).data).map((b: number) => b.toString(16).padStart(2, '0')).join('');
      // Re-read the granted source; mutation during snapshot copy cannot silently
      // substitute a mixed original even when its size did not change.
      run.phase = 'source-verify';
      await verifyMediaFd(input.fd, j.size, j.hash, () => this.check(run)); this.check(run);
      run.phase = 'journal-publish';
      this.save(j, () => { committed = true; run.staged.push(j); run.phase = 'journal-dir-sync'; }); this.check(run);
      this.visible.set(j.handle, j); return this.info(j);
    } catch (error) { const failure = this.failed(run, error as Error); if (!committed || run.cancelled) { this.remove(j); } throw failure; }
    finally { fs.closeSync(input); if (output) { fs.closeSync(output); } }
  }
  private remove(j: Journal): void {
    const preview = this.previews.get(j.handle); if (preview) { fs.closeSync(preview); this.previews.delete(j.handle); }
    this.visible.delete(j.handle);
    for (const suffix of ['.json', '.json.tmp', '.original']) {
      const path = this.path(j.handle, suffix); if (fs.accessSync(path)) { fs.unlinkSync(path); }
    }
  }
  async stageBegin(input: UploadScope, name: string, mime: string, size: number): Promise<UploadStagingHandle> {
    const run = this.start(input); run.copying = true; run.copyBusy = true;
    try {
      const owner = await this.owner(run), limits = await this.limits(run); this.check(run);
      run.phase = 'snapshot-create';
      if (!Number.isSafeInteger(size) || size <= 0 || size > limits.max_upload_bytes) { throw new Error('UPLOAD_HTTP_413'); }
      if (name.length > 1024 || mime.length > 256 || /[\x00-\x1f\x7f]/.test(mime)) { throw new Error('UPLOAD_REQUEST'); }
      const j = new Journal(); j.handle = util.generateRandomUUID(true); j.owner = owner; j.bot = input.botId; j.scope = input.scope;
      j.name = name.replace(/[\\/\x00-\x1f]/g, '_').slice(0, 160) || 'file'; j.type = mime || 'application/octet-stream';
      j.size = size; j.resumable = limits.resumable_enabled === true; run.journal = j;
      if (fs.accessSync(this.path(j.handle, '.original'))) { throw new Error('UPLOAD_IDENTITY'); }
      run.copyFile = fs.openSync(this.path(j.handle, '.original'), fs.OpenMode.CREATE | fs.OpenMode.READ_WRITE | fs.OpenMode.NOFOLLOW);
      return { handle: j.handle };
    } catch (error) { const failure = this.failed(run, error as Error); this.end(run); throw failure; }
    finally { run.copyBusy = false; }
  }
  private copying(operation: string, authorization: string): Running {
    const run = this.running;
    if (!run || !run.copying || !run.copyFile || !run.journal) { throw new Error('UPLOAD_IDENTITY'); }
    // Another command cannot replace an in-flight read/write or close its FD.
    if (run.copyBusy) { throw new Error('UPLOAD_BUSY'); }
    if (run.input.operation !== operation || run.input.authorization !== authorization) {
      this.end(run); throw new Error('UPLOAD_IDENTITY');
    }
    try { this.check(run); } catch (error) { this.end(run); throw error as Error; }
    run.copyBusy = true; return run;
  }
  async stageAppend(operation: string, authorization: string, offset: number, data: string): Promise<UploadStagingOffset> {
    const run = this.copying(operation, authorization);
    try {
      run.phase = 'snapshot-copy';
      if (!Number.isSafeInteger(offset) || offset !== run.copyOffset || !data || data.length > 87384 ||
        !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(data)) { throw new Error('UPLOAD_REQUEST'); }
      const bytes = new util.Base64Helper().decodeSync(data), j = run.journal as Journal, file = run.copyFile as fs.File;
      if (!bytes.byteLength || bytes.byteLength > 65536 || offset + bytes.byteLength > j.size) { throw new Error('UPLOAD_REQUEST'); }
      let written = 0;
      while (written < bytes.byteLength) {
        this.check(run);
        const n = await fs.write(file.fd, bytes.buffer.slice(bytes.byteOffset + written, bytes.byteOffset + bytes.byteLength), { offset: offset + written });
        this.check(run);
        if (n <= 0 || n > bytes.byteLength - written) { throw new Error('UPLOAD_SHORT_WRITE'); }
        written += n;
      }
      run.copyOffset += written; return { offset: run.copyOffset };
    } catch (error) { const failure = this.failed(run, error as Error); this.end(run); throw failure; }
    finally { run.copyBusy = false; }
  }
  async stageFinish(operation: string, authorization: string): Promise<UploadLocal> {
    const run = this.copying(operation, authorization);
    try {
      run.phase = 'snapshot-hash';
      const j = run.journal as Journal, file = run.copyFile as fs.File;
      if (run.copyOffset !== j.size || fs.statSync(file.fd).size !== j.size) { throw new Error('UPLOAD_SHORT_READ'); }
      fs.fsyncSync(file.fd);
      const md = cryptoFramework.createMd('SHA256'), buffer = new ArrayBuffer(262144); let at = 0;
      while (at < j.size) {
        this.check(run); fs.lseek(file.fd, at, fs.WhenceType.SEEK_SET);
        const n = await fs.read(file.fd, buffer, { length: Math.min(buffer.byteLength, j.size - at) }); this.check(run);
        if (n <= 0 || n > Math.min(buffer.byteLength, j.size - at)) { throw new Error('UPLOAD_SHORT_READ'); }
        await md.update({ data: new Uint8Array(buffer, 0, n) }); at += n;
      }
      j.hash = Array.from((await md.digest()).data).map((b: number) => b.toString(16).padStart(2, '0')).join(''); this.check(run);
      run.phase = 'journal-publish';
      this.save(j, () => { run.copyCommitted = true; run.staged.push(j); run.phase = 'journal-dir-sync'; }); this.check(run);
      this.visible.set(j.handle, j); return this.info(j);
    } catch (error) { throw this.failed(run, error as Error); }
    finally { run.copyBusy = false; this.end(run); }
  }
  async select(input: UploadScope, recordingPath?: string): Promise<UploadLocal[]> {
    const run = this.start(input);
    let photoSelection: NativePhotoSelection | undefined;
    try {
      const owner = await this.owner(run), limits = await this.limits(run); this.check(run);
      if (recordingPath) { return [await this.stagePath(run, owner, limits, recordingPath, '录音-' + input.handle + '.wav', 'audio/wav')]; }
      // Bound each picker invocation, not the user's total retained backlog.
      run.phase = 'picker';
      photoSelection = new NativePhotoSelection(this.context);
      const uris = await photoSelection.select(4, input.photos === true); this.check(run);
      const results: UploadLocal[] = [];
      for (const uri of uris) {
      run.phase = 'source-metadata';
      const name = new fileUri.FileUri(uri).name || 'file';
      const extension = name.includes('.') ? '.' + name.split('.').pop()?.toLowerCase() : '';
      const type = extension ? uniformTypeDescriptor.getUniformDataTypeByFilenameExtension(extension) : '';
      const mime = type ? uniformTypeDescriptor.getTypeDescriptor(type).mimeTypes[0] : '';
      results.push(await this.stagePath(run, owner, limits, uri, name, mime || 'application/octet-stream'));
      }
      return results;
    } catch (error) { throw this.failed(run, error as Error); }
    finally { if (photoSelection) { photoSelection.close(); } this.end(run); }
  }
  async list(input: UploadScope): Promise<UploadLocal[]> {
    const run = this.start(input);
    try {
      const owner = await this.owner(run); await this.limits(run); const result: UploadLocal[] = [];
      const names = input.handle ? [input.handle + '.json'] : fs.listFileSync(this.root);
      for (const name of names) {
        if (!/^[a-f0-9]{32}\.json$/.test(name)) { continue; }
        const j = this.read(name.slice(0, -5));
        const sameScope = j.scope === input.scope || (input.recoverDrafts === true && input.scope.startsWith('draft:') && j.scope.startsWith('draft:'));
        if (j.owner === owner && j.bot === input.botId && sameScope && j.state !== 'canceled') {
          await this.verify(run, j); this.check(run); this.visible.set(j.handle, j); result.push(this.info(j));
          // An explicit restore opens one bounded window. Sent/removed snapshots
          // leave the journal, so another restore can reveal the next window.
          if (result.length === 16) { break; }
        }
      }
      this.check(run); return result;
    } finally { this.end(run); }
  }
  private receipt(run: Running, j: Journal, reply: Reply, expected: number = 200): Receipt {
    this.status(reply, expected); const r = JSON.parse(reply.body) as Receipt;
    if (!/^[a-f0-9-]{36}$/.test(r.id) || (j.uploadId && r.id !== j.uploadId) || r.size_bytes !== j.size ||
      !Number.isSafeInteger(r.offset) || r.offset < 0 || r.offset > j.size || !Number.isFinite(Date.parse(r.expires_at)) ||
      typeof r.mime !== 'string' || !r.mime || !['uploading', 'ready', 'canceled'].includes(r.state) ||
      (r.state === 'ready' ? r.content_hash !== j.hash || r.offset !== j.size : !!r.content_hash)) { throw new Error('UPLOAD_RECEIPT_INVALID'); }
    const previous = j.offset;
    this.check(run); j.uploadId = r.id; j.offset = r.offset; j.expires = r.expires_at; j.state = r.state; j.type = r.mime;
    if (r.offset > previous) { j.retryDelay = undefined; }
    if ((j.retryAt ?? 0) <= Date.now() || r.state === 'ready') { j.retryAt = undefined; }
    if (r.state === 'ready') { j.completePending = false; }
    this.save(j); this.progress(run.input.operation, j.offset, j.size); return r;
  }
  private recoverableNetwork(error: Error): boolean {
    // Same official RCP transient codes already admitted by NativeTransferNetwork.
    // Certificate, permission, receipt and local integrity errors are excluded.
    return [1007900007, 1007900028, 1007900052, 1007900055, 1007900056].includes((error as UploadSDKError).code);
  }
  private deferRetry(run: Running, reply?: Reply): void {
    this.check(run); const j = run.journal;
    if (!j?.uploadId || !Number.isFinite(Date.parse(j.expires))) { throw new Error('UPLOAD_RECEIPT_INVALID'); }
    const now = Date.now(), expires = Date.parse(j.expires), remaining = Math.max(0, expires - now);
    // Retain the existing initial backoff, grow only while no confirmed progress
    // occurs, and bound lifetime by the actual receipt rather than a fault count.
    const delay = Math.min(j.retryDelay === undefined ? 250 : j.retryDelay * 2, remaining);
    let due = now + delay;
    if (reply) {
      const names = Object.keys(reply.headers).filter((key: string): boolean => key.toLowerCase() === 'retry-after');
      const value = names.length === 1 ? reply.headers[names[0]] : undefined;
      if (typeof value === 'string') {
        if (/^[0-9]+$/.test(value)) {
          const seconds = Number(value);
          // A cooldown beyond receipt lifetime stops at expiry, never retries early.
          due = Math.max(due, seconds >= remaining / 1000 ? expires : now + seconds * 1000);
        } else {
          const date = Date.parse(value);
          if (Number.isFinite(date)) { due = Math.max(due, date); }
        }
      }
    }
    j.retryDelay = delay; j.retryAt = Math.min(expires, Math.max(j.retryAt ?? 0, due)); this.save(j);
  }
  private async waitRetry(run: Running): Promise<void> {
    const j = run.journal;
    while (j?.uploadId) {
      this.check(run);
      const now = Date.now(), expires = Date.parse(j.expires);
      if (!Number.isFinite(expires)) { throw new Error('UPLOAD_RECEIPT_INVALID'); }
      if (now >= expires) { throw new Error('UPLOAD_RECEIPT_UNAVAILABLE'); }
      const due = Math.min(j.retryAt ?? now, expires);
      if (due <= now) { return; }
      await new Promise<void>((resolve: () => void): void => {
        // Timer platform bound only; neither the receipt deadline nor server
        // Retry-After is shortened when a wait exceeds one native timer.
        const timer = setTimeout((): void => { run.retryWake = undefined; resolve(); }, Math.min(due - now, 2147483647));
        run.retryWake = (): void => { clearTimeout(timer); run.retryWake = undefined; resolve(); };
      });
    }
  }
  private async retryCall(run: Running, path: string, method: rcp.HttpMethod = 'GET', body?: rcp.RequestContent,
    headers: rcp.RequestHeaders = {}): Promise<Reply> {
    while (true) {
      await this.waitRetry(run); this.check(run);
      let reply: Reply;
      try { reply = await this.call(run, path, method, body, headers); }
      catch (error) {
        this.check(run);
        if (method !== 'GET' || !this.recoverableNetwork(error as Error)) { throw error as Error; }
        this.deferRetry(run); continue;
      }
      // An uncertain write is never resent here. Its original receipt must be
      // read back before another PATCH; Create/complete remain single-send.
      if (method !== 'GET' || (reply.status !== 429 && (reply.status < 500 || reply.status > 599))) { return reply; }
      this.deferRetry(run, reply);
    }
  }
  async upload(input: UploadScope): Promise<MediaReply> {
    const run = this.start(input); run.accepted = true;
    try {
      const owner = await this.owner(run), j = await this.owned(run, owner), limit = await this.limits(run);
      await this.verify(run, j); this.check(run);
      const base = this.base(run);
      if (!j.uploadId && j.state === 'create-unknown') { throw new Error('UPLOAD_CREATE_UNCERTAIN'); }
      if (j.state === 'ready' || j.state === 'legacy-unknown') {
        const reply = await this.call(run, base + '/' + j.hash, 'HEAD');
        if (reply.status === 200) {
          const headers = new Map<string, string>();
          for (const key of Object.keys(reply.headers)) { const value = reply.headers[key]; if (typeof value === 'string') { headers.set(key.toLowerCase(), value); } }
          if (headers.get('content-length') !== String(j.size) || headers.get('etag') !== '\"' + j.hash + '\"' || !headers.get('content-type')) { throw new Error('UPLOAD_RECEIPT_INVALID'); }
          j.state = 'ready'; j.type = headers.get('content-type') ?? j.type; j.offset = j.size; this.save(j); this.progress(run.input.operation, j.offset, j.size);
          return { content_hash: j.hash, size_bytes: j.size, mime: j.type };
        }
        if (reply.status !== 404) { this.status(reply); }
        if (j.state === 'legacy-unknown') { throw new Error('UPLOAD_LEGACY_UNCERTAIN'); }
        // A ready receipt cannot override current evidence that its original
        // is unavailable. Keep the local source and do not return a stale ref.
        throw new Error('UPLOAD_ORIGINAL_UNAVAILABLE');
      }
      if (!j.uploadId && j.size > limit.max_upload_bytes) { throw new Error('UPLOAD_HTTP_413'); }
      if (!limit.resumable_enabled) {
        if (j.uploadId) { throw new Error('UPLOAD_RESUME_UNAVAILABLE'); }
        if (j.state === 'legacy-unknown') { throw new Error('UPLOAD_LEGACY_UNCERTAIN'); }
        j.resumable = false; j.state = 'legacy-unknown'; this.save(j);
        const form = new rcp.MultipartForm({ file: { contentOrPath: this.path(j.handle, '.original'), contentType: j.type, remoteFileName: j.name } });
        const reply = await this.call(run, base, 'POST', form); this.status(reply, 201);
        const ready = JSON.parse(reply.body) as MediaReply;
        if (ready.content_hash !== j.hash || ready.size_bytes !== j.size || !ready.mime) { throw new Error('UPLOAD_RECEIPT_INVALID'); }
        j.state = 'ready'; j.type = ready.mime; j.offset = j.size; this.save(j); this.progress(run.input.operation, j.offset, j.size); return ready;
      }
      j.resumable = true;
      if (j.uploadId) {
        const status = await this.retryCall(run, base + '/uploads/' + j.uploadId);
        if (status.status === 404) {
          // A missing/expired receipt is not permission to create another one.
          // Keep the original ID, offset, expiry and immutable local snapshot.
          throw new Error('UPLOAD_RECEIPT_UNAVAILABLE');
        } else { this.receipt(run, j, status); }
      }
      if (!j.uploadId) {
        if (j.size > limit.max_upload_bytes) { throw new Error('UPLOAD_HTTP_413'); }
        // Create has no idempotency key or lookup API. Persist uncertainty
        // BEFORE sending; a lost reply must not become a second POST on restart.
        j.state = 'create-unknown'; this.save(j);
        let reply: Reply;
        try { reply = await this.call(run, base + '/uploads', 'POST', JSON.stringify({ size_bytes: j.size, sha256: j.hash, mime: j.type }), { 'content-type': 'application/json' }); }
        catch { this.check(run); throw new Error('UPLOAD_CREATE_UNCERTAIN'); }
        this.receipt(run, j, reply, 201);
        if (j.offset !== 0 || j.state !== 'uploading') { throw new Error('UPLOAD_RECEIPT_INVALID'); }
      }
      if (j.completePending && j.state !== 'ready') { throw new Error('UPLOAD_COMPLETE_UNCERTAIN'); }
      if (j.state === 'canceled') { throw new Error('UPLOAD_CANCELLED'); }
      const file = fs.openSync(this.path(j.handle, '.original'), fs.OpenMode.READ_ONLY | fs.OpenMode.NOFOLLOW);
      const buffer = new ArrayBuffer(Math.min(262144, limit.chunk_max_bytes ?? 262144));
      try {
        while (j.offset < j.size) {
          this.check(run); if (j.state === 'canceled') { throw new Error('UPLOAD_CANCELLED'); }
          const start = j.offset, length = Math.min(buffer.byteLength, j.size - start);
          fs.lseek(file.fd, start, fs.WhenceType.SEEK_SET);
          const n = await fs.read(file.fd, buffer, { length }); this.check(run);
          if (n !== length) { throw new Error('UPLOAD_SHORT_READ'); }
          const bytes = n === buffer.byteLength ? buffer : buffer.slice(0, n);
          const checksum = await this.hash(new Uint8Array(bytes)); this.check(run);
          let reply: Reply;
          try { reply = await this.retryCall(run, base + '/uploads/' + j.uploadId, 'PATCH', bytes,
            { 'content-type': 'application/octet-stream', 'Upload-Offset': String(start), 'Upload-Length': String(n), 'Upload-SHA256': checksum }); }
          catch (error) {
            this.check(run); this.deferRetry(run);
            reply = await this.retryCall(run, base + '/uploads/' + j.uploadId);
            this.receipt(run, j, reply);
            if (j.offset === start && !this.recoverableNetwork(error as Error)) { throw error as Error; }
            continue;
          }
          if (reply.status === 409 || reply.status === 429 || (reply.status >= 500 && reply.status <= 599)) {
            this.deferRetry(run, reply);
            this.receipt(run, j, await this.retryCall(run, base + '/uploads/' + j.uploadId)); continue;
          }
          this.receipt(run, j, reply);
          if (j.offset !== start + n || j.state !== 'uploading') { throw new Error('UPLOAD_RECEIPT_INVALID'); }
        }
      } finally { fs.closeSync(file); }
      await this.verify(run, j);
      if (j.state !== 'ready') {
        j.completePending = true; this.save(j);
        let reply: Reply;
        try { reply = await this.call(run, base + '/uploads/' + j.uploadId + '/complete', 'POST'); }
        catch { this.check(run); reply = await this.retryCall(run, base + '/uploads/' + j.uploadId); }
        const receipt = this.receipt(run, j, reply);
        if (receipt.state !== 'ready') { throw new Error('UPLOAD_COMPLETE_UNCERTAIN'); }
      }
      this.check(run); return { content_hash: j.hash, size_bytes: j.size, mime: j.type };
    } finally { this.end(run); }
  }
  async discard(input: UploadScope): Promise<void> {
    const run = this.start(input);
    try {
      const owner = await this.owner(run), j = this.read(input.handle ?? '');
      if (j.owner !== owner || j.bot !== input.botId || j.scope !== input.scope) { throw new Error('UPLOAD_IDENTITY'); }
      run.journal = j;
      j.state = 'canceled'; this.save(j);
      if (j.uploadId) { try { await this.call(run, this.base(run) + '/uploads/' + j.uploadId, 'DELETE'); } catch { /* Local tombstone still prevents re-use; server expiry collects staging. */ } }
      this.check(run); this.remove(j);
    } finally { this.end(run); }
  }
  async consume(input: UploadScope): Promise<void> {
    const run = this.start(input);
    try {
      const owner = await this.owner(run), j = await this.owned(run, owner);
      if (j.state !== 'ready') { throw new Error('UPLOAD_NOT_READY'); }
      this.check(run); this.remove(j);
    } finally { this.end(run); }
  }
  abort(operation: string): void {
    const run = this.running;
    if (!run || run.input.operation !== operation) { return; }
    run.cancelled = true; run.retryWake?.();
    if (run.journal?.hash) { run.journal.state = 'canceled'; this.save(run.journal); }
    run.session.cancel();
    const preview = run.journal ? this.previews.get(run.journal.handle) : undefined;
    if (preview && run.journal) { fs.closeSync(preview); this.previews.delete(run.journal.handle); this.visible.delete(run.journal.handle); }
    if (run.journal?.uploadId && run.epoch === this.epoch && this.authorized()) { void this.cancelRemote(run); }
  }
  pause(operation: string): void {
    // Navigation and a cancelled send stop I/O but retain a resumable original.
    // Only explicit discard/abort tombstones the original or cancels staging.
    if (this.running?.input.operation === operation) { this.close(); }
  }
  private async cancelRemote(run: Running): Promise<void> {
    const j = run.journal; if (!j?.uploadId) { return; }
    const session = rcp.createSession();
    try {
      const request = new rcp.Request(run.apiOrigin.replace(/\/+$/, '') + this.base(run) + '/uploads/' + j.uploadId,
        'DELETE', { authorization: run.input.authorization });
      request.configuration = { transfer: { autoRedirect: false } };
      let received = 0;
      request.destination = (data: ArrayBuffer): void => { received += data.byteLength; if (received > 65536) { session.cancel(request); } };
      await run.network.fetch(session, run.apiOrigin.replace(/\/+$/, '') + this.base(run) + '/uploads/' + j.uploadId, 'DELETE', (): rcp.Request => request);
    } catch { /* Local tombstone is final. Without credentials on disk, an offline cancellation relies on server TTL. */ }
    finally { session.close(); }
  }
  preview(handle: string): UploadPreview {
    const journal = this.visible.get(handle);
    if (!this.authorized() || !journal || !/^(image|audio|video)\//.test(journal.type)) { throw new Error('UPLOAD_IDENTITY'); }
    let file = this.previews.get(handle);
    if (!file) { file = fs.openSync(this.path(handle, '.original'), fs.OpenMode.READ_ONLY | fs.OpenMode.NOFOLLOW); this.previews.set(handle, file); }
    if (fs.statSync(file.fd).size !== journal.size) { throw new Error('UPLOAD_IDENTITY'); }
    return { fd: file.fd, mime: journal.type };
  }
  activeLocal(input: UploadScope): UploadLocal[] {
    const run = this.running, j = run?.journal;
    if (!run || !j || run.input.authorization !== input.authorization || j.bot !== input.botId ||
      j.scope !== input.scope || (input.handle !== undefined && input.handle !== j.handle)) { throw new Error('UPLOAD_BUSY'); }
    this.check(run); this.visible.set(j.handle, j); return [this.info(j)];
  }
  releasePreviews(): void {
    this.visible.clear();
    this.previews.forEach((file: fs.File) => fs.closeSync(file)); this.previews.clear();
  }
  detachView(): void {
    this.releasePreviews();
    // A partial Web File copy still needs its document to supply bytes. It has
    // no committed original and is not an accepted network upload.
    if (this.running && !this.running.accepted) { this.close(); }
  }
  close(): void {
    this.epoch++; const run = this.running; run?.retryWake?.(); run?.session.cancel();
    // Between bridge chunks there is no pending await to run a finally block.
    // During I/O, its owner keeps the FD until the epoch check unwinds it.
    if (run?.copying && !run.copyBusy) { this.end(run); }
    this.releasePreviews();
  }
}

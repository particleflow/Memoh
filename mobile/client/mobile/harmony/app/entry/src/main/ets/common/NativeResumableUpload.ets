import { common } from '@kit.AbilityKit';
import { fileIo as fs, picker, fileUri } from '@kit.CoreFileKit';
import { cryptoFramework } from '@kit.CryptoArchitectureKit';
import { uniformTypeDescriptor } from '@kit.ArkData';
import { util } from '@kit.ArkTS';
import { rcp } from '@kit.RemoteCommunicationKit';
import { MEMOH_ORIGIN } from './MemohClientConfig';
import { copyMediaBytes, verifyMediaFd } from './NativeMediaFile';
import { NativePhotoSelection } from './NativePhotoSelection';

export interface UploadScope { operation: string; botId: string; scope: string; authorization: string; handle?: string; photos?: boolean; }
export interface UploadLocal { handle: string; name: string; type: string; size: number; hash: string; resumable: boolean; }
interface Limits { max_upload_bytes: number; resumable_enabled?: boolean; chunk_max_bytes?: number; }
interface Identity { id: string; }
interface Receipt { id: string; state: string; offset: number; size_bytes: number; expires_at: string; mime: string; content_hash?: string; }
interface MediaReply { content_hash: string; size_bytes: number; mime: string; }
interface Reply { status: number; body: string; headers: rcp.ResponseHeaders; }
export interface UploadPreview { fd: number; mime: string; }
class Journal {
  version: number = 1; handle: string = ''; owner: string = ''; bot: string = ''; scope: string = '';
  name: string = ''; type: string = ''; size: number = 0; hash: string = ''; uploadId: string = '';
  offset: number = 0; expires: string = ''; state: string = 'staged'; resumable: boolean = false;
}
class Running {
  apiOrigin: string = MEMOH_ORIGIN; epoch: number = 0; cancelled: boolean = false; staged: Journal[] = []; input: UploadScope; session: rcp.Session; journal?: Journal;
  constructor(input: UploadScope, epoch: number) { this.input = input; this.epoch = epoch; this.session = rcp.createSession(); }
}

// Foreground only. An app-owned immutable snapshot and atomic bounded journal
// survive process death. Tokens, arbitrary URIs and draft/model text never do.
export class NativeResumableUpload {
  private context: common.UIAbilityContext;
  private authorized: () => boolean;
  private epoch: number = 0;
  private running?: Running;
  private root: string;
  private visible: Map<string, string> = new Map();
  private previews: Map<string, fs.File> = new Map();
  constructor(context: common.UIAbilityContext, authorized: () => boolean) {
    this.context = context; this.authorized = authorized; this.root = context.filesDir + '/memoh-upload-journal-v1';
  }
  private check(run: Running): void {
    if (!this.authorized() || run.epoch !== this.epoch || run.cancelled) { throw new Error('UPLOAD_CANCELLED'); }
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
    run.session.close();
    if (run.cancelled || run.epoch !== this.epoch) { for (const staged of run.staged) { this.remove(staged); } }
    if (run.cancelled && run.journal?.hash) { const path = this.path(run.journal.handle, '.original'); if (fs.accessSync(path)) { fs.unlinkSync(path); } }
    if (this.running === run) { this.running = undefined; }
  }
  private base(run: Running): string { return '/bots/' + encodeURIComponent(run.input.botId) + '/media'; }
  private path(handle: string, suffix: string): string {
    if (!/^[a-f0-9]{32}$/.test(handle)) { throw new Error('UPLOAD_IDENTITY'); }
    return this.root + '/' + handle + suffix;
  }
  private save(j: Journal): void {
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
    const dir = fs.openSync(this.root, fs.OpenMode.READ_ONLY);
    try { fs.fsyncSync(dir.fd); } finally { fs.closeSync(dir); }
  }
  private read(handle: string): Journal {
    const path = this.path(handle, '.json');
    if (fs.statSync(path).size > 8192) { throw new Error('UPLOAD_JOURNAL_SIZE'); }
    const j = JSON.parse(fs.readTextSync(path)) as Journal;
    if (j.version !== 1 || j.handle !== handle || !/^[a-f0-9]{64}$/.test(j.hash) || !/^[a-f0-9]{64}$/.test(j.owner) ||
      !Number.isSafeInteger(j.size) || j.size <= 0 || !Number.isSafeInteger(j.offset) || j.offset < 0 || j.offset > j.size ||
      (j.uploadId !== '' && !/^[a-f0-9-]{36}$/.test(j.uploadId))) { throw new Error('UPLOAD_JOURNAL_INVALID'); }
    return j;
  }
  private info(j: Journal): UploadLocal { return { handle: j.handle, name: j.name, type: j.type, size: j.size, hash: j.hash, resumable: j.resumable }; }
  private async hash(bytes: Uint8Array): Promise<string> {
    const md = cryptoFramework.createMd('SHA256'); await md.update({ data: bytes });
    return Array.from((await md.digest()).data).map((b: number) => b.toString(16).padStart(2, '0')).join('');
  }
  private async call(run: Running, path: string, method: rcp.HttpMethod = 'GET', body?: rcp.RequestContent,
    headers: rcp.RequestHeaders = {}): Promise<Reply> {
    this.check(run);
    const requestHeaders: rcp.RequestHeaders = { authorization: run.input.authorization, accept: 'application/json', 'cache-control': 'no-store' };
    for (const key of Object.keys(headers)) { requestHeaders[key] = headers[key]; }
    const request = new rcp.Request(run.apiOrigin.replace(/\/+$/, '') + path, method, requestHeaders, body);
    request.configuration = { transfer: { autoRedirect: false } };
    const buffer = new Uint8Array(65536); let count = 0;
    request.destination = (data: ArrayBuffer): void => {
      this.check(run);
      if (count + data.byteLength > buffer.length) { run.session.cancel(request); throw new Error('UPLOAD_RECEIPT_SIZE'); }
      buffer.set(new Uint8Array(data), count); count += data.byteLength;
    };
    const response = await run.session.fetch(request); this.check(run);
    return { status: response.statusCode, body: new util.TextDecoder().decodeToString(buffer.subarray(0, count)), headers: response.headers };
  }
  private status(reply: Reply, expected: number = 200): void {
    if (reply.status !== expected) { throw new Error('UPLOAD_HTTP_' + reply.status); }
  }
  private async owner(run: Running): Promise<string> {
    const reply = await this.call(run, '/users/me'); this.status(reply);
    const id = (JSON.parse(reply.body) as Identity).id;
    if (typeof id !== 'string' || !id || id.length > 128) { throw new Error('UPLOAD_IDENTITY'); }
    return this.hash(new util.TextEncoder().encodeInto(run.apiOrigin + '\n' + id));
  }
  private async limits(run: Running): Promise<Limits> {
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
    const input = fs.openSync(uri, fs.OpenMode.READ_ONLY), j = new Journal();
    j.handle = util.generateRandomUUID(true); j.owner = owner; j.bot = run.input.botId; j.scope = run.input.scope;
    j.name = name.replace(/[\\/\x00-\x1f]/g, '_').slice(0, 160) || 'file'; j.type = mime;
    j.resumable = limits.resumable_enabled === true; run.journal = j;
    let output: fs.File | undefined;
    try {
      j.size = fs.statSync(input.fd).size;
      if (!Number.isSafeInteger(j.size) || j.size <= 0 || j.size > limits.max_upload_bytes) { throw new Error('UPLOAD_HTTP_413'); }
      if (fs.accessSync(this.path(j.handle, '.original'))) { throw new Error('UPLOAD_IDENTITY'); }
      output = fs.openSync(this.path(j.handle, '.original'), fs.OpenMode.CREATE | fs.OpenMode.READ_WRITE | fs.OpenMode.NOFOLLOW);
      await copyMediaBytes(input.fd, output.fd, j.size, () => this.check(run));
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
      await verifyMediaFd(input.fd, j.size, j.hash, () => this.check(run)); this.check(run); this.save(j); run.staged.push(j); this.visible.set(j.handle, j.type); return this.info(j);
    } catch (error) { this.remove(j); throw error as Error; }
    finally { fs.closeSync(input); if (output) { fs.closeSync(output); } }
  }
  private remove(j: Journal): void {
    const preview = this.previews.get(j.handle); if (preview) { fs.closeSync(preview); this.previews.delete(j.handle); }
    this.visible.delete(j.handle);
    for (const suffix of ['.json', '.json.tmp', '.original']) {
      const path = this.path(j.handle, suffix); if (fs.accessSync(path)) { fs.unlinkSync(path); }
    }
  }
  async select(input: UploadScope, recordingPath?: string): Promise<UploadLocal[]> {
    const run = this.start(input);
    try {
      const owner = await this.owner(run), limits = await this.limits(run); this.check(run);
      const existing = fs.listFileSync(this.root).filter((name: string) => /^[a-f0-9]{32}\.json$/.test(name) && this.read(name.slice(0, -5)).state !== 'canceled');
      if (existing.length >= 16 || existing.filter((name: string) => this.read(name.slice(0, -5)).owner === owner).length >= 4) { throw new Error('UPLOAD_LOCAL_QUOTA'); }
      if (recordingPath) { return [await this.stagePath(run, owner, limits, recordingPath, '录音-' + input.handle + '.wav', 'audio/wav')]; }
      const available = Math.min(16 - existing.length, 4 - existing.filter((name: string) => this.read(name.slice(0, -5)).owner === owner).length);
      const uris = await new NativePhotoSelection(this.context).select(available, input.photos === true); this.check(run);
      const results: UploadLocal[] = [];
      for (const uri of uris) {
      const name = new fileUri.FileUri(uri).name || 'file';
      const extension = name.includes('.') ? '.' + name.split('.').pop()?.toLowerCase() : '';
      const type = extension ? uniformTypeDescriptor.getUniformDataTypeByFilenameExtension(extension) : '';
      const mime = type ? uniformTypeDescriptor.getTypeDescriptor(type).mimeTypes[0] : '';
      results.push(await this.stagePath(run, owner, limits, uri, name, mime || 'application/octet-stream'));
      }
      return results;
    } finally { this.end(run); }
  }
  async list(input: UploadScope): Promise<UploadLocal[]> {
    const run = this.start(input);
    try {
      const owner = await this.owner(run); await this.limits(run); const result: UploadLocal[] = [];
      for (const name of fs.listFileSync(this.root)) {
        if (!/^[a-f0-9]{32}\.json$/.test(name)) { continue; }
        const j = this.read(name.slice(0, -5));
        if (j.owner === owner && j.bot === input.botId && j.scope === input.scope && j.state !== 'canceled') { this.visible.set(j.handle, j.type); result.push(this.info(j)); }
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
    this.check(run); j.uploadId = r.id; j.offset = r.offset; j.expires = r.expires_at; j.state = r.state; j.type = r.mime; this.save(j); return r;
  }
  private async retryCall(run: Running, path: string, method: rcp.HttpMethod = 'GET', body?: rcp.RequestContent,
    headers: rcp.RequestHeaders = {}): Promise<Reply> {
    for (let i = 0; ; i++) {
      const reply = await this.call(run, path, method, body, headers);
      if (reply.status !== 429 || i === 3) { return reply; }
      await new Promise<void>((resolve: () => void) => setTimeout(resolve, 250 * (2 ** i))); this.check(run);
    }
  }
  async upload(input: UploadScope): Promise<MediaReply> {
    const run = this.start(input);
    try {
      const owner = await this.owner(run), j = await this.owned(run, owner), limit = await this.limits(run);
      await this.verify(run, j); this.check(run);
      const base = this.base(run);
      if (j.state === 'ready' || j.state === 'legacy-unknown') {
        const reply = await this.call(run, base + '/' + j.hash, 'HEAD');
        if (reply.status === 200) {
          const headers = new Map<string, string>();
          for (const key of Object.keys(reply.headers)) { const value = reply.headers[key]; if (typeof value === 'string') { headers.set(key.toLowerCase(), value); } }
          if (headers.get('content-length') !== String(j.size) || headers.get('etag') !== '\"' + j.hash + '\"' || !headers.get('content-type')) { throw new Error('UPLOAD_RECEIPT_INVALID'); }
          j.state = 'ready'; j.type = headers.get('content-type') ?? j.type; j.offset = j.size; this.save(j);
          return { content_hash: j.hash, size_bytes: j.size, mime: j.type };
        }
        if (reply.status !== 404) { this.status(reply); }
        if (j.state === 'legacy-unknown') { throw new Error('UPLOAD_LEGACY_UNCERTAIN'); }
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
        j.state = 'ready'; j.type = ready.mime; j.offset = j.size; this.save(j); return ready;
      }
      j.resumable = true;
      if (j.uploadId) {
        const status = await this.retryCall(run, base + '/uploads/' + j.uploadId);
        if (status.status === 404) {
          // Owner and source already revalidated. Receipt may have expired or
          // been collected; at most one new admission per explicit foreground run.
          j.uploadId = ''; j.offset = 0; j.state = 'staged'; this.save(j);
        } else { this.receipt(run, j, status); }
      }
      if (!j.uploadId) {
        if (j.size > limit.max_upload_bytes) { throw new Error('UPLOAD_HTTP_413'); }
        const reply = await this.retryCall(run, base + '/uploads', 'POST', JSON.stringify({ size_bytes: j.size, sha256: j.hash, mime: j.type }), { 'content-type': 'application/json' });
        this.receipt(run, j, reply, 201);
        if (j.offset !== 0 || j.state !== 'uploading') { throw new Error('UPLOAD_RECEIPT_INVALID'); }
      }
      if (j.state === 'canceled') { throw new Error('UPLOAD_CANCELLED'); }
      const file = fs.openSync(this.path(j.handle, '.original'), fs.OpenMode.READ_ONLY | fs.OpenMode.NOFOLLOW);
      const buffer = new ArrayBuffer(Math.min(262144, limit.chunk_max_bytes ?? 262144)); let recoveries = 0;
      try {
        while (j.offset < j.size) {
          this.check(run); const start = j.offset, length = Math.min(buffer.byteLength, j.size - start);
          fs.lseek(file.fd, start, fs.WhenceType.SEEK_SET);
          const n = await fs.read(file.fd, buffer, { length }); this.check(run);
          if (n !== length) { throw new Error('UPLOAD_SHORT_READ'); }
          const bytes = n === buffer.byteLength ? buffer : buffer.slice(0, n);
          const checksum = await this.hash(new Uint8Array(bytes)); this.check(run);
          let reply: Reply;
          try { reply = await this.retryCall(run, base + '/uploads/' + j.uploadId, 'PATCH', bytes,
            { 'content-type': 'application/octet-stream', 'Upload-Offset': String(start), 'Upload-Length': String(n), 'Upload-SHA256': checksum }); }
          catch (error) {
            this.check(run); if (++recoveries > 3) { throw error as Error; }
            reply = await this.retryCall(run, base + '/uploads/' + j.uploadId);
            this.receipt(run, j, reply); continue;
          }
          if (reply.status === 409 || reply.status >= 500) {
            if (++recoveries > 3) { throw new Error('UPLOAD_RETRY_LIMIT'); }
            this.receipt(run, j, await this.retryCall(run, base + '/uploads/' + j.uploadId)); continue;
          }
          this.receipt(run, j, reply);
          if (j.offset !== start + n || j.state !== 'uploading') { throw new Error('UPLOAD_RECEIPT_INVALID'); }
        }
      } finally { fs.closeSync(file); }
      await this.verify(run, j);
      if (j.state !== 'ready') {
        let reply: Reply;
        try { reply = await this.retryCall(run, base + '/uploads/' + j.uploadId + '/complete', 'POST'); }
        catch { this.check(run); reply = await this.retryCall(run, base + '/uploads/' + j.uploadId); }
        let receipt = this.receipt(run, j, reply);
        if (receipt.state === 'uploading' && receipt.offset === j.size) {
          receipt = this.receipt(run, j, await this.retryCall(run, base + '/uploads/' + j.uploadId + '/complete', 'POST'));
        }
        if (receipt.state !== 'ready') { throw new Error('UPLOAD_NOT_READY'); }
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
  abort(operation: string): void {
    const run = this.running;
    if (!run || run.input.operation !== operation) { return; }
    run.cancelled = true;
    if (run.journal?.hash) { run.journal.state = 'canceled'; this.save(run.journal); }
    run.session.cancel();
    const preview = run.journal ? this.previews.get(run.journal.handle) : undefined;
    if (preview && run.journal) { fs.closeSync(preview); this.previews.delete(run.journal.handle); this.visible.delete(run.journal.handle); }
    if (run.journal?.uploadId && run.epoch === this.epoch && this.authorized()) { void this.cancelRemote(run); }
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
      await session.fetch(request);
    } catch { /* Local tombstone is final. Without credentials on disk, an offline cancellation relies on server TTL. */ }
    finally { session.close(); }
  }
  preview(handle: string): UploadPreview {
    const mime = this.visible.get(handle);
    if (!this.authorized() || !mime || !/^(image|audio|video)\//.test(mime)) { throw new Error('UPLOAD_IDENTITY'); }
    let file = this.previews.get(handle);
    if (!file) { file = fs.openSync(this.path(handle, '.original'), fs.OpenMode.READ_ONLY | fs.OpenMode.NOFOLLOW); this.previews.set(handle, file); }
    return { fd: file.fd, mime };
  }
  close(): void {
    this.epoch++; this.running?.session.cancel(); this.visible.clear();
    this.previews.forEach((file: fs.File) => fs.closeSync(file)); this.previews.clear();
  }
}

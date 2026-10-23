import { fileIo as fs } from '@kit.CoreFileKit';
import { cryptoFramework } from '@kit.CryptoArchitectureKit';
import { util } from '@kit.ArkTS';
import { rcp } from '@kit.RemoteCommunicationKit';
import { MEMOH_ORIGIN } from './MemohClientConfig';

export interface VoiceOriginalInfo { handle: string; size: number; hash: string; }
export interface VoiceUploadReply { status: number; body: string; }

// Foreground transport for NativeVoice-owned immutable originals. No path or
// arbitrary URL supplied by the Web page is accepted; no credentials are saved.
export class NativeVoiceTransfer {
  private epoch: number = 0;
  private owners: Map<string, string> = new Map();
  private cancelled: Map<string, string> = new Map();
  private sessions: Map<string, rcp.Session> = new Map();
  private operations: Map<string, string> = new Map();
  private previews: Map<string, fs.File> = new Map();
  private hashes: Map<string, VoiceOriginalInfo> = new Map();
  private resolve: (handle: string) => string;
  private authorized: () => boolean;
  constructor(resolve: (handle: string) => string, authorized: () => boolean) { this.resolve = resolve; this.authorized = authorized; }

  private check(handle: string, bot: string, operation: string, epoch: number): string {
    if (!this.authorized() || epoch !== this.epoch || this.cancelled.has(operation)) { throw new Error('VOICE_CANCELLED'); }
    if (!/^[a-f0-9]{32}$/.test(handle) || !/^[a-f0-9]{32}$/.test(operation) || !/^[A-Za-z0-9_-]{1,128}$/.test(bot)) {
      throw new Error('VOICE_REQUEST_INVALID');
    }
    if (this.operations.has(operation) && this.operations.get(operation) !== handle) { throw new Error('VOICE_REQUEST_INVALID'); }
    const owner = this.owners.get(handle);
    if (owner && owner !== bot) { throw new Error('VOICE_BOT_MISMATCH'); }
    const path = this.resolve(handle); this.owners.set(handle, bot); this.operations.set(operation, handle); return path;
  }

  async describe(handle: string, bot: string, operation: string): Promise<VoiceOriginalInfo> {
    const epoch = this.epoch, path = this.check(handle, bot, operation, epoch);
    const file = fs.openSync(path, fs.OpenMode.READ_ONLY);
    try {
      const size = fs.statSync(file.fd).size;
      if (!Number.isSafeInteger(size) || size <= 44) { throw new Error('VOICE_SIZE_INVALID'); }
      const known = this.hashes.get(handle);
      if (known && known.size === size) { return known; }
      const md = cryptoFramework.createMd('SHA256'), buffer = new ArrayBuffer(262144);
      let offset = 0;
      while (offset < size) {
        this.check(handle, bot, operation, epoch);
        fs.lseek(file.fd, offset, fs.WhenceType.SEEK_SET);
        const count = await fs.read(file.fd, buffer, { length: Math.min(buffer.byteLength, size - offset) });
        this.check(handle, bot, operation, epoch);
        if (!Number.isSafeInteger(count) || count <= 0 || count > Math.min(buffer.byteLength, size - offset)) { throw new Error('VOICE_SHORT_READ'); }
        await md.update({ data: new Uint8Array(buffer, 0, count) }); offset += count;
      }
      const hash = Array.from((await md.digest()).data).map((byte: number) => byte.toString(16).padStart(2, '0')).join('');
      this.check(handle, bot, operation, epoch);
      if (fs.statSync(file.fd).size !== size) { throw new Error('VOICE_SIZE_CHANGED'); }
      const result: VoiceOriginalInfo = { handle, size, hash }; this.hashes.set(handle, result); return result;
    } finally { fs.closeSync(file); }
  }

  async upload(handle: string, bot: string, operation: string, requestUrl: string, authorization: string): Promise<VoiceUploadReply> {
    const epoch = this.epoch, path = this.check(handle, bot, operation, epoch);
    const endpoint = MEMOH_ORIGIN.replace(/\/+$/, '') + '/bots/' + encodeURIComponent(bot) + '/media';
    if (requestUrl !== endpoint || !/^Bearer [^\s\x00-\x1f]{1,8192}$/.test(authorization) || this.sessions.has(operation)) {
      throw new Error('VOICE_REQUEST_INVALID');
    }
    const info = this.hashes.get(handle);
    if (!info || fs.statSync(path).size !== info.size) { throw new Error('VOICE_ORIGINAL_NOT_VERIFIED'); }
    const session = rcp.createSession(); this.sessions.set(operation, session);
    try {
      const form = new rcp.MultipartForm({ file: { contentOrPath: path, contentType: 'audio/wav', remoteFileName: '录音-' + handle + '.wav' } });
      const request = new rcp.Request(endpoint, 'POST', { authorization, accept: 'application/json' }, form);
      request.configuration = { transfer: { autoRedirect: false } };
      // The server receipt is small. Bound it independently of the uploaded file.
      const bytes = new Uint8Array(65536); let count = 0;
      request.destination = (chunk: ArrayBuffer): void => {
        this.check(handle, bot, operation, epoch);
        if (count + chunk.byteLength > bytes.length) { session.cancel(request); throw new Error('VOICE_RECEIPT_TOO_LARGE'); }
        bytes.set(new Uint8Array(chunk), count); count += chunk.byteLength;
      };
      const response = await session.fetch(request);
      this.check(handle, bot, operation, epoch);
      if (fs.statSync(path).size !== info.size) { throw new Error('VOICE_SIZE_CHANGED'); }
      return { status: response.statusCode, body: new util.TextDecoder().decodeToString(bytes.subarray(0, count)) };
    } finally { this.sessions.delete(operation); session.close(); }
  }

  abort(handle: string, operation: string): void {
    if (!/^[a-f0-9]{32}$/.test(handle) || !/^[a-f0-9]{32}$/.test(operation)) { throw new Error('VOICE_REQUEST_INVALID'); }
    if (this.operations.has(operation) && this.operations.get(operation) !== handle) { throw new Error('VOICE_REQUEST_INVALID'); }
    this.cancelled.set(operation, handle); this.sessions.get(operation)?.cancel();
  }

  preview(handle: string): number {
    if (!this.authorized() || !/^[a-f0-9]{32}$/.test(handle)) { throw new Error('VOICE_ORIGINAL_UNAVAILABLE'); }
    const path = this.resolve(handle);
    let file = this.previews.get(handle);
    if (!file) { file = fs.openSync(path, fs.OpenMode.READ_ONLY); this.previews.set(handle, file); }
    return file.fd;
  }

  release(handle: string): void {
    // Cancel all operations for this handle before its owner unlinks the file.
    this.operations.forEach((owner: string, operation: string) => { if (owner === handle) { this.abort(handle, operation); } });
    const file = this.previews.get(handle); if (file) { fs.closeSync(file); this.previews.delete(handle); }
    this.hashes.delete(handle); this.owners.delete(handle);
  }
  close(): void {
    this.epoch++;
    this.sessions.forEach((session: rcp.Session) => session.cancel());
    this.previews.forEach((file: fs.File) => fs.closeSync(file)); this.previews.clear();
    this.hashes.clear(); this.owners.clear(); this.cancelled.clear(); this.operations.clear();
  }
}

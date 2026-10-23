import { common } from '@kit.AbilityKit';
import { fileIo as fs, picker } from '@kit.CoreFileKit';
import { rcp } from '@kit.RemoteCommunicationKit';
import { MEMOH_ORIGIN } from './MemohClientConfig';
import { copyMediaBytes, verifyMediaFd } from './NativeMediaFile';

export interface MediaDownloadInput { operation: string; botId: string; hash: string; size: number; name: string; url: string; authorization: string; }
export interface MediaDownloadReady { operation: string; hash: string; size: number; }
class DownloadState {
  operation: string = ''; bot: string = ''; hash: string = ''; name: string = ''; path: string = '';
  size: number = 0; epoch: number = 0; received: number = 0;
  cancelled: boolean = false; ready: boolean = false; saving: boolean = false; running: boolean = true;
  session?: rcp.Session;
}

// One foreground save action. Network bytes stay in the app sandbox until the
// complete original is verified. Only DocumentViewPicker supplies a public URI.
export class NativeMediaDownload {
  private epoch: number = 0;
  private states: Map<string, DownloadState> = new Map();
  private cancelled: Map<string, string> = new Map();
  private context: common.UIAbilityContext;
  private authorized: () => boolean;
  constructor(context: common.UIAbilityContext, authorized: () => boolean) { this.context = context; this.authorized = authorized; }
  private check(state: DownloadState): void {
    if (!this.authorized() || state.cancelled || state.epoch !== this.epoch) { throw new Error('MEDIA_DOWNLOAD_CANCELLED'); }
  }
  private owned(operation: string, bot: string): DownloadState {
    const state = this.states.get(operation);
    if (!state || state.bot !== bot) { throw new Error('MEDIA_DOWNLOAD_IDENTITY'); }
    this.check(state); return state;
  }
  private remove(state: DownloadState): void {
    if (state.path && fs.accessSync(state.path)) { fs.unlinkSync(state.path); }
    this.states.delete(state.operation);
  }
  private header(headers: rcp.ResponseHeaders, name: string): string {
    for (const key of Object.keys(headers)) {
      if (key.toLowerCase() === name) { const value = headers[key]; return typeof value === 'string' ? value : ''; }
    }
    return '';
  }
  private validHeaders(state: DownloadState, start: number, status: number, headers: rcp.ResponseHeaders): boolean {
    const length = this.header(headers, 'content-length'), etag = this.header(headers, 'etag');
    if (!/^[0-9]+$/.test(length) || Number(length) !== state.size - start || etag !== '"' + state.hash + '"' ||
      !['', 'identity'].includes(this.header(headers, 'content-encoding'))) { return false; }
    if (status === 206) { return this.header(headers, 'content-range') === 'bytes ' + start + '-' + (state.size - 1) + '/' + state.size; }
    return start === 0 && status === 200 && !this.header(headers, 'content-range');
  }
  async fetch(input: MediaDownloadInput): Promise<MediaDownloadReady> {
    const endpoint = MEMOH_ORIGIN.replace(/\/+$/, '') + '/bots/' + encodeURIComponent(input.botId) + '/media/' + input.hash;
    if (!this.authorized() || !/^[a-f0-9]{32}$/.test(input.operation) || !/^[A-Za-z0-9_-]{1,128}$/.test(input.botId) ||
      !/^[a-f0-9]{64}$/.test(input.hash) || !Number.isSafeInteger(input.size) || input.size <= 0 || input.url !== endpoint ||
      !/^Bearer [^\s\x00-\x1f]{1,8192}$/.test(input.authorization)) { throw new Error('MEDIA_DOWNLOAD_REQUEST'); }
    if (this.cancelled.has(input.operation)) { throw new Error('MEDIA_DOWNLOAD_CANCELLED'); }
    if (this.states.size) { throw new Error('MEDIA_DOWNLOAD_BUSY'); }
    const state = new DownloadState();
    state.operation = input.operation; state.bot = input.botId; state.hash = input.hash; state.size = input.size; state.epoch = this.epoch;
    state.name = (input.name || 'file').replace(/[\\/\x00-\x1f]/g, '_').slice(0, 160);
    if (state.name === '.' || state.name === '..') { state.name = 'file'; }
    const directory = this.context.cacheDir + '/memoh-original-downloads';
    if (!fs.accessSync(directory)) { fs.mkdirSync(directory); }
    state.path = directory + '/' + input.operation + '.part';
    if (fs.accessSync(state.path)) { throw new Error('MEDIA_DOWNLOAD_REQUEST'); }
    this.states.set(input.operation, state);
    let file: fs.File | undefined;
    try {
      file = fs.openSync(state.path, fs.OpenMode.CREATE | fs.OpenMode.NOFOLLOW | fs.OpenMode.READ_WRITE);
      const buffer = new ArrayBuffer(262144), view = new Uint8Array(buffer);
      // One bounded foreground retry can resume an interrupted 206 response.
      // No durable scheduler, no credential persistence, no blind retry on 4xx.
      for (let attempt = 0; attempt < 2; attempt++) {
        this.check(state);
        const start = state.received, session = rcp.createSession(); state.session = session;
        let status = 0, headers: rcp.ResponseHeaders = {}, fatal = false;
        const request = new rcp.Request(endpoint, 'GET', { authorization: input.authorization,
          range: 'bytes=' + start + '-', 'if-range': '"' + input.hash + '"', 'accept-encoding': 'identity', 'cache-control': 'no-cache' });
        request.configuration = { transfer: { autoRedirect: false }, tracing: { httpEventsHandler: {
          onStatusCodeReceive: (value: number): void => { status = value; },
          onHeaderReceive: (value: rcp.ResponseHeaders): void => { headers = value; }
        } } };
        const fd = file.fd;
        request.destination = (chunk: ArrayBuffer): void => {
          try {
            this.check(state);
            if (!this.validHeaders(state, start, status, headers) || state.received + chunk.byteLength > state.size) {
              throw new Error('MEDIA_DOWNLOAD_RESPONSE');
            }
            const incoming = new Uint8Array(chunk);
            for (let at = 0; at < incoming.length;) {
              this.check(state);
              const count = Math.min(view.length, incoming.length - at); view.set(incoming.subarray(at, at + count));
              let left = count;
              while (left > 0) {
                fs.lseek(fd, state.received, fs.WhenceType.SEEK_SET);
                const written = fs.writeSync(fd, buffer, { length: left });
                if (!Number.isSafeInteger(written) || written <= 0 || written > left) { throw new Error('MEDIA_SHORT_WRITE'); }
                state.received += written; left -= written;
                if (left) { view.copyWithin(0, written, written + left); }
              }
              at += count;
            }
          } catch (error) { fatal = true; session.cancel(request); throw error as Error; }
        };
        try {
          const response = await session.fetch(request); this.check(state);
          if (!this.validHeaders(state, start, response.statusCode, response.headers) || state.received !== state.size) {
            fatal = true; throw new Error('MEDIA_DOWNLOAD_INCOMPLETE');
          }
          break;
        } catch (error) {
          this.check(state);
          if (status === 401 || status === 403) { throw new Error('MEDIA_DOWNLOAD_HTTP_' + status); }
          if (fatal || attempt > 0 || status !== 206 || !this.validHeaders(state, start, status, headers) ||
            state.received <= start || state.received >= state.size) { throw error as Error; }
        } finally { state.session = undefined; session.close(); }
      }
      fs.fsyncSync(file.fd);
      await verifyMediaFd(file.fd, state.size, state.hash, () => this.check(state));
      this.check(state); state.ready = true;
      return { operation: state.operation, hash: state.hash, size: state.size };
    } finally {
      if (file) { fs.closeSync(file); } state.running = false;
      if (!state.ready || state.cancelled || state.epoch !== this.epoch) { this.remove(state); }
    }
  }
  async save(operation: string, bot: string): Promise<boolean> {
    const state = this.owned(operation, bot);
    if (!state.ready || state.saving) { throw new Error('MEDIA_DOWNLOAD_NOT_READY'); }
    state.saving = true; state.running = true;
    let source: fs.File | undefined, destination: fs.File | undefined;
    let complete = false;
    try {
      const options = new picker.DocumentSaveOptions(); options.newFileNames = [state.name];
      const uris = await new picker.DocumentViewPicker(this.context).save(options);
      this.check(state);
      if (!uris[0]) { return false; }
      // Preserve the system URI and its grant, never derive a public pathname.
      source = fs.openSync(state.path, fs.OpenMode.READ_ONLY);
      destination = await fs.open(uris[0], fs.OpenMode.READ_WRITE | fs.OpenMode.TRUNC);
      this.check(state);
      await copyMediaBytes(source.fd, destination.fd, state.size, () => this.check(state));
      await verifyMediaFd(destination.fd, state.size, state.hash, () => this.check(state));
      this.check(state); complete = true; return true;
    } finally {
      // Clear our partial output, without unlinking a system-selected document.
      try {
        if (destination) {
          try { if (!complete) { fs.truncateSync(destination.fd, 0); fs.fsyncSync(destination.fd); } }
          finally { fs.closeSync(destination); }
        }
      } finally {
        try { if (source) { fs.closeSync(source); } }
        finally { state.running = false; this.remove(state); }
      }
    }
  }
  abort(operation: string, bot: string): void {
    if (!/^[a-f0-9]{32}$/.test(operation) || !/^[A-Za-z0-9_-]{1,128}$/.test(bot)) { throw new Error('MEDIA_DOWNLOAD_REQUEST'); }
    const state = this.states.get(operation);
    if (!state) { this.cancelled.set(operation, bot); return; }
    if (state.bot !== bot) { throw new Error('MEDIA_DOWNLOAD_IDENTITY'); }
    state.cancelled = true; state.session?.cancel();
    if (!state.running) { this.remove(state); }
  }
  close(): void {
    this.epoch++;
    this.states.forEach((state: DownloadState) => { this.abort(state.operation, state.bot); });
  }
}

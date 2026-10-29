import { common } from '@kit.AbilityKit';
import { NativeResumableUpload, UploadScope, UploadLocal, MediaReply } from './NativeResumableUpload';
import { NativeMediaDownload, MediaDownloadInput, MediaDownloadReady } from './NativeMediaDownload';
import { TransferBackgroundControl } from './NativeTransferBackground';
import { MEMOH_ORIGIN } from './MemohClientConfig';

interface UploadWork { key: string; input: UploadScope; pending: Promise<MediaReply>; }
interface DownloadWork { key: string; input: MediaDownloadInput; pending: Promise<MediaDownloadReady>; }

// Process-local ownership of the existing engines. No second queue, credential
// store, journal or retry loop is introduced.
export class NativeTransferOwner {
  readonly uploads: NativeResumableUpload;
  readonly downloads: NativeMediaDownload;
  private enabled: boolean = false;
  private epoch: number = 0;
  private credential: string = '';
  private view: number = 0;
  private upload?: UploadWork;
  private download?: DownloadWork;
  private downloadOperation: string = '';
  private uploadOperation: string = '';
  private background?: TransferBackgroundControl;
  private draining: Promise<void> = Promise.resolve();

  constructor(context: common.UIAbilityContext) {
    this.uploads = new NativeResumableUpload(context, (): boolean => this.enabled && MEMOH_ORIGIN !== '',
      (operation: string, received: number, total: number): void => this.background?.progress('upload:' + operation, received, total));
    this.downloads = new NativeMediaDownload(context, (): boolean => this.enabled && MEMOH_ORIGIN !== '',
      (operation: string, received: number, total: number): void => this.background?.progress('download:' + operation, received, total));
  }
  bindAbility(context: common.UIAbilityContext): void {
    this.view++; this.revoke();
    this.draining = this.draining.then((): void => {
      this.uploads.bindContext(context); this.downloads.bindContext(context);
    });
  }
  bindBackground(background: TransferBackgroundControl): void {
    const prior = this.background;
    if (prior) { prior.destroy(); this.draining = Promise.all([this.draining, prior.settled()]).then((): void => {}); }
    this.background = background;
  }
  destroyAbility(): void { this.view++; this.revoke(); this.background?.destroy(); }
  pauseBackground(): void {
    // OS entitlement loss pauses the existing engines, preserving their own
    // checkpoints. It never logs out or silently reacquires a continuous task.
    this.uploads.close(); this.downloads.close();
  }
  attach(): number { this.view++; return this.view; }
  current(view: number): boolean { return view === this.view; }
  detach(view: number): void {
    if (!this.current(view)) { return; }
    this.view++;
    this.uploads.detachView();
  }
  enable(): void { this.enabled = MEMOH_ORIGIN !== ''; }
  async ready(credential?: string): Promise<void> {
    this.enable();
    if (credential !== undefined) {
      if (!/^Bearer [^\s\x00-\x1f]{1,8192}$/.test(credential)) { this.revoke(); throw new Error('NATIVE_REQUEST_INVALID'); }
      await this.authorize(credential);
    }
    else if (this.credential) {
      // Legacy documents without an explicit current credential cannot keep
      // work from the preceding document's authenticated session alive.
      this.revoke(); this.enable();
    }
  }
  ownsCredential(credential: string): boolean { return this.credential === credential; }
  generation(): number { return this.epoch; }
  check(epoch: number): void {
    if (!this.enabled || epoch !== this.epoch || !MEMOH_ORIGIN) { throw new Error('NATIVE_SESSION_CHANGED'); }
  }
  revoke(): void {
    this.enabled = false; this.credential = ''; this.epoch++;
    this.background?.reset();
    this.uploads.close(); this.downloads.close();
    const upload = this.upload?.pending, download = this.download?.pending;
    this.downloadOperation = '';
    this.draining = Promise.all([this.draining, this.background ? this.background.settled() : Promise.resolve(),
      upload ? upload.then((): void => {}, (): void => {}) : Promise.resolve(),
      download ? download.then((): void => {}, (): void => {}) : Promise.resolve()
    ]).then((): void => {});
  }
  async authorize(credential: string): Promise<number> {
    if (!/^Bearer [^\s\x00-\x1f]{1,8192}$/.test(credential)) { throw new Error('NATIVE_REQUEST_INVALID'); }
    if (!this.enabled || !MEMOH_ORIGIN) { throw new Error('NATIVE_SESSION_CHANGED'); }
    if (this.credential && this.credential !== credential) { this.revoke(); this.enable(); }
    this.credential = credential;
    const epoch = this.epoch;
    await this.draining; this.check(epoch); return epoch;
  }
  async runUpload(input: UploadScope): Promise<MediaReply> {
    if (!/^[a-f0-9]{32}$/.test(input.operation)) { throw new Error('UPLOAD_REQUEST'); }
    const epoch = await this.authorize(input.authorization);
    const key = JSON.stringify([epoch, MEMOH_ORIGIN, input.botId, input.scope, input.handle]);
    let work = this.upload;
    if (work && work.key !== key) { throw new Error('UPLOAD_BUSY'); }
    if (!work) {
      const background = this.background, activity = 'upload:' + input.operation;
      if (background && !background.begin(activity)) { throw new Error('NATIVE_BACKGROUND_UNAVAILABLE'); }
      work = { key: key, input: input, pending: this.uploads.upload(input) };
      this.upload = work;
      const current = work;
      void work.pending.then((): void => { background?.finish(activity); if (this.upload === current) { this.upload = undefined; } },
        (): void => { background?.finish(activity); if (this.upload === current) { this.upload = undefined; } });
    }
    this.uploadOperation = input.operation;
    const result = await work.pending; this.check(epoch); return result;
  }
  pauseUpload(operation: string, abort: boolean): void {
    const original = this.upload && operation === this.uploadOperation ? this.upload.input.operation : operation;
    if (abort) { this.uploads.abort(original); } else { this.uploads.pause(original); }
  }
  async listUploads(input: UploadScope): Promise<UploadLocal[]> {
    const epoch = await this.authorize(input.authorization);
    const result = this.upload ? this.uploads.activeLocal(input) : await this.uploads.list(input);
    this.check(epoch); return result;
  }
  async fetchDownload(input: MediaDownloadInput): Promise<MediaDownloadReady> {
    if (!/^[a-f0-9]{32}$/.test(input.operation)) { throw new Error('MEDIA_DOWNLOAD_REQUEST'); }
    const epoch = await this.authorize(input.authorization);
    const key = JSON.stringify([epoch, MEMOH_ORIGIN, input.botId, input.url, input.hash, input.size]);
    let work = this.download;
    if (work && work.key !== key) { throw new Error('MEDIA_DOWNLOAD_BUSY'); }
    if (!work) {
      const background = this.background, activity = 'download:' + input.operation;
      if (background && !background.begin(activity)) { throw new Error('NATIVE_BACKGROUND_UNAVAILABLE'); }
      work = { key: key, input: input, pending: this.downloads.fetch(input) };
      this.download = work;
      const current = work;
      void work.pending.then((): void => { background?.finish(activity); },
        (): void => { background?.finish(activity); if (this.download === current) { this.download = undefined; } });
    }
    this.downloadOperation = input.operation;
    const result = await work.pending; this.check(epoch);
    return { operation: input.operation, hash: result.hash, size: result.size };
  }
  private ownedDownload(operation: string, bot: string): DownloadWork {
    const work = this.download;
    if (!work || operation !== this.downloadOperation || work.input.botId !== bot) { throw new Error('MEDIA_DOWNLOAD_IDENTITY'); }
    return work;
  }
  async saveDownload(operation: string, bot: string, view: number): Promise<boolean> {
    const work = this.ownedDownload(operation, bot), epoch = this.epoch;
    const visible = (): boolean => this.current(view) && epoch === this.epoch;
    await this.downloads.revalidate(work.input); this.check(epoch);
    if (!visible()) { throw new Error('NATIVE_VIEW_DETACHED'); }
    try { return await this.downloads.save(work.input.operation, bot, visible); }
    finally { if (this.download === work) { this.download = undefined; this.downloadOperation = ''; } }
  }
  releaseDownload(operation: string, bot: string, abort: boolean): void {
    if (!this.download) {
      if (abort) { this.downloads.abort(operation, bot); } else { this.downloads.release(operation, bot); }
      return;
    }
    const work = this.ownedDownload(operation, bot);
    if (abort) { this.downloads.abort(work.input.operation, bot); }
    else { this.downloads.release(work.input.operation, bot); }
    this.download = undefined; this.downloadOperation = '';
  }
}

let owner: NativeTransferOwner | undefined;
export function nativeTransferOwner(context: common.UIAbilityContext): NativeTransferOwner {
  if (!owner) { owner = new NativeTransferOwner(context); }
  return owner;
}

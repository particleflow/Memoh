import { webview } from '@kit.ArkWeb';
import { common } from '@kit.AbilityKit';
import { NativeVoice } from './NativeVoice';
import { NativeResumableUpload, UploadPreview, UploadScope, UploadStageFailure, UploadFailure } from './NativeResumableUpload';
import { MEMOH_ORIGIN, configureMemohOrigin, resetMemohOrigin } from './MemohClientConfig';
import { TransferNetworkPolicy } from './ServiceUrl';
import { NativeTransferOwner, nativeTransferOwner } from './NativeTransferOwner';

interface ClientRequest {
  version: number; command: string; state?: string; themeId?: string; handle?: string; offset?: number;
  apiOrigin?: string; networkPolicy?: TransferNetworkPolicy; operation?: string; botId?: string; hash?: string; size?: number;
  name?: string; type?: string; data?: string; url?: string; authorization?: string; scope?: string; photos?: boolean; recoverDrafts?: boolean;
}
const PERMISSION: string = '{"javascriptProxyPermission":{"urlPermissionList":[{"scheme":"begonia-client","host":"app","port":"","path":"/index.html"}]}}';

// Current page lifecycle, theme and foreground microphone.
export class NativeClient {
  private active: boolean = false;
  private controller: webview.WebviewController;
  private stateChanged: (state: string) => void;
  private themeChanged: (theme: string) => Promise<void>;
  private voice: NativeVoice;
  private uploads: NativeResumableUpload;
  private ready: boolean = false;
  private resetNetwork: () => void;
  private owner: NativeTransferOwner;
  private view: number = 0;
  constructor(controller: webview.WebviewController, stateChanged: (state: string) => void,
    themeChanged: (theme: string) => Promise<void>, context: common.UIAbilityContext, resetNetwork: () => void = (): void => {}) {
    this.controller = controller; this.stateChanged = stateChanged; this.themeChanged = themeChanged;
    this.voice = new NativeVoice(context);
    this.resetNetwork = resetNetwork;
    this.owner = nativeTransferOwner(context);
    this.uploads = this.owner.uploads;
  }

  register(): boolean {
    try {
      this.controller.registerJavaScriptProxy(this, 'begoniaNative', ['request'], [], PERMISSION);
      this.view = this.owner.attach(); this.active = true;
      return true;
    } catch { return false; }
  }

  close(): void {
    this.active = false;
    this.ready = false; this.resetNetwork(); this.voice.close(); this.owner.detach(this.view);
    try { this.controller.deleteJavaScriptRegister('begoniaNative'); } catch { /* Already detached. */ }
  }

  private stopTransfers(): void {
    this.ready = false; this.resetNetwork(); this.voice.close(); this.owner.revoke();
  }

  uploadPreview(handle: string): UploadPreview {
    this.checkView(); if (!this.ready) { throw new Error('NATIVE_SESSION_CHANGED'); }
    return this.uploads.preview(handle);
  }
  private checkView(): void {
    if (!this.active || !this.owner.current(this.view)) { throw new Error('NATIVE_VIEW_DETACHED'); }
    if (this.controller.getUrl().split('#')[0] !== 'begonia-client://app/index.html') { throw new Error('NATIVE_FRAME_DENIED'); }
  }
  private async transferReply<T>(pending: Promise<T>): Promise<string> {
    const epoch = this.owner.generation(), result = await pending;
    this.checkView(); this.owner.check(epoch);
    return JSON.stringify({ version: 1, ok: true, data: result });
  }

  async request(raw: string): Promise<string> {
    try {
      if (!this.active || !this.owner.current(this.view) || this.controller.getLastJavascriptProxyCallingFrameUrl().split('#')[0] !== 'begonia-client://app/index.html' ||
        this.controller.getUrl().split('#')[0] !== 'begonia-client://app/index.html') { throw new Error('NATIVE_FRAME_DENIED'); }
      const value = JSON.parse(raw) as ClientRequest;
      if (!value || value.version !== 1 || typeof value.command !== 'string') { throw new Error('NATIVE_REQUEST_INVALID'); }
      if (this.ready && value.command !== 'upload.close' && (value.command.startsWith('upload.') || value.command.startsWith('download.')) && typeof value.authorization === 'string') {
        await this.owner.authorize(value.authorization); this.checkView();
      }
      if (value.command === 'client.configuration') {
        this.stopTransfers(); resetMemohOrigin(); this.stateChanged('login-page');
      } else if (value.command === 'client.configure' && typeof value.apiOrigin === 'string') {
        if (this.ready) { throw new Error('SERVICE_CONFIG_LOCKED'); }
        configureMemohOrigin(value.apiOrigin, value.networkPolicy);
      } else if (value.command === 'client.state' && typeof value.state === 'string' &&
        ['login-page', 'ready', 'failed'].includes(value.state)) {
        if (value.state === 'ready' && !MEMOH_ORIGIN) { throw new Error('SERVICE_NOT_CONFIGURED'); }
        this.ready = value.state === 'ready';
        if (this.ready) {
          try { await this.owner.ready(value.authorization); this.checkView(); this.voice.prepare(); }
          catch (error) { this.ready = false; throw error as Error; }
        }
        else if (value.state === 'login-page') { this.stopTransfers(); }
        else { this.resetNetwork(); this.voice.close(); }
        this.stateChanged(value.state);
      }
      else if (value.command === 'client.theme' && typeof value.themeId === 'string') {
        await this.themeChanged(value.themeId);
      } else if (value.command === 'upload.view.release') {
        this.uploads.releasePreviews();
      } else if (value.command === 'upload.observe.detach' || value.command === 'download.observe.detach') {
        // The bridge call belongs to an observer; the existing native work retains ownership.
      } else if (value.command === 'upload.close') {
        if (value.authorization === undefined || this.owner.ownsCredential(value.authorization)) { this.stopTransfers(); }
      } else if (this.ready && value.command.startsWith('upload.')) {
        if (typeof value.operation !== 'string') { throw new Error('NATIVE_REQUEST_INVALID'); }
        if (value.command === 'upload.pause') { this.owner.pauseUpload(value.operation, false); }
        else if (value.command === 'upload.abort') { this.owner.pauseUpload(value.operation, true); }
        else if (value.command === 'upload.stage.append') {
          if (typeof value.authorization !== 'string' || typeof value.offset !== 'number' || typeof value.data !== 'string') { throw new Error('NATIVE_REQUEST_INVALID'); }
          return await this.transferReply(this.uploads.stageAppend(value.operation, value.authorization, value.offset, value.data));
        } else if (value.command === 'upload.stage.finish') {
          if (typeof value.authorization !== 'string') { throw new Error('NATIVE_REQUEST_INVALID'); }
          return await this.transferReply(this.uploads.stageFinish(value.operation, value.authorization));
        }
        else {
          if (typeof value.botId !== 'string' || typeof value.scope !== 'string' || typeof value.authorization !== 'string' ||
            (value.handle !== undefined && typeof value.handle !== 'string') ||
            (value.photos !== undefined && typeof value.photos !== 'boolean') ||
            (value.recoverDrafts !== undefined && typeof value.recoverDrafts !== 'boolean')) { throw new Error('NATIVE_REQUEST_INVALID'); }
          const scope: UploadScope = { operation: value.operation, botId: value.botId, scope: value.scope,
            authorization: value.authorization, handle: value.handle, photos: value.photos, recoverDrafts: value.recoverDrafts };
          if (value.command === 'upload.stage.begin') {
            if (typeof value.name !== 'string' || typeof value.type !== 'string' || typeof value.size !== 'number') { throw new Error('NATIVE_REQUEST_INVALID'); }
            return await this.transferReply(this.uploads.stageBegin(scope, value.name, value.type, value.size));
          }
          if (value.command === 'upload.select') { return await this.transferReply(this.uploads.select(scope)); }
          if (value.command === 'upload.list') { return await this.transferReply(this.owner.listUploads(scope)); }
          if (value.command === 'upload.run') { return await this.transferReply(this.owner.runUpload(scope)); }
          if (value.command === 'upload.consume') { await this.uploads.consume(scope); }
          else if (value.command === 'upload.discard') { await this.uploads.discard(scope); }
          else { throw new Error('NATIVE_COMMAND_UNAVAILABLE'); }
        }
      } else if (this.ready && value.command.startsWith('download.')) {
        if (typeof value.operation !== 'string' || typeof value.botId !== 'string') { throw new Error('NATIVE_REQUEST_INVALID'); }
        if (value.command === 'download.fetch') {
          if (typeof value.hash !== 'string' || typeof value.size !== 'number' || typeof value.name !== 'string' ||
            typeof value.url !== 'string' || typeof value.authorization !== 'string') { throw new Error('NATIVE_REQUEST_INVALID'); }
          return await this.transferReply(this.owner.fetchDownload({ operation: value.operation, botId: value.botId,
            hash: value.hash, size: value.size, name: value.name, url: value.url, authorization: value.authorization }));
        } else if (value.command === 'download.save') {
          return await this.transferReply(this.owner.saveDownload(value.operation, value.botId, this.view));
        } else if (value.command === 'download.release') { this.owner.releaseDownload(value.operation, value.botId, false); }
        else if (value.command === 'download.abort') { this.owner.releaseDownload(value.operation, value.botId, true); }
        else { throw new Error('NATIVE_COMMAND_UNAVAILABLE'); }
      } else if (this.ready && value.command.startsWith('voice.')) {
        const handle = value.handle ?? '';
        if (value.command === 'voice.start') { return JSON.stringify({ ok: true, data: await this.voice.start(handle) }); }
        if (value.command === 'voice.status') { return JSON.stringify({ ok: true, data: this.voice.status(handle) }); }
        if (value.command === 'voice.stop' || value.command === 'voice.transcribe') {
          return JSON.stringify({ ok: true, data: await this.voice.finish(handle, value.command === 'voice.transcribe') });
        }
        if (value.command === 'voice.cancel') { await this.voice.cancel(handle); }
        else if (value.command === 'voice.read') { return JSON.stringify({ ok: true, data: this.voice.read(handle, value.offset ?? 0) }); }
        else if (value.command === 'voice.release') { this.voice.release(handle); }
        else { throw new Error('NATIVE_COMMAND_UNAVAILABLE'); }
      } else { throw new Error('NATIVE_COMMAND_UNAVAILABLE'); }
      this.checkView();
      return '{"version":1,"ok":true}';
    } catch (error) {
      const message = (error as Error).message;
      const failure = (error as UploadStageFailure).uploadFailure;
      // Optional metadata on the existing error response, not a log/export command.
      // Reconstruct only fixed enums/numbers; never serialize the Error or cause.
      const phases = ['owner-read', 'limits-read', 'picker', 'source-metadata', 'source-open', 'snapshot-create',
        'snapshot-copy', 'snapshot-hash', 'source-verify', 'journal-publish', 'journal-dir-sync'];
      const allowed = this.active && this.ready && failure?.version === 1 && phases.includes(failure.phase) &&
        Number.isSafeInteger(failure.sdkCode) && failure.sdkCode >= 0 && failure.sdkCode <= 2147483647 &&
        Number.isSafeInteger(failure.at) && failure.at > 0;
      const metadata: UploadFailure | undefined = allowed && failure
        ? { version: 1, phase: failure.phase, sdkCode: failure.sdkCode, at: failure.at } : undefined;
      return JSON.stringify({ version: 1, ok: false, error: /^[A-Z0-9_]+$/.test(message) ? message : 'VOICE_UNAVAILABLE',
        failure: metadata });
    }
  }
}

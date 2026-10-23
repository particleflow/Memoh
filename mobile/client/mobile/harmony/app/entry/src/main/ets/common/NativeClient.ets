import { webview } from '@kit.ArkWeb';
import { common } from '@kit.AbilityKit';
import { NativeVoice } from './NativeVoice';
import { NativeMediaDownload } from './NativeMediaDownload';
import { MEMOH_ORIGIN, configureMemohOrigin, resetMemohOrigin } from './MemohClientConfig';

interface ClientRequest {
  version: number; command: string; state?: string; themeId?: string; handle?: string; offset?: number;
  apiOrigin?: string; operation?: string; botId?: string; hash?: string; size?: number;
  name?: string; url?: string; authorization?: string;
}
const PERMISSION: string = '{"javascriptProxyPermission":{"urlPermissionList":[{"scheme":"begonia-client","host":"app","port":"","path":"/index.html"}]}}';

// Current page lifecycle, theme and foreground microphone.
export class NativeClient {
  private active: boolean = false;
  private controller: webview.WebviewController;
  private stateChanged: (state: string) => void;
  private themeChanged: (theme: string) => Promise<void>;
  private voice: NativeVoice;
  private downloads: NativeMediaDownload;
  private ready: boolean = false;
  constructor(controller: webview.WebviewController, stateChanged: (state: string) => void,
    themeChanged: (theme: string) => Promise<void>, context: common.UIAbilityContext) {
    this.controller = controller; this.stateChanged = stateChanged; this.themeChanged = themeChanged;
    this.voice = new NativeVoice(context);
    this.downloads = new NativeMediaDownload(context, () => this.active && this.ready && MEMOH_ORIGIN !== '');
  }

  register(): boolean {
    try {
      this.controller.registerJavaScriptProxy(this, 'begoniaNative', ['request'], [], PERMISSION);
      this.active = true;
      return true;
    } catch { return false; }
  }

  close(): void {
    this.active = false;
    this.stopTransfers(); resetMemohOrigin();
    try { this.controller.deleteJavaScriptRegister('begoniaNative'); } catch { /* Already detached. */ }
  }

  private stopTransfers(): void {
    this.ready = false; this.voice.close(); this.downloads.close();
  }

  async request(raw: string): Promise<string> {
    try {
      if (!this.active || this.controller.getLastJavascriptProxyCallingFrameUrl().split('#')[0] !== 'begonia-client://app/index.html' ||
        this.controller.getUrl().split('#')[0] !== 'begonia-client://app/index.html') { throw new Error('NATIVE_FRAME_DENIED'); }
      const value = JSON.parse(raw) as ClientRequest;
      if (!value || value.version !== 1 || typeof value.command !== 'string') { throw new Error('NATIVE_REQUEST_INVALID'); }
      if (value.command === 'client.configuration') {
        this.stopTransfers(); resetMemohOrigin(); this.stateChanged('login-page');
      } else if (value.command === 'client.configure' && typeof value.apiOrigin === 'string') {
        if (this.ready) { throw new Error('SERVICE_CONFIG_LOCKED'); }
        configureMemohOrigin(value.apiOrigin);
      } else if (value.command === 'client.state' && typeof value.state === 'string' &&
        ['login-page', 'ready', 'failed'].includes(value.state)) {
        if (value.state === 'ready' && !MEMOH_ORIGIN) { throw new Error('SERVICE_NOT_CONFIGURED'); }
        this.ready = value.state === 'ready';
        if (this.ready) { this.voice.prepare(); } else { this.stopTransfers(); }
        this.stateChanged(value.state);
      }
      else if (value.command === 'client.theme' && typeof value.themeId === 'string') {
        await this.themeChanged(value.themeId);
      } else if (this.ready && value.command.startsWith('download.')) {
        if (typeof value.operation !== 'string' || typeof value.botId !== 'string') { throw new Error('NATIVE_REQUEST_INVALID'); }
        if (value.command === 'download.fetch') {
          if (typeof value.hash !== 'string' || typeof value.size !== 'number' || typeof value.name !== 'string' ||
            typeof value.url !== 'string' || typeof value.authorization !== 'string') { throw new Error('NATIVE_REQUEST_INVALID'); }
          const result = await this.downloads.fetch({ operation: value.operation, botId: value.botId,
            hash: value.hash, size: value.size, name: value.name, url: value.url, authorization: value.authorization });
          return JSON.stringify({ version: 1, ok: true, data: result });
        } else if (value.command === 'download.save') {
          return JSON.stringify({ version: 1, ok: true, data: await this.downloads.save(value.operation, value.botId) });
        } else if (value.command === 'download.abort') { this.downloads.abort(value.operation, value.botId); }
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
      return '{"version":1,"ok":true}';
    } catch (error) {
      const message = (error as Error).message;
      return JSON.stringify({ version: 1, ok: false, error: /^[A-Z0-9_]+$/.test(message) ? message : 'VOICE_UNAVAILABLE' });
    }
  }
}

import { webview } from '@kit.ArkWeb';
import { common } from '@kit.AbilityKit';
import { NativeVoice } from './NativeVoice';

interface ClientRequest { version: number; command: string; state?: string; themeId?: string; handle?: string; offset?: number; }
const PERMISSION: string = '{"javascriptProxyPermission":{"urlPermissionList":[{"scheme":"begonia-client","host":"app","port":"","path":"/index.html"}]}}';

// Current page lifecycle, theme and foreground microphone.
export class NativeClient {
  private active: boolean = false;
  private controller: webview.WebviewController;
  private stateChanged: (state: string) => void;
  private themeChanged: (theme: string) => Promise<void>;
  private voice: NativeVoice;
  private ready: boolean = false;
  constructor(controller: webview.WebviewController, stateChanged: (state: string) => void,
    themeChanged: (theme: string) => Promise<void>, context: common.UIAbilityContext) {
    this.controller = controller; this.stateChanged = stateChanged; this.themeChanged = themeChanged;
    this.voice = new NativeVoice(context);
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
    this.ready = false; this.voice.close();
    try { this.controller.deleteJavaScriptRegister('begoniaNative'); } catch { /* Already detached. */ }
  }

  async request(raw: string): Promise<string> {
    try {
      if (!this.active || this.controller.getLastJavascriptProxyCallingFrameUrl().split('#')[0] !== 'begonia-client://app/index.html' ||
        this.controller.getUrl().split('#')[0] !== 'begonia-client://app/index.html') { throw new Error('NATIVE_FRAME_DENIED'); }
      const value = JSON.parse(raw) as ClientRequest;
      if (value.version !== 1) { throw new Error('NATIVE_REQUEST_INVALID'); }
      if (value.command === 'client.state' && typeof value.state === 'string' &&
        ['login-page', 'ready', 'failed'].includes(value.state)) {
        this.ready = value.state === 'ready';
        if (this.ready) { this.voice.prepare(); } else { this.voice.close(); }
        this.stateChanged(value.state);
      }
      else if (value.command === 'client.theme' && typeof value.themeId === 'string') {
        await this.themeChanged(value.themeId);
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

import { webSocket } from '@kit.NetworkKit';
import { webview } from '@kit.ArkWeb';
import { BusinessError } from '@kit.BasicServicesKit';
import { util } from '@kit.ArkTS';
import { MEMOH_ORIGIN } from './MemohClientConfig';
import { memohRequest } from './SharedWebPolicy';
import { ClientDiagnostics } from './ClientDiagnostics';

interface ChatSocketState {
  id: string; socket: webSocket.WebSocket; authorization: string;
  opened: boolean; closed: boolean;
}
interface WsPushDetail { id: string; kind: string; data?: string; binary?: boolean; code?: number; }

// Native WebSocket bridge for the chat and terminal sockets. ArkWeb's own
// socket path shares the engine's default-network DNS view, so the page's
// window.WebSocket is wrapped (overlay side) onto this netstack-backed
// implementation, which follows the app's bound network like every other
// native transfer. The logical URL guard and the HTTP bridge's owner session
// remain the only admission rules; no new credential store or page route.
export class NativeChatSocket {
  private sockets: Map<string, ChatSocketState> = new Map();
  private controller: webview.WebviewController;
  private session: (authorization: string) => boolean;
  private page: () => boolean;
  private base64: util.Base64Helper = new util.Base64Helper();
  constructor(controller: webview.WebviewController, session: (authorization: string) => boolean, page: () => boolean) {
    this.controller = controller; this.session = session; this.page = page;
  }

  open(id: string, url: string, authorization: string): void {
    if (!/^[A-Za-z0-9_-]{1,128}$/.test(id)) { throw new Error('NATIVE_REQUEST_INVALID'); }
    const socketProtocol = MEMOH_ORIGIN.startsWith('https://') ? 'wss://' : 'ws://';
    if (!memohRequest(url) || !url.startsWith(socketProtocol) || !this.session(authorization)) {
      ClientDiagnostics.event('NETWORK', 'chat-socket', 'open-rejected', '');
      throw new Error('NATIVE_WS_REJECTED');
    }
    this.close(id);
    const socket = webSocket.createWebSocket();
    const state: ChatSocketState = { id, socket, authorization, opened: false, closed: false };
    this.sockets.set(id, state);
    socket.on('open', (_error: BusinessError, _value: Object): void => {
      if (state.closed) { return; }
      state.opened = true;
      ClientDiagnostics.event('NETWORK', 'chat-socket', 'connected', '');
      this.push({ id: state.id, kind: 'open' });
    });
    socket.on('message', (_error: BusinessError, value: string | ArrayBuffer): void => {
      if (state.closed) { return; }
      if (typeof value === 'string') { this.push({ id: state.id, kind: 'message', data: value, binary: false }); }
      else { this.push({ id: state.id, kind: 'message', data: this.base64.encodeToStringSync(new Uint8Array(value)), binary: true }); }
    });
    socket.on('close', (_error: BusinessError, value: webSocket.CloseResult): void => {
      if (state.closed) { return; }
      state.closed = true;
      ClientDiagnostics.event('NETWORK', 'chat-socket', 'closed', 'code=' + value.code);
      this.push({ id: state.id, kind: 'close', code: value.code });
      this.drop(state);
    });
    socket.on('error', (error: BusinessError): void => {
      if (state.closed) { return; }
      // SDK error text is not forwarded to the page; the close event that
      // follows carries the reconnect decision.
      this.push({ id: state.id, kind: 'error', code: typeof error.code === 'number' ? error.code : 0 });
    });
    const header: Record<string, string> = { 'Authorization': authorization };
    const options: webSocket.WebSocketRequestOptions = { header: header };
    socket.connect(url, options)
      .catch((): void => {
        if (state.closed) { return; }
        state.closed = true;
        ClientDiagnostics.event('NETWORK', 'chat-socket', 'connect-failed', '');
        this.push({ id: state.id, kind: 'error', code: 0 });
        this.push({ id: state.id, kind: 'close', code: 1006 });
        this.drop(state);
      });
  }

  send(id: string, data: string, binary: boolean): void {
    const state = this.sockets.get(id);
    if (!state || !state.opened || state.closed) { throw new Error('NATIVE_WS_UNAVAILABLE'); }
    let payload: string | ArrayBuffer = data;
    if (binary) {
      const bytes = this.base64.decodeSync(data);
      payload = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer;
    }
    state.socket.send(payload).catch((): void => {
      ClientDiagnostics.event('NETWORK', 'chat-socket', 'send-failed', '');
    });
  }

  close(id: string): void {
    const state = this.sockets.get(id);
    if (!state) { return; }
    state.closed = true;
    this.drop(state);
    try { state.socket.close(); } catch { /* Already closed. */ }
  }

  closeAll(): void {
    const ids: string[] = Array.from(this.sockets.keys());
    for (const id of ids) { this.close(id); }
  }

  private drop(state: ChatSocketState): void {
    if (this.sockets.get(state.id) !== state) { return; }
    this.sockets.delete(state.id);
    try { state.socket.off('open'); } catch { /* Already off. */ }
    try { state.socket.off('message'); } catch { /* Already off. */ }
    try { state.socket.off('close'); } catch { /* Already off. */ }
    try { state.socket.off('error'); } catch { /* Already off. */ }
  }

  private push(detail: WsPushDetail): void {
    if (!this.page()) { return; }
    // JSON.stringify output is a self-contained JavaScript literal; no page
    // data is interpolated outside it.
    const script = "window.dispatchEvent(new CustomEvent('garnet:ws', { detail: " + JSON.stringify(detail) + " }))";
    this.controller.runJavaScript(script).catch((): void => {});
  }
}

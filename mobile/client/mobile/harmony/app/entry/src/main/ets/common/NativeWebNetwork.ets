import { webview } from '@kit.ArkWeb';
import { rcp } from '@kit.RemoteCommunicationKit';
import { util, url } from '@kit.ArkTS';
import { MEMOH_ORIGIN } from './MemohClientConfig';
import { WEB_START, memohRequest } from './SharedWebPolicy';
import { NativeTransferNetwork, NativeTransferPolicy } from './NativeTransferNetwork';

interface WebIdentity { id: string; network_policy?: NativeTransferPolicy; }
interface WebTransfer {
  source: webview.WebSchemeHandlerRequest; sink: webview.WebResourceHandler;
  session: rcp.Session; epoch: number; base: string; authorization: string;
  ownerRead: boolean; stopped: boolean; request?: rcp.Request;
}

// Connection adapter below the unchanged logical URL guard. No new page URL,
// JavaScript command, file protocol, storage key, cookie jar or persistent route.
export class NativeWebNetwork {
  private epoch: number = 0;
  private authorization: string = '';
  private owner: string = '';
  private policySignature: string = '';
  private route?: NativeTransferNetwork;
  private transfers: Map<webview.WebSchemeHandlerRequest, WebTransfer> = new Map();
  private handler: webview.WebSchemeHandler = new webview.WebSchemeHandler();
  private alive: () => boolean;
  constructor(alive: () => boolean) { this.alive = alive; }

  attach(controller: webview.WebviewController): void {
    this.handler.onRequestStart((source: webview.WebSchemeHandlerRequest, sink: webview.WebResourceHandler): boolean => this.start(source, sink));
    this.handler.onRequestStop((source: webview.WebSchemeHandlerRequest): void => {
      const state = this.transfers.get(source);
      if (state) { this.stop(state); }
    });
    controller.setWebSchemeHandler('https', this.handler);
    controller.setWebSchemeHandler('http', this.handler);
  }

  reset(): void {
    this.epoch++; this.authorization = ''; this.owner = ''; this.policySignature = ''; this.route = undefined;
    this.transfers.forEach((state: WebTransfer): void => {
      try { state.sink.didFail(-3); } catch { /* ArkWeb may already have stopped it. */ }
      this.stop(state);
    });
  }

  private stop(state: WebTransfer): void {
    state.stopped = true; this.transfers.delete(state.source);
    if (state.request) { try { state.session.cancel(state.request); } catch { /* Already finished. */ } }
    try { state.session.close(); } catch { /* Already closed. */ }
  }

  private invalidate(state: WebTransfer): void {
    // Preserve this original profile response while canceling the invalidated
    // identity's pending file bodies. No fallback request is created.
    this.transfers.delete(state.source); this.reset(); state.epoch = this.epoch;
    this.transfers.set(state.source, state);
  }

  private check(state: WebTransfer): void {
    if (state.stopped || !this.alive() || state.epoch !== this.epoch || state.base !== MEMOH_ORIGIN ||
      (!state.ownerRead && state.authorization !== this.authorization)) { throw new Error('WEB_NETWORK_IDENTITY'); }
  }

  private start(source: webview.WebSchemeHandlerRequest, sink: webview.WebResourceHandler): boolean {
    const target = source.getRequestUrl();
    // Direct physical URLs, subframes and external URLs retain the old refusal.
    if (!this.alive() || !MEMOH_ORIGIN.startsWith('https://') || source.isMainFrame() ||
      source.getFrameUrl().split('#')[0] !== WEB_START || !memohRequest(target)) { return false; }
    if (!['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'].includes(source.getRequestMethod())) { return false; }
    const ownerRead = target === MEMOH_ORIGIN + '/users/me' && source.getRequestMethod() === 'GET';
    const path = target.slice(MEMOH_ORIGIN.length).split('?')[0];
    const file = /^\/bots\/[^/]+\/(?:media(?:\/|$)|container\/fs(?:\/|$)|sessions\/[^/]+\/delegations\/[^/]+\/attachments\/)/.test(path);
    if (!ownerRead && (!file || !this.route)) { return false; }
    const auth = source.getHeader().filter((header: webview.WebHeader): boolean => header.headerKey.toLowerCase() === 'authorization');
    const query = url.URL.parseURL(target).searchParams.getAll('token');
    if (auth.length > 1 || query.length > 1 || ownerRead && auth.length !== 1) { return false; }
    const authorization = auth.length === 1 ? auth[0].headerValue : query.length === 1 ? 'Bearer ' + query[0] : '';
    if (!/^Bearer [^\s\r\n]+$/.test(authorization) || query.length === 1 && authorization !== 'Bearer ' + query[0]) { return false; }
    if (ownerRead && authorization !== this.authorization) { this.reset(); this.authorization = authorization; }
    else if (authorization !== this.authorization) { return false; }
    const state: WebTransfer = { source, sink, session: rcp.createSession(), epoch: this.epoch,
      base: MEMOH_ORIGIN, authorization, ownerRead, stopped: false };
    this.transfers.set(source, state);
    void this.run(state).catch(() => {
      if (!state.stopped) { try { sink.didFail(-2); } catch { /* Stopped by ArkWeb. */ } }
    }).finally(() => this.stop(state));
    return true;
  }

  private response(state: WebTransfer, status: number, headers: rcp.ResponseHeaders): void {
    this.check(state);
    // Never let a browser redirect resend this request outside the logical guard.
    if (status < 200 || status >= 300 && status < 400) { throw new Error('WEB_NETWORK_REDIRECT'); }
    const encoding = headers['content-encoding'];
    if (encoding && encoding !== 'identity') { throw new Error('WEB_NETWORK_ENCODING'); }
    const response = new webview.WebSchemeHandlerResponse();
    response.setUrl(state.source.getRequestUrl()); response.setStatus(status);
    // Do not collapse case-alias duplicates into one apparently valid diagnostic
    // tag. Other response headers retain their existing forwarding behavior.
    const requestTagNames = Object.keys(headers).filter((name: string): boolean => name.toLowerCase() === 'x-nas-request-id');
    for (const key of Object.keys(headers)) {
      if (['connection', 'transfer-encoding', 'keep-alive', 'set-cookie'].includes(key.toLowerCase())) { continue; }
      if (key.toLowerCase() === 'x-nas-request-id' && requestTagNames.length !== 1) { continue; }
      const value = headers[key];
      if (typeof value === 'string') {
        response.setHeaderByName(key, value, true);
        if (key.toLowerCase() === 'content-type') { response.setMimeType(value.split(';')[0]); }
      }
      else if (Array.isArray(value)) { response.setHeaderByName(key, value.join(', '), true); }
    }
    state.sink.didReceiveResponse(response);
  }

  private async run(state: WebTransfer): Promise<void> {
    const source = state.source;
    const target = source.getRequestUrl();
    const headers: rcp.RequestHeaders = {};
    for (const header of source.getHeader()) {
      const key = header.headerKey.toLowerCase();
      if (['host', 'cookie', 'connection', 'transfer-encoding', 'content-length', 'accept-encoding'].includes(key)) { continue; }
      headers[key] = header.headerValue;
    }
    headers['accept-encoding'] = 'identity';
    const body = source.getHttpBodyStream();
    if (body) { await body.initialize(); this.check(state); }
    const method = source.getRequestMethod() as rcp.HttpMethod;
    const request = new rcp.Request(target, method, headers);
    state.request = request;
    request.configuration = { security: { remoteValidation: 'system' }, transfer: { autoRedirect: false } };
    if (body) {
      request.content = async (maximum: number): Promise<ArrayBuffer> => {
        this.check(state);
        const chunk = await body.read(Math.min(maximum, 65536));
        this.check(state); return chunk;
      };
    }
    if (state.ownerRead) {
      // Observe the original profile read; no extra probe or physical identity GET.
      const bytes = new Uint8Array(65536); let size = 0;
      request.destination = (data: ArrayBuffer): void => {
        this.check(state);
        if (size + data.byteLength > bytes.byteLength) { throw new Error('WEB_NETWORK_PROFILE'); }
        bytes.set(new Uint8Array(data), size); size += data.byteLength;
      };
      this.check(state);
      const response = await state.session.fetch(request);
      this.check(state);
      if (response.statusCode === 200) {
        try {
          const identity = JSON.parse(new util.TextDecoder().decodeToString(bytes.subarray(0, size))) as WebIdentity;
          if (typeof identity.id !== 'string' || !identity.id || identity.id.length > 128) { throw new Error('WEB_NETWORK_PROFILE'); }
          if (identity.network_policy?.version === 2) {
            const route = new NativeTransferNetwork(state.base); route.adopt(identity.network_policy);
            const signature = JSON.stringify(identity.network_policy);
            if (this.owner && (identity.id !== this.owner || signature !== this.policySignature)) { this.invalidate(state); }
            this.owner = identity.id; this.policySignature = signature;
            this.route = route; this.authorization = state.authorization;
          } else { this.invalidate(state); }
        } catch { this.invalidate(state); } // Invalid optional policy never authorizes a target.
      } else { this.invalidate(state); }
      this.response(state, response.statusCode, response.headers);
      if (size) { state.sink.didReceiveResponseBody(bytes.buffer.slice(0, size) as ArrayBuffer); }
    } else {
      let status = 0; let responded = false;
      request.configuration.tracing = { httpEventsHandler: {
        onStatusCodeReceive: (code: number): void => { this.check(state); status = code; },
        onHeaderReceive: (responseHeaders: rcp.ResponseHeaders): void => {
          if (status >= 200 && !responded) { this.response(state, status, responseHeaders); responded = true; }
        }
      } };
      request.destination = (data: ArrayBuffer): void => {
        this.check(state);
        if (!responded) { throw new Error('WEB_NETWORK_HEADERS'); }
        state.sink.didReceiveResponseBody(data);
      };
      const route = this.route;
      if (!route) { throw new Error('WEB_NETWORK_IDENTITY'); }
      const response = await route.resource(state.session, target, method, (): rcp.Request => { this.check(state); return request; });
      this.check(state);
      if (!responded) { this.response(state, response.statusCode, response.headers); }
    }
    this.check(state); state.sink.didFinish();
  }
}

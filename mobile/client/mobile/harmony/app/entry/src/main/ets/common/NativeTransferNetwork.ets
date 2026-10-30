import { connection } from '@kit.NetworkKit';
import { rcp } from '@kit.RemoteCommunicationKit';
import { url } from '@kit.ArkTS';
import { normalizeNetworkAddress, normalizeTransferNetworkPolicy, TransferNetworkPolicy } from './ServiceUrl';

interface NetworkFailure extends Error { code: number; }
// Exact authenticated /users/me v2 contract; no second settings document.
export interface NativeTransferEndpoint { logical_origin: string; physical_origin: string; }
export interface NativeTransferPolicy { version: number; lan_addresses?: string[]; endpoint?: NativeTransferEndpoint; }

// One instance per existing foreground transfer. No credentials, journal,
// timer, WebView cache, extra probe or cross-operation route store lives here.
export class NativeTransferNetwork {
  private base: string;
  private host: string = '';
  private port: number = 443;
  private policy?: TransferNetworkPolicy;
  private selected?: string;
  private ipv6?: string[];
  private proxyChecked: boolean = false;
  private platform: boolean = false;
  private physicalOrigin?: string;

  constructor(base: string, policy?: TransferNetworkPolicy) {
    this.base = base.replace(/\/+$/, '');
    if (policy === undefined) { return; } // Omitted policy preserves the old path.
    this.policy = normalizeTransferNetworkPolicy(policy);
    const parsed = url.URL.parseURL(this.base);
    if (parsed.protocol !== 'https:' || parsed.username || parsed.password || parsed.search || parsed.hash) { throw new Error('NETWORK_POLICY_HTTPS'); }
    // Static DNS mapping for literal authorities is unproven. Preserve normal
    // platform routing rather than blocking files or inventing another URL.
    this.authority(this.base);
  }

  private authority(value: string): void {
    const parsed = url.URL.parseURL(value);
    this.platform = parsed.hostname.startsWith('[') || /^[0-9.]+$/.test(parsed.hostname);
    this.host = parsed.hostname; this.port = Number(parsed.port || (parsed.protocol === 'http:' ? 80 : 443));
    this.selected = undefined; this.ipv6 = undefined; this.proxyChecked = false;
  }

  // Called only after the original logical identity GET has passed HTTP and
  // owner checks. Route changes never enter owner/receipt/journal identity.
  adopt(value?: NativeTransferPolicy): void {
    if (value === undefined || value === null) { return; }
    if (Array.isArray(value) || Object.keys(value).some((key: string): boolean => !['version', 'lan_addresses', 'endpoint'].includes(key))) { throw new Error('NETWORK_POLICY_INVALID'); }
    if (value.version === 1 && value.endpoint === undefined) {
      if (url.URL.parseURL(this.base).protocol !== 'https:') { throw new Error('NETWORK_POLICY_HTTPS'); }
      const old: TransferNetworkPolicy = { version: 1, lan_addresses: value.lan_addresses ?? [] };
      this.policy = normalizeTransferNetworkPolicy(old); this.physicalOrigin = undefined; this.authority(this.base); return;
    }
    const endpoint = value.endpoint;
    if (value.version !== 2 || !endpoint || Array.isArray(endpoint) || Object.keys(endpoint).sort().join(',') !== 'logical_origin,physical_origin') { throw new Error('NETWORK_POLICY_ENDPOINT'); }
    for (const origin of [endpoint.logical_origin, endpoint.physical_origin]) {
      if (typeof origin !== 'string' || !origin || origin.length > 2048 || /[?#%\\\s]/.test(origin)) { throw new Error('NETWORK_POLICY_ENDPOINT'); }
      const parsed = url.URL.parseURL(origin);
      if (!['https:', 'http:'].includes(parsed.protocol) || parsed.origin !== origin || parsed.username || parsed.password || parsed.pathname !== '/' ||
        (parsed.port !== '' && Number(parsed.port) < 1)) { throw new Error('NETWORK_POLICY_ENDPOINT'); }
    }
    if (endpoint.logical_origin !== url.URL.parseURL(this.base).origin) { throw new Error('NETWORK_POLICY_AUTHORITY'); }
    let policy: TransferNetworkPolicy | undefined;
    if (value.lan_addresses !== undefined && !Array.isArray(value.lan_addresses)) { throw new Error('NETWORK_POLICY_INVALID'); }
    if (value.lan_addresses !== undefined && value.lan_addresses.length) {
      const hints: TransferNetworkPolicy = { version: 1, lan_addresses: value.lan_addresses };
      policy = normalizeTransferNetworkPolicy(hints);
    }
    this.policy = policy; this.physicalOrigin = endpoint.physical_origin; this.authority(endpoint.physical_origin);
  }

  private dispatch(factory: () => rcp.Request, target: string, method: rcp.HttpMethod): rcp.Request {
    const request = factory();
    if (!this.physicalOrigin) { return request; }
    if (request.url.toString() !== target || request.method !== method) { throw new Error('NETWORK_POLICY_AUTHORITY'); }
    const logical = url.URL.parseURL(this.base).origin;
    request.url = url.URL.parseURL(this.physicalOrigin + target.slice(logical.length));
    const configuration: rcp.Configuration = request.configuration ?? {};
    configuration.security = { remoteValidation: 'system' };
    if (!configuration.transfer) { configuration.transfer = {}; }
    configuration.transfer.autoRedirect = false;
    request.configuration = configuration;
    return request; // Same request ID, content/chunk source, headers and callbacks.
  }

  private async checkProxy(): Promise<void> {
    if (this.proxyChecked) { return; }
    try { const proxy = await connection.getDefaultHttpProxy(); this.platform = typeof proxy.host !== 'string' || proxy.host !== ''; }
    catch { this.platform = true; }
    this.proxyChecked = true; // Never disable or replace a system proxy.
  }

  private safeRead(target: string, method: rcp.HttpMethod): boolean {
    if (method !== 'GET' || !target.startsWith(this.base + '/')) { return false; }
    const path = target.slice(this.base.length);
    // Only existing side-effect-free JSON reads, never observation or file bytes.
    return path === '/users/me' || /^\/bots\/[A-Za-z0-9_-]{1,128}\/media\/(limits|uploads\/[a-f0-9-]{36})$/.test(path);
  }

  private retryRead(error: Error): boolean {
    const code = (error as NetworkFailure).code;
    // A metadata GET may safely fail after receipt. This is NOT evidence that
    // an uncertain POST/PATCH was unsent. TLS/cert/HTTP never select another IP.
    return [1007900007, 1007900028, 1007900052, 1007900055, 1007900056].includes(code);
  }

  private request(factory: () => rcp.Request, address: string): rcp.Request {
    const request = factory();
    const configuration: rcp.Configuration = request.configuration ?? {};
    configuration.dns = { dnsRules: [{ host: this.host, port: this.port, ipAddresses: [address] }], happyEyeballOnDnsRule: false };
    configuration.security = { remoteValidation: 'system' };
    request.configuration = configuration;
    return request;
  }

  private async originalIPv6(): Promise<string[]> {
    if (this.ipv6 !== undefined) { return this.ipv6; }
    const network = await connection.getDefaultNet();
    const addresses = await network.getAddressesByName(this.host);
    const ipv6: string[] = [];
    for (const entry of addresses) {
      if (entry.family !== 2) { continue; }
      const address = normalizeNetworkAddress(entry.address);
      if (address.includes(':') && !ipv6.includes(address)) { ipv6.push(address); }
    }
    this.ipv6 = ipv6; return ipv6;
  }

  // ArkWeb's already-authorized logical resource request. Unlike the receipt
  // metadata reader this may include a query, but never retries a body/stream.
  async resource(session: rcp.Session, target: string, method: rcp.HttpMethod, factory: () => rcp.Request): Promise<rcp.Response> {
    if (/%(?:2f|5c|2e|25)/i.test(target.split(/[?#]/)[0])) { throw new Error('NETWORK_POLICY_AUTHORITY'); }
    const parsed = url.URL.parseURL(target);
    const base = url.URL.parseURL(this.base);
    if (parsed.origin !== base.origin || !parsed.pathname.startsWith(base.pathname.replace(/\/+$/, '') + '/') ||
      parsed.username || parsed.password || parsed.hash || /[\\\u0000-\u0020]/.test(target) ||
      /%(?:2f|5c|2e|25)/i.test(parsed.pathname) || parsed.pathname.includes('//')) { throw new Error('NETWORK_POLICY_AUTHORITY'); }
    const build = (): rcp.Request => this.dispatch(factory, target, method);
    if (!this.policy) { return session.fetch(build()); }
    await this.checkProxy();
    if (this.platform) { return session.fetch(build()); }
    return session.fetch(this.request(build, this.selected ?? this.policy.lan_addresses[0]));
  }

  async fetch(session: rcp.Session, target: string, method: rcp.HttpMethod, factory: () => rcp.Request): Promise<rcp.Response> {
    if (!this.policy && !this.physicalOrigin) { return session.fetch(factory()); }
    if (!target.startsWith(this.base + '/') || target.includes('?') || target.includes('#')) { throw new Error('NETWORK_POLICY_AUTHORITY'); }
    const build = (): rcp.Request => this.dispatch(factory, target, method);
    if (!this.policy || this.platform) { return session.fetch(build()); }
    await this.checkProxy();
    if (this.platform) { return session.fetch(build()); }
    const first = this.selected ?? this.policy.lan_addresses[0];
    if (!first) { throw new Error('NETWORK_POLICY_INVALID'); }
    if (!this.safeRead(target, method)) {
      // Initial selection is allowed; any write/stream outcome is returned
      // unchanged. Its original receipt reconciler owns further progress.
      return session.fetch(this.request(build, first));
    }
    const tried: string[] = [];
    let failure: Error = new Error('NETWORK_POLICY_UNAVAILABLE');
    const candidates = [first, ...this.policy.lan_addresses];
    for (let stage = 0; stage < 2; stage++) {
      const addresses = stage === 0 ? candidates : await this.originalIPv6();
      for (const address of addresses) {
        if (tried.includes(address)) { continue; }
        tried.push(address);
        try {
          const response = await session.fetch(this.request(build, address));
          if (response.statusCode >= 200 && response.statusCode < 300) { this.selected = address; }
          return response; // Includes 401/403/404/503; no HTTP-driven failover.
        } catch (error) {
          failure = error as Error;
          if (!this.retryRead(failure)) { throw failure; }
        }
      }
    }
    throw failure;
  }
}

import { connection } from '@kit.NetworkKit';
import { rcp } from '@kit.RemoteCommunicationKit';
import { url } from '@kit.ArkTS';
import { normalizeNetworkAddress, normalizeTransferNetworkPolicy, TransferNetworkPolicy } from './ServiceUrl';

interface NetworkFailure extends Error { code: number; }

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

  constructor(base: string, policy?: TransferNetworkPolicy) {
    this.base = base.replace(/\/+$/, '');
    if (policy === undefined) { return; } // Omitted policy preserves the old path.
    this.policy = normalizeTransferNetworkPolicy(policy);
    const parsed = url.URL.parseURL(this.base);
    if (parsed.protocol !== 'https:' || parsed.username || parsed.password || parsed.search || parsed.hash) { throw new Error('NETWORK_POLICY_HTTPS'); }
    // Static DNS mapping for literal authorities is unproven. Preserve normal
    // platform routing rather than blocking files or inventing another URL.
    this.platform = parsed.hostname.startsWith('[') || /^[0-9.]+$/.test(parsed.hostname);
    this.host = parsed.hostname; this.port = Number(parsed.port || 443);
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

  async fetch(session: rcp.Session, target: string, method: rcp.HttpMethod, factory: () => rcp.Request): Promise<rcp.Response> {
    if (!this.policy) { return session.fetch(factory()); }
    if (!target.startsWith(this.base + '/') || target.includes('?') || target.includes('#')) { throw new Error('NETWORK_POLICY_AUTHORITY'); }
    if (this.platform) { return session.fetch(factory()); }
    await this.checkProxy();
    if (this.platform) { return session.fetch(factory()); }
    const first = this.selected ?? this.policy.lan_addresses[0];
    if (!first) { throw new Error('NETWORK_POLICY_INVALID'); }
    if (!this.safeRead(target, method)) {
      // Initial selection is allowed; any write/stream outcome is returned
      // unchanged. Its original receipt reconciler owns further progress.
      return session.fetch(this.request(factory, first));
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
          const response = await session.fetch(this.request(factory, address));
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

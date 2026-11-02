import { connection } from '@kit.NetworkKit';
import { ClientDiagnostics } from './ClientDiagnostics';

export interface NetFact { netId: number; wifi: boolean; cellular: boolean; ipv6: boolean; }
export interface BindingDecision { action: string; netId: number; reason: string; }
interface AppNetSnapshot { version: number; started: boolean; boundNetId: number; action: string; reason: string; nets: NetFact[]; }

// Pure probe->select decision. Wi-Fi with IPv6 wins over cellular so the
// binding never spends metered data while an unmetered IPv6 path exists.
export function decideAppNet(facts: NetFact[]): BindingDecision {
  const ordered = facts.slice().sort((left: NetFact, right: NetFact): number => left.netId - right.netId);
  const wifi = ordered.find((fact: NetFact): boolean => fact.wifi && fact.ipv6);
  if (wifi) { return { action: 'bind', netId: wifi.netId, reason: 'wifi-ipv6' }; }
  const cellular = ordered.find((fact: NetFact): boolean => fact.cellular && fact.ipv6);
  if (cellular) { return { action: 'bind', netId: cellular.netId, reason: 'cellular-ipv6' }; }
  return { action: 'default', netId: 0, reason: ordered.length ? 'no-ipv6-network' : 'no-network' };
}

// A cellular activation request is held exactly while local Wi-Fi without IPv6
// exists: that request is the public way to bring cellular up beside Wi-Fi, and
// its property events are the only late-IPv6 signal on that bearer.
export function needCellularRequest(facts: NetFact[]): boolean {
  return facts.some((fact: NetFact): boolean => fact.wifi) &&
    !facts.some((fact: NetFact): boolean => fact.wifi && fact.ipv6);
}

// Whole-app IPv6 network binding, public Network Kit APIs only (INTERNET and
// the already declared GET_NETWORK_INFO). No interception, proxy or fallback
// route is installed; not binding leaves the system default untouched.
export class AppNetBinding {
  private static started: boolean = false;
  private static running: boolean = false;
  private static queued: boolean = false;
  private static boundNetId: number = 0;
  private static strandedNetId: number = 0;
  private static decision: BindingDecision = { action: 'default', netId: 0, reason: 'not-evaluated' };
  private static facts: NetFact[] = [];
  private static summary: string = '';
  private static handles: Map<number, connection.NetHandle> = new Map();
  private static watcher?: connection.NetConnection;
  private static cellularRequest?: connection.NetConnection;

  static start(): void {
    if (AppNetBinding.started) { return; }
    AppNetBinding.started = true;
    const watcher = connection.createNetConnection();
    watcher.on('netAvailable', (_net: connection.NetHandle): void => { void AppNetBinding.evaluate('net-available'); });
    watcher.on('netLost', (net: connection.NetHandle): void => { AppNetBinding.lost(net.netId, 'net-lost'); });
    watcher.on('netCapabilitiesChange', (_info: connection.NetCapabilityInfo): void => { void AppNetBinding.evaluate('capabilities-change'); });
    watcher.on('netConnectionPropertiesChange', (_info: connection.NetConnectionPropertyInfo): void => { void AppNetBinding.evaluate('properties-change'); });
    watcher.register((): void => {});
    AppNetBinding.watcher = watcher;
    ClientDiagnostics.event('NETWORK', 'app-net', 'start', '');
    void AppNetBinding.evaluate('start');
  }

  static stop(): void {
    if (!AppNetBinding.started) { return; }
    AppNetBinding.started = false;
    ClientDiagnostics.event('NETWORK', 'app-net', 'stop', 'bound=' + AppNetBinding.boundNetId);
    const watcher = AppNetBinding.watcher;
    AppNetBinding.watcher = undefined;
    if (watcher) { try { watcher.unregister((): void => {}); } catch { /* Already unregistered. */ } }
    AppNetBinding.releaseCellular();
    void AppNetBinding.unbind('stop');
  }

  // Read-only facts for the existing dump surface: current binding, decision
  // and per-network IPv6 observation. No promise about ArkWeb coverage.
  static dump(): string[] {
    const snapshot: AppNetSnapshot = { version: 1, started: AppNetBinding.started, boundNetId: AppNetBinding.boundNetId,
      action: AppNetBinding.decision.action, reason: AppNetBinding.decision.reason, nets: AppNetBinding.facts };
    return ['GarnetAppNet:' + JSON.stringify(snapshot)];
  }

  private static lost(netId: number, trigger: string): void {
    if (netId !== 0 && netId === AppNetBinding.boundNetId) { AppNetBinding.strandedNetId = netId; }
    void AppNetBinding.evaluate(trigger);
  }

  private static holdCellular(): void {
    if (AppNetBinding.cellularRequest) { return; }
    const specifier: connection.NetSpecifier = { netCapabilities: { bearerTypes: [connection.NetBearType.BEARER_CELLULAR] } };
    const request = connection.createNetConnection(specifier);
    request.on('netAvailable', (_net: connection.NetHandle): void => { void AppNetBinding.evaluate('cellular-available'); });
    request.on('netLost', (net: connection.NetHandle): void => { AppNetBinding.lost(net.netId, 'cellular-lost'); });
    request.on('netConnectionPropertiesChange', (_info: connection.NetConnectionPropertyInfo): void => { void AppNetBinding.evaluate('cellular-properties-change'); });
    try { request.register((): void => {}); AppNetBinding.cellularRequest = request; }
    catch { /* Activation request refused; later watcher events re-evaluate. */ }
  }

  private static releaseCellular(): void {
    const request = AppNetBinding.cellularRequest;
    AppNetBinding.cellularRequest = undefined;
    if (!request) { return; }
    try { request.unregister((): void => {}); } catch { /* Already unregistered. */ }
  }

  private static hasIPv6DefaultRoute(routes: connection.RouteInfo[]): boolean {
    for (const route of routes) {
      if (route.isDefaultRoute === true && route.destination?.address?.address?.includes(':')) { return true; }
    }
    return false;
  }

  private static async probe(): Promise<NetFact[]> {
    const nets = await connection.getAllNets();
    const facts: NetFact[] = [];
    AppNetBinding.handles.clear();
    for (const net of nets) {
      try {
        const capabilities = await connection.getNetCapabilities(net);
        const properties = await connection.getConnectionProperties(net);
        const bearers = capabilities?.bearerTypes ?? [];
        const fact: NetFact = {
          netId: net.netId,
          wifi: bearers.includes(connection.NetBearType.BEARER_WIFI),
          cellular: bearers.includes(connection.NetBearType.BEARER_CELLULAR),
          ipv6: properties?.isIPv6LinkValid === true || AppNetBinding.hasIPv6DefaultRoute(properties?.routes ?? [])
        };
        AppNetBinding.handles.set(net.netId, net);
        facts.push(fact);
      } catch { /* A network dissolving mid-probe is excluded, not fatal. */ }
    }
    return facts;
  }

  private static describe(facts: NetFact[]): string {
    return facts.map((fact: NetFact): string =>
      fact.netId + (fact.wifi ? 'w' : '') + (fact.cellular ? 'c' : '') + (fact.ipv6 ? '6' : '')).join(',');
  }

  private static async bind(netId: number): Promise<void> {
    if (netId === AppNetBinding.boundNetId) { return; }
    const handle = AppNetBinding.handles.get(netId);
    if (!handle) { ClientDiagnostics.event('NETWORK', 'app-net', 'bind-vanished', 'net=' + netId); return; }
    try {
      await connection.setAppNet(handle);
      AppNetBinding.boundNetId = netId;
      ClientDiagnostics.event('NETWORK', 'app-net', 'bound', 'net=' + netId);
    } catch {
      // The previous binding, if any, stays; the next evaluation retries.
      ClientDiagnostics.event('NETWORK', 'app-net', 'bind-failed', 'net=' + netId + '/bound=' + AppNetBinding.boundNetId);
    }
  }

  private static async unbind(trigger: string): Promise<void> {
    if (AppNetBinding.boundNetId === 0) { return; }
    try {
      const release = connection.getAppNetSync();
      release.netId = 0;
      await connection.setAppNet(release);
      AppNetBinding.boundNetId = 0;
      ClientDiagnostics.event('NETWORK', 'app-net', 'unbound', 'trigger=' + trigger);
    } catch {
      // boundNetId is kept so the next evaluation retries the release.
      ClientDiagnostics.event('NETWORK', 'app-net', 'unbind-failed', 'trigger=' + trigger + '/net=' + AppNetBinding.boundNetId);
    }
  }

  private static async evaluate(trigger: string): Promise<void> {
    if (!AppNetBinding.started && trigger !== 'start') { return; }
    if (AppNetBinding.running) { AppNetBinding.queued = true; return; }
    AppNetBinding.running = true;
    try {
      const facts = await AppNetBinding.probe();
      const decision = decideAppNet(facts);
      const summary = AppNetBinding.describe(facts);
      AppNetBinding.facts = facts;
      AppNetBinding.decision = decision;
      if (needCellularRequest(facts)) { AppNetBinding.holdCellular(); } else { AppNetBinding.releaseCellular(); }
      // Release a dead binding before the new decision takes effect so traffic
      // never stays stranded on a network the system has lost.
      if (AppNetBinding.strandedNetId !== 0 && AppNetBinding.strandedNetId === AppNetBinding.boundNetId) {
        await AppNetBinding.unbind('bound-net-lost');
      }
      if (decision.action === 'bind') { await AppNetBinding.bind(decision.netId); }
      else { await AppNetBinding.unbind('decision-default'); }
      if (AppNetBinding.boundNetId !== AppNetBinding.strandedNetId) { AppNetBinding.strandedNetId = 0; }
      else if (AppNetBinding.strandedNetId !== 0) { AppNetBinding.queued = true; }
      if (summary !== AppNetBinding.summary) {
        AppNetBinding.summary = summary;
        ClientDiagnostics.event('NETWORK', 'app-net', 'decision', 'trigger=' + trigger + '/action=' + decision.action +
          '/net=' + decision.netId + '/reason=' + decision.reason + '/nets=' + summary);
      }
    } catch {
      // SDK error text is not persisted; the trigger alone names the phase.
      ClientDiagnostics.event('NETWORK', 'app-net', 'evaluate-failed', 'trigger=' + trigger);
    } finally {
      AppNetBinding.running = false;
      if (AppNetBinding.queued) { AppNetBinding.queued = false; void AppNetBinding.evaluate('queued'); }
    }
  }
}

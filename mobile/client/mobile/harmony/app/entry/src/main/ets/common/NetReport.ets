import { bundleManager, common } from '@kit.AbilityKit';
import { fileIo as fs } from '@kit.CoreFileKit';
import { rcp } from '@kit.RemoteCommunicationKit';
import { util } from '@kit.ArkTS';
import { MEMOH_ORIGIN } from './MemohClientConfig';
import { AppNetBinding, NetFact } from './AppNetBinding';
import { ClientDiagnostics, DiagnosticEvent } from './ClientDiagnostics';

interface NetReportEvent { at: number; area: string; component: string; phase: string; detail: string; }
interface NetReportBinding {
  version: number; started: boolean; boundNetId: number; action: string; reason: string; nets: NetFact[];
}
interface NetReportBody {
  schema: number; at: number; version: string; build: number; reason: string;
  binding?: NetReportBinding; journal: NetReportEvent[];
}

// Failure self-report: when the app enters a network failure state it posts a
// machine-format snapshot (binding decision + journal tail) to the public
// /client/net-report intake and queues it for the next connectivity when the
// post itself cannot leave. No UI, no prose, no new credential surface; the
// payload fields mirror the server's strict schema exactly.
export class NetReport {
  private static readonly REASONS: string[] = ['client-failed', 'page-failed', 'client-timeout', 'manual'];
  private static readonly DEBOUNCE_MS: number = 30 * 60 * 1000;
  private static readonly MAX_BYTES: number = 65536;
  private static readonly MAX_QUEUE: number = 20;
  private static context?: common.Context;
  private static inFlight: boolean = false;
  private static lastReport: Map<string, number> = new Map();
  private static pending: string[] = [];

  static bind(context: common.Context): void { NetReport.context = context; }

  static report(reason: string): void {
    if (!MEMOH_ORIGIN || !NetReport.REASONS.includes(reason)) { return; }
    const now = Date.now();
    const previous = NetReport.lastReport.get(reason) ?? 0;
    if (now - previous < NetReport.DEBOUNCE_MS) { return; }
    NetReport.lastReport.set(reason, now);
    const line = NetReport.build(reason, now);
    if (!line) { return; }
    ClientDiagnostics.event('NETWORK', 'net-report', 'enqueued', 'reason=' + reason);
    void NetReport.enqueue(line).then((): void => { void NetReport.flush(); });
  }

  static flush(): void {
    if (NetReport.inFlight || !NetReport.context || !MEMOH_ORIGIN) { return; }
    NetReport.inFlight = true;
    void NetReport.drain().finally((): void => { NetReport.inFlight = false; });
  }

  private static async drain(): Promise<void> {
    const lines = NetReport.readQueue();
    if (!lines.length && !NetReport.pending.length) { return; }
    const remaining: string[] = [];
    for (const line of lines) {
      const status = await NetReport.post(line);
      if (status === 204) {
        ClientDiagnostics.event('NETWORK', 'net-report', 'sent', 'status=204');
      } else {
        ClientDiagnostics.event('NETWORK', 'net-report', 'post-failed', 'status=' + status);
        remaining.push(line);
      }
    }
    for (const item of NetReport.pending) { remaining.push(item); }
    NetReport.pending = [];
    NetReport.writeQueue(remaining.slice(-NetReport.MAX_QUEUE));
  }

  private static async enqueue(line: string): Promise<void> {
    if (NetReport.inFlight) { NetReport.pending.push(line); return; }
    const lines = NetReport.readQueue();
    lines.push(line);
    NetReport.writeQueue(lines.slice(-NetReport.MAX_QUEUE));
  }

  private static queuePath(): string {
    if (!NetReport.context) { throw new Error('NET_REPORT_NOT_BOUND'); }
    return NetReport.context.filesDir + '/net-report-queue.jsonl';
  }

  private static readQueue(): string[] {
    const path = NetReport.queuePath();
    if (!fs.accessSync(path)) { return []; }
    const lines: string[] = [];
    for (const line of fs.readTextSync(path).split('\n')) {
      if (line) { lines.push(line); }
    }
    return lines.slice(-NetReport.MAX_QUEUE);
  }

  private static writeQueue(lines: string[]): void {
    const path = NetReport.queuePath();
    const text = lines.length ? lines.join('\n') + '\n' : '';
    const file = fs.openSync(path, fs.OpenMode.CREATE | fs.OpenMode.READ_WRITE | fs.OpenMode.TRUNC);
    try {
      const bytes = new util.TextEncoder().encodeInto(text);
      let offset = 0;
      while (offset < bytes.byteLength) {
        const count = fs.writeSync(file.fd, bytes.buffer.slice(offset) as ArrayBuffer);
        if (count <= 0) { throw new Error('NET_REPORT_WRITE_INCOMPLETE'); }
        offset += count;
      }
      fs.fsyncSync(file.fd);
    } finally { fs.closeSync(file); }
  }

  private static async post(line: string): Promise<number> {
    const session = rcp.createSession();
    try {
      const request = new rcp.Request(MEMOH_ORIGIN + '/client/net-report', 'POST', { 'content-type': 'application/json' });
      request.content = line;
      request.configuration = { security: { remoteValidation: 'system' }, transfer: { autoRedirect: false } };
      const response = await session.fetch(request);
      return response.statusCode;
    } catch {
      return 0;
    } finally {
      try { session.close(); } catch { /* Already closed. */ }
    }
  }

  private static build(reason: string, at: number): string {
    let version = 'unknown'; let build = 0;
    try {
      const info = bundleManager.getBundleInfoForSelfSync(bundleManager.BundleFlag.GET_BUNDLE_INFO_DEFAULT);
      if (typeof info.versionName === 'string' && info.versionName.length >= 1 && info.versionName.length <= 32) { version = info.versionName; }
      if (Number.isSafeInteger(info.versionCode) && info.versionCode >= 0) { build = info.versionCode; }
    } catch { /* An unreadable self identity still reports with the fallback. */ }
    const body: NetReportBody = { schema: 1, at, version, build, reason, journal: NetReport.journal() };
    const binding = NetReport.binding();
    if (binding) { body.binding = binding; }
    let encoded = JSON.stringify(body);
    if (new util.TextEncoder().encodeInto(encoded).byteLength <= NetReport.MAX_BYTES) { return encoded; }
    // Over-budget reports shed their oldest journal lines first; the header
    // and binding are always far below the limit on their own.
    const trimmed: NetReportBody = { schema: 1, at, version, build, reason, journal: [] };
    if (binding) { trimmed.binding = binding; }
    while (body.journal.length) {
      body.journal.shift();
      encoded = JSON.stringify(body);
      if (new util.TextEncoder().encodeInto(encoded).byteLength <= NetReport.MAX_BYTES) { return encoded; }
    }
    return JSON.stringify(trimmed);
  }

  private static binding(): NetReportBinding | undefined {
    try {
      const line = AppNetBinding.dump()[0];
      const snapshot = JSON.parse(line.substring('GarnetAppNet:'.length)) as NetReportBinding;
      if (snapshot.version !== 1 || !Number.isSafeInteger(snapshot.boundNetId) || snapshot.boundNetId < 0 ||
        snapshot.action.length > 32 || snapshot.reason.length > 64 || !Array.isArray(snapshot.nets) ||
        !/^[A-Za-z0-9_.-]+$/.test(snapshot.action) || !/^[A-Za-z0-9_.-]+$/.test(snapshot.reason)) { return undefined; }
      snapshot.nets = snapshot.nets.slice(0, 16);
      return snapshot;
    } catch { return undefined; }
  }

  private static journal(): NetReportEvent[] {
    const events: NetReportEvent[] = [];
    for (const event of ClientDiagnostics.recentEvents()) {
      if (!NetReport.eventOk(event)) { continue; }
      events.push({ at: event.at, area: event.area, component: event.component, phase: event.phase,
        detail: event.detail.length > 512 ? event.detail.substring(0, 512) : event.detail });
    }
    return events;
  }

  private static eventOk(event: DiagnosticEvent): boolean {
    return Number.isSafeInteger(event.at) && event.at > 0 &&
      /^[A-Za-z0-9_.-]{1,64}$/.test(event.component) && /^[A-Za-z0-9_.-]{1,64}$/.test(event.phase);
  }
}

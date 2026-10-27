import { webview } from '@kit.ArkWeb';

interface ImageTimingRow {
  id: number; card?: number; request?: number; headers?: number; responseStart?: number; responseEnd?: number;
  encodedBodySize?: number; transferSize?: number; resourceMatch?: number;
  body?: number; bytes?: number; hashStart?: number; hashEnd?: number; blob?: number;
  load?: number; decodeStart?: number; decodeEnd?: number; render?: number; error?: number;
}
interface ImageTimingSnapshot { version: number; active: boolean; paintSupported: boolean; rows: ImageTimingRow[]; }

// No file, HiLog, network, arbitrary-JS or new JS->native bridge command.
export class ImageTimingDump {
  private static controller?: webview.WebviewController;
  private static allowed: () => boolean = (): boolean => false;
  private static epoch: number = 0;
  private static value: string = '';
  private static timer: number = -1;
  static bind(controller: webview.WebviewController, allowed: () => boolean): void {
    ImageTimingDump.controller = controller; ImageTimingDump.allowed = allowed;
  }
  static detach(): void {
    ImageTimingDump.command('clear'); ImageTimingDump.controller = undefined;
    ImageTimingDump.allowed = (): boolean => false;
  }
  private static number(value: number | undefined): number | undefined {
    return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : undefined;
  }
  static command(action: string): void {
    if (!['arm', 'take', 'clear'].includes(action)) { return; }
    const epoch = ++ImageTimingDump.epoch;
    ImageTimingDump.value = ''; clearTimeout(ImageTimingDump.timer);
    const controller = ImageTimingDump.controller;
    if (!controller || (action !== 'clear' && !ImageTimingDump.allowed())) { return; }
    try {
      if (controller.getUrl().split('#')[0] !== 'begonia-client://app/index.html') { return; }
    } catch { return; }
    // These three literals are the entire supported command set. Caller input is never evaluated.
    const script = action === 'arm' ? 'JSON.stringify(window.__garnetImageTiming?.arm() ?? null)' :
      action === 'take' ? 'JSON.stringify(window.__garnetImageTiming?.take() ?? null)' :
      'window.__garnetImageTiming?.clear(); null';
    controller.runJavaScript(script).then((encoded: string): void => {
      if (epoch !== ImageTimingDump.epoch || action === 'clear') { return; }
      try {
        // ArkWeb may JSON-encode a JavaScript string result. Accept at most one wrapper.
        const decoded = JSON.parse(encoded) as ImageTimingSnapshot | string;
        const source = (typeof decoded === 'string' ? JSON.parse(decoded) : decoded) as ImageTimingSnapshot;
        if (!source || source.version !== 1 || !Array.isArray(source.rows) || source.rows.length > 5) { return; }
        const rows: ImageTimingRow[] = [];
        for (const item of source.rows) {
          if (!Number.isInteger(item.id) || item.id < 1 || item.id > 5) { return; }
          rows.push({ id: item.id, card: ImageTimingDump.number(item.card), request: ImageTimingDump.number(item.request),
            headers: ImageTimingDump.number(item.headers),
            responseStart: typeof item.responseStart === 'number' && item.responseStart > 0 ? ImageTimingDump.number(item.responseStart) : undefined,
            responseEnd: typeof item.responseEnd === 'number' && item.responseEnd > 0 ? ImageTimingDump.number(item.responseEnd) : undefined,
            encodedBodySize: typeof item.encodedBodySize === 'number' && item.encodedBodySize > 0 ? ImageTimingDump.number(item.encodedBodySize) : undefined,
            transferSize: typeof item.transferSize === 'number' && item.transferSize > 0 ? ImageTimingDump.number(item.transferSize) : undefined,
            resourceMatch: item.resourceMatch === 1 || item.resourceMatch === 2 ? item.resourceMatch : undefined,
            body: ImageTimingDump.number(item.body), bytes: ImageTimingDump.number(item.bytes),
            hashStart: ImageTimingDump.number(item.hashStart), hashEnd: ImageTimingDump.number(item.hashEnd),
            blob: ImageTimingDump.number(item.blob), load: ImageTimingDump.number(item.load),
            decodeStart: ImageTimingDump.number(item.decodeStart), decodeEnd: ImageTimingDump.number(item.decodeEnd),
            render: ImageTimingDump.number(item.render), error: ImageTimingDump.number(item.error) });
        }
        const safe: ImageTimingSnapshot = { version: 1, active: source.active === true,
          paintSupported: source.paintSupported === true, rows };
        ImageTimingDump.value = JSON.stringify(safe);
        ImageTimingDump.timer = setTimeout((): void => { ImageTimingDump.value = ''; }, 120000);
      } catch { /* Do not log script output, exceptions, URL or page state. */ }
    }).catch((): void => {});
  }
  static dump(): string[] {
    return ['GarnetImageTiming:' + (ImageTimingDump.value || '{"version":1,"active":false,"paintSupported":false,"rows":[]}')];
  }
}

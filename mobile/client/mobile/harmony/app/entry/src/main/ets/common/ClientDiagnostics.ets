import { common } from '@kit.AbilityKit';
import { fileIo as fs } from '@kit.CoreFileKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { util } from '@kit.ArkTS';
import { BusinessError } from '@kit.BasicServicesKit';

interface DiagnosticEvent { at: number; area: string; component: string; phase: string; detail: string; }

// Engineering state only: callers never pass messages, credentials or media content.
export class ClientDiagnostics {
  private static context?: common.Context;
  static bind(context: common.Context): void { ClientDiagnostics.context = context; }
  static failure(area: string, component: string, phase: string, error: Error, detail: string = ''): void {
    const code = (error as BusinessError).code;
    // SDK messages can contain source URIs, private paths or credentials. Retain only codes and our enums.
    const reason = /^(ASSET|NATIVE|MEDIA|VOICE_TEXT)_[A-Z0-9_]+$/.test(error.message) ? error.message : 'SDK_ERROR';
    ClientDiagnostics.event(area, component, phase, detail + '/code=' + (Number.isInteger(code) ? code : 0) + '/reason=' + reason);
  }
  static event(area: string, component: string, phase: string, detail: string): void {
    hilog.info(0xB001, 'BegoniaClient', '%{public}s %{public}s', component, phase);
    const context = ClientDiagnostics.context;
    if (!context) { return; }
    const path = context.filesDir + '/client-diagnostics.jsonl';
    try {
      // Keep the preceding journal as well, without accumulating unbounded files.
      if (fs.accessSync(path) && fs.statSync(path).size > 1048576) {
        const previous = path + '.previous';
        if (fs.accessSync(previous)) { fs.unlinkSync(previous); }
        fs.renameSync(path, previous);
      }
      const record: DiagnosticEvent = { at: Date.now(), area, component, phase, detail };
      const bytes = new util.TextEncoder().encodeInto(JSON.stringify(record) + '\n');
      const file = fs.openSync(path, fs.OpenMode.CREATE | fs.OpenMode.WRITE_ONLY | fs.OpenMode.APPEND);
      try {
        let offset = 0;
        while (offset < bytes.byteLength) {
          const count = fs.writeSync(file.fd, bytes.buffer.slice(offset) as ArrayBuffer);
          if (count <= 0) { throw new Error('DIAGNOSTIC_WRITE_INCOMPLETE'); }
          offset += count;
        }
        fs.fsyncSync(file.fd);
      } finally { fs.closeSync(file); }
    } catch { hilog.error(0xB001, 'BegoniaClient', 'Local diagnostic write failed'); }
  }
}

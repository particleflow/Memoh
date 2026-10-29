import { UIAbility, Want } from '@kit.AbilityKit';
import { window } from '@kit.ArkUI';
import { BusinessError } from '@kit.BasicServicesKit';
import { ClientDiagnostics } from '../common/ClientDiagnostics';
import { ImageTimingDump } from '../common/ImageTimingDump';
import { nativeTransferOwner } from '../common/NativeTransferOwner';

export default class EntryAbility extends UIAbility {
  onCreate(_want: Want): void {
    nativeTransferOwner(this.context).bindAbility(this.context);
    ClientDiagnostics.bind(this.context);
    ClientDiagnostics.event('SYSTEM', 'lifecycle', 'ability-create', '');
  }
  // Foregrounding must retain the current chat, drafts and native transfers.
  onNewWant(want: Want): void {
    ClientDiagnostics.event('SYSTEM', 'lifecycle', 'want-received', '');
    const action = want.parameters?.garnetImageTiming;
    if (typeof action === 'string') { ImageTimingDump.command(action); }
  }
  onDestroy(): void { nativeTransferOwner(this.context).destroyAbility(); }
  onDump(_params: string[]): string[] { return ImageTimingDump.dump(); }
  onWindowStageCreate(stage: window.WindowStage): void {
    stage.loadContent('pages/SharedChat', (error: BusinessError) => {
      ClientDiagnostics.event('SYSTEM', 'lifecycle', error.code ? 'main-page-load-failed' : 'main-page-loaded',
        error.code ? 'errorCode=' + error.code : 'page=pages/SharedChat');
    });
  }
  onWindowStageDestroy(): void { ClientDiagnostics.event('SYSTEM', 'lifecycle', 'window-stage-destroyed', ''); }
  onForeground(): void { ClientDiagnostics.event('SYSTEM', 'lifecycle', 'foreground', ''); }
  onBackground(): void { ImageTimingDump.command('clear'); ClientDiagnostics.event('SYSTEM', 'lifecycle', 'background', ''); }
}

import { UIAbility, Want } from '@kit.AbilityKit';
import { window } from '@kit.ArkUI';
import { BusinessError } from '@kit.BasicServicesKit';
import { AppNetBinding } from '../common/AppNetBinding';
import { ClientDiagnostics } from '../common/ClientDiagnostics';
import { ImageTimingDump } from '../common/ImageTimingDump';
import { nativeTransferOwner } from '../common/NativeTransferOwner';
import { NativeTransferBackground } from '../common/NativeTransferBackground';

export default class EntryAbility extends UIAbility {
  onCreate(_want: Want): void {
    ClientDiagnostics.bind(this.context);
    const owner = nativeTransferOwner(this.context);
    owner.bindAbility(this.context);
    owner.bindBackground(new NativeTransferBackground(this.context, (_reason: string): void => owner.pauseBackground()));
    ClientDiagnostics.event('SYSTEM', 'lifecycle', 'ability-create', '');
    AppNetBinding.start();
  }
  // Foregrounding must retain the current chat, drafts and native transfers.
  onNewWant(want: Want): void {
    ClientDiagnostics.event('SYSTEM', 'lifecycle', 'want-received', '');
    const action = want.parameters?.garnetImageTiming;
    if (typeof action === 'string') { ImageTimingDump.command(action); }
  }
  onDestroy(): void { AppNetBinding.stop(); nativeTransferOwner(this.context).destroyAbility(); }
  onDump(_params: string[]): string[] { return ImageTimingDump.dump().concat(AppNetBinding.dump()); }
  onWindowStageCreate(stage: window.WindowStage): void {
    // The first connection must not race the initial binding decision: hold
    // the first page load until the first evaluation settles (bounded by the
    // gate timeout; the system default then stays in charge as before).
    void AppNetBinding.whenSettled(1500).then((): void => {
      try {
        stage.loadContent('pages/SharedChat', (error: BusinessError) => {
          ClientDiagnostics.event('SYSTEM', 'lifecycle', error.code ? 'main-page-load-failed' : 'main-page-loaded',
            error.code ? 'errorCode=' + error.code : 'page=pages/SharedChat');
        });
      } catch {
        ClientDiagnostics.event('SYSTEM', 'lifecycle', 'main-page-load-failed', 'stage-closed');
      }
    });
  }
  onWindowStageDestroy(): void { ClientDiagnostics.event('SYSTEM', 'lifecycle', 'window-stage-destroyed', ''); }
  onForeground(): void { ClientDiagnostics.event('SYSTEM', 'lifecycle', 'foreground', ''); ClientDiagnostics.foreground(); ClientDiagnostics.lifecycle('foreground'); AppNetBinding.refresh(); }
  onBackground(): void { ImageTimingDump.command('clear'); ClientDiagnostics.event('SYSTEM', 'lifecycle', 'background', ''); ClientDiagnostics.lifecycle('background'); }
}

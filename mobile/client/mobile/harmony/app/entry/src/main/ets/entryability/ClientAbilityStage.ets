import { AbilityStage } from '@kit.AbilityKit';
import { ClientDiagnostics } from '../common/ClientDiagnostics';
import { initializeSharedWeb } from '../common/SharedWebPolicy';

export default class ClientAbilityStage extends AbilityStage {
  onCreate(): void {
    ClientDiagnostics.bind(this.context);
    try { initializeSharedWeb(); }
    catch { ClientDiagnostics.event('SYSTEM', 'arkweb-capability-gate', 'web-engine-init-failed', ''); }
    ClientDiagnostics.event('SYSTEM', 'module-lifecycle', 'module-stage-create', '');
  }
}

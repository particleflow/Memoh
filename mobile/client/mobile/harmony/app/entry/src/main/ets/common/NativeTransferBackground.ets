import { common, wantAgent, WantAgent } from '@kit.AbilityKit';
import { backgroundTaskManager } from '@kit.BackgroundTasksKit';
import { notificationManager } from '@kit.NotificationKit';
import { BusinessError } from '@kit.BasicServicesKit';
import { ClientDiagnostics } from './ClientDiagnostics';

export interface TransferBackgroundControl {
  begin(key: string): boolean;
  progress(key: string, received: number, total: number): void;
  finish(key: string): void;
  reset(): void;
  destroy(): void;
  settled(): Promise<void>;
}
export interface TransferBackgroundPlatform {
  start(): Promise<backgroundTaskManager.ContinuousTaskNotification>;
  publish(lease: backgroundTaskManager.ContinuousTaskNotification, percent: number): Promise<void>;
  stop(lease: backgroundTaskManager.ContinuousTaskNotification, canceled: boolean): Promise<void>;
  onCancel(callback: (info: backgroundTaskManager.ContinuousTaskCancelInfo) => void): void;
  offCancel(callback: (info: backgroundTaskManager.ContinuousTaskCancelInfo) => void): void;
}
interface Progress { received: number; total: number; }

class SystemTransferBackground implements TransferBackgroundPlatform {
  private context: common.UIAbilityContext;
  constructor(context: common.UIAbilityContext) { this.context = context; }
  async start(): Promise<backgroundTaskManager.ContinuousTaskNotification> {
    const info: wantAgent.WantAgentInfo = {
      wants: [{ bundleName: this.context.abilityInfo.bundleName, abilityName: this.context.abilityInfo.name }],
      actionType: wantAgent.OperationType.START_ABILITY, requestCode: 0,
      wantAgentFlags: [wantAgent.WantAgentFlags.UPDATE_PRESENT_FLAG]
    };
    const target: WantAgent = await wantAgent.getWantAgent(info);
    return await backgroundTaskManager.startBackgroundRunning(this.context, ['dataTransfer'], target);
  }
  async publish(lease: backgroundTaskManager.ContinuousTaskNotification, percent: number): Promise<void> {
    const request: notificationManager.NotificationRequest = {
      id: lease.notificationId, slotType: lease.slotType,
      content: {
        notificationContentType: notificationManager.ContentType.NOTIFICATION_CONTENT_SYSTEM_LIVE_VIEW,
        systemLiveView: { typeCode: 8, title: '文件传输', text: '正在传输并校验原件' }
      },
      template: { name: 'downloadTemplate', data: { title: '文件传输', fileName: '原件', progressValue: percent } }
    };
    await notificationManager.publish(request);
  }
  async stop(lease: backgroundTaskManager.ContinuousTaskNotification, canceled: boolean): Promise<void> {
    // The API12 acquisition is the sole continuous task in this EntryAbility.
    // API26 normally supplies its task ID; a missing ID is cleaned up
    // through that same ability, never by guessing a task or another owner.
    if (!canceled && lease.continuousTaskId !== undefined) {
      await backgroundTaskManager.stopBackgroundRunning(this.context, lease.continuousTaskId);
    } else if (!canceled) { await backgroundTaskManager.stopBackgroundRunning(this.context); }
    // Serialize this after any in-flight publish. A late publish after an OS
    // cancellation cannot leave an orphan notification behind.
    try { await notificationManager.cancel(lease.notificationId); }
    catch (error) { if ((error as BusinessError).code !== 1600007) { throw error; } }
  }
  onCancel(callback: (info: backgroundTaskManager.ContinuousTaskCancelInfo) => void): void {
    backgroundTaskManager.on('continuousTaskCancel', callback);
  }
  offCancel(callback: (info: backgroundTaskManager.ContinuousTaskCancelInfo) => void): void {
    backgroundTaskManager.off('continuousTaskCancel', callback);
  }
}

// Owns only the system entitlement for existing native work. No timer, queued
// transfer, source locator, credential or file/task journal is kept here.
export class NativeTransferBackground implements TransferBackgroundControl {
  private platform: TransferBackgroundPlatform;
  private stopIO: (reason: string) => void;
  private active: Map<string, Progress | undefined> = new Map();
  private generation: number = 0;
  private lease?: backgroundTaskManager.ContinuousTaskNotification;
  private work: Promise<void> = Promise.resolve();
  private publishing: boolean = false;
  private acquiring: boolean = false;
  private canceledDuringStart: Set<number> = new Set();
  private leaseGeneration: number = 0;
  private leaseCanceled: boolean = false;
  private signature: string = '';
  private disposed: boolean = false;
  private poisoned: boolean = false;
  private subscribed: boolean = false;
  private phase: string = 'idle';
  private canceled: (info: backgroundTaskManager.ContinuousTaskCancelInfo) => void =
    (info: backgroundTaskManager.ContinuousTaskCancelInfo): void => this.systemCanceled(info);

  constructor(context: common.UIAbilityContext, stopIO: (reason: string) => void, platform?: TransferBackgroundPlatform) {
    this.platform = platform ?? new SystemTransferBackground(context); this.stopIO = stopIO;
    try { this.platform.onCancel(this.canceled); this.subscribed = true; }
    catch (error) { this.poisoned = true; this.phase = 'subscription-failed'; this.failure('subscription', error as Error); }
  }
  private failure(phase: string, error: Error): void {
    ClientDiagnostics.failure('SYSTEM', 'transfer-background', phase, error);
  }
  private enqueue(action: () => Promise<void>): void {
    this.work = this.work.then(action).catch((error: Error): void => {
      this.poisoned = true; this.phase = 'cleanup-unknown'; this.failure('cleanup', error);
      this.active.clear(); this.generation++; this.stopIO('background-cleanup-unknown');
    });
  }
  private current(generation: number): boolean {
    return !this.disposed && !this.poisoned && this.active.size > 0 && generation === this.generation;
  }
  begin(key: string): boolean {
    if (this.disposed || this.poisoned) { return false; }
    if (this.active.has(key)) { return true; }
    const first = this.active.size === 0; this.active.set(key, undefined);
    if (first) {
      const generation = ++this.generation; this.phase = 'requesting';
      this.enqueue(async (): Promise<void> => {
        if (!this.current(generation)) { return; }
        this.acquiring = true; this.canceledDuringStart.clear();
        let lease: backgroundTaskManager.ContinuousTaskNotification;
        try { lease = await this.platform.start(); }
        catch (error) {
          // A rejected start has no task ID to reconcile. Even if the old
          // operation ended meanwhile, never acquire a second uncertain grant.
          const hadWork = this.active.size > 0;
          this.phase = 'request-failed'; this.poisoned = true; this.failure('request', error as Error);
          this.active.clear(); this.generation++;
          if (hadWork) { this.stopIO('background-request-failed'); }
          return;
        } finally { this.acquiring = false; }
        const canceled = lease.continuousTaskId !== undefined && this.canceledDuringStart.has(lease.continuousTaskId);
        this.canceledDuringStart.clear();
        if (!this.current(generation) || canceled) {
          // A successful late acquisition must always be released, including
          // logout/completion while the platform's start promise was pending.
          await this.platform.stop(lease, canceled);
          if (canceled && this.current(generation)) {
            this.phase = 'system-canceled'; this.active.clear(); this.generation++; this.stopIO('background-system-canceled');
          }
          return;
        }
        this.lease = lease; this.leaseGeneration = generation; this.leaseCanceled = false; this.signature = ''; this.phase = 'running'; this.queueProgress();
      });
    }
    return true;
  }
  progress(key: string, received: number, total: number): void {
    if (!this.active.has(key) || !Number.isSafeInteger(received) || !Number.isSafeInteger(total) ||
      total <= 0 || received < 0 || received > total || this.disposed || this.poisoned) { return; }
    const prior = this.active.get(key);
    if (prior && prior.received === received && prior.total === total) { return; }
    this.active.set(key, { received: received, total: total }); this.queueProgress();
  }
  private queueProgress(): void {
    if (this.publishing || !this.lease || !this.current(this.generation)) { return; }
    this.publishing = true; const generation = this.generation;
    this.enqueue(async (): Promise<void> => {
      this.publishing = false;
      const lease = this.lease;
      if (!lease || !this.current(generation)) { return; }
      const values: number[] = []; let received = 0, total = 0;
      for (const value of this.active.values()) {
        // No guessed percentage before an engine's real checkpoint report.
        if (!value) { return; }
        values.push(value.received, value.total); received += value.received; total += value.total;
      }
      const signature = JSON.stringify(values);
      if (signature === this.signature) { return; }
      try {
        await this.platform.publish(lease, Math.floor(100 * received / total));
        this.signature = signature;
      } catch (error) {
        if (this.current(generation)) {
          this.phase = 'notification-failed'; this.failure('notification', error as Error);
          this.reset(); this.stopIO('background-notification-failed');
        }
      }
    });
  }
  finish(key: string): void {
    if (!this.active.delete(key)) { return; }
    if (this.active.size === 0) { this.reset(); }
    else { this.queueProgress(); }
  }
  reset(): void {
    this.active.clear(); this.generation++; this.signature = '';
    this.enqueue(async (): Promise<void> => {
      const lease = this.lease;
      if (lease) {
        this.lease = undefined;
        await this.platform.stop(lease, this.leaseCanceled);
      }
      if (!this.poisoned && !this.disposed && this.active.size === 0) { this.phase = 'idle'; }
    });
  }
  private systemCanceled(info: backgroundTaskManager.ContinuousTaskCancelInfo): void {
    if (this.acquiring) { this.canceledDuringStart.add(info.id); }
    if (this.disposed || !this.lease || this.lease.continuousTaskId !== info.id) { return; }
    this.leaseCanceled = true;
    if (!this.current(this.leaseGeneration)) { return; }
    this.phase = 'system-canceled';
    // Keep the lease until serialized release finishes: a publish already in
    // flight must settle before cleanup, and cannot overlap a fresh start.
    this.reset(); this.stopIO('background-system-canceled');
  }
  destroy(): void {
    if (this.disposed) { return; }
    this.disposed = true; this.reset();
    if (this.subscribed) {
      try { this.platform.offCancel(this.canceled); }
      catch (error) { this.failure('unsubscribe', error as Error); }
      this.subscribed = false;
    }
  }
  async settled(): Promise<void> {
    let pending: Promise<void>;
    do { pending = this.work; await pending; } while (pending !== this.work);
  }
  snapshot(): string { return this.phase; }
}

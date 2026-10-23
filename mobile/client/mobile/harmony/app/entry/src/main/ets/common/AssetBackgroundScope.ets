import { ClientDiagnostics } from './ClientDiagnostics';

// Lifetime of one WorkScheduler callback, not a transfer/task queue. It owns
// only pending SDK request cancellation and never files, credentials or state.
export class AssetBackgroundScope {
  private stopped: boolean = false;
  private callbacks: Set<() => void> = new Set();
  run<T>(pending: Promise<T>, cancel: () => void): Promise<T> {
    return new Promise<T>((resolve: (value: T) => void, reject: (reason: Error) => void) => {
      const abort = (): void => {
        this.callbacks.delete(abort);
        try { cancel(); }
        catch (error) { ClientDiagnostics.failure('assets', 'background', 'request-cancel-unfinished', error as Error); }
        reject(new Error('ASSET_BACKGROUND_STOPPED'));
      };
      pending.then((value: T) => { this.callbacks.delete(abort); resolve(value); },
        (error: Error) => { this.callbacks.delete(abort); reject(error); });
      if (this.stopped) { abort(); } else { this.callbacks.add(abort); }
    });
  }
  stop(): void {
    this.stopped = true;
    Array.from(this.callbacks).forEach((cancel: () => void) => cancel());
  }
}

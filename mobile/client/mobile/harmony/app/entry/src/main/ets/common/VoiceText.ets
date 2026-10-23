import { worker, MessageEvents } from '@kit.ArkTS';
import { BusinessError } from '@kit.BasicServicesKit';
import { ClientDiagnostics } from './ClientDiagnostics';
import { VOICE_PCM, SpeechRequest, SpeechReply } from './VoiceTextProtocol';
export { VOICE_PCM } from './VoiceTextProtocol';

interface SpeechPending { session: string; timer: number; resolve: (text: string) => void; reject: (error: Error) => void; }

// Authority stays on the main thread. The synchronous SDK never runs on the UI/ArkWeb thread.
export class VoiceText {
  private thread?: worker.ThreadWorker;
  private warming?: Promise<void>;
  private closing?: Promise<void>;
  private epoch: number = 0;
  private retirementFailure: string = '';
  private sequence: number = 0;
  private pending: Map<number, SpeechPending> = new Map();
  private id: string = '';
  private ready: boolean = false;
  private text: string = '';
  private failure: string = '';

  private auth(id: string): void {
    if (!id || id !== this.id || !this.ready) { throw new Error('VOICE_TEXT_SESSION_INVALID'); }
    if (this.failure) { throw new Error(this.failure); }
  }
  private rejectPending(session?: string): void {
    for (const entry of this.pending) {
      const key = entry[0], pending = entry[1];
      if (session === undefined || pending.session === session) {
        clearTimeout(pending.timer); this.pending.delete(key); pending.reject(new Error('VOICE_TEXT_CANCELLED'));
      }
    }
  }
  private getThread(): worker.ThreadWorker {
    if (this.thread) { return this.thread; }
    const thread = new worker.ThreadWorker('entry/ets/workers/VoiceRecognition.ets', { name: 'VoiceRecognition' });
    this.thread = thread;
    thread.onmessage = (event: MessageEvents) => {
      const reply = event.data as SpeechReply;
      if (reply.kind === 'diagnostic') { ClientDiagnostics.event('MEDIA', 'dictation', reply.phase ?? 'worker', (reply.detail ?? '') + '/workerAt=' + (reply.at ?? 0)); return; }
      if (this.thread !== thread) { return; }
      if (reply.session && reply.session !== this.id) { return; }
      if (reply.kind === 'partial') {
        this.text = reply.value ?? '';
        return;
      }
      if (reply.kind === 'failure') { this.failure = reply.error ?? 'VOICE_TEXT_FAILED'; return; }
      const pending = this.pending.get(reply.request);
      if (!pending) { return; }
      clearTimeout(pending.timer); this.pending.delete(reply.request);
      if (reply.error) { pending.reject(new Error(reply.error)); } else { pending.resolve(reply.value ?? ''); }
    };
    const failed = (exited: boolean = false) => {
      if (this.thread !== thread) { return; }
      this.failure = 'VOICE_TEXT_WORKER_FAILED';
      ClientDiagnostics.event('MEDIA', 'dictation', 'worker-failed', 'content=false'); this.close(exited);
    };
    thread.onAllErrors = () => failed(); thread.onmessageerror = () => failed(); thread.onexit = () => failed(true);
    return thread;
  }
  private async request(command: string, session: string, language?: string, durationMs?: number): Promise<string> {
    const epoch = this.epoch;
    if (this.retirementFailure) { throw new Error(this.retirementFailure); }
    await this.closing;
    if (this.retirementFailure) { throw new Error(this.retirementFailure); }
    if (epoch !== this.epoch || (session && session !== this.id)) { throw new Error('VOICE_TEXT_CANCELLED'); }
    const thread = this.getThread(), request = ++this.sequence;
    return new Promise<string>((resolve: (text: string) => void, reject: (error: Error) => void) => {
      const timer = setTimeout(() => { this.pending.delete(request); reject(new Error('VOICE_TEXT_TIMEOUT')); }, 35000);
      this.pending.set(request, { session, timer, resolve, reject });
      const message: SpeechRequest = { command, session, request, language, durationMs };
      try { thread.postMessage(message); } catch { clearTimeout(timer); this.pending.delete(request); reject(new Error('VOICE_TEXT_WORKER_FAILED')); }
    });
  }
  async prepare(): Promise<void> {
    if (this.warming) { return this.warming; }
    const work = this.request('prepare', '').then(() => {}); this.warming = work;
    try { await work; } catch (error) { if (this.warming === work) { this.warming = undefined; } throw error as Error; }
  }
  async start(id: string, language: string, durationMs: number): Promise<string> {
    if (!/^dictation-[a-f0-9]{32}$/.test(id)) { throw new Error('VOICE_TEXT_SESSION_INVALID'); }
    if (language !== 'zh-CN' || !Number.isInteger(durationMs) || durationMs < 20000 || durationMs > VOICE_PCM.maxRecognitionMs) { throw new Error('VOICE_TEXT_PARAMS_INVALID'); }
    if (this.id) { throw new Error('VOICE_TEXT_BUSY'); }
    this.id = id; this.ready = false; this.text = ''; this.failure = '';
    try {
      await this.request('start', id, language, durationMs);
      if (id !== this.id) { throw new Error('VOICE_TEXT_CANCELLED'); }
      this.ready = true; return id;
    } catch (error) { this.cancel(id); throw error as Error; }
  }
  write(id: string, bytes: Uint8Array): void {
    this.auth(id);
    if (bytes.length !== VOICE_PCM.blockBytes && bytes.length !== VOICE_PCM.blockBytes * 2) { throw new Error('VOICE_TEXT_AUDIO_INVALID'); }
    const message: SpeechRequest = { request: 0, command: 'write', session: id, bytes };
    this.getThread().postMessage(message);
  }
  snapshot(id: string): string { if (!id || id !== this.id || !this.ready) { return ''; } this.auth(id); return this.text; }
  async finish(id: string): Promise<string> {
    try { this.auth(id); const text = await this.request('finish', id); this.auth(id); return text; }
    finally { this.cancel(id); }
  }
  cancel(id: string): void {
    if (!id || id !== this.id) { return; }
    this.id = ''; this.ready = false; this.text = ''; this.rejectPending(id);
    // Reuse the same engine for identical parameters. FIFO worker commands keep
    // synchronous SDK cancellation before the next start, without blocking ArkWeb.
    try {
      const message: SpeechRequest = { request: 0, command: 'cancel', session: id };
      this.thread?.postMessage(message);
    } catch (error) {
      ClientDiagnostics.failure('MEDIA', 'dictation', 'cancel-dispatch-failed', error as Error);
      this.close();
    }
  }
  close(exited: boolean = false): void {
    ++this.epoch;
    const thread = this.thread; this.thread = undefined; this.warming = undefined;
    this.id = ''; this.ready = false; this.text = ''; this.rejectPending();
    if (!thread || exited) { return; }
    // CoreSpeech service shutdown must finish before creating its replacement; it may invalidate concurrent engines.
    const previous = this.closing;
    const retiring = new Promise<void>((resolve: () => void, reject: (error: Error) => void) => {
      let settled: boolean = false;
      let timeout: number = -1;
      const settle = () => { this.retirementFailure = ''; if (!settled) { settled = true; if (timeout !== -1) { clearTimeout(timeout); } resolve(); } };
      const terminate = () => { try { thread.terminate(); } catch (error) { ClientDiagnostics.failure('MEDIA', 'dictation', 'worker-terminate-failed', error as Error); if ((error as BusinessError).code === 10200004) { settle(); } } };
      timeout = setTimeout(() => { ClientDiagnostics.event('MEDIA', 'dictation', 'worker-close-timeout', 'content=false'); terminate(); if (!settled) { settled = true; this.retirementFailure = 'VOICE_TEXT_WORKER_CLOSE_TIMEOUT'; reject(new Error(this.retirementFailure)); } }, 20000);
      try {
        thread.onexit = settle;
        thread.onAllErrors = () => terminate();
        // Keep numeric-only cleanup diagnostics; main onmessage ignores all retired content and responses.
        const message: SpeechRequest = { request: 0, command: 'close', session: '' }; thread.postMessage(message);
      } catch (error) {
        ClientDiagnostics.failure('MEDIA', 'dictation', 'worker-close-failed', error as Error);
        terminate();
        // SDK 10200004 means no running thread remains, so no replacement can overlap it.
        if ((error as BusinessError).code === 10200004) { settle(); }
      }
    });
    const closing = Promise.all([previous, retiring]).then(() => {}); this.closing = closing;
    const clear = () => { if (this.closing === closing) { this.closing = undefined; } };
    void closing.then(clear, clear);
  }
}

import { speechRecognizer } from '@kit.CoreSpeechKit';
import { worker, MessageEvents } from '@kit.ArkTS';
import { BusinessError } from '@kit.BasicServicesKit';
import { VOICE_PCM, SpeechRequest, SpeechReply } from '../common/VoiceTextProtocol';

const port = worker.workerPort;
class WorkerDiagnostics {
  static event(_area: string, _component: string, phase: string, detail: string): void {
    const reply: SpeechReply = { request: 0, kind: 'diagnostic', session: '', phase, detail, at: Date.now() }; port.postMessage(reply);
  }
  static failure(area: string, component: string, phase: string, error: Error, detail: string = ''): void {
    const code = (error as BusinessError).code;
    const reason = /^VOICE_TEXT_[A-Z0-9_]+$/.test(error.message) ? error.message : 'SDK_ERROR';
    WorkerDiagnostics.event(area, component, phase, detail + '/code=' + (Number.isInteger(code) ? code : 0) + '/reason=' + reason);
  }
}

// Local keyboard-style dictation only. This does not create a chat event or invoke the Agent.
class SpeechEngine {
  private engine?: speechRecognizer.SpeechRecognitionEngine;
  private engineEpoch: number = 0;
  private epoch: number = 0;
  private id: string = '';
  private listeningId: string = '';
  private completed: string = '';
  private current: string = '';
  private ready: boolean = false;
  private result?: Promise<string>;
  private resolve?: (text: string) => void;
  private reject?: (error: Error) => void;
  private audioBlocks: number = 0;
  private audioBytes: number = 0;
  private startedAt: number = 0;
  private lastWriteAt: number = 0;
  private shortestWriteGap: number = -1;
  private longestWriteGap: number = 0;
  private creating?: Promise<speechRecognizer.SpeechRecognitionEngine>;
  private rejectStart?: (error: Error) => void;

  private auth(id: string): void {
    if (!id || id !== this.id || !this.engine || !this.ready) { throw new Error('VOICE_TEXT_SESSION_INVALID'); }
  }

  private async bounded<T>(work: Promise<T>, milliseconds: number): Promise<T> {
    let timer: number = -1;
    try {
      return await Promise.race([work, new Promise<T>((_resolve: (value: T) => void, reject: (error: Error) => void) => {
        timer = setTimeout(() => { reject(new Error('VOICE_TEXT_TIMEOUT')); }, milliseconds);
      })]);
    } finally { if (timer !== -1) { clearTimeout(timer); } }
  }

  private discardInvalidEngine(code: number): void {
    // Official errors 7/8 require a new instance; ordinary sessions keep their cached engine.
    if ((code !== 1002200007 && code !== 1002200008) || !this.engine) { return; }
    const engine = this.engine; this.engine = undefined; ++this.engineEpoch;
    this.listeningId = ''; this.ready = false;
    try { engine.shutdown(); }
    catch (error) { WorkerDiagnostics.failure('MEDIA', 'dictation', 'invalid-engine-cleanup-failed', error as Error); }
    WorkerDiagnostics.event('MEDIA', 'dictation', 'invalid-engine-discarded', 'code=' + code);
  }

  async prepare(): Promise<speechRecognizer.SpeechRecognitionEngine> {
    if (this.engine) { return this.engine; }
    if (this.creating) { return this.creating; }
    const epoch = this.engineEpoch;
    let abandoned = false;
    const creation = speechRecognizer.createEngine({ language: 'zh-CN', online: 1,
      extraParams: { locate: 'CN', recognizerMode: 'long' } }).then((engine: speechRecognizer.SpeechRecognitionEngine) => {
      if (abandoned || epoch !== this.engineEpoch) { engine.shutdown(); throw new Error('VOICE_TEXT_CANCELLED'); }
      return engine;
    });
    const work = this.bounded(creation, 15000);
    this.creating = work;
    try {
      const engine = await work;
      if (epoch !== this.engineEpoch) { engine.shutdown(); throw new Error('VOICE_TEXT_CANCELLED'); }
      this.engine = engine; return engine;
    } catch (error) { abandoned = true; throw error as Error; }
    finally { if (this.creating === work) { this.creating = undefined; } }
  }

  async start(id: string, language: string, durationMs: number): Promise<string> {
    if (typeof id !== 'string' || !/^dictation-[a-f0-9]{32}$/.test(id)) { throw new Error('VOICE_TEXT_SESSION_INVALID'); }
    // CoreSpeechKit currently supports zh-CN only; this is the SDK contract, not the UI language.
    if (language !== 'zh-CN' || !Number.isInteger(durationMs) || durationMs < 20000 || durationMs > VOICE_PCM.maxRecognitionMs) {
      throw new Error('VOICE_TEXT_PARAMS_INVALID');
    }
    if (this.id) { throw new Error('VOICE_TEXT_BUSY'); }
    const epoch = this.epoch;
    this.id = id;
    const cancelled = new Promise<speechRecognizer.SpeechRecognitionEngine>((_resolve, reject: (error: Error) => void) => { this.rejectStart = reject; });
    let phase: string = 'create';
    try {
      const engine = await Promise.race([this.prepare(), cancelled]);
      if (epoch !== this.epoch) { throw new Error('VOICE_TEXT_CANCELLED'); }
      phase = 'idle';
      if (engine.isBusy()) { throw new Error('VOICE_TEXT_BUSY'); }
      this.engine = engine; this.completed = ''; this.current = '';
      this.audioBlocks = 0; this.audioBytes = 0; this.startedAt = 0;
      this.lastWriteAt = 0; this.shortestWriteGap = -1; this.longestWriteGap = 0;
      this.result = new Promise<string>((resolve: (text: string) => void, reject: (error: Error) => void) => {
        this.resolve = resolve; this.reject = reject;
      });
      this.result.catch(() => {});
      const started = new Promise<void>((resolve: () => void, reject: (error: Error) => void) => {
        engine.setListener({
          onStart: (session: string, _message: string) => { if (session === this.id && engine === this.engine) { this.ready = true; this.startedAt = Date.now(); resolve(); } },
          onEvent: (session: string, code: number, _message: string) => {
            if (session === this.id && engine === this.engine) { WorkerDiagnostics.event('MEDIA', 'dictation', 'sdk-event', 'code=' + code); }
          },
          onResult: (session: string, result: speechRecognizer.SpeechRecognitionResult) => {
            if (session !== this.id || engine !== this.engine) { return; }
            this.current = result.result;
            if (result.isFinal) { this.completed += this.current; this.current = ''; }
            const partial: SpeechReply = { request: 0, kind: 'partial', session, value: this.completed + this.current }; port.postMessage(partial);
            WorkerDiagnostics.event('MEDIA', 'dictation', 'sdk-result', 'characters=' + (this.completed.length + this.current.length) + '/final=' + result.isFinal + '/last=' + result.isLast);
            if (result.isFinal && result.isLast) { this.resolve?.(this.completed + this.current); }
          },
          onComplete: (session: string, _message: string) => {
            if (session === this.id && engine === this.engine) {
              this.listeningId = '';
              WorkerDiagnostics.event('MEDIA', 'dictation', 'sdk-complete', 'characters=' + (this.completed.length + this.current.length));
              this.resolve?.(this.completed + this.current);
            }
          },
          onError: (session: string, code: number, _message: string) => {
            if (session !== this.id || engine !== this.engine) { return; }
            WorkerDiagnostics.event('MEDIA', 'dictation', 'failed', 'code=' + code);
            const error = new Error('VOICE_TEXT_ERROR_' + code); reject(error); this.reject?.(error);
            this.discardInvalidEngine(code);
          }
        });
        this.listeningId = id;
        engine.startListening({ sessionId: id, audioInfo: { audioType: 'pcm', sampleRate: 16000, soundChannel: 1, sampleBit: 16 },
          extraParams: { recognitionMode: 1, maxAudioDuration: durationMs } });
      });
      phase = 'start';
      await this.bounded(Promise.race([started, cancelled.then(() => {})]), 15000); this.rejectStart = undefined; this.auth(id);
      WorkerDiagnostics.event('MEDIA', 'dictation', 'started', 'online=1/same-source-pcm=true');
      return id;
    } catch (error) {
      this.discardInvalidEngine((error as BusinessError).code);
      WorkerDiagnostics.failure('MEDIA', 'dictation', phase + '-failed', error as Error);
      if (epoch === this.epoch) { this.cancel(id); } throw error as Error;
    }
  }

  write(id: string, bytes: Uint8Array): void {
    try {
      this.auth(id);
      if (bytes.length !== VOICE_PCM.blockBytes && bytes.length !== VOICE_PCM.blockBytes * 2) { throw new Error('VOICE_TEXT_AUDIO_INVALID'); }
      this.engine?.writeAudio(id, bytes);
      const at = Date.now();
      if (this.lastWriteAt) {
        const gap = at - this.lastWriteAt;
        this.shortestWriteGap = this.shortestWriteGap >= 0 ? Math.min(this.shortestWriteGap, gap) : gap;
        this.longestWriteGap = Math.max(this.longestWriteGap, gap);
      }
      this.lastWriteAt = at;
      this.audioBlocks += 1; this.audioBytes += bytes.length;
    } catch (error) {
      this.discardInvalidEngine((error as BusinessError).code);
      WorkerDiagnostics.failure('MEDIA', 'dictation', 'write-failed', error as Error, 'blocks=' + this.audioBlocks + '/bytes=' + this.audioBytes);
      throw error as Error;
    }
  }

  snapshot(id: string): string {
    if (!id || id !== this.id || !this.ready) { return ''; }
    this.auth(id); return this.completed + this.current;
  }

  async finish(id: string): Promise<string> {
    try {
      this.auth(id);
      WorkerDiagnostics.event('MEDIA', 'dictation', 'finish-requested', 'blocks=' + this.audioBlocks + '/bytes=' + this.audioBytes + '/elapsedMs=' + (Date.now() - this.startedAt) + '/minWriteGapMs=' + this.shortestWriteGap + '/maxWriteGapMs=' + this.longestWriteGap);
      const finishAt = Date.now();
      this.engine?.finish(id);
      WorkerDiagnostics.event('MEDIA', 'dictation', 'finish-returned', 'elapsedMs=' + (Date.now() - finishAt));
      const text = await this.bounded(this.result as Promise<string>, 20000);
      this.auth(id);
      // Completion with no words is valid silence, not a failed audio message.
      WorkerDiagnostics.event('MEDIA', 'dictation', 'completed', 'characters=' + text.length);
      return text;
    } catch (error) {
      this.discardInvalidEngine((error as BusinessError).code);
      WorkerDiagnostics.failure('MEDIA', 'dictation', 'finish-failed', error as Error, 'characters=' + (this.completed.length + this.current.length));
      throw error as Error;
    }
    // Main authority clears the completed session; the cached engine stays for the next recording.
  }

  cancel(id: string): void {
    if (id !== this.id || !id) { return; }
    const engine = this.engine, listening = this.listeningId === id; ++this.epoch;
    if (listening) { this.listeningId = ''; }
    this.id = ''; this.ready = false;
    this.rejectStart?.(new Error('VOICE_TEXT_CANCELLED')); this.rejectStart = undefined;
    this.reject?.(new Error('VOICE_TEXT_CANCELLED')); this.resolve = undefined; this.reject = undefined; this.result = undefined;
    const cancelAt = Date.now();
    try { if (listening && engine?.isBusy()) { engine.cancel(id); } }
    catch (error) { this.discardInvalidEngine((error as BusinessError).code); WorkerDiagnostics.failure('MEDIA', 'dictation', 'cancel-cleanup-failed', error as Error); }
    WorkerDiagnostics.event('MEDIA', 'dictation', 'sdk-cancelled', 'worker=true/elapsedMs=' + (Date.now() - cancelAt));
  }

  close(): void {
    ++this.epoch; ++this.engineEpoch;
    const engine = this.engine, id = this.listeningId;
    this.engine = undefined; this.creating = undefined; this.id = ''; this.listeningId = ''; this.ready = false;
    this.rejectStart?.(new Error('VOICE_TEXT_CANCELLED')); this.rejectStart = undefined;
    this.reject?.(new Error('VOICE_TEXT_CANCELLED')); this.resolve = undefined; this.reject = undefined; this.result = undefined;
    const cancelAt = Date.now();
    try { if (engine && id && engine.isBusy()) { engine.cancel(id); } }
    catch (error) { this.discardInvalidEngine((error as BusinessError).code); WorkerDiagnostics.failure('MEDIA', 'dictation', 'cancel-cleanup-failed', error as Error); }
    WorkerDiagnostics.event('MEDIA', 'dictation', 'worker-cancel-returned', 'elapsedMs=' + (Date.now() - cancelAt));
    const shutdownAt = Date.now();
    try { engine?.shutdown(); }
    catch (error) { WorkerDiagnostics.failure('MEDIA', 'dictation', 'shutdown-cleanup-failed', error as Error); }
    WorkerDiagnostics.event('MEDIA', 'dictation', 'worker-shutdown-returned', 'elapsedMs=' + (Date.now() - shutdownAt));
  }
}

const engine = new SpeechEngine();
port.onmessage = (event: MessageEvents): void => {
  const message = event.data as SpeechRequest;
  const work = async (): Promise<string> => {
    switch (message.command) {
      case 'prepare': await engine.prepare(); return '';
      case 'start': return engine.start(message.session, message.language ?? '', message.durationMs ?? 0);
      case 'write': engine.write(message.session, message.bytes ?? new Uint8Array(0)); return '';
      case 'finish': return engine.finish(message.session);
      case 'cancel': engine.cancel(message.session); return '';
      case 'close': engine.close(); port.close(); return '';
      default: throw new Error('VOICE_TEXT_PARAMS_INVALID');
    }
  };
  void work().then((value: string) => {
    if (message.request) { const reply: SpeechReply = { request: message.request, kind: 'response', session: message.session, value }; port.postMessage(reply); }
  }).catch((error: Error) => {
    WorkerDiagnostics.failure('MEDIA', 'dictation', message.command + '-failed', error);
    const reason = /^VOICE_TEXT_[A-Z0-9_]+$/.test(error.message) ? error.message : 'VOICE_TEXT_FAILED';
    const reply: SpeechReply = { request: message.request, kind: message.request ? 'response' : 'failure', session: message.session, error: reason };
    port.postMessage(reply);
  });
};

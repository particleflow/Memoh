import { common, abilityAccessCtrl } from '@kit.AbilityKit';
import { audio } from '@kit.AudioKit';
import { fileIo as fs } from '@kit.CoreFileKit';
import { util } from '@kit.ArkTS';
import { VoiceText, VOICE_PCM } from './VoiceText';
import { ClientDiagnostics } from './ClientDiagnostics';

export interface CaptureStatus {
  handle: string; state: string; durationMs: number; peaks: number[]; text: string; speechError: string;
}
export interface CaptureResult extends CaptureStatus { bytes: number; audible: boolean; }

// Device-owned foreground capture. One copied PCM stream feeds the WAV and the native recognizer.
// This object has no network, Bot credentials, asset service or room identity.
export class NativeVoice {
  private context: common.UIAbilityContext;
  private speech: VoiceText = new VoiceText();
  private capturer?: audio.AudioCapturer;
  private file?: fs.File;
  private handle: string = '';
  private state: string = 'idle';
  private starting?: Promise<CaptureStatus>;
  private speechStart?: Promise<string>;
  private speechReady: boolean = false;
  private speechError: string = '';
  private pending: Uint8Array = new Uint8Array(0);
  private pcmBytes: number = 0;
  private audible: boolean = false;
  private peaks: number[] = [];
  private peak: number = 0;
  private samples: number = 0;
  private timer: number = -1;
  private epoch: number = 0;
  private originals: Map<string, string> = new Map();
  constructor(context: common.UIAbilityContext) { this.context = context; }
  private path(handle: string): string { return this.context.cacheDir + '/memoh-voice-' + handle + '.wav'; }
  private assertHandle(handle: string): void {
    if (!/^[a-f0-9]{32}$/.test(handle) || handle !== this.handle) { throw new Error('VOICE_SESSION_INVALID'); }
  }
  prepare(): void { void this.speech.prepare().catch(() => {}); }
  async start(handle: string): Promise<CaptureStatus> {
    if (this.state !== 'idle' || !/^[a-f0-9]{32}$/.test(handle)) { throw new Error('VOICE_BUSY'); }
    this.state = 'starting'; this.handle = handle;
    const work = this.begin(handle, this.epoch); this.starting = work;
    try { return await work; } finally { this.starting = undefined; }
  }
  private async begin(handle: string, epoch: number): Promise<CaptureStatus> {
    try {
      const manager = abilityAccessCtrl.createAtManager();
      if (await manager.checkAccessToken(this.context.applicationInfo.accessTokenId, 'ohos.permission.MICROPHONE') !== abilityAccessCtrl.GrantStatus.PERMISSION_GRANTED) {
        const permission = await manager.requestPermissionsFromUser(this.context, ['ohos.permission.MICROPHONE']);
        if (permission.authResults[0] !== 0) { throw new Error('VOICE_MICROPHONE_DENIED'); }
        // The system dialog owns this press. A fresh foreground press starts capture.
        throw new Error('VOICE_MICROPHONE_RETRY');
      }
      if (epoch !== this.epoch) { throw new Error('VOICE_CANCELLED'); }
      this.pcmBytes = 0; this.audible = false; this.peaks = []; this.peak = 0; this.samples = 0;
      this.pending = new Uint8Array(0); this.speechError = ''; this.speechReady = false;
      this.file = fs.openSync(this.path(handle), fs.OpenMode.CREATE | fs.OpenMode.TRUNC | fs.OpenMode.READ_WRITE);
      this.write(new ArrayBuffer(44), 0);
      this.speechStart = this.speech.start('dictation-' + handle, 'zh-CN', VOICE_PCM.maxRecognitionMs);
      void this.speechStart.then(() => { if (handle === this.handle && epoch === this.epoch) { this.speechReady = true; } })
        .catch((error: Error) => { if (handle === this.handle) { this.speechError = error.message; this.pending = new Uint8Array(0); } });
      this.capturer = await audio.createAudioCapturer({ streamInfo: { samplingRate: audio.AudioSamplingRate.SAMPLE_RATE_16000,
        channels: audio.AudioChannel.CHANNEL_1, sampleFormat: audio.AudioSampleFormat.SAMPLE_FORMAT_S16LE,
        encodingType: audio.AudioEncodingType.ENCODING_TYPE_RAW }, capturerInfo: { source: audio.SourceType.SOURCE_TYPE_MIC, capturerFlags: 0 } });
      if (epoch !== this.epoch) { throw new Error('VOICE_CANCELLED'); }
      this.capturer.on('readData', (buffer: ArrayBuffer) => {
        if (epoch !== this.epoch || handle !== this.handle || !this.file) { return; }
        try { this.accept(buffer); } catch (error) {
          ClientDiagnostics.failure('MEDIA', 'voice', 'capture-failed', error as Error);
          setTimeout(() => { void this.cancel(handle); }, 0);
        }
      });
      this.capturer.on('audioInterrupt', (event: audio.InterruptEvent) => {
        if (event.eventType === audio.InterruptType.INTERRUPT_TYPE_BEGIN) { void this.cancel(handle); }
      });
      await this.capturer.start();
      if (epoch !== this.epoch) { throw new Error('VOICE_CANCELLED'); }
      this.state = 'recording'; this.feed();
      ClientDiagnostics.event('MEDIA', 'voice', 'recording-started', 'sameSourcePcm=true/handle=' + handle);
      return this.status(handle);
    } catch (error) { ClientDiagnostics.failure('MEDIA', 'voice', 'start-failed', error as Error); await this.dispose(true); this.state = 'idle'; this.handle = ''; throw error as Error; }
  }
  private write(bytes: ArrayBuffer, offset: number): void {
    if (!this.file) { throw new Error('VOICE_FILE_UNAVAILABLE'); }
    let at = 0;
    while (at < bytes.byteLength) {
      const count = fs.writeSync(this.file.fd, bytes.slice(at), { offset: offset + at });
      if (count <= 0) { throw new Error('VOICE_SHORT_WRITE'); } at += count;
    }
  }
  private accept(buffer: ArrayBuffer): void {
    if (buffer.byteLength % 2) { throw new Error('VOICE_PCM_INVALID'); }
    const bytes = new Uint8Array(buffer.slice(0));
    this.write(bytes.buffer as ArrayBuffer, 44 + this.pcmBytes); this.pcmBytes += bytes.length;
    const view = new DataView(bytes.buffer);
    for (let at = 0; at < bytes.length; at += 2) {
      const value = Math.abs(view.getInt16(at, true)); this.audible = this.audible || value !== 0;
      this.peak = Math.max(this.peak, value); ++this.samples;
      if (this.samples === VOICE_PCM.meterSamples) {
        this.peaks.push(this.peak / 32768); this.peak = 0; this.samples = 0;
        if (this.peaks.length >= 2048) { this.peaks = this.peaks.filter((_v: number, i: number) => i % 2 === 0); }
      }
    }
    if (!this.speechError) {
      const joined = new Uint8Array(this.pending.length + bytes.length);
      joined.set(this.pending); joined.set(bytes, this.pending.length); this.pending = joined;
    }
  }
  private feed(): void {
    let interval = VOICE_PCM.blockMs;
    if (this.speechReady && !this.speechError && this.pending.length >= VOICE_PCM.blockBytes) {
      try {
        const count = this.pending.length >= VOICE_PCM.blockBytes * 2 ? VOICE_PCM.blockBytes * 2 : VOICE_PCM.blockBytes;
        this.speech.write('dictation-' + this.handle, this.pending.slice(0, count)); this.pending = this.pending.slice(count);
        interval = VOICE_PCM.blockMs * count / VOICE_PCM.blockBytes;
      } catch (error) { this.speechError = (error as Error).message; this.pending = new Uint8Array(0); }
    }
    this.timer = setTimeout(() => { this.timer = -1; this.feed(); }, interval);
  }
  status(handle: string): CaptureStatus {
    this.assertHandle(handle);
    let text = '';
    if (this.state === 'recording' && this.speechReady && !this.speechError && this.audible) {
      try { text = this.speech.snapshot('dictation-' + handle); }
      catch (error) { this.speechError = (error as Error).message; }
    }
    return { handle, state: this.state, durationMs: this.pcmBytes / 32, peaks: this.peaks.concat(this.samples ? [this.peak / 32768] : []),
      text, speechError: this.speechError };
  }
  async finish(handle: string, transcribe: boolean): Promise<CaptureResult> {
    this.assertHandle(handle); await this.starting; this.assertHandle(handle);
    if (this.state !== 'recording') { throw new Error('VOICE_NOT_RECORDING'); }
    this.state = 'finishing';
    try {
      await this.capturer?.stop();
      let text = '';
      if (transcribe && this.audible) {
        await this.speechStart;
        if (this.speechError) { throw new Error(this.speechError); }
        if (this.pending.length % VOICE_PCM.blockBytes) {
          const padded = new Uint8Array(Math.ceil(this.pending.length / VOICE_PCM.blockBytes) * VOICE_PCM.blockBytes); padded.set(this.pending); this.pending = padded;
        }
        while (this.pending.length && !this.speechError) { await new Promise<void>((resolve: () => void) => { setTimeout(resolve, 20); }); }
        if (this.speechError) { throw new Error(this.speechError); }
        text = await this.speech.finish('dictation-' + handle);
      }
      const header = new ArrayBuffer(44), view = new DataView(header);
      const tag = (offset: number, value: string) => { for (let i = 0; i < value.length; ++i) { view.setUint8(offset + i, value.charCodeAt(i)); } };
      tag(0, 'RIFF'); view.setUint32(4, this.pcmBytes + 36, true); tag(8, 'WAVE'); tag(12, 'fmt '); view.setUint32(16, 16, true);
      view.setUint16(20, 1, true); view.setUint16(22, 1, true); view.setUint32(24, 16000, true); view.setUint32(28, 32000, true);
      view.setUint16(32, 2, true); view.setUint16(34, 16, true); tag(36, 'data'); view.setUint32(40, this.pcmBytes, true);
      this.write(header, 0); if (this.file) { fs.fsyncSync(this.file.fd); }
      const status = this.status(handle);
      const result: CaptureResult = { handle, state: 'complete', durationMs: status.durationMs, peaks: status.peaks, text,
        speechError: this.speechError, bytes: this.pcmBytes + 44, audible: this.audible };
      ClientDiagnostics.event('MEDIA', 'voice', 'capture-finalized', 'handle=' + handle + '/pcmBytes=' + this.pcmBytes + '/durationMs=' + result.durationMs + '/peaks=' + result.peaks.length + '/transcribe=' + transcribe + '/characters=' + text.length + '/audible=' + this.audible);
      if (!transcribe && this.audible) { this.originals.set(handle, this.path(handle)); }
      await this.dispose(transcribe || !this.audible); return result;
    } catch (error) { ClientDiagnostics.failure('MEDIA', 'voice', 'finish-failed', error as Error); await this.dispose(true); throw error as Error; }
    finally { this.state = 'idle'; this.handle = ''; }
  }
  read(handle: string, offset: number): string {
    const path = this.originals.get(handle);
    if (!path || !Number.isInteger(offset) || offset < 0) { throw new Error('VOICE_ORIGINAL_UNAVAILABLE'); }
    const file = fs.openSync(path, fs.OpenMode.READ_ONLY);
    try {
      const size = fs.statSync(file.fd).size; if (offset > size) { throw new Error('VOICE_OFFSET_INVALID'); }
      const buffer = new ArrayBuffer(Math.min(65536, size - offset));
      const count = buffer.byteLength ? fs.readSync(file.fd, buffer, { offset }) : 0;
      return new util.Base64Helper().encodeToStringSync(new Uint8Array(buffer.slice(0, count)));
    } finally { fs.closeSync(file); }
  }
  release(handle: string): void {
    const path = this.originals.get(handle); if (!path) { return; }
    fs.unlinkSync(path); this.originals.delete(handle);
  }
  async cancel(handle: string): Promise<void> {
    this.assertHandle(handle); ++this.epoch;
    await this.starting?.catch(() => {});
    await this.dispose(true); this.handle = ''; this.state = 'idle';
  }
  private async dispose(discard: boolean): Promise<void> {
    if (this.timer !== -1) { clearTimeout(this.timer); this.timer = -1; }
    this.speech.cancel('dictation-' + this.handle); this.speechReady = false; this.pending = new Uint8Array(0);
    const capturer = this.capturer; this.capturer = undefined;
    if (capturer) {
      try { if (capturer.state === audio.AudioState.STATE_RUNNING) { await capturer.stop(); } }
      finally { capturer.off('readData'); capturer.off('audioInterrupt'); await capturer.release(); }
    }
    if (this.file) { fs.closeSync(this.file); this.file = undefined; }
    if (discard && this.handle && fs.accessSync(this.path(this.handle))) { fs.unlinkSync(this.path(this.handle)); }
  }
  close(): void {
    if (this.handle) { void this.cancel(this.handle).catch(() => {}); }
    this.speech.close();
    for (const handle of this.originals.keys()) { try { this.release(handle); } catch { /* Private cache remains owned until next cleanup. */ } }
  }
}

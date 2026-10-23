// One PCM format and private worker contract. No credentials or chat state cross this boundary.
interface VoicePcmContract { rate: number; channels: number; bits: number; blockBytes: number; blockMs: number; meterSamples: number; maxRecognitionMs: number; }
export const VOICE_PCM: VoicePcmContract = { rate: 16000, channels: 1, bits: 16, blockBytes: 640, blockMs: 20,
  meterSamples: 3200, maxRecognitionMs: 28800000 };
export interface SpeechRequest { request: number; command: string; session: string; language?: string; durationMs?: number; bytes?: Uint8Array; }
export interface SpeechReply { request: number; kind: string; session: string; value?: string; error?: string; phase?: string; detail?: string; at?: number; }

export interface Capture {
  handle: string
  state: string
  durationMs: number
  peaks: number[]
  text: string
  speechError: string
}
export interface FinishedCapture extends Capture { bytes: number; audible: boolean }
export interface Recording { file: File; durationMs: number; peaks: number[] }
export interface VoiceSubmission { capture: Capture; recording: Promise<Recording | undefined> }
export const nativeVoiceAvailable = typeof window !== 'undefined' && !!(window as Window & {
  begoniaNative?: unknown
}).begoniaNative

export async function nativeVoice<T>(command: string, handle: string, offset?: number): Promise<T> {
  const host = window as Window & { begoniaNative?: { request: (request: string) => Promise<string> } }
  if (!host.begoniaNative) throw new Error('此设备暂不支持原生录音')
  const reply = JSON.parse(await host.begoniaNative.request(JSON.stringify({ version: 1, command, handle, offset })))
  if (!reply.ok) throw new Error(reply.error === 'VOICE_MICROPHONE_RETRY'
    ? '麦克风已允许，请重新按住录音'
    : command === 'voice.transcribe' ? '转字未完成，请重新录音' : '录音未完成，请检查麦克风权限后重试')
  return reply.data as T
}

export async function finishRecording(handle: string): Promise<Recording | undefined> {
  const result = await nativeVoice<FinishedCapture>('voice.stop', handle)
  try {
    if (!result.audible || !result.durationMs) return
    const chunks: ArrayBuffer[] = []
    let offset = 0
    while (offset < result.bytes) {
      const encoded = await nativeVoice<string>('voice.read', handle, offset)
      const bytes = Uint8Array.from(atob(encoded), char => char.charCodeAt(0))
      if (!bytes.length || offset + bytes.length > result.bytes) throw new Error('录音原件读取未完成')
      chunks.push(bytes.buffer as ArrayBuffer)
      offset += bytes.length
    }
    const file = new File(chunks, '录音-' + handle + '.wav', { type: 'audio/wav' })
    if (file.size !== result.bytes) throw new Error('录音原件大小不一致')
    return { file, durationMs: result.durationMs, peaks: result.peaks }
  } finally {
    await nativeVoice('voice.release', handle)
  }
}

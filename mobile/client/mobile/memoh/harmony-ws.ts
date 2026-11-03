// Harmony WebSocket bridge overlay. Inside the Harmony shell the page origin
// (begonia-client://app) is cross-origin to the service, and ArkWeb's own
// socket path shares the engine's default-network DNS view — the exact N1
// failure. When the native bridge is present this replaces window.WebSocket
// with a shim that runs each socket on the native netstack (which follows the
// app's bound network) through the existing begoniaNative command channel;
// socket events come back on the existing garnet CustomEvent channel. Browser
// and desktop builds never load this module, so the plain browser WebSocket
// path stays untouched.

type Bridge = { request: (raw: string) => Promise<string> }

interface WsNativeEventDetail {
  id: string
  kind: 'open' | 'message' | 'close' | 'error'
  data?: string
  binary?: boolean
  code?: number
}

interface WsCommandOptions {
  id?: string
  url?: string
  authorization?: string
  data?: string
  binary?: boolean
}

const nativeBridge = (): Bridge | undefined => (window as Window & { begoniaNative?: Bridge }).begoniaNative

async function nativeWsCommand(command: string, options: WsCommandOptions): Promise<void> {
  const native = nativeBridge()
  if (!native) throw new Error('SERVICE_BRIDGE_UNAVAILABLE')
  const result = JSON.parse(await native.request(JSON.stringify({ version: 1, command, ...options }))) as { ok?: boolean }
  if (!result.ok) throw new Error('SERVICE_BRIDGE_REJECTED')
}

function encodeBase64(bytes: Uint8Array): string {
  let binary = ''
  const chunkSize = 0x8000
  for (let offset = 0; offset < bytes.length; offset += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + chunkSize))
  }
  return btoa(binary)
}

function decodeBase64(text: string): Uint8Array {
  const binary = atob(text)
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index)
  return bytes
}

let socketSequence = 0
const liveSockets = new Map<string, NativeWebSocket>()

// Only the interface subset the chat and terminal call sites use; the full
// WebSocket shape exists so the unchanged call sites stay type-compatible.
// Subprotocols are not forwarded: neither call site negotiates one.
class NativeWebSocket extends EventTarget implements WebSocket {
  static readonly CONNECTING = 0 as const
  static readonly OPEN = 1 as const
  static readonly CLOSING = 2 as const
  static readonly CLOSED = 3 as const
  readonly CONNECTING = 0 as const
  readonly OPEN = 1 as const
  readonly CLOSING = 2 as const
  readonly CLOSED = 3 as const
  readonly url: string
  readonly extensions = ''
  readonly protocol = ''
  bufferedAmount = 0
  binaryType: BinaryType = 'blob'
  onopen: ((this: WebSocket, event: Event) => unknown) | null = null
  onmessage: ((this: WebSocket, event: MessageEvent) => unknown) | null = null
  onerror: ((this: WebSocket, event: Event) => unknown) | null = null
  onclose: ((this: WebSocket, event: CloseEvent) => unknown) | null = null
  readyState: 0 | 1 | 2 | 3 = NativeWebSocket.CONNECTING
  private readonly id: string
  private finished = false

  constructor(url: string | URL, _protocols?: string | string[]) {
    super()
    this.url = String(url)
    this.id = `ws-${Date.now().toString(36)}-${(socketSequence++).toString(36)}-${Math.random().toString(36).slice(2, 10)}`
    liveSockets.set(this.id, this)
    // The service socket URL carries the same single token query the SDK
    // already uses for element loads; no second credential store is created.
    const token = new URL(this.url).searchParams.get('token') ?? ''
    if (!token) { this.fail(); return }
    void nativeWsCommand('ws.open', { id: this.id, url: this.url, authorization: `Bearer ${token}` })
      .catch(() => { this.fail() })
  }

  send(data: string | ArrayBufferLike | Blob | ArrayBufferView): void {
    if (this.readyState !== NativeWebSocket.OPEN) {
      throw new DOMException('WebSocket is not open', 'InvalidStateError')
    }
    if (typeof data === 'string') {
      void nativeWsCommand('ws.send', { id: this.id, data, binary: false }).catch(() => { this.fail() })
      return
    }
    if (data instanceof Blob) {
      void data.arrayBuffer().then(buffer => { this.sendBytes(new Uint8Array(buffer)) })
      return
    }
    if (data instanceof ArrayBuffer) {
      this.sendBytes(new Uint8Array(data))
      return
    }
    const view = data as ArrayBufferView
    this.sendBytes(new Uint8Array(view.buffer as ArrayBuffer, view.byteOffset, view.byteLength))
  }

  close(_code?: number, _reason?: string): void {
    if (this.readyState === NativeWebSocket.CLOSING || this.readyState === NativeWebSocket.CLOSED) return
    this.readyState = NativeWebSocket.CLOSING
    void nativeWsCommand('ws.close', { id: this.id }).catch(() => { this.fail() })
  }

  private sendBytes(bytes: Uint8Array): void {
    void nativeWsCommand('ws.send', { id: this.id, data: encodeBase64(bytes), binary: true }).catch(() => { this.fail() })
  }

  private fail(): void {
    if (this.finished) return
    this.finished = true
    this.setClosed()
    const errorEvent = new Event('error')
    this.onerror?.call(this as unknown as WebSocket, errorEvent)
    this.dispatchEvent(errorEvent)
    const closeEvent = new CloseEvent('close', { code: 1006 })
    this.onclose?.call(this as unknown as WebSocket, closeEvent)
    this.dispatchEvent(closeEvent)
  }

  private setClosed(): void {
    this.readyState = NativeWebSocket.CLOSED
    if (liveSockets.get(this.id) === this) liveSockets.delete(this.id)
  }

  // Bridge-internal: called by the module-level garnet:ws listener.
  handle(detail: WsNativeEventDetail): void {
    if (this.finished) return
    if (detail.kind === 'open') {
      this.readyState = NativeWebSocket.OPEN
      const event = new Event('open')
      this.onopen?.call(this as unknown as WebSocket, event)
      this.dispatchEvent(event)
      return
    }
    if (detail.kind === 'message') {
      // Binary frames are always delivered as ArrayBuffer; the chat socket
      // ignores non-string data and the terminal sets binaryType accordingly.
      const data: string | ArrayBuffer = detail.binary
        ? decodeBase64(detail.data ?? '').buffer as ArrayBuffer
        : detail.data ?? ''
      const event = new MessageEvent('message', { data })
      this.onmessage?.call(this as unknown as WebSocket, event)
      this.dispatchEvent(event)
      return
    }
    if (detail.kind === 'error') {
      const event = new Event('error')
      this.onerror?.call(this as unknown as WebSocket, event)
      this.dispatchEvent(event)
      return
    }
    this.finished = true
    this.setClosed()
    const event = new CloseEvent('close', { code: detail.code ?? 1005 })
    this.onclose?.call(this as unknown as WebSocket, event)
    this.dispatchEvent(event)
  }
}

// The handler types above bind `this` to WebSocket; the shim keeps the plain
// property-call semantics the two call sites rely on.

export function installNativeWebSocket(): void {
  if (!nativeBridge()) return
  window.addEventListener('garnet:ws', (event: Event) => {
    const detail = (event as CustomEvent<WsNativeEventDetail>).detail
    if (!detail || typeof detail.id !== 'string') return
    const socket = liveSockets.get(detail.id)
    if (socket) socket.handle(detail)
  })
  // The document is going away; the native sockets must not outlive it.
  window.addEventListener('pagehide', () => {
    for (const socket of [...liveSockets.values()]) socket.close()
  })
  ;(window as { WebSocket: typeof WebSocket }).WebSocket = NativeWebSocket as unknown as typeof WebSocket
}

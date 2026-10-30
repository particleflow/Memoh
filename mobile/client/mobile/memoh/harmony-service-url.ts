// One validator, also packaged as ServiceUrl.ets with the platform URL parser.
// No default endpoint or automatic fallback. Explicit LAN endpoints require TLS.
export function normalizeServiceUrl(value: string, _api: boolean = true): string {
  if (!value || value.length > 2048 || value !== value.trim() || /[\\\u0000-\u0020%?#]/.test(value) || !/^https?:\/\//.test(value)) throw new Error('SERVICE_URL_INVALID')
  const authority = value.replace(/^https?:\/\//, '').split('/')[0] ?? ''
  const hostPort = /^(?:\[[0-9a-fA-F:]+\]|[A-Za-z0-9.-]+)(?::([0-9]+))?$/.exec(authority)
  if (!hostPort || (hostPort[1] !== undefined && (Number(hostPort[1]) < 1 || Number(hostPort[1]) > 65535))) throw new Error('SERVICE_URL_INVALID')
  const parsed = new URL(value)
  if (parsed.username || parsed.password || !parsed.hostname || !['http:', 'https:'].includes(parsed.protocol)) throw new Error('SERVICE_URL_INVALID')
  const host = parsed.hostname.toLowerCase()
  if (host.endsWith('.') || host === 'localhost' || host.endsWith('.localhost')) throw new Error('SERVICE_URL_LOCAL')
  let lan = /\.(local|lan)$/.test(host)
  if (host.startsWith('[')) {
    lan = /^\[f[cd][0-9a-f]{2}:/.test(host)
    if (!lan && !/^\[[23][0-9a-f]{3}:/.test(host)) throw new Error('SERVICE_URL_LOCAL')
  } else if (/^\d+\.\d+\.\d+\.\d+$/.test(host)) {
    if (authority.split(':')[0] !== host) throw new Error('SERVICE_URL_INVALID')
    const n = host.split('.').map((part: string) => Number(part))
    const first = n[0] ?? 0, second = n[1] ?? 0, third = n[2] ?? 0, fourth = n[3] ?? 0
    lan = first === 10 || (first === 172 && second >= 16 && second <= 31) || (first === 192 && second === 168)
    if (first === 0 || first === 127 || first >= 224 ||
      (first === 100 && second >= 64 && second <= 127) || (first === 169 && second === 254) ||
      (first === 192 && second === 0 && (third === 2 || (third === 0 && fourth !== 9 && fourth !== 10))) ||
      (first === 198 && ([18, 19].includes(second) || (second === 51 && third === 100))) || (first === 203 && second === 0 && third === 113)) throw new Error('SERVICE_URL_LOCAL')
  } else if (!/^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{1,62}$/.test(host)) throw new Error('SERVICE_URL_INVALID')
  if (lan && parsed.protocol !== 'https:') throw new Error('SERVICE_URL_TLS')
  const path = parsed.pathname.replace(/\/+$/, '')
  const rawPath = value.replace(/^https?:\/\/[^/]+/, '')
  if (/\/\.{1,2}(\/|$)/.test(rawPath) || rawPath.includes('//') || !/^(\/[A-Za-z0-9_.~-]+)*\/?$/.test(rawPath)) throw new Error('SERVICE_URL_INVALID')
  return parsed.origin + path
}

// A credential-free scoped API path, never a URL or an owner/hash fallback.
// This export is also consumed by the existing native ServiceUrl module.
export function normalizeDelegationPath(value: string, hash: string): string {
  if (typeof value !== 'string') throw new Error('MEDIA_DOWNLOAD_SCOPE')
  const uuid = '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}'
  const match = new RegExp(`^/bots/(${uuid})/sessions/(${uuid})/delegations/(${uuid})/attachments/([0-9a-f]{64})$`).exec(value)
  if (!match || match[4] !== hash) throw new Error('MEDIA_DOWNLOAD_SCOPE')
  return value
}

// Same payload as the existing Windows transfer-network.json. Address hints
// never supply an HTTP authority, port, API path or credential.
export interface TransferNetworkPolicy { version: number; lan_addresses: string[] }

export function normalizeNetworkAddress(value: string): string {
  if (typeof value !== 'string' || !value) throw new Error('NETWORK_POLICY_ADDRESS')
  if (!value.includes(':')) {
    if (!/^(0|[1-9][0-9]{0,2})(\.(0|[1-9][0-9]{0,2})){3}$/.test(value)) throw new Error('NETWORK_POLICY_ADDRESS')
    const parts = value.split('.').map((part: string) => Number(part))
    if (parts.some((part: number) => part > 255) || parts.every((part: number) => part === 0) || ((parts[0] ?? 0) >= 224 && (parts[0] ?? 0) <= 239)) throw new Error('NETWORK_POLICY_ADDRESS')
    return value
  }
  const percent = value.indexOf('%')
  const address = percent < 0 ? value : value.slice(0, percent)
  const zone = percent < 0 ? '' : value.slice(percent + 1)
  if ((percent >= 0 && !zone) || !/^[0-9a-fA-F:.]+$/.test(address)) throw new Error('NETWORK_POLICY_ADDRESS')
  let parsed: string
  try { parsed = new URL('https://[' + address + ']/').hostname.replace(/^\[|\]$/g, '') }
  catch { throw new Error('NETWORK_POLICY_ADDRESS') }
  const halves = parsed.split('::')
  const left = (halves[0] ?? '').split(':').filter((part: string) => part !== '')
  const right = halves.length === 1 ? [] : (halves[1] ?? '').split(':').filter((part: string) => part !== '')
  const words: number[] = left.map((part: string) => parseInt(part, 16))
  if (halves.length > 1) { for (let i = left.length + right.length; i < 8; i++) words.push(0) }
  for (const part of right) words.push(parseInt(part, 16))
  if (words.length !== 8 || words.every((part: number) => part === 0) || ((words[0] ?? 0) & 0xff00) === 0xff00) throw new Error('NETWORK_POLICY_ADDRESS')
  if (words.slice(0, 5).every((part: number) => part === 0) && words[5] === 0xffff) {
    const high = words[6] ?? 0, low = words[7] ?? 0
    return normalizeNetworkAddress([high >> 8, high & 255, low >> 8, low & 255].join('.'))
  }
  return parsed + (percent < 0 ? '' : '%' + zone)
}

export function normalizeTransferNetworkPolicy(value: TransferNetworkPolicy): TransferNetworkPolicy {
  if (!value || typeof value !== 'object' || Array.isArray(value) || Object.keys(value).sort().join(',') !== 'lan_addresses,version' || value.version !== 1 || !Array.isArray(value.lan_addresses) || !value.lan_addresses.length) throw new Error('NETWORK_POLICY_INVALID')
  // The existing private policy document/seed slot is bounded, not the file
  // queue or transfer size. Count UTF-8 bytes without allocating file buffers.
  const raw = JSON.stringify(value); let bytes = 0
  for (let i = 0; i < raw.length; i++) {
    const unit = raw.charCodeAt(i)
    if (unit < 128) bytes++
    else if (unit < 2048) bytes += 2
    else if (unit >= 0xd800 && unit <= 0xdbff && i + 1 < raw.length && raw.charCodeAt(i + 1) >= 0xdc00 && raw.charCodeAt(i + 1) <= 0xdfff) { bytes += 4; i++ }
    else bytes += 3
  }
  if (bytes > 8192) throw new Error('NETWORK_POLICY_SIZE')
  const addresses: string[] = []
  for (const rawAddress of value.lan_addresses) {
    const address = normalizeNetworkAddress(rawAddress)
    if (addresses.includes(address)) throw new Error('NETWORK_POLICY_DUPLICATE')
    addresses.push(address)
  }
  return { version: 1, lan_addresses: addresses }
}

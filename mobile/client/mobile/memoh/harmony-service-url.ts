// One validator, also packaged as ServiceUrl.ets with the platform URL parser.
// No default endpoint and no relative/LAN development fallback.
export function normalizeServiceUrl(value: string, api: boolean = true): string {
  if (!value || value.length > 2048 || value !== value.trim() || /[\\\u0000-\u0020%?#]/.test(value) || !/^https?:\/\//.test(value)) throw new Error('SERVICE_URL_INVALID')
  const parsed = new URL(value)
  if (parsed.username || parsed.password || !parsed.hostname || !['http:', 'https:'].includes(parsed.protocol)) throw new Error('SERVICE_URL_INVALID')
  const host = parsed.hostname.toLowerCase()
  if (host.endsWith('.') || host === 'localhost' || /\.(localhost|local|lan)$/.test(host)) throw new Error('SERVICE_URL_LOCAL')
  if (host.startsWith('[')) {
    if (!/^\[[23][0-9a-f]{3}:/.test(host)) throw new Error('SERVICE_URL_LOCAL')
  } else if (/^\d+\.\d+\.\d+\.\d+$/.test(host)) {
    const n = host.split('.').map((part: string) => Number(part))
    const first = n[0] ?? 0, second = n[1] ?? 0, third = n[2] ?? 0, fourth = n[3] ?? 0
    if (first === 0 || first === 10 || first === 127 || first >= 224 ||
      (first === 100 && second >= 64 && second <= 127) || (first === 169 && second === 254) ||
      (first === 172 && second >= 16 && second <= 31) || (first === 192 && (second === 168 || (second === 0 && (third === 2 || (third === 0 && fourth !== 9 && fourth !== 10))))) ||
      (first === 198 && ([18, 19].includes(second) || (second === 51 && third === 100))) || (first === 203 && second === 0 && third === 113)) throw new Error('SERVICE_URL_LOCAL')
  } else if (!/^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{1,62}$/.test(host)) throw new Error('SERVICE_URL_INVALID')
  const path = parsed.pathname.replace(/\/+$/, '')
  const rawPath = value.replace(/^https?:\/\/[^/]+/, '')
  if (/\/\.{1,2}(\/|$)/.test(rawPath) || rawPath.includes('//') || !/^(\/[A-Za-z0-9_.~-]+)*\/?$/.test(rawPath)) throw new Error('SERVICE_URL_INVALID')
  if (api && !path.endsWith('/api')) throw new Error('SERVICE_URL_API_PATH')
  return parsed.origin + path
}

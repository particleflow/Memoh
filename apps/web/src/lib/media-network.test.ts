import { describe, expect, it } from 'vitest'
import { mediaDispatch, mediaEndpoint } from './media-network'

describe('explicit media endpoint routing', () => {
  const logical = 'https://nas.example.invalid'
  const endpoint = { logical_origin: logical, physical_origin: 'http://127.0.0.1:18068' }
  it('preserves v1 and omitted routing and snapshots an explicit v2 mapping', () => {
    expect(mediaEndpoint(logical + '/api')).toBeUndefined()
    expect(mediaEndpoint(logical + '/api', { version: 1, lan_addresses: ['192.0.2.1'] })).toBeUndefined()
    expect(mediaEndpoint(logical + '/api', { version: 2, endpoint })).toBe(endpoint.physical_origin)
  })
  it('rejects mismatched identities and ambiguous destinations', () => {
    expect(() => mediaEndpoint('https://other.invalid/api', { version: 2, endpoint })).toThrow()
    for (const physical_origin of ['http://user:pass@host.invalid', 'http://host.invalid/path', 'http://host.invalid?', 'http://host.invalid#', 'http://host.invalid:0', 'file:///tmp/data', '//host.invalid']) {
      expect(() => mediaEndpoint(logical + '/api', { version: 2, endpoint: { ...endpoint, physical_origin } })).toThrow()
    }
  })
  it('maps the physical destination once while retaining original request, bytes and headers', async () => {
    const bytes = new Uint8Array([0, 255, 128, 42])
    const request = new Request(logical + '/api/bots/bot/media/uploads/receipt', { method: 'PATCH', headers: { Authorization: 'Bearer synthetic-no-authority', 'Upload-Offset': '1048576' }, body: bytes })
    const routed = await mediaDispatch(request, endpoint.physical_origin)
    expect(request.url).toBe(logical + '/api/bots/bot/media/uploads/receipt')
    expect(routed.url).toBe(endpoint.physical_origin + '/api/bots/bot/media/uploads/receipt')
    expect(routed.headers.get('Authorization')).toBe('Bearer synthetic-no-authority')
    expect(routed.headers.get('Upload-Offset')).toBe('1048576')
    expect(routed.credentials).toBe('omit')
    expect(routed.redirect).toBe('error')
    expect(new Uint8Array(await routed.arrayBuffer())).toEqual(bytes)
  })
})

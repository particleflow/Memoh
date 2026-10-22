import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  deleteBotsByBotIdMcpById,
  getBotsByBotIdMcpById,
  getBotsByBotIdMcpOpsExport,
  postBotsByBotIdMcp,
  postBotsByBotIdMcpByIdProbe,
  postBotsByBotIdMcpOpsBatchDelete,
  putBotsByBotIdMcpById,
  putBotsByBotIdMcpOpsImport,
} from '@memohai/sdk'
import { client } from '@memohai/sdk/client'

afterEach(() => vi.unstubAllGlobals())

describe('generated Bot MCP paths', () => {
  it('substitutes the required Bot and connection path parameters for management calls', async () => {
    const requests: Request[] = []
    const fetchMock = vi.fn<typeof fetch>(async (input, init) => {
      const request = new Request(input, init)
      requests.push(request)
      return new Response('{}', { status: 200, headers: { 'content-type': 'application/json' } })
    })
    vi.stubGlobal('fetch', fetchMock)
    const baseUrl = 'https://example.invalid'
    const path = { bot_id: 'bot / one', id: 'connection / one' }
    const options = { baseUrl, path, throwOnError: true as const }
    const before = client.getConfig()
    try {
      client.setConfig({ fetch: fetchMock })
      await postBotsByBotIdMcp({ ...options, body: { name: 'synthetic' } })
      await getBotsByBotIdMcpById(options)
      await putBotsByBotIdMcpById({ ...options, body: { name: 'synthetic-updated' } })
      await postBotsByBotIdMcpByIdProbe(options)
      await deleteBotsByBotIdMcpById(options)
      await putBotsByBotIdMcpOpsImport({ ...options, body: { mcpServers: {} } })
      await postBotsByBotIdMcpOpsBatchDelete({ ...options, body: { ids: ['synthetic-id'] } })
      await getBotsByBotIdMcpOpsExport(options)
    } finally {
      client.setConfig(before)
    }
    const bot = '/bots/bot%20%2F%20one'
    const connection = `${bot}/mcp/connection%20%2F%20one`
    expect(requests.map(request => [request.method, new URL(request.url).pathname])).toEqual([
      ['POST', `${bot}/mcp`],
      ['GET', connection],
      ['PUT', connection],
      ['POST', `${connection}/probe`],
      ['DELETE', connection],
      ['PUT', `${bot}/mcp-ops/import`],
      ['POST', `${bot}/mcp-ops/batch-delete`],
      ['GET', `${bot}/mcp-ops/export`],
    ])
  })
})

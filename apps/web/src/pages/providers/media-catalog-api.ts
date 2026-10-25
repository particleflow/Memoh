import { client } from '@memohai/sdk/client'

// Uses the application's SDK client, including its base URL and authentication.
// Generated OpenAPI/SDK can be refreshed during integration; no separate client.
export interface MediaCatalogModel {
  id: string
  kind?: string
  input: string[]
  output: string[]
  capability_source: 'unspecified' | 'catalog_kind' | 'catalog_modalities' | 'catalog_kind_and_modalities'
}
export interface MediaCatalog {
  provider_id: string
  protocol: 'magpie'
  checked_at: string
  models: MediaCatalogModel[]
  transcription_status: 'unverified'
}
export async function readMediaCatalog(providerId: string): Promise<MediaCatalog> {
  const { data } = await client.get<{ 200: MediaCatalog }, unknown, true>({
    url: '/providers/{id}/media-catalog',
    path: { id: providerId },
    query: { protocol: 'magpie' },
    throwOnError: true,
  })
  return data
}

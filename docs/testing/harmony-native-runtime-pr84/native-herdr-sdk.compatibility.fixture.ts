import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  deleteBotsByBotIdWorkspaceTargetsByTargetId,
  deleteUsersMeRuntimesByIdTransferCredential,
  getUsersMeRuntimes,
  getUsersMeRuntimesByIdAdmission,
  getUsersMeRuntimesByIdTransferCredential,
  postUsersMeRuntimesByIdTransferCredential,
  putBotsByBotIdWorkspaceTargetsRemotesByRuntimeId,
  putUsersMeRuntimesByIdAdmission,
  putUsersMeRuntimesByIdNativeHerdr,
  type AgenvoNativeHerdrProfile,
} from '@memohai/sdk'
import { client } from '@memohai/sdk/client'
import { runtimeAvailability, workspaceTargetAvailable } from '@/utils/workspace-target'

const baseUrl = 'https://nas.example.invalid/api'
const runtimeId = '11111111-1111-4111-8111-111111111111'
const targetId = '22222222-2222-4222-8222-222222222222'
const botId = '33333333-3333-4333-8333-333333333333'
const requestId = '44444444-4444-4444-8444-444444444444'
const credentialId = '55555555-5555-4555-8555-555555555555'
const originalConfig = client.getConfig()
const profile: AgenvoNativeHerdrProfile = {
  instance_id: 'synthetic-instance', session: 'default',
  work_dir: 'C:\\Memoh\\work', bash_path: 'C:\\Git\\bin\\bash.exe',
  herdr_cli: 'C:\\Memoh\\herdr.exe', herdr_config_root: 'C:\\Memoh\\config\\herdr',
  agent: { execution_mode: 'pi-json-v1', provider: 'synthetic-provider', model: 'synthetic-model', config_dir: 'C:\\Memoh\\pi' },
}
function transport(body: unknown = {}, status = 200) {
  const requests: Request[] = []
  const fetcher = vi.fn<typeof fetch>(async input => {
    const request = input instanceof Request ? input : new Request(input)
    requests.push(request.clone())
    return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
  })
  client.setConfig({ baseUrl, fetch: fetcher })
  function at(index = 0) {
    const request = requests[index]
    if (!request) throw new Error('Expected synthetic SDK request')
    return request
  }
  return { requests, fetcher, at }
}
afterEach(() => client.setConfig(originalConfig))

describe('fixed PR84 SDK with existing phone consumers', () => {
  it('keeps optional native identity without treating the PC as the NAS workspace', async () => {
    const runtime = { id: runtimeId, provider: 'memoh', online: false, native_herdr: { connection_id: runtimeId, device_id: runtimeId, instance_id: 'synthetic-instance', session: 'default', target_fingerprint: 'a'.repeat(64) } }
    const wire = transport([runtime])
    const { data } = await getUsersMeRuntimes({ throwOnError: true })
    expect(data).toEqual([runtime])
    expect(wire.at().url).toBe(`${baseUrl}/users/me/runtimes`)
    expect(runtimeAvailability(runtime)).toBe('offline')
    expect(workspaceTargetAvailable({ kind: 'remote', provider: 'memoh', online: false, target_id: targetId })).toBe(false)
    expect(workspaceTargetAvailable({ kind: 'remote', provider: 'memoh', online: true, target_id: targetId })).toBe(true)
  })

  it('preserves different registered Runtime and Bot mount IDs on grant/revoke', async () => {
    const wire = transport()
    await putBotsByBotIdWorkspaceTargetsRemotesByRuntimeId({ path: { bot_id: botId, runtime_id: runtimeId }, throwOnError: true })
    await deleteBotsByBotIdWorkspaceTargetsByTargetId({ path: { bot_id: botId, target_id: targetId }, throwOnError: true })
    expect(wire.at().url).toBe(`${baseUrl}/bots/${botId}/workspace-targets/remotes/${runtimeId}`)
    expect(wire.at(1).url).toBe(`${baseUrl}/bots/${botId}/workspace-targets/${targetId}`)
    expect(wire.requests.map(r => r.method)).toEqual(['PUT', 'DELETE'])
  })

  it('retains the exact admission revision and request UUID through the existing API', async () => {
    const revision = '9007199254740993'
    const wire = transport({ revision, request_id: requestId, paused: true })
    const { data } = await getUsersMeRuntimesByIdAdmission({ path: { id: runtimeId }, throwOnError: true })
    expect(data?.revision).toBe(revision)
    const body = { expected_revision: revision, request_id: requestId, paused: false }
    await putUsersMeRuntimesByIdAdmission({ path: { id: runtimeId }, body, throwOnError: true })
    expect(wire.at(1).url).toBe(`${baseUrl}/users/me/runtimes/${runtimeId}/admission`)
    expect(await wire.at(1).json()).toEqual(body)
    expect(wire.requests).toHaveLength(2)
  })

  it('serializes the new native configuration through the generated method only', async () => {
    const wire = transport({ id: runtimeId, provider: 'memoh' })
    await putUsersMeRuntimesByIdNativeHerdr({ path: { id: runtimeId }, body: profile, throwOnError: true })
    expect(wire.at().url).toBe(`${baseUrl}/users/me/runtimes/${runtimeId}/native-herdr`)
    expect(wire.at().method).toBe('PUT')
    expect(await wire.at().json()).toEqual(profile)
    expect(wire.requests).toHaveLength(1)
  })

  it.each([403, 409])('propagates native configuration rejection %s without retry or fallback', async status => {
    const problem = { status, code: 'synthetic_rejection' }
    const wire = transport(problem, status)
    await expect(putUsersMeRuntimesByIdNativeHerdr({ path: { id: runtimeId }, body: profile, throwOnError: true })).rejects.toEqual(problem)
    expect(wire.requests).toHaveLength(1)
    expect(wire.at().url).toBe(`${baseUrl}/users/me/runtimes/${runtimeId}/native-herdr`)
  })

  it('keeps transfer enrollment/rotation CAS on the same registered Runtime route', async () => {
    const wire = transport()
    await getUsersMeRuntimesByIdTransferCredential({ path: { id: runtimeId }, throwOnError: true })
    await postUsersMeRuntimesByIdTransferCredential({ path: { id: runtimeId }, body: { expected_id: credentialId }, throwOnError: true })
    await deleteUsersMeRuntimesByIdTransferCredential({ path: { id: runtimeId }, query: { expected_id: credentialId }, throwOnError: true })
    expect(wire.requests.map(r => r.method)).toEqual(['GET', 'POST', 'DELETE'])
    for (const request of wire.requests) expect(new URL(request.url).pathname).toBe(`/api/users/me/runtimes/${runtimeId}/transfer-credential`)
    expect(await wire.at(1).json()).toEqual({ expected_id: credentialId })
    expect(new URL(wire.at(2).url).searchParams.get('expected_id')).toBe(credentialId)
  })
})

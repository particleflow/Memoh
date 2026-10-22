import { describe, expect, it } from 'vitest'
import { agenvoCapabilityLabel, runtimeAvailability, workspaceTargetAvailable, workspaceTargetStatusLabel } from './workspace-target'

describe('computer observation labels', () => {
  const t = (key: string) => key
  it('separates unavailable authentication and unknown connection from confirmed offline', () => {
    expect(runtimeAvailability({ provider: 'agenvo', online: false })).toBe('connection_unavailable')
    for (const availability of ['authentication_unavailable', 'connection_unavailable', 'runtime_unavailable', 'offline']) {
      expect(runtimeAvailability({ provider: 'agenvo', online: false, availability })).toBe(availability)
      const target = { kind: 'remote', provider: 'agenvo', online: false, status: availability }
      expect(workspaceTargetAvailable(target)).toBe(false)
      expect(workspaceTargetStatusLabel(target, key => `translated:${key}`)).toBe(`translated:runtimes.status.${availability}`)
    }
    expect(workspaceTargetAvailable({ kind: 'remote', provider: 'agenvo', online: false })).toBe(false)
    expect(runtimeAvailability({ online: false })).toBe('offline')
  })
  it('labels configured Pi task support without promising workspace management', () => {
    expect(agenvoCapabilityLabel({ capabilities: ['exec_foreground'] }, t)).toBe('agenvo.capability')
    expect(agenvoCapabilityLabel({ capabilities: ['exec_foreground', 'herdr_pi_task'] }, t)).toBe('agenvo.agentCapability')
  })
})

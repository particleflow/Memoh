import type { UserruntimeRuntime, WorkspaceWorkspaceTarget } from '@memohai/sdk'

// Display + availability rules for workspace targets, shared by the composer
// Continue-on selector and chat-pane's selection guard. All label helpers take
// `t` because i18n is only available inside setup scope.

type I18nT = (key: string, ...args: unknown[]) => string

export function workspaceTargetName(target: Pick<WorkspaceWorkspaceTarget, 'kind' | 'name'>, t: I18nT): string {
  if (target.kind === 'native') return t('bots.remoteRuntime.nativeWorkspace')
  return target.name || t('bots.remoteRuntime.unknownComputer')
}

export function workspaceTargetStatus(target: WorkspaceWorkspaceTarget): string {
  if (target.kind === 'native') return 'online'
  return target.status || runtimeAvailability(target)
}

export function workspaceTargetStatusLabel(target: WorkspaceWorkspaceTarget, t: I18nT): string {
  const status = workspaceTargetStatus(target)
  const key = `runtimes.status.${status}`
  const label = t(key)
  return label === key ? status : label
}

export function workspaceTargetAvailable(target: WorkspaceWorkspaceTarget): boolean {
  if (target.provider === 'agenvo') return false // Historical snapshots never authorize a new action.
  return target.kind === 'native'
    || (workspaceTargetStatus(target) === 'online' && target.online !== false)
}

// A failed Agenvo probe does not establish that the device is offline.
export function runtimeAvailability(runtime: Pick<UserruntimeRuntime, 'provider' | 'online' | 'availability'>): string {
  if (runtime.provider === 'agenvo') return runtime.availability || (runtime.online ? 'online' : 'connection_unavailable')
  return runtime.online ? 'online' : 'offline'
}

// Compact navigation only, never a route/auth restriction. Existing Bot
// management stays reachable; server permissions remain authoritative.
export const harmonyChatOnly = import.meta.env.VITE_HARMONY_CHAT_ONLY === 'true'

export function harmonySettingAllowed(name: unknown): boolean {
  return !harmonyChatOnly || ['bots', 'profile', 'appearance', 'keyboard', 'about'].includes(String(name ?? ''))
}

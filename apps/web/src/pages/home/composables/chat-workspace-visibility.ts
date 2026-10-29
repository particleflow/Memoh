import type { InjectionKey, Ref } from 'vue'

// Home can mask a mounted workspace before its panes receive a new task anchor.
export const chatWorkspaceVisibleKey: InjectionKey<Readonly<Ref<boolean>>> = Symbol('chat-workspace-visible')

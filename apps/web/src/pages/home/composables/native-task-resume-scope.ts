import { computed, provide, type InjectionKey, type Ref } from 'vue'

export interface NativeTaskResumeHost {
  botId?: string
  sessionId?: string
  visible?: boolean
}

export const nativeTaskResumeScope: InjectionKey<Ref<NativeTaskResumeHost>> = Symbol('native-task-resume-scope')

export function provideNativeTaskResumeScope(props: NativeTaskResumeHost) {
  provide(nativeTaskResumeScope, computed(() => ({
    botId: props.botId, sessionId: props.sessionId, visible: props.visible,
  })))
}

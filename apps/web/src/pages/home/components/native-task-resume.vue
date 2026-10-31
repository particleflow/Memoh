<script setup lang="ts">
import { computed, inject, ref, toRef } from 'vue'
import { useLocalStorage } from '@vueuse/core'
import { Button } from '@felinic/ui'
import { useI18n } from 'vue-i18n'
import type { ToolCallBlock } from '@/store/chat-list'
import { useUserStore } from '@/store/user'
import { nativeTaskResumeScope, type NativeTaskResumeHost } from '../composables/native-task-resume-scope'
import { useNativeTaskResume } from '../composables/useNativeTaskResume'

const props = defineProps<{ block: ToolCallBlock }>()
const { t } = useI18n()
const host = inject(nativeTaskResumeScope, ref<NativeTaskResumeHost>({}))
const user = useUserStore()
const token = useLocalStorage('token', '')
const { status, canSend, send } = useNativeTaskResume(computed(() => host.value.botId && host.value.sessionId && host.value.visible !== false ? {
  bot: host.value.botId, session: host.value.sessionId, identity: user.userInfo.id,
} : null), toRef(props, 'block'), token)
</script>

<template>
  <div class="space-y-2 pt-2">
    <p
      role="status"
      class="text-body text-muted-foreground"
    >
      {{ t(`chat.tasks.resume.${status}`) }}
    </p>
    <Button
      v-if="canSend || status === 'sending'"
      variant="outline"
      :disabled="!canSend"
      :loading="status === 'sending'"
      @click.stop="send()"
    >
      {{ t('chat.tasks.resume.action') }}
    </Button>
  </div>
</template>

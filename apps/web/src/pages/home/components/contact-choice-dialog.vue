<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, CalloutBanner, Dialog, DialogBody, DialogFooter, DialogHeader, DialogPanel, DialogTitle, Label, RadioGroup, RadioGroupItem } from '@felinic/ui'
import { useChatStore } from '@/store/chat-list'
import { resolveApiErrorMessage } from '@/utils/api-error'

const chat = useChatStore()
const { t } = useI18n()
const selected = ref('')
const error = ref('')
watch(() => chat.contactChoice?.botId, () => { selected.value = ''; error.value = '' })
async function confirm() {
  error.value = ''
  try { await chat.confirmContact(selected.value) }
  catch (cause) { error.value = resolveApiErrorMessage(cause, t('chat.contact.failed')) }
}
async function more() {
  try { await chat.loadContactCandidates() }
  catch (cause) { error.value = resolveApiErrorMessage(cause, t('chat.contact.failed')) }
}
</script>

<template>
  <Dialog
    :open="!!chat.contactChoice"
    @update:open="open => { if (!open) chat.cancelContactChoice() }"
  >
    <DialogPanel
      width="lg"
      footer
    >
      <DialogHeader>
        <DialogTitle>{{ t('chat.contact.chooseTitle') }}</DialogTitle>
      </DialogHeader>
      <DialogBody class="space-y-4">
        <CalloutBanner
          v-if="error"
          tone="destructive"
          :description="error"
        />
        <RadioGroup
          v-model="selected"
          :disabled="chat.contactBusy"
          class="gap-3"
        >
          <Label
            v-for="session in chat.contactChoice?.candidates"
            :key="session.id"
            class="flex min-w-0 items-center gap-2 text-label"
          >
            <RadioGroupItem :value="session.id" />
            <span class="min-w-0 break-words">{{ session.title || t('chat.untitledSession') }} · {{ session.id.slice(0, 8) }}{{ session.id === chat.sessionId ? ` · ${t('chat.contact.current')}` : '' }}</span>
          </Label>
        </RadioGroup>
        <Button
          v-if="chat.contactChoice?.nextCursor"
          variant="ghost"
          :disabled="chat.contactBusy"
          @click="more"
        >
          {{ t('chat.contact.more') }}
        </Button>
      </DialogBody>
      <DialogFooter>
        <Button
          variant="secondary"
          :disabled="chat.contactBusy"
          @click="chat.cancelContactChoice()"
        >
          {{ t('common.cancel') }}
        </Button>
        <Button
          :disabled="!selected || chat.contactBusy"
          @click="confirm"
        >
          {{ t('common.confirm') }}
        </Button>
      </DialogFooter>
    </DialogPanel>
  </Dialog>
</template>

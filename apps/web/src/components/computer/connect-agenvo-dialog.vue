<template>
  <Dialog v-model:open="open">
    <DialogPanel
      width="lg"
      footer
    >
      <form
        class="contents"
        @submit.prevent="register"
      >
        <DialogHeader>
          <DialogTitle>{{ t('agenvo.connect') }}</DialogTitle>
          <DialogDescription>{{ t('agenvo.description') }}</DialogDescription>
        </DialogHeader>
        <DialogBody>
          <FormStack>
            <CalloutBanner
              tone="neutral"
              :description="t('agenvo.limits')"
            />
            <FieldStack
              :label="t('agenvo.sourceBot')"
              for="agenvo-source"
            >
              <Select
                v-model="botId"
                :disabled="busy"
              >
                <SelectTrigger id="agenvo-source">
                  <SelectValue :placeholder="t('agenvo.choose')" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-for="bot in bots"
                    :key="bot.id"
                    :value="bot.id!"
                  >
                    {{ bot.display_name || bot.id }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </FieldStack>
            <FieldStack
              :label="t('agenvo.connection')"
              for="agenvo-connection"
              :help="t('agenvo.connectionHelp')"
            >
              <Select
                v-model="connectionId"
                :disabled="busy || loadingConnections"
              >
                <SelectTrigger id="agenvo-connection">
                  <SelectValue :placeholder="t('agenvo.choose')" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-for="connection in connections"
                    :key="connection.id"
                    :value="connection.id"
                  >
                    {{ connection.name }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </FieldStack>
            <FieldStack
              :label="t('agenvo.device')"
              for="agenvo-device"
            >
              <Select
                v-model="instanceKey"
                :disabled="busy || discovering"
              >
                <SelectTrigger id="agenvo-device">
                  <SelectValue :placeholder="t('agenvo.choose')" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem
                    v-for="(instance, index) in instances"
                    :key="index"
                    :value="String(index)"
                    :disabled="!instance.online"
                  >
                    {{ instance.device_name }} · {{ instance.name }}{{ instance.online ? '' : ` (${t('runtimes.status.offline')})` }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </FieldStack>
            <FieldStack
              :label="t('agenvo.name')"
              for="agenvo-name"
            >
              <Input
                id="agenvo-name"
                v-model="name"
                required
                maxlength="100"
                :disabled="busy"
              />
            </FieldStack>
            <FieldStack
              :label="t('agenvo.session')"
              for="agenvo-session"
              :help="t('agenvo.sessionHelp')"
            >
              <Input
                id="agenvo-session"
                v-model="session"
                required
                maxlength="64"
                :disabled="busy"
              />
            </FieldStack>
            <FieldStack
              :label="t('agenvo.workDir')"
              for="agenvo-workdir"
            >
              <Input
                id="agenvo-workdir"
                v-model="workDir"
                required
                :disabled="busy"
                placeholder="C:\Work"
              />
            </FieldStack>
            <InlineLoadingRow v-if="loadingConnections || discovering">
              {{ t('agenvo.discovering') }}
            </InlineLoadingRow>
            <CalloutBanner
              v-if="error"
              tone="destructive"
              :description="error"
            />
          </FormStack>
        </DialogBody>
        <DialogFooter>
          <Button
            variant="outline"
            type="button"
            :disabled="busy"
            @click="open = false"
          >
            {{ t('common.cancel') }}
          </Button>
          <Button
            type="submit"
            :loading="busy"
            :disabled="!canRegister"
          >
            {{ t('agenvo.register') }}
          </Button>
        </DialogFooter>
      </form>
    </DialogPanel>
  </Dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getBots, getBotsByBotIdMcp, postUsersMeRuntimes, postUsersMeRuntimesAgenvoDiscover,
  type AgenvoDiscoveredInstance, type BotsBot, type UserruntimeRuntime,
} from '@memohai/sdk'
import {
  Button, CalloutBanner, Dialog, DialogBody, DialogDescription, DialogFooter,
  DialogHeader, DialogPanel, DialogTitle, FieldStack, FormStack, InlineLoadingRow,
  Input, Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@felinic/ui'
import { resolveApiErrorMessage } from '@/utils/api-error'

const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ registered: [runtime: UserruntimeRuntime] }>()
const { t } = useI18n()
const bots = ref<BotsBot[]>([])
// Keep only identity/display fields from existing MCP records, never credentials.
const connections = ref<{ id: string, name: string }[]>([])
const instances = ref<AgenvoDiscoveredInstance[]>([])
const botId = ref('')
const connectionId = ref('')
const instanceKey = ref('')
const name = ref('')
const session = ref('')
const workDir = ref('')
const busy = ref(false)
const loadingConnections = ref(false)
const discovering = ref(false)
const error = ref('')
const selected = computed(() => instanceKey.value === '' ? undefined : instances.value[Number(instanceKey.value)])
const canRegister = computed(() => !busy.value && !discovering.value && !!selected.value?.online && !!name.value.trim() && !!session.value.trim() && !!workDir.value.trim())
let connectionsVersion = 0
let discoverVersion = 0

watch(open, async (value) => {
  if (!value) return
  error.value = ''
  try {
    const { data } = await getBots({ throwOnError: true })
    bots.value = (data.items ?? []).filter(bot => !!bot.id)
  } catch (cause) { error.value = resolveApiErrorMessage(cause, t('errors.internal')) }
})
watch(botId, async (id) => {
  const version = ++connectionsVersion
  connectionId.value = ''
  connections.value = []
  if (!id) return
  loadingConnections.value = true
  error.value = ''
  try {
    const { data } = await getBotsByBotIdMcp({ path: { bot_id: id }, throwOnError: true })
    if (version !== connectionsVersion) return
    connections.value = (data.items ?? []).filter(item => item.id && item.is_active && item.type === 'http').map(item => ({ id: item.id!, name: item.name || item.id! }))
    if (!connections.value.length) error.value = t('agenvo.noConnections')
  } catch (cause) { if (version === connectionsVersion) error.value = resolveApiErrorMessage(cause, t('errors.internal')) }
  finally { if (version === connectionsVersion) loadingConnections.value = false }
})
watch(connectionId, async (id) => {
  const version = ++discoverVersion
  instanceKey.value = ''
  instances.value = []
  if (!id) { discovering.value = false; return }
  discovering.value = true
  error.value = ''
  try {
    const { data } = await postUsersMeRuntimesAgenvoDiscover({ body: { connection_id: id }, throwOnError: true })
    if (version !== discoverVersion) return
    instances.value = data
    if (!data.length) error.value = t('agenvo.noDevices')
  } catch (cause) { if (version === discoverVersion) error.value = resolveApiErrorMessage(cause, t('errors.internal')) }
  finally { if (version === discoverVersion) discovering.value = false }
})
async function register() {
  if (!canRegister.value || !selected.value) return
  busy.value = true
  error.value = ''
  try {
    const { data } = await postUsersMeRuntimes({
      body: { name: name.value.trim(), provider: 'agenvo', agenvo: {
        connection_id: connectionId.value, device_id: selected.value.device_id!, instance_id: selected.value.instance_id!,
        session: session.value.trim(), work_dir: workDir.value.trim(),
      } }, throwOnError: true,
    })
    // The existing access dialog starts with no grants. Never grant all Bots.
    open.value = false
    emit('registered', data)
  } catch (cause) { error.value = resolveApiErrorMessage(cause, t('errors.internal')) }
  finally { busy.value = false }
}
</script>

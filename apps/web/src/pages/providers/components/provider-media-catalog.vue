<template>
  <Dialog v-model:open="open">
    <DialogTrigger as-child>
      <Button
        type="button"
        variant="outline"
        size="sm"
        :disabled="disabled"
      >
        {{ $t('provider.mediaCatalog.title') }}
      </Button>
    </DialogTrigger>
    <DialogPanel
      width="xl"
      footer
    >
      <DialogHeader>
        <DialogTitle>{{ $t('provider.mediaCatalog.title') }}</DialogTitle>
      </DialogHeader>
      <DialogBody>
        <SettingsSection :title="$t('provider.mediaCatalog.protocol')">
          <SettingsRow
            v-if="error"
            :label="$t('provider.mediaCatalog.errorTitle')"
            :description="error"
          />
          <template v-if="catalog">
            <SettingsRow
              :label="$t('provider.mediaCatalog.checkedAt')"
              :description="catalog.checked_at"
            />
            <SettingsRow
              :label="$t('provider.mediaCatalog.transcription')"
            />
            <SettingsRow
              v-if="catalog.models.length === 0"
              :label="$t('provider.mediaCatalog.empty')"
            />
            <SettingsRow
              v-for="model in catalog.models"
              :key="model.id"
              :label="model.id"
              :description="describeModel(model)"
            />
          </template>
          <SettingsRow
            v-else-if="!error"
            :label="$t('provider.mediaCatalog.readOnly')"
          />
        </SettingsSection>
      </DialogBody>
      <DialogFooter>
        <DialogClose as-child>
          <Button
            type="button"
            variant="outline"
          >
            {{ $t('provider.mediaCatalog.close') }}
          </Button>
        </DialogClose>
        <Button
          type="button"
          :disabled="disabled || loading"
          :loading="loading"
          @click="readCatalog"
        >
          {{ $t('provider.mediaCatalog.read') }}
        </Button>
      </DialogFooter>
    </DialogPanel>
  </Dialog>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle, DialogTrigger, SettingsRow, SettingsSection } from '@felinic/ui'
import { resolveApiErrorMessage } from '@/utils/api-error'
import { readMediaCatalog, type MediaCatalog, type MediaCatalogModel } from '../media-catalog-api'

const props = defineProps<{ providerId: string, disabled: boolean }>()
const { t } = useI18n()
const open = ref(false)
const loading = ref(false)
const catalog = ref<MediaCatalog | null>(null)
const error = ref('')
let generation = 0

function reset() {
  generation++
  catalog.value = null
  error.value = ''
  loading.value = false
}
watch(() => [props.providerId, props.disabled], reset)
onBeforeUnmount(reset)

function describeModel(model: MediaCatalogModel) {
  const absent = t('provider.mediaCatalog.unspecified')
  return t('provider.mediaCatalog.modelFacts', {
    kind: model.kind || absent,
    input: model.input.join(', ') || absent,
    output: model.output.join(', ') || absent,
    source: t('provider.mediaCatalog.sources.' + model.capability_source),
  })
}
async function readCatalog() {
  if (props.disabled || loading.value) return
  const current = ++generation
  catalog.value = null
  error.value = ''
  loading.value = true
  try {
    const result = await readMediaCatalog(props.providerId)
    if (current === generation) catalog.value = result
  } catch (cause) {
    if (current === generation) error.value = resolveApiErrorMessage(cause, t('provider.mediaCatalog.failed'))
  } finally {
    if (current === generation) loading.value = false
  }
}
</script>

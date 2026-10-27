import { computed, ref, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { useQuery } from '@pinia/colada'
import { getModelsById, type ModelsGetResponse } from '@memohai/sdk'
import { useI18n } from 'vue-i18n'

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

// Keep the selected label separate from the selectable catalog. The catalog
// deliberately excludes disabled/unavailable rows; a missing row must never be
// put back in the menu or cause the selection to change.
export function useModelSelectionLabel(
  selected: MaybeRefOrGetter<string>,
  models: MaybeRefOrGetter<ModelsGetResponse[]>,
  lookupEnabled: MaybeRefOrGetter<boolean> = true,
) {
  const { t } = useI18n()
  const selectedId = computed(() => toValue(selected)?.trim() ?? '')
  const selectedModel = computed(() => toValue(models).find(m => (m.id || m.model_id) === selectedId.value))
  const remembered = ref<{ id: string; label: string }>()
  watch([selectedId, selectedModel], ([id, model]) => {
    if (!id) { remembered.value = undefined; return }
    if (model) remembered.value = { id, label: model.name || model.model_id || id }
    else if (remembered.value?.id !== id) remembered.value = undefined
  }, { immediate: true, flush: 'sync' })

  // Native model UUIDs remain addressable after removal/disable. This restores
  // the label after a page reload without putting the row in the picker.
  const lookup = useQuery({
    key: () => ['selected-model-label', selectedId.value],
    query: async ({ signal }) => {
      const { data } = await getModelsById({ path: { id: selectedId.value }, signal, throwOnError: true })
      return data
    },
    enabled: () => toValue(lookupEnabled) && !!selectedId.value && !selectedModel.value && UUID.test(selectedId.value),
  })

  const isUnavailable = computed(() => !!selectedId.value && !selectedModel.value)
  const label = computed(() => {
    if (!selectedId.value) return ''
    if (selectedModel.value) return selectedModel.value.name || selectedModel.value.model_id || selectedId.value
    const prior = remembered.value?.id === selectedId.value ? remembered.value.label : ''
    const fetched = lookup.data.value?.id === selectedId.value ? lookup.data.value : undefined
    const name = prior || fetched?.name || fetched?.model_id
    if (!name && lookup.isLoading.value) return t('common.loading')
    return name ? t('models.unavailableSelection', { name }) : t('models.unknownSelection')
  })
  return { label, isUnavailable }
}

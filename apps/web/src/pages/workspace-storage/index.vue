<script setup lang="ts">
import { computed, onScopeDispose, ref, shallowRef, watch } from 'vue'
import { useLocalStorage } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { Button, FieldStack, FormDialogShell, FormStack, Input, PageShell, SettingsRow, SettingsSection, toast } from '@felinic/ui'
import { getUsersMeRuntimes, getUsersMeRuntimesByIdWorkspaceStorage, postUsersMeRuntimesByIdWorkspaceStorageDirectories, putUsersMeRuntimesByIdWorkspaceStorage, type UserruntimeRuntime } from '@memohai/sdk'
import DirectoryPicker from '@/components/file-manager/directory-picker.vue'
import { isApiErrorCode, resolveApiErrorMessage, UserFacingError } from '@/utils/api-error'

const { t } = useI18n()
const token = useLocalStorage('token', '')
type Computer = UserruntimeRuntime & { id: string }
const computers = shallowRef<Computer[]>([])
const loadError = ref(''), directoryError = ref(''), loading = ref(false), saving = ref(false)
const selected = shallowRef<Computer>(), selection = ref('.'), newName = ref(''), treeKey = ref(0)
const open = ref(false), ready = ref(false)
let epoch = 0, controller = new AbortController()
function invalidate() { epoch++; controller.abort(); controller = new AbortController(); selected.value = undefined; open.value = false; ready.value = false; saving.value = false; loading.value = false }
async function load() {
  const current = epoch
  loading.value = true; loadError.value = ''; computers.value = []
  try {
    const { data } = await getUsersMeRuntimes({ signal: controller.signal, cache: 'no-store', throwOnError: true })
    if (current === epoch) computers.value = data.filter((item): item is Computer => item.provider === 'memoh' && !!item.id)
  } catch (error) { if (current === epoch) loadError.value = resolveApiErrorMessage(error, t('workspaceStorage.loadFailed')) }
  finally { if (current === epoch) loading.value = false }
}
watch(token, () => { invalidate(); if (token.value) void load() }, { immediate: true })
watch(open, value => { if (!value) invalidate() })
onScopeDispose(invalidate)

async function choose(computer: Computer) {
  epoch++; controller.abort(); controller = new AbortController(); saving.value = false
  const current = epoch
  selected.value = computer; open.value = true; selection.value = '.'; newName.value = ''; ready.value = false; directoryError.value = ''
  try {
    const { data } = await getUsersMeRuntimesByIdWorkspaceStorage({ path: { id: computer.id }, signal: controller.signal, cache: 'no-store', throwOnError: true })
    if (current !== epoch || selected.value?.id !== computer.id || !open.value) return
    selection.value = data.selected_path || '.'; ready.value = true; treeKey.value++
  } catch (error) { if (current === epoch && selected.value?.id === computer.id) directoryError.value = resolveApiErrorMessage(error, t('workspaceStorage.unavailable')) }
}
async function directories(path: string) {
  const computer = selected.value
  if (!computer) throw new Error('No computer selected')
  try {
    const { data } = await getUsersMeRuntimesByIdWorkspaceStorage({ path: { id: computer.id }, query: { path }, signal: controller.signal, cache: 'no-store', throwOnError: true })
    return { path: data.path || '.', directories: (data.directories ?? []).map(item => ({ name: item.name || '', path: item.path || '' })) }
  } catch (error) { throw new UserFacingError(resolveApiErrorMessage(error, t('workspaceStorage.unavailable'))) }
}
const validName = computed(() => !!newName.value.trim() && !/[\\/:\x00\r\n]/.test(newName.value) && !['.', '..'].includes(newName.value.trim()))
function directoryFailure(error: unknown) {
  directoryError.value = resolveApiErrorMessage(error, t('workspaceStorage.unavailable'))
  if (['workspace.storage_unavailable', 'http.unauthorized', 'http.forbidden', 'http.not_found', 'user_runtime.not_found'].some(code => isApiErrorCode(error, code))) ready.value = false
}
async function createDirectory() {
  const computer = selected.value, current = epoch
  if (!computer || !ready.value || saving.value || !validName.value) return
  saving.value = true
  const path = (selection.value === '.' ? '' : selection.value + '/') + newName.value.trim()
  try {
    await postUsersMeRuntimesByIdWorkspaceStorageDirectories({ path: { id: computer.id }, body: { path }, signal: controller.signal, throwOnError: true })
    if (current === epoch && selected.value?.id === computer.id && open.value) { selection.value = path; newName.value = ''; treeKey.value++ }
  } catch (error) { if (current === epoch) directoryFailure(error) }
  finally { if (current === epoch) saving.value = false }
}
async function save() {
  const computer = selected.value, current = epoch
  if (!computer || !ready.value || saving.value) return
  saving.value = true
  try {
    await putUsersMeRuntimesByIdWorkspaceStorage({ path: { id: computer.id }, body: { path: selection.value }, signal: controller.signal, throwOnError: true })
    if (current === epoch && selected.value?.id === computer.id && open.value) { open.value = false; toast.success(t('workspaceStorage.saved')) }
  } catch (error) { if (current === epoch) directoryFailure(error) }
  finally { if (current === epoch) saving.value = false }
}
</script>

<template>
  <PageShell :title="t('workspaceStorage.title')">
    <SettingsSection>
      <SettingsRow
        v-if="loading"
        :label="t('workspaceStorage.loading')"
      />
      <SettingsRow
        v-else-if="loadError"
        :label="loadError"
      >
        <Button
          variant="outline"
          size="sm"
          @click="load"
        >
          {{ t('common.retry') }}
        </Button>
      </SettingsRow>
      <SettingsRow
        v-else-if="!computers.length"
        :label="t('workspaceStorage.empty')"
      />
      <SettingsRow
        v-for="computer in computers"
        :key="computer.id"
        :label="computer.name"
      >
        <Button
          variant="outline"
          size="sm"
          :disabled="!computer.online"
          @click="choose(computer)"
        >
          {{ t('workspaceStorage.choose') }}
        </Button>
      </SettingsRow>
    </SettingsSection>
    <FormDialogShell
      v-model:open="open"
      :title="selected?.name || t('workspaceStorage.title')"
      :submit-text="t('common.save')"
      :cancel-text="t('common.cancel')"
      :submit-disabled="!ready || saving"
      :loading="saving"
      @submit="save"
    >
      <template #body>
        <FormStack>
          <SettingsRow
            v-if="directoryError"
            :label="directoryError"
          />
          <DirectoryPicker
            v-if="ready"
            :key="treeKey"
            root-path="."
            :root-label="selected?.name || ''"
            :selected-path="selection"
            :directory-loader="directories"
            @select="selection = $event"
          />
          <FieldStack
            v-if="ready"
            :label="t('workspaceStorage.newDirectory')"
            for="nas-directory-name"
          >
            <Input
              id="nas-directory-name"
              v-model="newName"
              :disabled="saving"
            />
            <Button
              type="button"
              variant="outline"
              size="sm"
              :disabled="!validName || saving"
              @click="createDirectory"
            >
              {{ t('workspaceStorage.create') }}
            </Button>
          </FieldStack>
        </FormStack>
      </template>
    </FormDialogShell>
  </PageShell>
</template>

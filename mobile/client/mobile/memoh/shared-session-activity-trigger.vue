<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { ListTree } from 'lucide-vue-next'
import { Button } from '@felinic/ui'
import { useWorkspaceTabsStore } from '@/store/workspace-tabs'

const { t } = useI18n()
const tabs = useWorkspaceTabsStore()
function openActivity() {
  tabs.selectSidebarView('sessions')
  tabs.openMobileNav()
}
</script>

<template>
  <Button variant="ghost" size="sm" @click="openActivity">
    <ListTree />
    {{ t('chat.tasks.title') }}
  </Button>
</template>

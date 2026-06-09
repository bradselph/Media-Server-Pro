<script setup lang="ts">
// HLS Jobs (monitor) and Updates (self-update) fold into System: the HLS and
// updater config they pair with already live in the Settings panel.
const route = useRoute()
const subTabs = [
  {label: 'Status', value: 'status', icon: 'i-lucide-activity'},
  {label: 'Settings', value: 'settings', icon: 'i-lucide-settings'},
  {label: 'HLS Jobs', value: 'streaming', icon: 'i-lucide-video'},
  {label: 'Tasks & Logs', value: 'ops', icon: 'i-lucide-clock'},
  {label: 'Backups & DB', value: 'data', icon: 'i-lucide-archive'},
  {label: 'Updates', value: 'updates', icon: 'i-lucide-download'},
]
// Legacy deep-links: ?tab=settings | streaming | updates open the matching sub-tab.
const SUB_FROM_TAB: Record<string, string> = {settings: 'settings', streaming: 'streaming', updates: 'updates'}
const subTab = ref(SUB_FROM_TAB[route.query.tab as string] ?? 'status')
</script>

<template>
  <div class="space-y-4">
    <UTabs v-model="subTab" :items="subTabs" size="sm">
      <template #content="{ item }">
        <div class="pt-3">
          <AdminSystemStatusPanel v-if="item.value === 'status'"/>
          <AdminSystemSettingsPanel v-else-if="item.value === 'settings'"/>
          <AdminStreamingTab v-else-if="item.value === 'streaming'"/>
          <AdminSystemOpsPanel v-else-if="item.value === 'ops'"/>
          <AdminSystemDataPanel v-else-if="item.value === 'data'"/>
          <AdminUpdatesTab v-else-if="item.value === 'updates'"/>
        </div>
      </template>
    </UTabs>
  </div>
</template>

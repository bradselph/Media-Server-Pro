<script setup lang="ts">
const subTab = ref('status')
const subTabs = [
  { label: 'Status', value: 'status', icon: 'i-lucide-activity' },
  { label: 'Settings', value: 'settings', icon: 'i-lucide-settings' },
  { label: 'Tasks & Logs', value: 'ops', icon: 'i-lucide-clock' },
  { label: 'Backups & DB', value: 'data', icon: 'i-lucide-archive' },
]
</script>

<template>
  <div class="space-y-4">
    <UTabs v-model="subTab" :items="subTabs" size="sm" />

    <SystemStatusPanel v-if="subTab === 'status'" />
    <SystemSettingsPanel v-else-if="subTab === 'settings'" />
    <SystemOpsPanel v-else-if="subTab === 'ops'" />
    <SystemDataPanel v-else-if="subTab === 'data'" />
  </div>
</template>

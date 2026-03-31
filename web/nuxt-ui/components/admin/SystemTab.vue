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
    <UTabs v-model="subTab" :items="subTabs" size="sm">
      <template #content="{ item }">
        <div class="pt-3">
          <SystemStatusPanel v-if="item.value === 'status'" />
          <SystemSettingsPanel v-else-if="item.value === 'settings'" />
          <SystemOpsPanel v-else-if="item.value === 'ops'" />
          <SystemDataPanel v-else-if="item.value === 'data'" />
        </div>
      </template>
    </UTabs>
  </div>
</template>

<script setup lang="ts">
const subTab = ref('chat')
const subTabs = [
  { label: 'Chat', value: 'chat', icon: 'i-lucide-message-square' },
  { label: 'Settings', value: 'settings', icon: 'i-lucide-settings' },
  { label: 'Audit Log', value: 'audit', icon: 'i-lucide-scroll-text' },
]
</script>

<template>
  <div class="space-y-4">
    <UTabs v-model="subTab" :items="subTabs" size="sm">
      <template #content="{ item }">
        <div class="pt-3">
          <AdminClaudeChatPanel v-if="item.value === 'chat'" />
          <AdminClaudeSettingsPanel v-else-if="item.value === 'settings'" />
          <AdminClaudeAuditPanel v-else-if="item.value === 'audit'" />
        </div>
      </template>
    </UTabs>
  </div>
</template>

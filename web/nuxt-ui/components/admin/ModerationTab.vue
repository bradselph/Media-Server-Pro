<script setup lang="ts">
// Shell tab: content-safety / classification. "Mature & Tagging" is the former
// Content tab (scanner, validator, auto-tag rules); "Discovery & AI" is the
// former Discovery tab (auto-discovery, rec-engine, HF classify).
// Media reports live here too, alongside the actions that resolve them.
const subTabs = [
  {label: 'Mature & Tagging', value: 'content', icon: 'i-lucide-scan'},
  {label: 'Discovery & AI', value: 'discovery', icon: 'i-lucide-compass'},
  {label: 'Reports', value: 'reports', icon: 'i-lucide-flag'},
]
// Legacy deep-links continue to open the matching sub-tab.
const subTab = useSubTabRoute({content: 'content', discovery: 'discovery', reports: 'reports'}, 'content')
</script>

<template>
  <div class="space-y-4">
    <UTabs v-model="subTab" :items="subTabs" size="sm">
      <template #content="{ item }">
        <div class="pt-3">
          <AdminContentTab v-if="item.value === 'content'"/>
          <AdminDiscoveryTab v-else-if="item.value === 'discovery'"/>
          <AdminMediaReportsPanel v-else-if="item.value === 'reports'"/>
        </div>
      </template>
    </UTabs>
  </div>
</template>

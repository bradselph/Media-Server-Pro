<script setup lang="ts">
const subTab = ref('remote')
const subTabs = [
  { label: 'Remote Sources', value: 'remote', icon: 'i-lucide-server' },
  { label: 'Crawler & Extractor', value: 'crawler', icon: 'i-lucide-globe' },
  { label: 'Receiver', value: 'receiver', icon: 'i-lucide-radio-tower' },
]
</script>

<template>
  <div class="space-y-4">
    <UTabs v-model="subTab" :items="subTabs" orientation="horizontal" class="w-full">
      <template #content="{ item }">
        <div class="pt-3">
          <SourcesRemotePanel v-if="item.value === 'remote'" />
          <SourcesCrawlerPanel v-else-if="item.value === 'crawler'" />
          <SourcesReceiverPanel v-else-if="item.value === 'receiver'" />
        </div>
      </template>
    </UTabs>
  </div>
</template>

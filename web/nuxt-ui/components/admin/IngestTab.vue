<script setup lang="ts">
// Shell tab: every way media enters the library. The downloader fetches from
// URLs; the remote panels (synced HTTP sources, extractor, receiver/follower)
// were previously the Sources tab — flattened in here so the whole ingestion
// surface is one tab with a single sub-tab bar (no nesting).
const subTabs = [
  {label: 'Downloader', value: 'downloader', icon: 'i-lucide-cloud-download'},
  {label: 'Remote Sources', value: 'remote', icon: 'i-lucide-server'},
  {label: 'Extractor', value: 'extractor', icon: 'i-lucide-download-cloud'},
  {label: 'Receiver', value: 'receiver', icon: 'i-lucide-radio-tower'},
]
// Legacy deep-links: ?tab=downloader and ?tab=sources land on the right sub-tab.
const subTab = useSubTabRoute({downloader: 'downloader', sources: 'remote'}, 'downloader')
</script>

<template>
  <div class="space-y-4">
    <UTabs v-model="subTab" :items="subTabs" size="sm">
      <template #content="{ item }">
        <div class="pt-3">
          <AdminDownloaderTab v-if="item.value === 'downloader'"/>
          <AdminSourcesRemotePanel v-else-if="item.value === 'remote'"/>
          <AdminSourcesExtractorPanel v-else-if="item.value === 'extractor'"/>
          <AdminSourcesReceiverPanel v-else-if="item.value === 'receiver'"/>
        </div>
      </template>
    </UTabs>
  </div>
</template>

<script setup lang="ts">
// Shell tab: groups the media library CRUD and the duplicate-resolution view,
// both of which act on the same MediaItem records. The library panel keeps the
// original MediaTab body (incl. the ?edit= deep-link handling).
const route = useRoute()
const subTabs = [
  {label: 'All Media', value: 'library', icon: 'i-lucide-film'},
  {label: 'Duplicates', value: 'duplicates', icon: 'i-lucide-copy-x'},
]
// Legacy deep-link: /admin?tab=duplicates opens the Duplicates sub-tab.
const SUB_FROM_TAB: Record<string, string> = {duplicates: 'duplicates'}
const subTab = ref(SUB_FROM_TAB[route.query.tab as string] ?? 'library')
</script>

<template>
  <div class="space-y-4">
    <UTabs v-model="subTab" :items="subTabs" size="sm">
      <template #content="{ item }">
        <div class="pt-3">
          <AdminMediaLibraryPanel v-if="item.value === 'library'"/>
          <AdminDuplicatesTab v-else-if="item.value === 'duplicates'"/>
        </div>
      </template>
    </UTabs>
  </div>
</template>

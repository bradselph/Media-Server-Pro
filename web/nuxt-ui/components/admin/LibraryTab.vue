<script setup lang="ts">
// Shell tab: collections and playlists are both admin-managed orderings of
// media items with identical CRUD shape, so they share one tab.
const route = useRoute()
const subTabs = [
  {label: 'Collections', value: 'collections', icon: 'i-lucide-layers'},
  {label: 'Playlists', value: 'playlists', icon: 'i-lucide-list-music'},
]
// Legacy deep-links: /admin?tab=collections | ?tab=playlists open their sub-tab.
const SUB_FROM_TAB: Record<string, string> = {collections: 'collections', playlists: 'playlists'}
const subTab = ref(SUB_FROM_TAB[route.query.tab as string] ?? 'collections')
</script>

<template>
  <div class="space-y-4">
    <UTabs v-model="subTab" :items="subTabs" size="sm">
      <template #content="{ item }">
        <div class="pt-3">
          <AdminCollectionsTab v-if="item.value === 'collections'"/>
          <AdminPlaylistsTab v-else-if="item.value === 'playlists'"/>
        </div>
      </template>
    </UTabs>
  </div>
</template>

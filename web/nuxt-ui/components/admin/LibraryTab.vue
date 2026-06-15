<script setup lang="ts">
// Shell tab: categories and playlists are both admin-managed orderings of
// media items with identical CRUD shape, so they share one tab.
const route = useRoute()
const subTabs = [
  {label: 'Categories', value: 'categories', icon: 'i-lucide-layers'},
  {label: 'Playlists', value: 'playlists', icon: 'i-lucide-list-music'},
]
// Deep-links: /admin?tab=categories | ?tab=playlists open their sub-tab.
// ?tab=collections is kept as a legacy alias for the renamed categories tab.
const SUB_FROM_TAB: Record<string, string> = {
  categories: 'categories',
  collections: 'categories',
  playlists: 'playlists',
}
const subTab = ref(SUB_FROM_TAB[route.query.tab as string] ?? 'categories')
</script>

<template>
  <div class="space-y-4">
    <UTabs v-model="subTab" :items="subTabs" size="sm">
      <template #content="{ item }">
        <div class="pt-3">
          <AdminCategoriesTab v-if="item.value === 'categories'"/>
          <AdminPlaylistsTab v-else-if="item.value === 'playlists'"/>
        </div>
      </template>
    </UTabs>
  </div>
</template>

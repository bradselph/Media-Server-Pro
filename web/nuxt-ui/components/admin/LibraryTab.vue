<script setup lang="ts">
// Shell tab: categories and playlists are both admin-managed orderings of
// media items with identical CRUD shape, so they share one tab.
const subTabs = [
  {label: 'Categories', value: 'categories', icon: 'i-lucide-layers'},
  {label: 'Playlists', value: 'playlists', icon: 'i-lucide-list-music'},
]
// Deep-links: /admin?tab=categories | ?tab=playlists open their sub-tab.
// ?tab=collections is kept as a legacy alias for the renamed categories tab.
const subTab = useSubTabRoute({categories: 'categories', collections: 'categories', playlists: 'playlists'}, 'categories')
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

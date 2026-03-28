<script setup lang="ts">
import type { FavoriteItem, MediaItem } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'
import { useFavoritesApi } from '~/composables/useApiEndpoints'

definePageMeta({ layout: 'default', title: 'Favorites', middleware: 'auth' })

const favoritesApi = useFavoritesApi()
const mediaApi = useMediaApi()
const authStore = useAuthStore()
const toast = useToast()

const favorites = ref<FavoriteItem[]>([])
const mediaMap = ref<Record<string, MediaItem>>({})
const loading = ref(true)
const removing = ref<Set<string>>(new Set())

async function load() {
  loading.value = true
  try {
    favorites.value = (await favoritesApi.list()) ?? []
    // Resolve media items by stable ID for display
    const ids = favorites.value.map(f => f.media_id)
    if (ids.length > 0) {
      const results = await Promise.allSettled(ids.map(id => mediaApi.getById(id)))
      const newMap: Record<string, MediaItem> = {}
      results.forEach((r, i) => {
        if (r.status === 'fulfilled' && r.value) newMap[ids[i]] = r.value
      })
      mediaMap.value = newMap
    }
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load favorites', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally {
    loading.value = false
  }
}

async function removeFavorite(mediaId: string) {
  const next = new Set(removing.value)
  next.add(mediaId)
  removing.value = next
  try {
    await favoritesApi.remove(mediaId)
    favorites.value = favorites.value.filter(f => f.media_id !== mediaId)
    toast.add({ title: 'Removed from favorites', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    const cleared = new Set(removing.value)
    cleared.delete(mediaId)
    removing.value = cleared
  }
}

function getThumbnailUrl(item: MediaItem): string | null {
  if (!item?.id) return null
  return `/api/thumbnails/${encodeURIComponent(item.id)}`
}

onMounted(load)
</script>

<template>
  <UContainer class="py-6 max-w-5xl">
    <div class="flex items-center gap-2 mb-6">
      <UIcon name="i-lucide-heart" class="size-5 text-primary" />
      <h1 class="text-xl font-semibold">Favorites</h1>
      <UBadge v-if="favorites.length > 0" :label="String(favorites.length)" color="neutral" variant="subtle" size="xs" />
    </div>

    <div v-if="loading" class="flex justify-center py-16">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-8 text-primary" />
    </div>

    <div v-else-if="favorites.length === 0" class="text-center py-16 text-muted">
      <UIcon name="i-lucide-heart" class="size-10 mb-3 mx-auto opacity-40" />
      <p class="text-lg font-medium">No favorites yet</p>
      <p class="text-sm mt-1">Browse media and click the heart icon to save items here.</p>
      <UButton class="mt-4" label="Browse Media" to="/" variant="outline" />
    </div>

    <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
      <div
        v-for="fav in favorites"
        :key="fav.id"
        class="group relative"
      >
        <NuxtLink :to="`/player?id=${encodeURIComponent(fav.media_id)}`" class="block">
          <div class="aspect-video relative rounded overflow-hidden bg-muted mb-1.5">
            <img
              v-if="mediaMap[fav.media_id]"
              :src="getThumbnailUrl(mediaMap[fav.media_id])!"
              :alt="getDisplayTitle(mediaMap[fav.media_id])"
              class="w-full h-full object-cover"
              loading="lazy"
            />
            <div v-else class="w-full h-full flex items-center justify-center">
              <UIcon name="i-lucide-film" class="size-8 text-muted" />
            </div>
            <!-- Duration badge -->
            <div
              v-if="mediaMap[fav.media_id]?.duration"
              class="absolute bottom-1 right-1 bg-black/70 text-white text-xs px-1 rounded font-mono"
            >
              {{ (() => { const d = mediaMap[fav.media_id]!.duration; const m = Math.floor(d/60); const s = Math.floor(d%60); return `${m}:${s.toString().padStart(2,'0')}` })() }}
            </div>
          </div>
          <p class="text-sm font-medium truncate" :title="mediaMap[fav.media_id] ? getDisplayTitle(mediaMap[fav.media_id]) : fav.media_path">
            {{ mediaMap[fav.media_id] ? getDisplayTitle(mediaMap[fav.media_id]) : fav.media_path.split('/').pop() }}
          </p>
          <p class="text-xs text-muted">Saved {{ new Date(fav.added_at).toLocaleDateString() }}</p>
        </NuxtLink>
        <button
          class="absolute top-1 right-1 p-0.5 rounded-full bg-black/60 opacity-0 group-hover:opacity-100 transition-opacity"
          aria-label="Remove from favorites"
          :disabled="removing.has(fav.media_id)"
          @click.prevent.stop="removeFavorite(fav.media_id)"
        >
          <UIcon name="i-lucide-x" class="size-3.5 text-white" />
        </button>
      </div>
    </div>
  </UContainer>
</template>

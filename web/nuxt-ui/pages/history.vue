<script setup lang="ts">
import type { WatchHistoryItem, MediaItem } from '~/types/api'
import { useWatchHistoryApi } from '~/composables/useApiEndpoints'
import { getDisplayTitle } from '~/utils/mediaTitle'
import { formatDuration, formatRelativeDate } from '~/utils/format'

definePageMeta({ layout: 'default', title: 'Watch History', middleware: 'auth' })

const watchHistoryApi = useWatchHistoryApi()
const mediaApi = useMediaApi()
const authStore = useAuthStore()
const toast = useToast()

const history = ref<WatchHistoryItem[]>([])
const mediaMap = ref<Record<string, MediaItem>>({})
const loading = ref(true)
const filter = ref<'all' | 'in_progress' | 'completed'>('all')
const removingId = ref<string | null>(null)
const confirmClear = ref(false)
const clearing = ref(false)
const failedThumbs = reactive(new Set<string>())
let hasFetched = false

const filtered = computed(() => {
  if (filter.value === 'completed') return history.value.filter(h => h.completed)
  if (filter.value === 'in_progress') return history.value.filter(h => !h.completed)
  return history.value
})

async function load() {
  hasFetched = true
  loading.value = true
  try {
    history.value = (await watchHistoryApi.list()) ?? []
    const ids = [...new Set(history.value.map(h => h.media_id))]
    if (ids.length > 0) {
      const res = await mediaApi.getBatch(ids)
      mediaMap.value = res?.items ?? {}
    }
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load history', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally {
    loading.value = false
  }
}

async function remove(mediaId: string) {
  if (removingId.value) return
  removingId.value = mediaId
  try {
    await watchHistoryApi.remove(mediaId)
    history.value = history.value.filter(h => h.media_id !== mediaId)
    toast.add({ title: 'Removed from history', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to remove', color: 'error', icon: 'i-lucide-x' })
  } finally {
    removingId.value = null
  }
}

async function clearAll() {
  clearing.value = true
  try {
    await watchHistoryApi.clear()
    history.value = []
    mediaMap.value = {}
    toast.add({ title: 'Watch history cleared', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to clear history', color: 'error', icon: 'i-lucide-x' })
  } finally {
    clearing.value = false
    confirmClear.value = false
  }
}

function resumeUrl(item: WatchHistoryItem): string {
  const pos = Math.floor(item.position)
  return pos > 5
    ? `/player?id=${encodeURIComponent(item.media_id)}&t=${pos}`
    : `/player?id=${encodeURIComponent(item.media_id)}`
}

function thumbUrl(mediaId: string): string {
  return mediaApi.getThumbnailUrl(mediaId)
}

onMounted(() => {
  if (!authStore.isLoading && authStore.user) load()
})
watch(() => authStore.user, (user) => {
  if (user && !hasFetched) load()
})
</script>

<template>
  <UContainer class="py-6 max-w-5xl">
    <!-- Header -->
    <div class="flex items-center justify-between flex-wrap gap-3 mb-6">
      <div class="flex items-center gap-2">
        <UIcon name="i-lucide-history" class="size-5 text-primary" />
        <h1 class="text-xl font-semibold">Watch History</h1>
        <UBadge v-if="history.length > 0" :label="String(history.length)" color="neutral" variant="subtle" size="xs" />
      </div>
      <div class="flex gap-2">
        <USelect
          v-model="filter"
          :items="[
            { label: 'All', value: 'all' },
            { label: 'In Progress', value: 'in_progress' },
            { label: 'Completed', value: 'completed' },
          ]"
          size="sm"
          class="w-36"
        />
        <UButton
          v-if="history.length > 0"
          icon="i-lucide-trash-2"
          label="Clear All"
          size="sm"
          color="error"
          variant="outline"
          @click="confirmClear = true"
        />
      </div>
    </div>

    <!-- Loading -->
    <MediaCardSkeleton v-if="loading" :count="10" />

    <!-- Empty -->
    <div v-else-if="filtered.length === 0" class="text-center py-16 text-muted">
      <UIcon name="i-lucide-history" class="size-10 mb-3 mx-auto opacity-40" />
      <p class="text-lg font-medium">
        {{ history.length === 0 ? 'No watch history yet' : 'No items match this filter' }}
      </p>
      <p class="text-sm mt-1">Items appear here after you start watching them.</p>
      <UButton class="mt-4" label="Browse Media" to="/" variant="outline" />
    </div>

    <!-- History grid -->
    <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
      <div
        v-for="item in filtered"
        :key="item.media_id"
        class="group relative"
      >
        <NuxtLink :to="resumeUrl(item)" class="block">
          <!-- Thumbnail -->
          <div class="aspect-video relative rounded-lg overflow-hidden bg-muted mb-1.5 media-card-lift scanline-thumb">
            <img
              v-if="!failedThumbs.has(item.media_id)"
              :src="thumbUrl(item.media_id)"
              :alt="item.media_name"
              class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
              loading="lazy"
              @error="failedThumbs.add(item.media_id)"
            />
            <div v-else-if="mediaMap[item.media_id]?.type === 'audio'" class="w-full h-full flex items-center justify-center bg-linear-to-br from-primary/10 to-primary/5">
              <AudioBars size="sm" :bars="5" />
            </div>
            <div v-else class="w-full h-full flex items-center justify-center">
              <UIcon name="i-lucide-film" class="size-8 text-muted" />
            </div>

            <!-- Duration / completion badge -->
            <div class="absolute bottom-1 right-1 bg-black/70 text-white text-xs px-1 rounded font-mono">
              <template v-if="item.completed">
                Watched
              </template>
              <template v-else>
                {{ formatDuration(Math.floor(item.position)) }} / {{ formatDuration(Math.floor(item.duration)) }}
              </template>
            </div>

            <!-- Progress bar -->
            <div v-if="!item.completed && item.progress > 0" class="absolute bottom-0 left-0 right-0 h-0.5 bg-black/40">
              <div
                class="h-full bg-primary transition-all"
                :style="{ width: `${Math.min(100, item.progress * 100)}%` }"
              />
            </div>

            <!-- Completed overlay -->
            <div v-if="item.completed" class="absolute inset-0 bg-black/40 flex items-center justify-center">
              <UIcon name="i-lucide-check-circle" class="size-6 text-success" />
            </div>
          </div>

          <!-- Title -->
          <p
            class="text-sm font-semibold truncate"
            :title="mediaMap[item.media_id] ? getDisplayTitle(mediaMap[item.media_id]) : item.media_name"
          >
            {{ mediaMap[item.media_id] ? getDisplayTitle(mediaMap[item.media_id]) : (item.media_name || '—') }}
          </p>

          <!-- Watched-at timestamp -->
          <p v-if="item.watched_at" class="text-xs text-muted" :title="new Date(item.watched_at).toLocaleString()">
            {{ formatRelativeDate(item.watched_at) }}
          </p>
        </NuxtLink>

        <!-- Remove button -->
        <button
          class="absolute top-1 right-1 p-0.5 rounded-full bg-black/60 opacity-0 group-hover:opacity-100 transition-opacity"
          aria-label="Remove from history"
          :disabled="removingId === item.media_id"
          @click.prevent.stop="remove(item.media_id)"
        >
          <UIcon name="i-lucide-x" class="size-3.5 text-white" />
        </button>
      </div>
    </div>

    <!-- Clear all confirmation -->
    <UModal
      :open="confirmClear"
      title="Clear Watch History"
      @update:open="val => { if (!val) confirmClear = false }"
    >
      <template #body>
        <p>This will permanently remove all {{ history.length }} item{{ history.length !== 1 ? 's' : '' }} from your watch history. This cannot be undone.</p>
      </template>
      <template #footer>
        <UButton color="error" label="Clear All" :loading="clearing" @click="clearAll" />
        <UButton variant="ghost" color="neutral" label="Cancel" @click="confirmClear = false" />
      </template>
    </UModal>
  </UContainer>
</template>

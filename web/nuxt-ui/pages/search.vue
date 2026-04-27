<script setup lang="ts">
import type { MediaItem, Playlist } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'
import { formatDuration, formatBytes } from '~/utils/format'

definePageMeta({ layout: 'default', title: 'Search' })

const route = useRoute()
const router = useRouter()
const mediaApi = useMediaApi()
const playlistApi = usePlaylistApi()
const authStore = useAuthStore()
const toast = useToast()

const query = computed(() => (route.query.q as string | undefined)?.trim() ?? '')
const items = ref<MediaItem[]>([])
const loading = ref(false)
const error = ref('')
const localQuery = ref(query.value)
const lastFetchedFor = ref<string | null>(null)

async function runSearch(q: string) {
  if (!q) {
    items.value = []
    lastFetchedFor.value = ''
    error.value = ''
    return
  }
  loading.value = true
  error.value = ''
  try {
    const res = await mediaApi.list({ search: q, limit: 60 })
    items.value = res?.items ?? []
    lastFetchedFor.value = q
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Search failed'
    items.value = []
  } finally {
    loading.value = false
  }
}

watch(query, (q) => {
  localQuery.value = q
  if (q !== lastFetchedFor.value) runSearch(q)
}, { immediate: true })

function submitInline() {
  const q = localQuery.value.trim()
  if (!q) return
  // Push to /search?q= even if we're already there so the URL stays in
  // sync with the input — the watch on `query` handles the actual fetch.
  router.replace({ path: '/search', query: { q } })
}

// ── Multi-select + bulk-add (parity with the Home page) ─────────────────
const selectionMode = ref(false)
const selectedIds = ref<Set<string>>(new Set())
const myPlaylists = ref<Playlist[]>([])
const bulkAddPlaylistId = ref<string | undefined>(undefined)
const bulkAdding = ref(false)

function toggleSelectionMode() {
  selectionMode.value = !selectionMode.value
  if (!selectionMode.value) selectedIds.value = new Set()
}

function toggleSelect(id: string) {
  const next = new Set(selectedIds.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selectedIds.value = next
}

async function loadMyPlaylists() {
  if (!authStore.isLoggedIn || myPlaylists.value.length > 0) return
  try {
    myPlaylists.value = (await playlistApi.list()) ?? []
  } catch { /* non-critical */ }
}

watch(selectionMode, (on) => { if (on) loadMyPlaylists() })

const playlistOptions = computed(() => myPlaylists.value.map(p => ({ value: p.id, label: p.name })))

async function bulkAddToPlaylist() {
  if (!bulkAddPlaylistId.value || selectedIds.value.size === 0) return
  bulkAdding.value = true
  const ids = [...selectedIds.value]
  let added = 0
  for (const id of ids) {
    try { await playlistApi.addItem(bulkAddPlaylistId.value, id); added++ }
    catch { /* skip duplicates */ }
  }
  toast.add({
    title: `Added ${added} item${added === 1 ? '' : 's'} to playlist`,
    color: 'success',
    icon: 'i-lucide-check',
  })
  bulkAddPlaylistId.value = undefined
  selectedIds.value = new Set()
  selectionMode.value = false
  bulkAdding.value = false
}
</script>

<template>
  <UContainer class="py-6 max-w-6xl space-y-5">
    <!-- Header / search bar -->
    <div class="flex items-center gap-3 flex-wrap">
      <h1 class="text-xl font-semibold flex items-center gap-2">
        <UIcon name="i-lucide-search" class="size-5 text-primary" />
        Search
      </h1>
      <form class="flex-1 min-w-[240px] max-w-xl" @submit.prevent="submitInline">
        <UInput
          v-model="localQuery"
          icon="i-lucide-search"
          placeholder="Search titles, tags…"
          size="md"
          type="search"
          class="w-full"
          autofocus
        />
      </form>
      <UButton
        v-if="query && authStore.isLoggedIn && items.length > 0"
        :icon="selectionMode ? 'i-lucide-x' : 'i-lucide-check-square'"
        :label="selectionMode ? 'Cancel' : 'Select'"
        variant="outline"
        color="neutral"
        size="sm"
        @click="toggleSelectionMode"
      />
    </div>

    <!-- Bulk-add bar (sticky) -->
    <div
      v-if="selectionMode && authStore.isLoggedIn"
      class="sticky top-14 z-30 bg-elevated border border-default rounded-lg px-3 py-2 flex items-center gap-3 flex-wrap"
    >
      <span class="text-sm text-muted">{{ selectedIds.size }} selected</span>
      <USelectMenu
        v-model="bulkAddPlaylistId"
        :items="playlistOptions"
        value-key="value"
        placeholder="Add to playlist…"
        size="sm"
        class="min-w-[200px]"
      />
      <UButton
        icon="i-lucide-plus"
        label="Add"
        size="sm"
        :loading="bulkAdding"
        :disabled="!bulkAddPlaylistId || selectedIds.size === 0"
        @click="bulkAddToPlaylist"
      />
      <UButton
        v-if="items.length > 0"
        variant="ghost"
        color="neutral"
        size="sm"
        label="Select All"
        @click="selectedIds = new Set(items.map(i => i.id))"
      />
      <UButton
        v-if="selectedIds.size > 0"
        variant="ghost"
        color="neutral"
        size="sm"
        label="Clear"
        @click="selectedIds = new Set()"
      />
    </div>

    <!-- States -->
    <div v-if="!query" class="text-center py-16 text-muted">
      <UIcon name="i-lucide-search" class="size-10 mb-3 mx-auto opacity-40" />
      <p>Type a query above and press Enter to search the library.</p>
    </div>

    <MediaCardSkeleton v-else-if="loading" :count="10" />

    <UAlert v-else-if="error" :title="error" color="error" icon="i-lucide-alert-circle" />

    <div v-else-if="items.length === 0" class="text-center py-16 text-muted">
      <UIcon name="i-lucide-search-x" class="size-10 mb-3 mx-auto opacity-40" />
      <p class="font-medium">No results for <em class="text-default">{{ query }}</em>.</p>
      <p class="text-sm mt-1">Try fewer keywords or check spelling.</p>
    </div>

    <!-- Result grid -->
    <div v-else class="space-y-3">
      <p class="text-xs text-muted">
        {{ items.length }} result{{ items.length === 1 ? '' : 's' }} for
        <em class="text-default">{{ query }}</em>
      </p>
      <div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
        <component
          :is="selectionMode ? 'div' : resolveComponent('NuxtLink')"
          v-for="item in items"
          :key="item.id"
          :to="selectionMode ? undefined : `/player?id=${encodeURIComponent(item.id)}`"
          :class="[
            'group block cursor-pointer',
            selectionMode && selectedIds.has(item.id) ? 'ring-2 ring-primary rounded-lg' : '',
          ]"
          @click="selectionMode ? toggleSelect(item.id) : undefined"
        >
          <div class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-1.5 media-card-lift scanline-thumb">
            <img
              v-if="item.thumbnail_url || true"
              :src="mediaApi.getThumbnailUrl(item.id)"
              :alt="getDisplayTitle(item)"
              width="320"
              height="180"
              class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
              loading="lazy"
              @error="($event.target as HTMLImageElement).style.display='none'"
            />
            <div v-if="selectionMode" class="absolute top-1.5 left-1.5 z-10">
              <div :class="['w-5 h-5 rounded border-2 flex items-center justify-center', selectedIds.has(item.id) ? 'bg-primary border-primary' : 'bg-black/40 border-white/70']">
                <UIcon v-if="selectedIds.has(item.id)" name="i-lucide-check" class="size-3 text-white" />
              </div>
            </div>
            <div v-if="item.duration" class="absolute bottom-1 right-1 bg-black/70 text-white text-[10px] font-mono px-1 rounded">
              {{ formatDuration(item.duration) }}
            </div>
            <div v-if="item.is_mature" class="absolute top-1 right-1 bg-black/70 text-white text-[9px] font-bold px-1 rounded">18+</div>
          </div>
          <p class="text-xs font-medium truncate group-hover:text-primary transition-colors" :title="getDisplayTitle(item)">
            {{ getDisplayTitle(item) }}
          </p>
          <p class="text-[10px] text-muted truncate">
            {{ item.category || item.type }}<span v-if="item.size"> · {{ formatBytes(item.size) }}</span>
          </p>
        </component>
      </div>
    </div>
  </UContainer>
</template>

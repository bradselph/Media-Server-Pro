<script setup lang="ts">
import type { MediaItem, Playlist } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'
import { formatDuration, formatBytes } from '~/utils/format'
import { useSavedSearchesApi } from '~/composables/useApiEndpoints'
import { useRecentSearches } from '~/composables/useRecentSearches'
import { highlightMatch } from '~/utils/highlight'
import { blurHashBgStyle } from '~/utils/blurhash'

definePageMeta({ layout: 'default', title: 'Search' })

const route = useRoute()
const router = useRouter()
const mediaApi = useMediaApi()
const playlistApi = usePlaylistApi()
const playbackApi = usePlaybackApi()
const savedSearchesApi = useSavedSearchesApi()
const authStore = useAuthStore()
const toast = useToast()

const query = computed(() => (route.query.q as string | undefined)?.trim() ?? '')
const items = ref<MediaItem[]>([])
const playbackProgress = ref<Record<string, number>>({})
const loading = ref(false)
const error = ref('')
const localQuery = ref(query.value)
const lastFetchedFor = ref<string | null>(null)
const PAGE_SIZE = 60
const total = ref(0)
const page = ref(1)
const loadingMore = ref(false)
const hasMore = computed(() => items.value.length < total.value)

// Request token discards stale results when the user types again before the
// previous /api/media response lands — the equivalent of an abort without
// plumbing AbortController through the api wrapper.
let searchToken = 0

// Watched marker — best-effort batch fetch of playback positions for the given
// items so we can render the "Watched" badge / progress bar. Merges into the
// existing map so appended pages keep earlier results' progress.
async function loadPositions(list: MediaItem[], token: number) {
  if (!authStore.isLoggedIn || list.length === 0) return
  try {
    const r = await playbackApi.getBatchPositions(list.map(i => i.id))
    if (token !== searchToken) return
    const positions = r?.positions ?? {}
    const next: Record<string, number> = { ...playbackProgress.value }
    for (const item of list) {
      const pos = positions[item.id]
      if (pos && item.duration > 0) next[item.id] = pos / item.duration
    }
    playbackProgress.value = next
  } catch { /* non-critical */ }
}

async function runSearch(q: string) {
  const token = ++searchToken
  if (!q) {
    items.value = []
    total.value = 0
    page.value = 1
    lastFetchedFor.value = ''
    error.value = ''
    loading.value = false
    return
  }
  loading.value = true
  error.value = ''
  try {
    const res = await mediaApi.list({ search: q, limit: PAGE_SIZE })
    if (token !== searchToken) return // stale — newer query already in flight
    items.value = res?.items ?? []
    total.value = res?.total_items ?? items.value.length
    page.value = 1
    lastFetchedFor.value = q
    pushRecent(q)
    playbackProgress.value = {}
    await loadPositions(items.value, token)
  } catch (e: unknown) {
    if (token !== searchToken) return
    error.value = e instanceof Error ? e.message : 'Search failed'
    items.value = []
    total.value = 0
  } finally {
    if (token === searchToken) loading.value = false
  }
}

// Append the next page of results for the current query.
async function loadMore() {
  const q = lastFetchedFor.value
  if (!q || loadingMore.value || !hasMore.value) return
  const token = searchToken
  loadingMore.value = true
  try {
    const res = await mediaApi.list({ search: q, limit: PAGE_SIZE, page: page.value + 1 })
    if (token !== searchToken) return // a newer search superseded this load
    const more = res?.items ?? []
    items.value = [...items.value, ...more]
    total.value = res?.total_items ?? total.value
    page.value += 1
    await loadPositions(more, token)
  } catch { /* non-critical — leave existing results in place */ } finally {
    loadingMore.value = false
  }
}

// ── Recent searches (checklist §7) ────────────────────────────────────
const { recent, push: pushRecent, remove: removeRecent, clear: clearRecent } = useRecentSearches()

function applyRecent(q: string) {
  localQuery.value = q
  router.replace({ path: '/search', query: { q } })
}

watch(query, (q) => {
  localQuery.value = q
  if (q !== lastFetchedFor.value) runSearch(q)
}, { immediate: true })

// Debounce while-typing input — pushes to /search?q= 250ms after the user
// stops typing, which trips the `query` watcher above and fires runSearch.
const SEARCH_DEBOUNCE_MS = 250
let inputDebounce: ReturnType<typeof setTimeout> | null = null

watch(localQuery, (next, prev) => {
  if (next === prev) return
  if (inputDebounce) clearTimeout(inputDebounce)
  inputDebounce = setTimeout(() => {
    inputDebounce = null
    const q = next.trim()
    if (q === query.value) return
    if (!q) {
      router.replace({ path: '/search', query: {} })
    }
    else {
      router.replace({ path: '/search', query: { q } })
    }
  }, SEARCH_DEBOUNCE_MS)
})

onBeforeUnmount(() => {
  if (inputDebounce) clearTimeout(inputDebounce)
})

function submitInline() {
  if (inputDebounce) { clearTimeout(inputDebounce); inputDebounce = null }
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

// ── Save this search (retention plan B.5) ─────────────────────────────
const savingSearch = ref(false)
async function saveCurrentSearch() {
  const q = query.value.trim()
  if (!q || !authStore.isLoggedIn) return
  savingSearch.value = true
  try {
    await savedSearchesApi.create({ name: q, query: q, tag_mode: 'or' })
    toast.add({
      title: 'Search saved',
      description: 'You\'ll see new matches on the homepage when they show up.',
      color: 'success',
      icon: 'i-lucide-bookmark-check',
    })
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : 'Could not save search'
    toast.add({ title: msg, color: 'error', icon: 'i-lucide-alert-circle' })
  } finally {
    savingSearch.value = false
  }
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
      <UButton
        v-if="query && authStore.isLoggedIn && items.length > 0"
        icon="i-lucide-bookmark-plus"
        label="Save this search"
        variant="outline"
        color="primary"
        size="sm"
        :loading="savingSearch"
        title="Save so you can spot new matches on the home page"
        @click="saveCurrentSearch"
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
    <div v-if="!query" class="space-y-6">
      <div class="text-center py-10 text-muted">
        <UIcon name="i-lucide-search" class="size-10 mb-3 mx-auto opacity-40" />
        <p>Type a query above and press Enter to search the library.</p>
      </div>
      <div v-if="recent.length > 0" class="max-w-xl mx-auto">
        <div class="flex items-center justify-between mb-2">
          <h2 class="text-sm font-semibold text-default flex items-center gap-1.5">
            <UIcon name="i-lucide-clock" class="size-4 text-muted" />
            Recent searches
          </h2>
          <button
            type="button"
            class="text-xs text-muted hover:text-default transition-colors"
            @click="clearRecent"
          >
            Clear all
          </button>
        </div>
        <ul class="flex flex-wrap gap-2">
          <li v-for="r in recent" :key="r" class="group flex items-center bg-elevated border border-default rounded-full overflow-hidden">
            <button
              type="button"
              class="px-3 py-1 text-sm hover:text-primary transition-colors"
              @click="applyRecent(r)"
            >
              {{ r }}
            </button>
            <button
              type="button"
              class="px-2 py-1 text-muted hover:text-error transition-colors opacity-0 group-hover:opacity-100"
              :aria-label="`Remove ${r} from recent searches`"
              @click="removeRecent(r)"
            >
              <UIcon name="i-lucide-x" class="size-3.5" />
            </button>
          </li>
        </ul>
      </div>
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
        Showing {{ items.length }} of {{ total }} result{{ total === 1 ? '' : 's' }} for
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
          <div
            class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-1.5 media-card-lift scanline-thumb"
            :style="item.type !== 'audio' ? blurHashBgStyle(item.blur_hash) : {}"
          >
            <HoverPreviewImg
              :media-id="item.id"
              :src="mediaApi.getThumbnailUrl(item.id)"
              :alt="getDisplayTitle(item)"
              :width="320"
              :height="180"
              img-class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
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
            <div
              v-if="playbackProgress[item.id] && (playbackProgress[item.id] ?? 0) < 0.9"
              class="absolute bottom-0 left-0 right-0 h-1 bg-white/20"
            >
              <div
                class="h-full bg-primary"
                :style="{ width: `${Math.min(100, Math.round((playbackProgress[item.id] ?? 0) * 100))}%` }"
              />
            </div>
            <div
              v-if="(playbackProgress[item.id] ?? 0) >= 0.9"
              class="absolute bottom-1 left-1 flex items-center gap-1 bg-emerald-600/85 text-white text-[10px] font-semibold px-1.5 py-0.5 rounded shadow-sm"
              :title="`Watched (${Math.round((playbackProgress[item.id] ?? 0) * 100)}%)`"
            >
              <UIcon name="i-lucide-check" class="size-3" />
              <span>Watched</span>
            </div>
          </div>
          <p
            class="text-xs font-medium truncate group-hover:text-primary transition-colors"
            :title="getDisplayTitle(item)"
            v-html="highlightMatch(getDisplayTitle(item), query)"
          />
          <p class="text-[10px] text-muted truncate">
            <span v-html="highlightMatch(item.category || item.type, query)" />
            <span v-if="item.size"> · {{ formatBytes(item.size) }}</span>
          </p>
        </component>
      </div>
      <div v-if="hasMore" class="flex justify-center pt-2">
        <UButton
          :loading="loadingMore"
          color="neutral"
          variant="soft"
          icon="i-lucide-chevron-down"
          :label="loadingMore ? 'Loading…' : `Load more (${total - items.length} remaining)`"
          @click="loadMore"
        />
      </div>
    </div>
  </UContainer>
</template>

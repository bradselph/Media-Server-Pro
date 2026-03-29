<script setup lang="ts">
import type { MediaItem, MediaCategory, Suggestion, RecentItem, NewSinceResponse, OnDeckItem } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'
import { useApiEndpoints, useFavoritesApi } from '~/composables/useApiEndpoints'
import { formatDuration } from '~/utils/format'
import { blurHashToDataUrl } from '~/utils/blurhash'

const TYPE_OPTIONS = [
  { label: 'All Types', value: 'all' },
  { label: 'Video', value: 'video' },
  { label: 'Audio', value: 'audio' },
  { label: 'Image', value: 'image' },
]

const MIN_RATING_OPTIONS = [
  { label: 'Any Rating', value: 0 },
  { label: '★ 1+', value: 1 },
  { label: '★★ 2+', value: 2 },
  { label: '★★★ 3+', value: 3 },
  { label: '★★★★ 4+', value: 4 },
  { label: '★★★★★ 5', value: 5 },
]

const SORT_OPTIONS_BASE = [
  { label: 'Name', value: 'name' },
  { label: 'Date Added', value: 'date_added' },
  { label: 'Size', value: 'size' },
  { label: 'Duration', value: 'duration' },
  { label: 'Bitrate', value: 'bitrate' },
  { label: 'Codec', value: 'codec' },
  { label: 'Views', value: 'views' },
]
const SORT_OPTION_MY_RATING = { label: 'My Rating', value: 'my_rating' }

definePageMeta({ title: 'Media Library' })

const mediaApi = useMediaApi()
const suggestionsApi = useSuggestionsApi()
const playbackApi = usePlaybackApi()
const favoritesApi = useFavoritesApi()
const authStore = useAuthStore()
const router = useRouter()
const route = useRoute()
const toast = useToast()

const sortOptions = computed(() =>
  authStore.isLoggedIn ? [...SORT_OPTIONS_BASE, SORT_OPTION_MY_RATING] : SORT_OPTIONS_BASE
)
const { updatePreferences } = useApiEndpoints()

// Favorite media IDs for the current user
const favoriteIds = ref<Set<string>>(new Set())

async function loadFavorites() {
  if (!authStore.isLoggedIn) return
  try {
    const recs = await favoritesApi.list()
    favoriteIds.value = new Set((recs ?? []).map(r => r.media_id))
  } catch { /* non-critical */ }
}

async function toggleFavorite(e: Event, item: MediaItem) {
  e.preventDefault()
  e.stopPropagation()
  if (!authStore.isLoggedIn) { router.push('/login'); return }
  const wasFav = favoriteIds.value.has(item.id)
  // Optimistic update
  const next = new Set(favoriteIds.value)
  if (wasFav) { next.delete(item.id) } else { next.add(item.id) }
  favoriteIds.value = next
  try {
    if (wasFav) { await favoritesApi.remove(item.id) }
    else { await favoritesApi.add(item.id) }
  } catch {
    // Revert on error
    const reverted = new Set(favoriteIds.value)
    if (wasFav) { reverted.add(item.id) } else { reverted.delete(item.id) }
    favoriteIds.value = reverted
  }
}

// Playback progress (ratio 0-1) per media ID — for progress bar overlays
const playbackProgress = ref<Record<string, number>>({})

// User ratings per media ID (from list response — authenticated users only)
const userRatings = ref<Record<string, number>>({})

// Recommendations (only for logged-in users)
const continueWatching = ref<Suggestion[]>([])
const trending = ref<Suggestion[]>([])
const recommended = ref<Suggestion[]>([])
const recentlyAdded = ref<RecentItem[]>([])
const newSinceLastVisit = ref<NewSinceResponse | null>(null)
const onDeck = ref<OnDeckItem[]>([])
// General suggestions (shown to logged-out users — public endpoint)
const general = ref<Suggestion[]>([])

// State — declared BEFORE any watch({immediate:true}) that references params
// to avoid a Temporal Dead Zone (TDZ) crash when the user is already logged in
// at page-load time (e.g. page refresh). An immediate watcher fires synchronously
// during setup; if `params` isn't yet declared, `params.limit` throws TDZ.
const items = ref<MediaItem[]>([])
const categories = ref<MediaCategory[]>([])
const total = ref(0)
const loading = ref(true)
const loadError = ref('')
const scanning = ref(false)
const initializing = ref(false)

// URL deep-link: query params take precedence over saved preferences so that
// shared / bookmarked URLs open with the exact filters the sender intended.
const params = reactive({
  page: 1,
  limit: authStore.user?.preferences?.items_per_page ?? 24,
  search: typeof route.query.search === 'string' ? route.query.search : '',
  type: typeof route.query.type === 'string' ? route.query.type : (authStore.user?.preferences?.filter_media_type || 'all'),
  category: typeof route.query.category === 'string' ? route.query.category : (authStore.user?.preferences?.filter_category || 'all'),
  sort_by: typeof route.query.sort_by === 'string' ? route.query.sort_by : (authStore.user?.preferences?.sort_by || 'name'),
  sort_order: (typeof route.query.sort_order === 'string' ? route.query.sort_order : (authStore.user?.preferences?.sort_order ?? 'asc')) as 'asc' | 'desc',
  min_rating: typeof route.query.min_rating === 'string' ? (parseInt(route.query.min_rating, 10) || 0) : 0,
})

async function loadGeneralSuggestions() {
  try { general.value = (await suggestionsApi.get()) ?? [] } catch { /* non-critical */ }
}

async function loadRecommendations() {
  if (!authStore.isLoggedIn) return
  try {
    const [cw, tr, rec, recent, newSince, deck] = await Promise.allSettled([
      suggestionsApi.getContinueWatching(),
      suggestionsApi.getTrending(),
      suggestionsApi.getPersonalized(12),
      suggestionsApi.getRecent(14, 20),
      suggestionsApi.getNewSinceLastVisit(20),
      suggestionsApi.getOnDeck(10),
    ])
    if (cw.status === 'fulfilled') continueWatching.value = cw.value ?? []
    if (tr.status === 'fulfilled') trending.value = tr.value ?? []
    if (rec.status === 'fulfilled') recommended.value = rec.value ?? []
    if (recent.status === 'fulfilled') recentlyAdded.value = recent.value ?? []
    if (newSince.status === 'fulfilled' && newSince.value?.total > 0) newSinceLastVisit.value = newSince.value
    if (deck.status === 'fulfilled') onDeck.value = deck.value?.items ?? []
  } catch { /* non-critical */ }
}

// When the user logs in mid-session (logged-out → logged-in), reload
// recommendations and refresh the grid with their preference-based limit.
// This watcher is NOT immediate — loadRecommendations() is called from onMounted
// when the user is already logged in at page load, and we avoid a double
// load() by only reloading when the limit preference actually changed.
watch(() => authStore.isLoggedIn, (loggedIn) => {
  if (loggedIn) {
    general.value = []
    loadRecommendations()
    const pref = authStore.user?.preferences?.items_per_page
    if (pref && pref !== params.limit) {
      params.limit = pref
      params.page = 1
      load()
    }
  } else {
    continueWatching.value = []
    trending.value = []
    recommended.value = []
    recentlyAdded.value = []
    newSinceLastVisit.value = null
    onDeck.value = []
    userRatings.value = {}
    loadGeneralSuggestions()
  }
}, { immediate: false })

let searchTimer: ReturnType<typeof setTimeout> | null = null

function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => { params.page = 1; load() }, 300)
}

// Surprise Me — navigate to a random item from the current suggestions or library
async function surpriseMe() {
  try {
    // Prefer personalized/trending suggestions for logged-in users, fall back to library
    const pool = authStore.isLoggedIn
      ? [...recommended.value, ...trending.value, ...general.value]
      : general.value
    if (pool.length > 0) {
      const pick = pool[Math.floor(Math.random() * pool.length)]
      router.push(`/player?id=${encodeURIComponent(pick.media_id)}`)
      return
    }
    // Fall back to a random item from the loaded library grid
    if (items.value.length > 0) {
      const pick = items.value[Math.floor(Math.random() * items.value.length)]
      router.push(`/player?id=${encodeURIComponent(pick.id)}`)
    }
  } catch { /* non-critical */ }
}

// Tag filter — single active tag (clicking a card tag sets this; clear X removes it)
const filterTag = ref('')

// Hide watched toggle — only active for logged-in users
const hideWatched = ref(route.query.hide_watched === 'true')

function setTagFilter(tag: string) {
  filterTag.value = tag
  params.page = 1
  load()
}

function clearTagFilter() {
  filterTag.value = ''
  params.page = 1
  load()
}

watch(hideWatched, () => { params.page = 1; load() })
watch(() => params.min_rating, () => { params.page = 1; load() })

// Keep URL in sync with current filter state for deep-linking / bookmarking.
// Uses router.replace so the browser back button is not polluted.
let urlSyncTimer: ReturnType<typeof setTimeout> | null = null
watch(
  [() => params.type, () => params.category, () => params.sort_by, () => params.sort_order, () => params.min_rating, () => params.search, hideWatched],
  () => {
    if (urlSyncTimer) clearTimeout(urlSyncTimer)
    urlSyncTimer = setTimeout(() => {
      const query: Record<string, string> = {}
      if (params.type !== 'all') query.type = params.type
      if (params.category !== 'all') query.category = params.category
      if (params.sort_by !== 'name') query.sort_by = params.sort_by
      if (params.sort_order !== 'asc') query.sort_order = params.sort_order
      if (params.min_rating > 0) query.min_rating = String(params.min_rating)
      if (params.search) query.search = params.search
      if (hideWatched.value) query.hide_watched = 'true'
      router.replace({ query })
    }, 300)
  },
)

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    // Exclude min_rating from the spread — 0 is the "no filter" sentinel and must
    // NOT be forwarded to the backend (the backend would treat min_rating=0 as a
    // real filter condition and return only media with ≥0 stars, which skips
    // unrated items depending on backend implementation).
    const { min_rating: _minRating, ...paramsWithoutRating } = params
    const apiParams = {
      ...paramsWithoutRating,
      type: params.type === 'all' ? '' : params.type,
      category: params.category === 'all' ? '' : params.category,
      ...(filterTag.value ? { tags: [filterTag.value] } : {}),
      ...(hideWatched.value && authStore.isLoggedIn ? { hide_watched: true } : {}),
      ...(params.min_rating > 0 && authStore.isLoggedIn ? { min_rating: params.min_rating } : {}),
    }
    const res = await mediaApi.list(apiParams)
    items.value = res.items ?? []
    total.value = res.total_items ?? res.total ?? 0
    scanning.value = res.scanning ?? false
    initializing.value = res.initializing ?? false
    userRatings.value = res.user_ratings ?? {}
    // Pre-warm the browser image cache for visible thumbnails in this page.
    // The batch endpoint returns the same /thumbnail?id=X URLs so the browser
    // deduplicates and serves them instantly when the grid renders.
    const batchIds = items.value.slice(0, 50).map(i => i.id)
    if (batchIds.length > 0) {
      mediaApi.getThumbnailBatch(batchIds, 320).then(r => {
        for (const url of Object.values(r?.thumbnails ?? {})) {
          const img = new Image()
          img.src = url
        }
      }).catch(() => {})
    }
    // Batch-fetch playback positions for logged-in users to show progress bars.
    if (authStore.isLoggedIn && batchIds.length > 0) {
      playbackApi.getBatchPositions(batchIds).then(r => {
        const positions = r?.positions ?? {}
        const newProgress: Record<string, number> = {}
        for (const item of items.value.slice(0, 50)) {
          const pos = positions[item.id]
          if (pos && item.duration > 0) {
            newProgress[item.id] = pos / item.duration
          }
        }
        playbackProgress.value = newProgress
      }).catch(() => {})
    }
  } catch (e: unknown) {
    loadError.value = e instanceof Error ? e.message : 'Failed to load media'
    toast.add({ title: loadError.value, color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { loading.value = false }
}

async function loadCategories() {
  try { categories.value = (await mediaApi.getCategories()) ?? [] }
  catch { /* categories are non-critical; silently skip */ }
}

watch([() => params.type, () => params.category, () => params.sort_by, () => params.sort_order], () => {
  params.page = 1
  load()
})

// Persist filter preferences when logged-in users change the type or category filter.
// Saves silently in the background — failures are non-critical.
let filterSaveTimer: ReturnType<typeof setTimeout> | null = null
watch([() => params.type, () => params.category], ([newType, newCategory]) => {
  if (!authStore.isLoggedIn) return
  if (filterSaveTimer) clearTimeout(filterSaveTimer)
  filterSaveTimer = setTimeout(() => {
    updatePreferences({
      filter_media_type: newType,
      filter_category: newCategory,
    }).catch(() => { /* non-critical */ })
  }, 1000)
})

onMounted(() => {
  // Apply user preferences before the first load so we don't need a second request.
  const prefs = authStore.user?.preferences
  if (prefs) {
    if (prefs.items_per_page && prefs.items_per_page !== params.limit) params.limit = prefs.items_per_page
    if (prefs.view_mode && (prefs.view_mode === 'grid' || prefs.view_mode === 'list')) viewMode.value = prefs.view_mode
  }
  loadCategories()
  load()
  // Fetch recommendations for already-logged-in users (page refresh).
  // When the user logs in mid-session, the watch above handles this instead.
  if (authStore.isLoggedIn) { loadRecommendations(); loadFavorites() }
  else loadGeneralSuggestions()
})

// View mode — initialized from user preference; defaults to grid
const viewMode = ref<'grid' | 'list'>((authStore.user?.preferences?.view_mode as 'grid' | 'list') || 'grid')

// Mature content gate — true only when logged in, show_mature enabled, and can_view_mature permission granted
const canViewMature = computed(() =>
  authStore.isLoggedIn &&
  (authStore.user?.preferences?.show_mature ?? false) &&
  (authStore.user?.permissions?.can_view_mature ?? false),
)

function matureGateHref(item: MediaItem): string {
  if (item.is_mature && !canViewMature.value) {
    return authStore.isLoggedIn ? '/profile' : '/login'
  }
  return `/player?id=${encodeURIComponent(item.id)}`
}

const totalPages = computed(() => Math.ceil(total.value / params.limit))

// formatDuration imported from ~/utils/format

// ── Suggestion thumbnail error tracking ───────────────────────────────────────
// reactive() is used so that Vue's dependency tracking picks up .add() calls
// and re-evaluates the v-if guards that hide broken images.
const failedSuggestions = reactive(new Set<string>())
function onSuggestionThumbnailError(id: string) {
  failedSuggestions.add(id)
  scheduleThumbnailRetry(id, failedSuggestions)
}

// ── Thumbnail cycling on hover ─────────────────────────────────────────────────
const previewCache = new Map<string, string[]>()
// reactive() so that v-if="!failedThumbnails.has(item.id)" re-evaluates on error
const failedThumbnails = reactive(new Set<string>())

// ── Thumbnail self-healing retry ───────────────────────────────────────────────
// When a thumbnail fails to load (thumbnail not yet generated), schedule up to
// 3 probes with exponential backoff. On success, remove from the failed set so
// Vue re-renders the <img> instead of the fallback icon.
const retryCounters = new Map<string, number>()
const RETRY_DELAYS_MS = [5_000, 15_000, 45_000] // 5s, 15s, 45s

function scheduleThumbnailRetry(id: string, failedSet: Set<string>) {
  const attempt = retryCounters.get(id) ?? 0
  if (attempt >= RETRY_DELAYS_MS.length) return // give up after max attempts
  retryCounters.set(id, attempt + 1)
  setTimeout(() => {
    const probe = new Image()
    probe.onload = () => {
      // Thumbnail is now available — remove from the failed set so Vue shows the image
      failedSet.delete(id)
      retryCounters.delete(id)
    }
    probe.onerror = () => {
      // Still failing — schedule the next retry
      scheduleThumbnailRetry(id, failedSet)
    }
    // Cache-bust so the browser doesn't return the cached error response
    probe.src = `/thumbnail?id=${encodeURIComponent(id)}&_r=${Date.now()}`
  }, RETRY_DELAYS_MS[attempt])
}
const hoverItemId = ref<string | null>(null)
const hoverFrameIdx = ref(0)
let hoverCycleTimer: ReturnType<typeof setInterval> | null = null

async function onMediaHoverEnter(id: string, isAudio: boolean) {
  if (isAudio) return
  hoverItemId.value = id
  hoverFrameIdx.value = 0

  if (!previewCache.has(id)) {
    try {
      const result = await mediaApi.getThumbnailPreviews(id)
      if (result?.previews?.length > 1) previewCache.set(id, result.previews)
    } catch { /* no previews — stay on default */ }
  }

  const frames = previewCache.get(id)
  if (frames && frames.length > 1) {
    if (hoverCycleTimer) clearInterval(hoverCycleTimer)
    hoverCycleTimer = setInterval(() => {
      hoverFrameIdx.value = (hoverFrameIdx.value + 1) % frames.length
    }, 600)
  }
}

function onMediaHoverLeave() {
  hoverItemId.value = null
  hoverFrameIdx.value = 0
  if (hoverCycleTimer) { clearInterval(hoverCycleTimer); hoverCycleTimer = null }
}

function getThumbSrc(id: string): string {
  if (hoverItemId.value === id) {
    const frames = previewCache.get(id)
    if (frames?.length) return frames[hoverFrameIdx.value % frames.length]
  }
  return mediaApi.getThumbnailUrl(id)
}

function onThumbnailError(event: Event, id: string) {
  const src = (event.target as HTMLImageElement)?.src ?? ''
  // Preview frames have '_preview_' in the path; if one 404s (still generating),
  // clear the cache and stop cycling — do NOT mark the item as a failed thumbnail.
  if (src.includes('_preview_')) {
    previewCache.delete(id)
    if (hoverItemId.value === id) onMediaHoverLeave()
    return
  }
  failedThumbnails.add(id)
  scheduleThumbnailRetry(id, failedThumbnails)
}

onUnmounted(() => {
  if (hoverCycleTimer) clearInterval(hoverCycleTimer)
})
</script>

<template>
  <UContainer class="py-6 space-y-6">
    <!-- Recommendations (logged-in only) -->
    <template v-if="authStore.isLoggedIn">
      <!-- Continue Watching -->
      <RecommendationRow
        v-if="authStore.user?.preferences?.show_continue_watching !== false"
        title="Continue Watching"
        icon="i-lucide-play-circle"
        :items="continueWatching"
        :failed-ids="failedSuggestions"
        @thumbnail-error="onSuggestionThumbnailError"
      />

      <!-- On Deck (next episode per TV show / Anime series) -->
      <div v-if="onDeck.length > 0" class="space-y-2">
        <h2 class="text-sm font-semibold text-muted flex items-center gap-2">
          <UIcon name="i-lucide-tv-2" class="size-4 text-primary" />
          On Deck
        </h2>
        <div class="flex gap-3 overflow-x-auto pb-2">
          <NuxtLink
            v-for="ep in onDeck"
            :key="ep.media_id"
            :to="`/player?id=${encodeURIComponent(ep.media_id)}`"
            class="group shrink-0 w-40"
          >
            <div class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-1.5">
              <img
                v-if="ep.thumbnail_url"
                :src="ep.thumbnail_url"
                :alt="ep.name"
                class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
                loading="lazy"
              />
              <div v-else class="w-full h-full flex items-center justify-center">
                <UIcon name="i-lucide-tv-2" class="size-6 text-muted" />
              </div>
              <div v-if="ep.season > 0 || ep.episode > 0" class="absolute bottom-1 left-1 text-[10px] font-bold text-white bg-black/60 rounded px-1">
                {{ ep.season > 0 ? `S${String(ep.season).padStart(2,'0')}` : '' }}{{ ep.episode > 0 ? `E${String(ep.episode).padStart(2,'0')}` : '' }}
              </div>
            </div>
            <p class="text-xs font-medium truncate group-hover:text-primary transition-colors leading-tight" :title="ep.show_name">{{ ep.show_name }}</p>
            <p class="text-[10px] text-muted truncate" :title="ep.name">{{ ep.name }}</p>
          </NuxtLink>
        </div>
      </div>

      <!-- Trending -->
      <RecommendationRow
        v-if="authStore.user?.preferences?.show_trending !== false"
        title="Trending"
        icon="i-lucide-trending-up"
        :items="trending"
        :failed-ids="failedSuggestions"
        @thumbnail-error="onSuggestionThumbnailError"
      />

      <!-- Recommended For You -->
      <RecommendationRow
        v-if="authStore.user?.preferences?.show_recommended !== false"
        title="Recommended For You"
        icon="i-lucide-sparkles"
        :items="recommended"
        :failed-ids="failedSuggestions"
        @thumbnail-error="onSuggestionThumbnailError"
      />
      <!-- New Since Last Visit -->
      <div v-if="newSinceLastVisit && newSinceLastVisit.items.length > 0" class="space-y-2">
        <h2 class="text-sm font-semibold text-muted flex items-center gap-2">
          <UIcon name="i-lucide-bell" class="size-4 text-primary" />
          New Since Your Last Visit
        </h2>
        <div class="flex gap-3 overflow-x-auto pb-2">
          <NuxtLink
            v-for="r in newSinceLastVisit.items"
            :key="r.id"
            :to="`/player?id=${encodeURIComponent(r.id)}`"
            class="group shrink-0 w-40"
          >
            <div class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-1.5">
              <img
                v-if="r.thumbnail_url"
                :src="r.thumbnail_url"
                :alt="r.name"
                class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
                loading="lazy"
              />
              <div v-else class="w-full h-full flex items-center justify-center">
                <UIcon name="i-lucide-film" class="size-6 text-muted" />
              </div>
            </div>
            <p class="text-xs font-medium truncate group-hover:text-primary transition-colors" :title="r.name">{{ r.name }}</p>
            <p class="text-xs text-muted truncate">{{ r.category || r.type }}</p>
          </NuxtLink>
        </div>
      </div>
      <!-- Recently Added -->
      <div v-if="recentlyAdded.length > 0" class="space-y-2">
        <h2 class="text-sm font-semibold text-muted flex items-center gap-2">
          <UIcon name="i-lucide-sparkle" class="size-4 text-primary" />
          Recently Added
        </h2>
        <div class="flex gap-3 overflow-x-auto pb-2">
          <NuxtLink
            v-for="r in recentlyAdded"
            :key="r.id"
            :to="`/player?id=${encodeURIComponent(r.id)}`"
            class="group shrink-0 w-40"
          >
            <div class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-1.5">
              <img
                v-if="r.thumbnail_url"
                :src="r.thumbnail_url"
                :alt="r.name"
                class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
                loading="lazy"
              />
              <div v-else class="w-full h-full flex items-center justify-center">
                <UIcon name="i-lucide-film" class="size-6 text-muted" />
              </div>
            </div>
            <p class="text-xs font-medium truncate group-hover:text-primary transition-colors" :title="r.name">{{ r.name }}</p>
            <p class="text-xs text-muted truncate">{{ r.category || r.type }}</p>
          </NuxtLink>
        </div>
      </div>
    </template>

    <!-- Popular suggestions (logged-out users only) -->
    <template v-else>
      <RecommendationRow
        title="Popular"
        icon="i-lucide-star"
        :items="general"
        :failed-ids="failedSuggestions"
        @thumbnail-error="onSuggestionThumbnailError"
      />
    </template>

    <!-- Filters -->
    <div class="flex flex-wrap gap-3 items-center">
      <UInput
        v-model="params.search"
        icon="i-lucide-search"
        placeholder="Search media…"
        class="w-64"
        @input="onSearchInput"
      />
      <USelect
        v-model="params.type"
        :items="TYPE_OPTIONS"
        class="w-36"
      />
      <USelect
        v-if="categories.length > 0"
        v-model="params.category"
        :items="[{ label: 'All Categories', value: 'all' }, ...categories.map(c => ({ label: `${c.name} (${c.count})`, value: c.name }))]"
        class="w-48"
      />
      <USelect
        v-model="params.sort_by"
        :items="sortOptions"
        class="w-36"
      />
      <UButton
        :icon="params.sort_order === 'asc' ? 'i-lucide-arrow-up-az' : 'i-lucide-arrow-down-az'"
        :aria-label="params.sort_order === 'asc' ? 'Sort descending' : 'Sort ascending'"
        variant="ghost"
        color="neutral"
        size="sm"
        @click="params.sort_order = params.sort_order === 'asc' ? 'desc' : 'asc'"
      />
      <UButton
        icon="i-lucide-shuffle"
        label="Surprise Me"
        variant="soft"
        color="primary"
        size="sm"
        aria-label="Pick a random item to watch"
        @click="surpriseMe"
      />
      <!-- Active tag filter chip -->
      <UButton
        v-if="filterTag"
        :label="filterTag"
        icon="i-lucide-tag"
        trailing-icon="i-lucide-x"
        variant="soft"
        color="primary"
        size="sm"
        aria-label="Clear tag filter"
        @click="clearTagFilter"
      />
      <!-- Hide watched toggle (logged-in users only) -->
      <UButton
        v-if="authStore.isLoggedIn"
        :icon="hideWatched ? 'i-lucide-eye-off' : 'i-lucide-eye'"
        :label="hideWatched ? 'Showing unwatched' : 'Hide watched'"
        :variant="hideWatched ? 'solid' : 'ghost'"
        :color="hideWatched ? 'primary' : 'neutral'"
        size="sm"
        :aria-label="hideWatched ? 'Show all items' : 'Hide completed items'"
        @click="hideWatched = !hideWatched"
      />
      <!-- Min rating filter (logged-in users only) -->
      <USelect
        v-if="authStore.isLoggedIn"
        v-model="params.min_rating"
        :items="MIN_RATING_OPTIONS"
        class="w-32"
        aria-label="Minimum rating filter"
      />
      <!-- RSS subscribe link -->
      <UButton
        v-if="authStore.isLoggedIn"
        icon="i-lucide-rss"
        aria-label="Subscribe via RSS"
        variant="ghost"
        color="neutral"
        size="sm"
        to="/api/feed"
        target="_blank"
        external
      />
      <div class="ml-auto flex items-center gap-1">
        <p class="text-sm text-muted mr-2">{{ total.toLocaleString() }} items</p>
        <UButtonGroup>
          <UButton
            icon="i-lucide-grid-2x2"
            aria-label="Grid view"
            :variant="viewMode === 'grid' ? 'solid' : 'ghost'"
            :color="viewMode === 'grid' ? 'primary' : 'neutral'"
            size="sm"
            @click="viewMode = 'grid'"
          />
          <UButton
            icon="i-lucide-list"
            aria-label="List view"
            :variant="viewMode === 'list' ? 'solid' : 'ghost'"
            :color="viewMode === 'list' ? 'primary' : 'neutral'"
            size="sm"
            @click="viewMode = 'list'"
          />
        </UButtonGroup>
      </div>
    </div>

    <!-- Server initializing / scanning banner -->
    <UAlert
      v-if="initializing && !loading"
      title="Server is initializing"
      description="The media library is starting up. Some items may not appear yet."
      color="info"
      variant="soft"
      icon="i-lucide-loader-2"
      class="mb-2"
    />
    <UAlert
      v-else-if="scanning && !loading"
      title="Media scan in progress"
      description="New files may appear shortly as the library scan completes."
      color="info"
      variant="soft"
      icon="i-lucide-scan"
      class="mb-2"
    />

    <!-- Error -->
    <UAlert
      v-if="loadError && !loading"
      :title="loadError"
      color="error"
      variant="soft"
      icon="i-lucide-alert-circle"
      class="mb-2"
    />

    <!-- Loading -->
    <div v-if="loading" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-6 gap-4">
      <div
        v-for="n in 12"
        :key="n"
        class="aspect-video rounded-lg bg-muted animate-pulse"
      />
    </div>

    <!-- Grid view -->
    <div
      v-else-if="viewMode === 'grid'"
      class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-6 gap-4"
    >
      <NuxtLink
        v-for="item in items"
        :key="item.id"
        :to="matureGateHref(item)"
        class="group block"
        @mouseenter="onMediaHoverEnter(item.id, item.type === 'audio')"
        @mouseleave="onMediaHoverLeave"
      >
        <div
          class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-2"
          :style="item.blur_hash && item.type !== 'audio' ? { backgroundImage: `url(${blurHashToDataUrl(item.blur_hash)})`, backgroundSize: 'cover' } : {}"
        >
          <img
            v-if="item.type !== 'audio' && !failedThumbnails.has(item.id)"
            :src="getThumbSrc(item.id)"
            :alt="getDisplayTitle(item)"
            :class="['w-full h-full object-cover transition-all duration-200 group-hover:scale-105', item.is_mature && !canViewMature ? 'blur-2xl scale-125 saturate-0' : '']"
            loading="lazy"
            @error="onThumbnailError($event, item.id)"
          />
          <div v-else class="w-full h-full flex items-center justify-center">
            <UIcon :name="item.type === 'audio' ? 'i-lucide-music' : 'i-lucide-film'" class="size-8 text-muted" />
          </div>
          <!-- Mature gate overlay (guests + users with show_mature disabled) -->
          <div
            v-if="item.is_mature && !canViewMature"
            class="absolute inset-0 flex flex-col items-center justify-center bg-black/85 gap-1.5 px-2 text-center"
          >
            <UIcon name="i-lucide-lock" class="size-5 text-white" />
            <p class="text-white text-xs font-semibold leading-tight">
              {{ authStore.isLoggedIn ? 'Enable mature content\nin profile settings' : 'Sign in to view' }}
            </p>
          </div>
          <!-- Playback progress bar (logged-in, partially watched, not gated) -->
          <div
            v-if="authStore.isLoggedIn && playbackProgress[item.id] && !(item.is_mature && !canViewMature)"
            class="absolute bottom-0 left-0 right-0 h-1 bg-white/20"
          >
            <div
              class="h-full bg-primary"
              :style="{ width: `${Math.min(100, Math.round((playbackProgress[item.id] ?? 0) * 100))}%` }"
            />
          </div>
          <!-- Duration badge (hidden when gated) -->
          <div
            v-if="item.duration && !(item.is_mature && !canViewMature)"
            class="absolute bottom-1 right-1 bg-black/70 text-white text-xs px-1 rounded font-mono"
          >
            {{ formatDuration(item.duration) }}
          </div>
          <!-- Type badge -->
          <div class="absolute top-1 left-1">
            <UBadge
              v-if="item.type !== 'video'"
              :label="item.type"
              color="neutral"
              variant="solid"
              size="xs"
              class="bg-black/70"
            />
          </div>
          <!-- Mature badge (only when user can view it) -->
          <div v-if="item.is_mature && canViewMature" class="absolute top-1 right-1">
            <UBadge label="18+" color="error" variant="solid" size="xs" />
          </div>
          <!-- User star rating badge (hidden when mature badge occupies the same corner) -->
          <div
            v-if="userRatings[item.id] && !(item.is_mature && canViewMature)"
            class="absolute top-1 right-1 flex items-center gap-0.5 bg-black/70 text-yellow-400 text-xs px-1 rounded"
          >
            <UIcon name="i-lucide-star" class="size-3 fill-current" />
            <span>{{ userRatings[item.id] }}</span>
          </div>
          <!-- Favorite button -->
          <button
            v-if="authStore.isLoggedIn"
            class="absolute bottom-6 right-1 p-0.5 rounded-full bg-black/50 opacity-0 group-hover:opacity-100 transition-opacity"
            :aria-label="favoriteIds.has(item.id) ? 'Remove from favorites' : 'Add to favorites'"
            @click.prevent.stop="toggleFavorite($event, item)"
          >
            <UIcon
              name="i-lucide-heart"
              :class="favoriteIds.has(item.id) ? 'size-4 text-red-400 [&>svg]:fill-current' : 'size-4 text-white'"
            />
          </button>
        </div>
        <p class="text-sm font-medium text-default truncate group-hover:text-primary transition-colors" :title="getDisplayTitle(item)">
          {{ getDisplayTitle(item) }}
        </p>
        <p v-if="item.category && !(item.is_mature && !canViewMature)" class="text-xs text-muted truncate">{{ item.category }}</p>
        <!-- Tag chips — click to filter by tag (hidden for gated items) -->
        <div
          v-if="item.tags?.length && !(item.is_mature && !canViewMature)"
          class="flex gap-1 flex-wrap mt-0.5"
        >
          <button
            v-for="tag in item.tags.slice(0, 2)"
            :key="tag"
            class="text-xs px-1.5 py-0.5 rounded bg-muted text-muted hover:bg-primary/20 hover:text-primary transition-colors"
            :aria-label="`Filter by tag: ${tag}`"
            @click.prevent.stop="setTagFilter(tag)"
          >{{ tag }}</button>
        </div>
      </NuxtLink>
      <p v-if="items.length === 0" class="col-span-full text-center py-12 text-muted">
        No media found.
      </p>
    </div>

    <!-- List view -->
    <UCard v-else>
      <UTable
        :data="items"
        :columns="[
          { accessorKey: 'name', header: 'Name' },
          { accessorKey: 'type', header: 'Type' },
          { accessorKey: 'duration', header: 'Duration' },
          { accessorKey: 'category', header: 'Category' },
          { accessorKey: 'views', header: 'Views' },
          { accessorKey: 'date_added', header: 'Added' },
        ]"
      >
        <template #name-cell="{ row }">
          <NuxtLink :to="matureGateHref(row.original)" class="flex items-center gap-3 hover:text-primary">
            <div
              class="relative w-16 h-9 rounded overflow-hidden bg-muted shrink-0"
              :style="row.original.blur_hash ? { backgroundImage: `url(${blurHashToDataUrl(row.original.blur_hash)})`, backgroundSize: 'cover' } : {}"
            >
              <img
                v-if="!failedThumbnails.has(row.original.id)"
                :src="mediaApi.getThumbnailUrl(row.original.id)"
                :alt="getDisplayTitle(row.original)"
                :class="['w-full h-full object-cover', row.original.is_mature && !canViewMature ? 'blur-xl saturate-0' : '']"
                loading="lazy"
                @error="onThumbnailError($event, row.original.id)"
              />
              <div v-else class="w-full h-full flex items-center justify-center">
                <UIcon :name="row.original.type === 'audio' ? 'i-lucide-music' : 'i-lucide-film'" class="size-4 text-muted" />
              </div>
              <div v-if="row.original.is_mature && !canViewMature" class="absolute inset-0 flex items-center justify-center bg-black/80">
                <UIcon name="i-lucide-lock" class="size-3 text-white" />
              </div>
            </div>
            <span class="font-medium truncate max-w-xs">{{ getDisplayTitle(row.original) }}</span>
          </NuxtLink>
        </template>
        <template #type-cell="{ row }">
          <UBadge :label="row.original.type" color="neutral" variant="subtle" size="xs" />
        </template>
        <template #duration-cell="{ row }">
          <span class="font-mono text-sm">{{ formatDuration(row.original.duration) || '—' }}</span>
        </template>
        <template #views-cell="{ row }">{{ (row.original.views ?? 0).toLocaleString() }}</template>
        <template #date_added-cell="{ row }">
          <span class="text-sm text-muted">{{ row.original.date_added ? new Date(row.original.date_added).toLocaleDateString() : '—' }}</span>
        </template>
      </UTable>
      <p v-if="items.length === 0" class="text-center py-8 text-muted">No media found.</p>
    </UCard>

    <!-- Pagination -->
    <div v-if="totalPages > 1" class="flex justify-center">
      <UPagination
        v-model:page="params.page"
        :total="total"
        :items-per-page="params.limit"
        @update:page="load"
      />
    </div>
  </UContainer>
</template>

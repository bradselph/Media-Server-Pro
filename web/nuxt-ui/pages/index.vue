<script setup lang="ts">
import type { MediaItem, MediaCategory, Suggestion, RecentItem, NewSinceResponse, OnDeckItem, Playlist, MediaStats } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'
import { useApiEndpoints, useFavoritesApi, usePlaylistApi } from '~/composables/useApiEndpoints'
import { resolveComponent } from 'vue'
import { formatDuration, formatBytes, formatRelativeDate, formatResolution } from '~/utils/format'
import { blurHashToDataUrl } from '~/utils/blurhash'
import { useQueueStore } from '~/stores/queue'

const PALETTES: [string, string][] = [
  ['#1a0835','#9333ea'],['#081530','#2563eb'],['#1a0808','#dc2626'],
  ['#081508','#16a34a'],['#1a1208','#d97706'],['#081515','#0891b2'],
  ['#150815','#db2777'],['#0a0815','#6366f1'],['#150a0a','#ea580c'],
  ['#0a1515','#059669'],['#0f0a20','#a855f7'],['#1a1000','#ca8a04'],
]

function getItemGradient(id: string): string {
  let hash = 0
  for (let i = 0; i < id.length; i++) hash = (hash * 31 + id.charCodeAt(i)) & 0xffff
  const [c1, c2] = PALETTES[hash % PALETTES.length]
  return `linear-gradient(135deg, ${c1}, ${c2})`
}

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
const playlistApi = usePlaylistApi()
const authStore = useAuthStore()
const router = useRouter()
const route = useRoute()
const toast = useToast()

// Library stats (public, shown to guests too)
const libraryStats = ref<MediaStats | null>(null)
mediaApi.getStats().then(s => { libraryStats.value = s }).catch(() => {})

// Multi-select / bulk-add-to-playlist
const selectionMode = ref(false)
const selectedIds = ref<Set<string>>(new Set())
const myPlaylists = ref<Playlist[]>([])
const bulkAddPlaylistId = ref<string | undefined>(undefined)
const bulkAdding = ref(false)

function toggleSelectionMode() {
  selectionMode.value = !selectionMode.value
  if (!selectionMode.value) {
    selectedIds.value = new Set()
    // Do NOT reset params.page here — without a matching load() call the pagination
    // component would show page 1 while the grid still shows the previous page's items.
  }
}

function toggleSelect(id: string, event: Event) {
  event.preventDefault()
  event.stopPropagation()
  const next = new Set(selectedIds.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selectedIds.value = next
}

async function loadMyPlaylists() {
  if (!authStore.isLoggedIn) return
  try {
    const result = await playlistApi.list()
    if (indexMounted) myPlaylists.value = result
  } catch { /* non-critical */ }
}

async function bulkAddToPlaylist() {
  if (!bulkAddPlaylistId.value || selectedIds.value.size === 0) return
  bulkAdding.value = true
  const ids = [...selectedIds.value]
  let added = 0
  for (const id of ids) {
    try { await playlistApi.addItem(bulkAddPlaylistId.value, id); added++ } catch { /* skip duplicates */ }
  }
  bulkAdding.value = false
  toast.add({ title: `Added ${added} of ${ids.length} items to playlist`, color: added > 0 ? 'success' : 'warning', icon: 'i-lucide-list-music' })
  toggleSelectionMode()
  bulkAddPlaylistId.value = undefined
}

watch(selectionMode, (on) => { if (on) loadMyPlaylists() })

// Per-card quick "add to playlist" (does not navigate, keeps playback running)
async function quickAddToPlaylist(itemId: string, playlistId: string) {
  try {
    await playlistApi.addItem(playlistId, itemId)
    toast.add({ title: 'Added to playlist', color: 'success', icon: 'i-lucide-list-music' })
  } catch {
    toast.add({ title: 'Already in playlist or failed to add', color: 'warning', icon: 'i-lucide-list-x' })
  }
}

const queueStore = useQueueStore()

function addToQueue(item: MediaItem) {
  queueStore.addToQueue({ id: item.id, name: item.name, type: item.type, duration: item.duration, thumbnail_url: item.thumbnail_url })
  toast.add({ title: 'Added to queue', color: 'success', icon: 'i-lucide-list-ordered' })
}

function playlistMenuItemsFor(itemId: string) {
  const plItems = myPlaylists.value.map(pl => ({
    label: pl.name,
    icon: 'i-lucide-list-music',
    click: () => quickAddToPlaylist(itemId, pl.id),
  }))
  const newPl = [{ label: 'New Playlist…', icon: 'i-lucide-plus', to: '/playlists' }]
  return plItems.length > 0 ? [plItems, newPl] : [newPl]
}

const sortOptions = computed(() =>
  authStore.isLoggedIn ? [...SORT_OPTIONS_BASE, SORT_OPTION_MY_RATING] : SORT_OPTIONS_BASE
)
const { updatePreferences } = useApiEndpoints()

// Favorite media IDs for the current user
const favoriteIds = ref<Set<string>>(new Set())
const togglingIds = ref(new Set<string>())

let indexMounted = false
onMounted(() => { indexMounted = true })
onUnmounted(() => { indexMounted = false })

async function loadFavorites() {
  if (!authStore.isLoggedIn) return
  try {
    const recs = await favoritesApi.list()
    if (!indexMounted) return
    favoriteIds.value = new Set((recs ?? []).map(r => r.media_id))
  } catch { /* non-critical */ }
}

async function toggleFavorite(e: Event, item: MediaItem) {
  e.preventDefault()
  e.stopPropagation()
  if (!authStore.isLoggedIn) { router.push('/login'); return }
  if (togglingIds.value.has(item.id)) return
  const wasFav = favoriteIds.value.has(item.id)
  // Optimistic update
  const next = new Set(favoriteIds.value)
  if (wasFav) { next.delete(item.id) } else { next.add(item.id) }
  favoriteIds.value = next
  togglingIds.value.add(item.id)
  try {
    if (wasFav) { await favoritesApi.remove(item.id) }
    else { await favoritesApi.add(item.id) }
    if (!indexMounted) return
  } catch {
    if (!indexMounted) return
    // Revert on error
    const reverted = new Set(favoriteIds.value)
    if (wasFav) { reverted.add(item.id) } else { reverted.delete(item.id) }
    favoriteIds.value = reverted
  } finally {
    togglingIds.value.delete(item.id)
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
const typeCounts = ref<Record<string, number>>({})

// Generation counter — incremented on every load() call so that responses
// arriving out of order (due to network jitter or rapid filter changes) are
// discarded rather than overwriting a more recent result.
let loadSeq = 0

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
  min_rating: typeof route.query.min_rating === 'string' ? (Number.parseInt(route.query.min_rating, 10) || 0) : 0,
})

async function loadGeneralSuggestions() {
  try {
    const result = await suggestionsApi.get()
    if (indexMounted) general.value = result ?? []
  } catch { /* non-critical */ }
}

async function loadRecommendations() {
  if (!authStore.isLoggedIn) return
  try {
    const [cw, tr, rec, recent, newSince, deck] = await Promise.allSettled([
      suggestionsApi.getContinueWatching(20),
      suggestionsApi.getTrending(20),
      suggestionsApi.getPersonalized(12),
      suggestionsApi.getRecent(14, 20),
      suggestionsApi.getNewSinceLastVisit(20),
      suggestionsApi.getOnDeck(10),
    ])
    if (!indexMounted) return
    // Deduplicate across rows: higher-priority rows "claim" their IDs so lower-priority
    // rows don't show the same item twice. Priority: continueWatching > onDeck > trending > recommended > recent.
    const seenIds = new Set<string>()
    function dedup<T extends { id?: string; media_id?: string }>(items: T[]): T[] {
      return items.filter(item => {
        const id = item.id ?? item.media_id ?? ''
        if (!id || seenIds.has(id)) return false
        seenIds.add(id)
        return true
      })
    }
    if (cw.status === 'fulfilled') continueWatching.value = dedup(cw.value ?? [])
    if (deck.status === 'fulfilled') onDeck.value = dedup((deck.value?.items ?? []) as Parameters<typeof dedup>[0]) as typeof onDeck.value
    if (tr.status === 'fulfilled') trending.value = dedup(tr.value ?? [])
    if (rec.status === 'fulfilled') recommended.value = dedup(rec.value ?? [])
    if (recent.status === 'fulfilled') recentlyAdded.value = dedup(recent.value ?? [])
    if (newSince.status === 'fulfilled' && newSince.value?.total > 0) newSinceLastVisit.value = newSince.value
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

// Play All — navigate to player with the first item from the current grid
function playAll() {
  if (items.value.length === 0) return
  const first = items.value[0]
  router.push(`/player?id=${encodeURIComponent(first.id)}`)
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

// Filter presets — design handoff §6.3. Chip strip above the grid that toggles
// common curation modes (Trending → most-viewed, New → recently added).
type Preset = 'trending' | 'new' | null
const activePreset = computed<Preset>(() => {
  if (params.sort_by === 'views' && params.sort_order === 'desc') return 'trending'
  if (params.sort_by === 'date_added' && params.sort_order === 'desc') return 'new'
  return null
})

function togglePreset(preset: 'trending' | 'new') {
  if (activePreset.value === preset) {
    params.sort_by = 'name'
    params.sort_order = 'asc'
  } else if (preset === 'trending') {
    params.sort_by = 'views'
    params.sort_order = 'desc'
  } else {
    params.sort_by = 'date_added'
    params.sort_order = 'desc'
  }
  params.page = 1
}

const hasActiveFilters = computed(() =>
  params.type !== 'all' ||
  params.category !== 'all' ||
  params.sort_by !== 'name' ||
  params.sort_order !== 'asc' ||
  params.min_rating > 0 ||
  !!params.search ||
  !!filterTag.value ||
  hideWatched.value,
)

function clearAllFilters() {
  params.type = 'all'
  params.category = 'all'
  params.sort_by = 'name'
  params.sort_order = 'asc'
  params.min_rating = 0
  params.search = ''
  filterTag.value = ''
  hideWatched.value = false
  params.page = 1
}

watch(hideWatched, () => { params.page = 1; load() })
watch(() => params.min_rating, () => { params.page = 1; load() })

// Sync nav-bar search into params when the route query is updated while on this page
watch(() => route.query.search, (q) => {
  const s = typeof q === 'string' ? q : ''
  if (s !== params.search) {
    params.search = s
    params.page = 1
    load()
  }
})

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
  const seq = ++loadSeq
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
    // Discard response if a newer load() has already been dispatched.
    if (seq !== loadSeq) return
    items.value = res.items ?? []
    total.value = res.total_items ?? 0
    scanning.value = res.scanning ?? false
    initializing.value = res.initializing ?? false
    if (res.type_counts && params.type === 'all') typeCounts.value = res.type_counts
    userRatings.value = res.user_ratings ?? {}
    // Pre-warm the browser image cache for visible thumbnails in this page.
    // The batch endpoint returns the same /thumbnail?id=X URLs so the browser
    // deduplicates and serves them instantly when the grid renders.
    const batchIds = items.value.slice(0, 50).map(i => i.id)
    if (batchIds.length > 0) {
      mediaApi.getThumbnailBatch(batchIds, 320).then(r => {
        if (seq !== loadSeq) return
        for (const url of Object.values(r?.thumbnails ?? {})) {
          const img = new Image()
          img.src = url
        }
      }).catch(() => {})
    }
    // Batch-fetch playback positions for logged-in users to show progress bars.
    if (authStore.isLoggedIn && batchIds.length > 0) {
      playbackApi.getBatchPositions(batchIds).then(r => {
        if (seq !== loadSeq) return
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
    if (seq !== loadSeq) return
    loadError.value = e instanceof Error ? e.message : 'Failed to load media'
    toast.add({ title: loadError.value, color: 'error', icon: 'i-lucide-alert-circle' })
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}

async function loadCategories() {
  try {
    const result = await mediaApi.getCategories()
    if (indexMounted) categories.value = result ?? []
  } catch { /* categories are non-critical; silently skip */ }
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
    if (prefs.view_mode && ['grid', 'list', 'compact'].includes(prefs.view_mode)) viewMode.value = prefs.view_mode as ViewMode
  }
  loadCategories()
  load()
  // Fetch recommendations for already-logged-in users (page refresh).
  // When the user logs in mid-session, the watch above handles this instead.
  if (authStore.isLoggedIn) { loadRecommendations(); loadFavorites(); loadMyPlaylists() }
  else loadGeneralSuggestions()
})

// View mode — initialized from user preference; supports 'grid', 'list', and 'compact'
type ViewMode = 'grid' | 'list' | 'compact'
const viewMode = ref<ViewMode>(
  (['grid', 'list', 'compact'] as ViewMode[]).includes(authStore.user?.preferences?.view_mode as ViewMode)
    ? (authStore.user?.preferences?.view_mode as ViewMode)
    : 'grid'
)

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
const retryTimers = new Map<string, ReturnType<typeof setTimeout>>()
const RETRY_DELAYS_MS = [5_000, 15_000, 45_000] // 5s, 15s, 45s

function scheduleThumbnailRetry(id: string, failedSet: Set<string>) {
  const attempt = retryCounters.get(id) ?? 0
  if (attempt >= RETRY_DELAYS_MS.length) return // give up after max attempts
  retryCounters.set(id, attempt + 1)
  const timer = setTimeout(() => {
    retryTimers.delete(id)
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
  retryTimers.set(id, timer)
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

function getThumbnailUrl(id: string): string {
  const base = mediaApi.getThumbnailUrl(id)
  const nonce = authStore.thumbnailNonce
  return nonce > 0 ? `${base}&_n=${nonce}` : base
}

function getThumbSrc(id: string): string {
  if (hoverItemId.value === id) {
    const frames = previewCache.get(id)
    if (frames?.length) return frames[hoverFrameIdx.value % frames.length]
  }
  return getThumbnailUrl(id)
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
  if (searchTimer) clearTimeout(searchTimer)
  if (urlSyncTimer) clearTimeout(urlSyncTimer)
  if (filterSaveTimer) clearTimeout(filterSaveTimer)
  retryTimers.forEach(t => clearTimeout(t))
  retryTimers.clear()
})
</script>

<template>
  <!-- Hero — compact banner per design handoff §6.2 -->
  <template v-if="authStore.isLoggedIn ? trending.length > 0 : general.length > 0">
    <div
      class="relative overflow-hidden min-h-[240px] flex items-end"
      :style="{ background: getItemGradient((authStore.isLoggedIn ? trending[0] : general[0]).media_id) }"
    >
      <!-- Actual media thumbnail as background -->
      <img
        :src="mediaApi.getThumbnailUrl((authStore.isLoggedIn ? trending[0] : general[0]).media_id)"
        class="absolute inset-0 w-full h-full object-cover pointer-events-none select-none"
        aria-hidden="true"
        @error="($event.target as HTMLImageElement).style.display='none'"
      />
      <!-- Scanline texture -->
      <div class="absolute inset-0 pointer-events-none scanline-thumb" />
      <!-- Bottom gradient fade to page bg -->
      <div class="absolute bottom-0 inset-x-0 h-[70%] pointer-events-none bg-gradient-to-t from-[var(--surface-page)] to-transparent" />
      <div class="relative z-10 max-w-[1400px] mx-auto px-5 pb-6 w-full">
        <div class="flex items-center gap-3.5 flex-wrap">
          <span class="inline-block bg-white/10 backdrop-blur-md border border-white/15 rounded-full px-2.5 py-0.5 text-[9px] font-bold text-[var(--accent-soft)] uppercase tracking-[1.5px]">Featured</span>
          <h1 class="text-[clamp(20px,3vw,28px)] font-bold text-white leading-tight line-clamp-1" style="text-wrap: pretty;">
            {{ getDisplayTitle(authStore.isLoggedIn ? trending[0] : general[0]) }}
          </h1>
          <div class="flex gap-2 flex-wrap ml-auto">
            <NuxtLink
              :to="`/player?id=${encodeURIComponent((authStore.isLoggedIn ? trending[0] : general[0]).media_id)}`"
              class="inline-flex items-center gap-1.5 bg-[var(--accent)] text-white rounded-[7px] px-[18px] py-2 text-[13px] font-bold no-underline hover:brightness-110 transition-all"
            >
              <UIcon name="i-lucide-play" class="size-3.5" />Watch Now
            </NuxtLink>
            <NuxtLink
              to="/categories"
              class="inline-flex items-center bg-white/10 border border-white/20 backdrop-blur-md text-white rounded-[7px] px-4 py-2 text-[13px] font-medium no-underline hover:bg-white/15 transition-all"
            >Browse</NuxtLink>
          </div>
        </div>
      </div>
    </div>
  </template>

  <UContainer class="py-6 space-y-6">
    <h1 class="sr-only">Media Library</h1>
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
      <div v-if="onDeck.length > 0" class="space-y-3">
        <div class="flex items-center justify-between">
          <h2 class="text-lg font-bold text-[var(--text-strong)] flex items-center gap-2">
            <UIcon name="i-lucide-tv-2" class="size-4 text-[var(--accent)]" />
            On Deck
          </h2>
          <div class="flex items-center gap-2">
            <NuxtLink to="/history" class="text-xs font-medium text-[var(--accent-soft)] hover:underline flex items-center gap-1">See all <UIcon name="i-lucide-arrow-right" class="size-3" /></NuxtLink>
            <div class="flex gap-1">
              <UButton icon="i-lucide-chevron-left" size="xs" variant="ghost" color="neutral" aria-label="Scroll left" @click="($refs.onDeckScroll as HTMLElement)?.scrollBy({ left: -320, behavior: 'smooth' })" />
              <UButton icon="i-lucide-chevron-right" size="xs" variant="ghost" color="neutral" aria-label="Scroll right" @click="($refs.onDeckScroll as HTMLElement)?.scrollBy({ left: 320, behavior: 'smooth' })" />
            </div>
          </div>
        </div>
        <div ref="onDeckScroll" class="flex gap-3 overflow-x-auto pb-2 scrollbar-hide">
          <NuxtLink
            v-for="ep in onDeck"
            :key="ep.media_id"
            :to="`/player?id=${encodeURIComponent(ep.media_id)}`"
            class="group shrink-0 w-40"
          >
            <div class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-1.5 media-card-lift scanline-thumb">
              <img
                v-if="ep.thumbnail_url"
                :src="ep.thumbnail_url"
                :alt="getDisplayTitle(ep)"
                width="320"
                height="180"
                class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
                loading="lazy"
              />
              <div v-else class="w-full h-full flex items-center justify-center">
                <UIcon name="i-lucide-tv-2" class="size-6 text-muted" />
              </div>
              <div v-if="ep.season > 0 || ep.episode > 0" class="absolute bottom-1 left-1 text-[10px] font-bold text-white bg-black/60 rounded px-1">
                {{ ep.season > 0 ? `S${String(ep.season).padStart(2,'0')}` : '' }}{{ ep.episode > 0 ? `E${String(ep.episode).padStart(2,'0')}` : '' }}
              </div>
              <div v-if="ep.duration" class="absolute bottom-1 right-1 bg-black/70 text-white text-[10px] font-mono px-1 rounded">
                {{ formatDuration(ep.duration) }}
              </div>
            </div>
            <p class="text-xs font-medium truncate group-hover:text-primary transition-colors leading-tight" :title="ep.show_name">{{ ep.show_name }}</p>
            <p class="text-[10px] text-muted truncate" :title="getDisplayTitle(ep)">{{ getDisplayTitle(ep) }}</p>
          </NuxtLink>
        </div>
      </div>

      <!-- Trending -->
      <RecommendationRow
        v-if="authStore.user?.preferences?.show_trending !== false"
        title="Trending"
        icon="i-lucide-flame"
        :items="trending"
        :failed-ids="failedSuggestions"
        to="/categories"
        @thumbnail-error="onSuggestionThumbnailError"
      />

      <!-- Recommended For You -->
      <RecommendationRow
        v-if="authStore.user?.preferences?.show_recommended !== false"
        title="Recommended For You"
        icon="i-lucide-thumbs-up"
        :items="recommended"
        :failed-ids="failedSuggestions"
        to="/"
        @thumbnail-error="onSuggestionThumbnailError"
      />
      <!-- New Since Last Visit -->
      <div v-if="newSinceLastVisit && newSinceLastVisit.items.length > 0" class="space-y-3">
        <div class="flex items-center justify-between">
          <h2 class="text-lg font-bold text-[var(--text-strong)] flex items-center gap-2">
            <UIcon name="i-lucide-bell" class="size-4 text-[var(--accent)]" />
            New Since Your Last Visit
          </h2>
          <div class="flex items-center gap-2">
            <NuxtLink to="/?sort_by=date_added&sort_order=desc" class="text-xs font-medium text-[var(--accent-soft)] hover:underline flex items-center gap-1">See all <UIcon name="i-lucide-arrow-right" class="size-3" /></NuxtLink>
            <div class="flex gap-1">
              <UButton icon="i-lucide-chevron-left" size="xs" variant="ghost" color="neutral" aria-label="Scroll left" @click="($refs.newSinceScroll as HTMLElement)?.scrollBy({ left: -320, behavior: 'smooth' })" />
              <UButton icon="i-lucide-chevron-right" size="xs" variant="ghost" color="neutral" aria-label="Scroll right" @click="($refs.newSinceScroll as HTMLElement)?.scrollBy({ left: 320, behavior: 'smooth' })" />
            </div>
          </div>
        </div>
        <div ref="newSinceScroll" class="flex gap-3 overflow-x-auto pb-2 scrollbar-hide">
          <NuxtLink
            v-for="r in newSinceLastVisit.items"
            :key="r.id"
            :to="`/player?id=${encodeURIComponent(r.id)}`"
            class="group shrink-0 w-40"
          >
            <div class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-1.5 media-card-lift scanline-thumb">
              <img
                v-if="r.thumbnail_url"
                :src="r.thumbnail_url"
                :alt="getDisplayTitle(r)"
                width="320"
                height="180"
                class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
                loading="lazy"
              />
              <div v-else class="w-full h-full flex items-center justify-center">
                <UIcon name="i-lucide-film" class="size-6 text-muted" />
              </div>
              <div v-if="r.duration" class="absolute bottom-1 right-1 bg-black/70 text-white text-[10px] font-mono px-1 rounded">
                {{ formatDuration(r.duration) }}
              </div>
            </div>
            <p class="text-xs font-medium truncate group-hover:text-primary transition-colors" :title="getDisplayTitle(r)">{{ getDisplayTitle(r) }}</p>
            <p class="text-xs text-muted truncate">{{ r.category || r.type }}</p>
          </NuxtLink>
        </div>
      </div>
      <!-- Recently Added -->
      <div v-if="recentlyAdded.length > 0" class="space-y-3">
        <div class="flex items-center justify-between">
          <h2 class="text-lg font-bold text-[var(--text-strong)] flex items-center gap-2">
            <UIcon name="i-lucide-sparkle" class="size-4 text-[var(--accent)]" />
            Recently Added
          </h2>
          <div class="flex items-center gap-2">
            <NuxtLink to="/?sort_by=date_added&sort_order=desc" class="text-xs font-medium text-[var(--accent-soft)] hover:underline flex items-center gap-1">See all <UIcon name="i-lucide-arrow-right" class="size-3" /></NuxtLink>
            <div class="flex gap-1">
              <UButton icon="i-lucide-chevron-left" size="xs" variant="ghost" color="neutral" aria-label="Scroll left" @click="($refs.recentScroll as HTMLElement)?.scrollBy({ left: -320, behavior: 'smooth' })" />
              <UButton icon="i-lucide-chevron-right" size="xs" variant="ghost" color="neutral" aria-label="Scroll right" @click="($refs.recentScroll as HTMLElement)?.scrollBy({ left: 320, behavior: 'smooth' })" />
            </div>
          </div>
        </div>
        <div ref="recentScroll" class="flex gap-3 overflow-x-auto pb-2 scrollbar-hide">
          <NuxtLink
            v-for="r in recentlyAdded"
            :key="r.id"
            :to="`/player?id=${encodeURIComponent(r.id)}`"
            class="group shrink-0 w-40"
          >
            <div class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-1.5 media-card-lift scanline-thumb">
              <img
                v-if="r.thumbnail_url"
                :src="r.thumbnail_url"
                :alt="getDisplayTitle(r)"
                width="320"
                height="180"
                class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
                loading="lazy"
              />
              <div v-else class="w-full h-full flex items-center justify-center">
                <UIcon name="i-lucide-film" class="size-6 text-muted" />
              </div>
              <div v-if="r.duration" class="absolute bottom-1 right-1 bg-black/70 text-white text-[10px] font-mono px-1 rounded">
                {{ formatDuration(r.duration) }}
              </div>
            </div>
            <p class="text-xs font-medium truncate group-hover:text-primary transition-colors" :title="getDisplayTitle(r)">{{ getDisplayTitle(r) }}</p>
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
        to="/categories"
        @thumbnail-error="onSuggestionThumbnailError"
      />
    </template>

    <!-- Library stats (public) -->
    <div v-if="libraryStats && !authStore.isLoggedIn" class="flex items-center gap-4 text-xs text-muted">
      <span class="flex items-center gap-1"><UIcon name="i-lucide-database" class="size-3.5" />{{ libraryStats.total_count.toLocaleString() }} items</span>
      <span v-if="libraryStats.video_count" class="flex items-center gap-1"><UIcon name="i-lucide-film" class="size-3.5" />{{ libraryStats.video_count.toLocaleString() }} videos</span>
      <span v-if="libraryStats.audio_count" class="flex items-center gap-1"><UIcon name="i-lucide-music" class="size-3.5" />{{ libraryStats.audio_count.toLocaleString() }} audio</span>
      <span class="flex items-center gap-1"><UIcon name="i-lucide-hard-drive" class="size-3.5" />{{ formatBytes(libraryStats.total_size) }}</span>
    </div>

    <!-- Filters -->
    <div class="rounded-[10px] border border-[var(--hairline)] bg-[var(--surface-card)] p-4 space-y-3">
      <!-- Type chips (desktop) + search row -->
      <div class="flex flex-wrap gap-2 items-center">
        <button
          v-for="opt in TYPE_OPTIONS"
          v-show="opt.value === 'all' || params.type === opt.value || typeCounts[opt.value] === undefined || typeCounts[opt.value] > 0"
          :key="opt.value"
          :class="[
            'hidden md:inline-flex items-center px-3 py-1.5 rounded-full text-xs font-semibold transition-all border',
            params.type === opt.value
              ? 'bg-primary text-white border-primary'
              : 'bg-transparent text-muted border-white/10 hover:border-white/25 hover:text-default'
          ]"
          @click="params.type = opt.value"
        >{{ opt.label }}</button>
        <!-- Preset chips — design handoff §6.3 chip strip of curation presets -->
        <span class="hidden md:inline-block h-[22px] w-px bg-[var(--hairline-strong)] mx-1" aria-hidden="true" />
        <button
          :class="[
            'inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full text-xs font-semibold transition-all border',
            activePreset === 'trending'
              ? 'bg-[var(--accent-bg-med)] text-[var(--accent-soft)] border-[var(--accent-border)]'
              : 'bg-transparent text-muted border-white/10 hover:border-white/25 hover:text-default'
          ]"
          aria-label="Filter by trending (most viewed)"
          :aria-pressed="activePreset === 'trending'"
          @click="togglePreset('trending')"
        >
          <UIcon name="i-lucide-flame" class="size-3.5" />Trending
        </button>
        <button
          :class="[
            'inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full text-xs font-semibold transition-all border',
            activePreset === 'new'
              ? 'bg-[var(--accent-bg-med)] text-[var(--accent-soft)] border-[var(--accent-border)]'
              : 'bg-transparent text-muted border-white/10 hover:border-white/25 hover:text-default'
          ]"
          aria-label="Filter by new (recently added)"
          :aria-pressed="activePreset === 'new'"
          @click="togglePreset('new')"
        >
          <UIcon name="i-lucide-sparkle" class="size-3.5" />New
        </button>
        <!-- Clear all filters (shown only when anything is active) -->
        <button
          v-if="hasActiveFilters"
          class="inline-flex items-center gap-1 px-2 py-1.5 text-xs font-medium text-muted hover:text-default transition-colors"
          aria-label="Clear all filters"
          @click="clearAllFilters"
        >
          <UIcon name="i-lucide-x" class="size-3.5" />Clear
        </button>
        <UInput
          v-model="params.search"
          icon="i-lucide-search"
          placeholder="Search media…"
          autocomplete="on"
          name="media-search"
          class="w-64 ml-auto"
          @input="onSearchInput"
        />
      </div>
      <!-- Secondary filters row -->
      <div class="flex flex-wrap gap-3 items-center">
      <!-- Type select (mobile only) -->
      <USelect
        v-model="params.type"
        :items="TYPE_OPTIONS.filter(opt => opt.value === 'all' || params.type === opt.value || !typeCounts[opt.value] || typeCounts[opt.value] > 0)"
        class="w-36 md:hidden"
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
        icon="i-lucide-play-circle"
        label="Play All"
        variant="soft"
        color="primary"
        size="sm"
        aria-label="Play all items"
        :disabled="items.length === 0"
        @click="playAll"
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
        <p class="text-sm text-muted mr-2" aria-live="polite" aria-atomic="true">{{ total.toLocaleString() }} items</p>
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
          <UButton
            icon="i-lucide-rows-3"
            aria-label="Compact view"
            :variant="viewMode === 'compact' ? 'solid' : 'ghost'"
            :color="viewMode === 'compact' ? 'primary' : 'neutral'"
            size="sm"
            @click="viewMode = 'compact'"
          />
        </UButtonGroup>
        <UButton
          v-if="authStore.isLoggedIn"
          :icon="selectionMode ? 'i-lucide-x' : 'i-lucide-check-square'"
          :label="selectionMode ? 'Cancel' : 'Select'"
          :color="selectionMode ? 'neutral' : 'neutral'"
          variant="ghost"
          size="sm"
          @click="toggleSelectionMode"
        />
      </div>
      </div><!-- end secondary filters row -->
    </div><!-- end filter card -->

    <!-- Bulk action bar -->
    <div v-if="selectionMode && authStore.isLoggedIn" class="sticky top-14 z-30 bg-elevated border-b border-default py-2">
      <UContainer class="flex items-center gap-3 flex-wrap">
        <span class="text-sm text-muted">{{ selectedIds.size }} selected</span>
        <template v-if="selectedIds.size > 0">
          <USelect
            v-model="bulkAddPlaylistId"
            :items="myPlaylists.map(p => ({ label: p.name, value: p.id }))"
            placeholder="Choose playlist…"
            size="sm"
            class="min-w-40"
          />
          <UButton
            :loading="bulkAdding"
            :disabled="!bulkAddPlaylistId || selectedIds.size === 0"
            icon="i-lucide-list-plus"
            label="Add to Playlist"
            color="primary"
            size="sm"
            @click="bulkAddToPlaylist"
          />
        </template>
        <UButton variant="ghost" color="neutral" size="sm" label="Select All" @click="selectedIds = new Set(items.map(i => i.id))" />
        <UButton v-if="selectedIds.size > 0" variant="ghost" color="neutral" size="sm" label="Clear" @click="selectedIds = new Set()" />
      </UContainer>
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
      <component
        :is="selectionMode ? 'div' : resolveComponent('NuxtLink')"
        v-for="item in items"
        :key="item.id"
        :to="selectionMode ? undefined : matureGateHref(item)"
        :class="['group block cursor-pointer', selectionMode && selectedIds.has(item.id) ? 'ring-2 ring-primary rounded-lg' : '']"
        @mouseenter="!selectionMode && onMediaHoverEnter(item.id, item.type === 'audio')"
        @mouseleave="!selectionMode && onMediaHoverLeave()"
        @click="selectionMode ? toggleSelect(item.id, $event) : undefined"
      >
        <div
          class="relative aspect-video rounded-lg overflow-hidden mb-2 media-card-lift scanline-thumb"
          :style="item.blur_hash && item.type !== 'audio' ? { backgroundImage: `url(${blurHashToDataUrl(item.blur_hash)})`, backgroundSize: 'cover' } : {}"
        >
          <!-- Gradient fallback layer (always present for video/image, sits beneath thumbnail) -->
          <div
            v-if="item.type !== 'audio'"
            class="absolute inset-0"
            :style="{ background: getItemGradient(item.id) }"
          />
          <!-- Selection checkbox -->
          <div v-if="selectionMode" class="absolute top-1.5 left-1.5 z-10">
            <div :class="['w-5 h-5 rounded border-2 flex items-center justify-center', selectedIds.has(item.id) ? 'bg-primary border-primary' : 'bg-black/40 border-white/70']">
              <UIcon v-if="selectedIds.has(item.id)" name="i-lucide-check" class="size-3 text-white" />
            </div>
          </div>
          <img
            v-if="item.type !== 'audio' && !failedThumbnails.has(item.id)"
            :src="getThumbSrc(item.id)"
            :alt="getDisplayTitle(item)"
            width="320"
            height="180"
            :class="['absolute inset-0 w-full h-full object-cover transition-all duration-200 group-hover:scale-105', item.is_mature && !canViewMature ? 'blur-2xl scale-125 saturate-0' : '']"
            loading="lazy"
            @error="($event.target as HTMLImageElement).style.display = 'none'; onThumbnailError($event, item.id)"
          />
          <div
            v-else-if="item.type === 'audio'"
            class="w-full h-full flex flex-col items-center justify-center gap-2"
            :style="{ background: getItemGradient(item.id) }"
          >
            <AudioBars size="lg" :bars="7" class="opacity-70 group-hover:opacity-100 transition-opacity" />
            <span class="text-[10px] font-medium text-white/60 uppercase tracking-wider">{{ item.codec || 'Audio' }}</span>
          </div>
          <div v-else class="w-full h-full flex items-center justify-center">
            <UIcon name="i-lucide-film" class="size-8 text-muted" />
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
          <!-- Hover play button overlay (desktop hover) — hidden on touch
               devices since hover never fires there; a persistent mobile chip
               (below) provides the affordance instead. -->
          <div
            v-if="!selectionMode && !(item.is_mature && !canViewMature)"
            class="absolute inset-0 hidden items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity pointer-events-none [@media(hover:hover)]:flex"
          >
            <!-- 42px glass circle per handoff §6.5 hover play overlay. -->
            <div class="w-[42px] h-[42px] rounded-full bg-white/18 backdrop-blur-sm border-2 border-white/45 flex items-center justify-center">
              <UIcon name="i-lucide-play" class="size-4 text-white ml-0.5" />
            </div>
          </div>
          <!-- Mobile play hint per handoff §6.5 — small always-on chip in the
               bottom-right corner on touch devices (where hover doesn't fire).
               Uses @media (pointer: coarse) via the [@media(pointer:coarse)]
               Tailwind variant. Sits above the duration badge (bottom-1 right-1
               is reserved for duration, so we offset to bottom-7). -->
          <div
            v-if="!selectionMode && !(item.is_mature && !canViewMature)"
            class="absolute bottom-7 left-1 hidden [@media(pointer:coarse)]:flex items-center justify-center w-6 h-6 rounded-full bg-black/60 backdrop-blur-sm border border-white/30 pointer-events-none"
            aria-hidden="true"
          >
            <UIcon name="i-lucide-play" class="size-3 text-white ml-0.5" />
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
            class="absolute top-1 right-1 flex items-center gap-0.5 bg-black/70 text-[var(--rating-star)] text-xs px-1 rounded"
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
          <!-- Quick add to queue button (hover only) -->
          <button
            v-if="authStore.isLoggedIn"
            class="absolute bottom-6 right-14 p-0.5 rounded-full bg-black/50 opacity-0 group-hover:opacity-100 transition-opacity"
            aria-label="Add to queue"
            @click.prevent.stop="addToQueue(item)"
          >
            <UIcon name="i-lucide-list-ordered" class="size-4 text-white" />
          </button>
          <!-- Quick add to playlist button (hover only, does not interrupt playback) -->
          <div
            v-if="authStore.isLoggedIn"
            class="absolute bottom-6 right-7 opacity-0 group-hover:opacity-100 transition-opacity"
            @click.prevent.stop
          >
            <UDropdownMenu :items="playlistMenuItemsFor(item.id)">
              <button
                class="p-0.5 rounded-full bg-black/50"
                aria-label="Add to playlist"
              >
                <UIcon name="i-lucide-list-plus" class="size-4 text-white" />
              </button>
            </UDropdownMenu>
          </div>
        </div>
        <p class="text-sm font-semibold text-default truncate group-hover:text-primary transition-colors" :title="getDisplayTitle(item)">
          {{ getDisplayTitle(item) }}
        </p>
        <p v-if="!(item.is_mature && !canViewMature) && (item.category || item.codec || item.height || item.size || item.views)" class="text-xs text-muted truncate">
          {{ [
            item.category,
            item.type === 'audio' && item.codec ? item.codec.toUpperCase() : null,
            item.type === 'video' && item.height ? formatResolution(item.width, item.height) : null,
            !item.category && !item.codec && !item.height && item.size ? formatBytes(item.size) : null,
            item.views > 0 ? item.views.toLocaleString() + (item.views === 1 ? ' view' : ' views') : null,
          ].filter(Boolean).join(' · ') }}
        </p>
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
      </component>
      <p v-if="items.length === 0" class="col-span-full text-center py-12 text-muted">
        No media found.
      </p>
    </div>

    <!-- Compact view -->
    <div
      v-else-if="viewMode === 'compact'"
      class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 xl:grid-cols-4 gap-1.5"
    >
      <NuxtLink
        v-for="item in items"
        :key="item.id"
        :to="matureGateHref(item)"
        class="flex items-center gap-2 px-2.5 py-1.5 rounded-md hover:bg-muted/50 transition-colors group"
      >
        <UIcon
          :name="item.type === 'audio' ? 'i-lucide-music' : 'i-lucide-film'"
          :class="['size-4 shrink-0', item.type === 'audio' ? 'text-primary' : 'text-muted']"
        />
        <span class="text-sm truncate group-hover:text-primary transition-colors" :title="getDisplayTitle(item)">
          {{ getDisplayTitle(item) }}
        </span>
        <span v-if="item.codec" class="text-[10px] text-muted/60 shrink-0 uppercase">{{ item.codec }}</span>
        <span class="text-xs text-muted shrink-0 ml-auto font-mono tabular-nums">{{ formatDuration(item.duration) || formatBytes(item.size) }}</span>
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
          { accessorKey: 'size', header: 'Size' },
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
                v-if="row.original.type !== 'audio' && !failedThumbnails.has(row.original.id)"
                :src="getThumbnailUrl(row.original.id)"
                :alt="getDisplayTitle(row.original)"
                width="64"
                height="36"
                :class="['w-full h-full object-cover', row.original.is_mature && !canViewMature ? 'blur-xl saturate-0' : '']"
                loading="lazy"
                @error="($event.target as HTMLImageElement).style.display = 'none'; onThumbnailError($event, row.original.id)"
              />
              <div v-else-if="row.original.type === 'audio'" class="w-full h-full flex items-center justify-center bg-linear-to-br from-primary/10 to-primary/5">
                <AudioBars size="xs" :bars="5" />
              </div>
              <div v-else class="w-full h-full flex items-center justify-center">
                <UIcon name="i-lucide-film" class="size-4 text-muted" />
              </div>
              <div v-if="row.original.is_mature && !canViewMature" class="absolute inset-0 flex items-center justify-center bg-black/80">
                <UIcon name="i-lucide-lock" class="size-3 text-white" />
              </div>
            </div>
            <div class="min-w-0">
              <span class="font-medium truncate block max-w-xs">{{ getDisplayTitle(row.original) }}</span>
              <span v-if="row.original.codec" class="text-[10px] text-muted uppercase">{{ row.original.codec }}</span>
            </div>
          </NuxtLink>
        </template>
        <template #type-cell="{ row }">
          <UBadge
            :label="row.original.type"
            :color="row.original.type === 'audio' ? 'info' : 'neutral'"
            variant="subtle"
            size="xs"
          />
        </template>
        <template #duration-cell="{ row }">
          <span class="font-mono text-sm tabular-nums">{{ formatDuration(row.original.duration) || '—' }}</span>
        </template>
        <template #size-cell="{ row }">
          <span class="text-sm text-muted">{{ formatBytes(row.original.size) }}</span>
        </template>
        <template #views-cell="{ row }">{{ (row.original.views ?? 0).toLocaleString() }}</template>
        <template #date_added-cell="{ row }">
          <span class="text-sm text-muted" :title="row.original.date_added ? new Date(row.original.date_added).toLocaleString() : ''">
            {{ formatRelativeDate(row.original.date_added) }}
          </span>
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

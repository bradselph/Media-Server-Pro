<script setup lang="ts">
/**
 * /hub — BETA external embed catalog.
 *
 * Browses the imported hub_embeds catalog (see internal/hub). The whole catalog
 * is 18+, so the page is gated on useCanViewMature() (logged in + permission +
 * preference) AND the server-side hub.enabled flag — the same two conditions
 * that decide whether the nav tab appears. Embeds are click-to-load: the grid
 * only renders thumbnails, and a single sandboxed <iframe> is mounted inside a
 * modal on demand, never mass-mounted on page load.
 */
import type {HubEmbed, Playlist} from '~/types/api'
import {useHubApi, usePlaylistApi} from '~/composables/useApiEndpoints'
import {useHubProxyPlayback} from '~/composables/useHubProxyPlayback'
import {trackEvent} from '~/composables/useAnalytics'

definePageMeta({title: 'Hub'})

const hubApi = useHubApi()
const playlistApi = usePlaylistApi()
const authStore = useAuthStore()
const toast = useToast()
const {settings: serverSettings, load: loadServerSettings} = useServerSettings()
const canViewMature = useCanViewMature()

const hubEnabled = computed(() => serverSettings.value?.hub?.enabled === true)
const allowed = computed(() => hubEnabled.value && canViewMature.value)

// ── Query state ──────────────────────────────────────────────────────────────
// Filters, sort and the current page all live in the URL query so a refresh (or
// a shared/bookmarked link) restores the exact view the user was on instead of
// dropping them back at page 1 — the core "hold my position" fix. We use the
// short keys q/cat/sort/page to keep URLs tidy and avoid clashing with the
// global nav search (?search=).
const route = useRoute()
const router = useRouter()

const SORTS = ['views', 'rating', 'duration', 'title', 'newest'] as const
type SortKey = typeof SORTS[number]

function initialSort(): SortKey {
  const s = route.query.sort
  return typeof s === 'string' && (SORTS as readonly string[]).includes(s) ? (s as SortKey) : 'views'
}

function initialPage(): number {
  const p = route.query.page
  const n = typeof p === 'string' ? Number.parseInt(p, 10) : NaN
  return Number.isFinite(n) && n > 0 ? n : 1
}

const search = ref(typeof route.query.q === 'string' ? route.query.q : '')
const category = ref(typeof route.query.cat === 'string' ? route.query.cat : '')
const sort = ref<SortKey>(initialSort())
const page = ref(initialPage())
const categories = ref<string[]>([])

const items = ref<HubEmbed[]>([])
const total = ref(0)
const limit = 60
const loading = ref(false)
const error = ref('')

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / limit)))

const sortItems = [
  {label: 'Most viewed', value: 'views'},
  {label: 'Top rated', value: 'rating'},
  {label: 'Longest', value: 'duration'},
  {label: 'Title A–Z', value: 'title'},
  // Ordered by import position, which is the only recency signal the catalog
  // carries — labelled honestly rather than as a publish date.
  {label: 'Recently added', value: 'newest'},
]
const categoryItems = computed(() => [
  {label: 'All categories', value: ''},
  ...categories.value.map(c => ({label: c, value: c})),
])

// Discard a stale response if the user changed filters/page before it landed.
let fetchSeq = 0

async function fetchPage() {
  if (!allowed.value) return
  const seq = ++fetchSeq
  loading.value = true
  error.value = ''
  try {
    const res = await hubApi.list({
      limit,
      offset: (page.value - 1) * limit,
      search: search.value.trim() || undefined,
      category: category.value || undefined,
      sort: sort.value,
    })
    if (seq !== fetchSeq) return
    items.value = res?.items ?? []
    total.value = res?.total ?? 0
  } catch (e: unknown) {
    if (seq !== fetchSeq) return
    error.value = e instanceof Error ? e.message : 'Failed to load Hub'
  } finally {
    if (seq === fetchSeq) loading.value = false
  }
}

// A filter or sort change resets to page 1; the page number itself is driven by
// the pagination control (onPageChange). Search is debounced.
let searchTimer: ReturnType<typeof setTimeout> | null = null
watch(search, () => {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    page.value = 1
    fetchPage()
  }, 350)
})
watch([category, sort], () => {
  page.value = 1
  fetchPage()
})

function onPageChange() {
  // v-model:page has already updated `page`; fetch that page and return to the
  // top of the grid so the user isn't dropped into the middle of a fresh page.
  fetchPage()
  scrollToTop()
}

// Keep the URL in sync (debounced, router.replace so Back isn't polluted).
let urlSyncTimer: ReturnType<typeof setTimeout> | null = null
watch([search, category, sort, page], () => {
  if (urlSyncTimer) clearTimeout(urlSyncTimer)
  urlSyncTimer = setTimeout(() => {
    const query: Record<string, string> = {}
    const q = search.value.trim()
    if (q) query.q = q
    if (category.value) query.cat = category.value
    if (sort.value !== 'views') query.sort = sort.value
    if (page.value > 1) query.page = String(page.value)
    router.replace({query})
  }, 300)
})

// ── Scroll position persistence ───────────────────────────────────────────────
// The page number in the URL already restores the right page across a refresh;
// this restores the exact scroll offset within it. Keyed to a signature of the
// active view so it only re-applies when the same page+filters are shown.
function viewSignature(): string {
  return `${page.value}|${sort.value}|${category.value}|${search.value.trim()}`
}

let scrollSaveRaf = 0

function onScroll() {
  if (scrollSaveRaf) return
  scrollSaveRaf = requestAnimationFrame(() => {
    scrollSaveRaf = 0
    try {
      sessionStorage.setItem('hub:scroll', JSON.stringify({sig: viewSignature(), y: window.scrollY}))
    } catch { /* storage unavailable — degrade to no restore */ }
  })
}

function restoreScrollIfMatch() {
  try {
    const raw = sessionStorage.getItem('hub:scroll')
    if (!raw) return
    const saved = JSON.parse(raw) as { sig?: string; y?: number }
    if (saved?.sig === viewSignature() && typeof saved.y === 'number') {
      window.scrollTo(0, saved.y)
    }
  } catch { /* ignore malformed entry */ }
}

function scrollToTop() {
  if (typeof window === 'undefined') return
  window.scrollTo({top: 0, behavior: 'smooth'})
}

// ── Hover preview (single shared interval; grid mounts thumbnails only) ───────
// Frames are fetched into the browser cache before the scrub starts. Previously
// each 700ms tick swapped in a URL that had never been requested, so on a slow
// link the card flashed empty on every frame — the scrub looked broken exactly
// when it was least able to recover.
const hoverId = ref('')
const hoverFrame = ref(0)
let hoverTimer: ReturnType<typeof setInterval> | null = null
const preloadedPreviews = new Set<string>()

// A handful of frames conveys the clip; preloading all 20 would fire 20 requests
// for every card the pointer crosses.
const MAX_SCRUB_FRAMES = 8
const SCRUB_INTERVAL_MS = 700

function scrubFrames(item: HubEmbed): string[] {
  return item.preview_urls.slice(0, MAX_SCRUB_FRAMES)
}

function preloadPreviews(item: HubEmbed): Promise<void> {
  if (preloadedPreviews.has(item.embed_id)) return Promise.resolve()
  preloadedPreviews.add(item.embed_id)
  const frames = scrubFrames(item)
  return Promise.all(frames.map(src => new Promise<void>((resolve) => {
    const img = new Image()
    // Resolve on failure too: one dead frame must not stall the whole scrub.
    img.onload = () => resolve()
    img.onerror = () => resolve()
    img.src = src
  }))).then(() => undefined)
}

function startHover(item: HubEmbed) {
  hoverId.value = item.embed_id
  hoverFrame.value = 0
  if (hoverTimer) clearInterval(hoverTimer)
  if (scrubFrames(item).length < 2) return
  void preloadPreviews(item).then(() => {
    // The pointer may have left, or moved to another card, while frames loaded.
    if (hoverId.value !== item.embed_id) return
    if (hoverTimer) clearInterval(hoverTimer)
    hoverTimer = setInterval(() => {
      hoverFrame.value++
    }, SCRUB_INTERVAL_MS)
  })
}

function stopHover() {
  hoverId.value = ''
  if (hoverTimer) {
    clearInterval(hoverTimer)
    hoverTimer = null
  }
}

function cardThumb(item: HubEmbed): string {
  const frames = scrubFrames(item)
  if (hoverId.value === item.embed_id && frames.length > 0) {
    return frames[hoverFrame.value % frames.length]
  }
  return item.thumb_url
}

// Artwork can 404 for removed videos, and when it is served straight from the
// provider it can be blocked outright for some viewers. Remember which failed so
// the card renders a deliberate placeholder instead of an empty hole.
const brokenThumbs = ref<Record<string, boolean>>({})

function onThumbError(item: HubEmbed) {
  brokenThumbs.value = {...brokenThumbs.value, [item.embed_id]: true}
}

// ── Player modal (click-to-load iframe) ──────────────────────────────────────
const modalOpen = ref(false)
const active = ref<HubEmbed | null>(null)

function openEmbed(item: HubEmbed) {
  // Leaving a previous server stream attached would keep it downloading behind
  // the newly opened item.
  serverPlay.deactivate()
  active.value = item
  modalOpen.value = true
  // Try the server first when it is available to this viewer. Waiting for a
  // click meant a blocked viewer had to recognise a dead iframe and find a
  // button to fix it — but a blocked embed renders as a silent black box, and a
  // cross-origin iframe failure is not observable from JS, so nothing would ever
  // prompt them. Attaching up front is also what makes the feature work for the
  // people it exists for. Failure is free: every path in the composable clears
  // `active`, which restores the iframe below.
  if (canServerPlay.value && preferServerPlay.value) {
    const attempt = ++attemptSeq
    autoAttemptId = attempt
    // Cleared only once this attach settles, and only if it is still the current
    // one — so the sync watcher below still suppresses a failure raised during
    // the attach itself, while later mid-stream failures are reported normally.
    void serverPlay.activate(item.embed_id).finally(() => {
      if (autoAttemptId === attempt) autoAttemptId = 0
    })
  }
  // Record the play. The grid modal already has the full embed data, so this
  // fetch exists purely to trigger the server-side hub_view tracking in
  // GET /api/hub/embeds/:id (the same call the full player makes). Fire-and-
  // forget — a tracking failure must never block playback.
  void hubApi.get(item.embed_id).catch(() => {})
  trackEvent('hub_play', {embed_id: item.embed_id, title: item.title})
}

watch(modalOpen, (open) => {
  if (!open) {
    serverPlay.deactivate()
    active.value = null
  }
})

// Server-side playback.
// The provider's iframe loads in the viewer's browser, so the provider sees the
// viewer's IP - and where it blocks a region, nothing plays at all. Server-side
// playback has this server fetch the media instead and stream it on.
//
// Visibility is driven by public server settings rather than a build-time flag,
// so widening the feature from admins to everyone is a config change that takes
// effect on the next page load, with no redeploy.
const videoRef = ref<HTMLVideoElement | null>(null)
const serverPlay = useHubProxyPlayback(videoRef)

const canServerPlay = computed(() =>
  serverSettings.value?.hub?.proxy_enabled === true
  && (authStore.isAdmin || serverSettings.value?.hub?.proxy_all_users === true),
)

// Whether to attach the server stream automatically on every item. Remembered
// across items and sessions: a viewer who deliberately went back to the provider
// embed should not be pulled onto the proxy again by the next click, and a
// blocked viewer should not have to re-engage it for every video.
const PREFER_SERVER_KEY = 'hub:preferServerPlay'
const preferServerPlay = ref(true)

// Identifies the in-flight automatic attach, or 0 when the current attempt is
// the viewer's own. Automatic attempts fail quietly — a viewer who never asked
// for server playback should not get a warning toast on an item the iframe can
// play fine.
//
// A token rather than a boolean because activate() also resolves for superseded
// attempts (it returns early on a stale generation without reporting anything),
// so a previous item's `finally` would otherwise clear the flag belonging to the
// attach that replaced it, and that one's failure would toast.
let attemptSeq = 0
let autoAttemptId = 0

function toggleServerPlay() {
  if (!active.value) return
  // The viewer asked for this, so from here on failures are worth reporting.
  attemptSeq++
  autoAttemptId = 0
  if (serverPlay.active.value) {
    preferServerPlay.value = false
    persistServerPlayPreference()
    serverPlay.deactivate()
    return
  }
  preferServerPlay.value = true
  persistServerPlayPreference()
  void serverPlay.activate(active.value.embed_id)
}

function persistServerPlayPreference() {
  try {
    localStorage.setItem(PREFER_SERVER_KEY, preferServerPlay.value ? '1' : '0')
  } catch { /* storage unavailable — preference just won't persist */ }
}

// Surface failures as a toast. The iframe is still mounted underneath, so this
// reports that the enhancement did not apply - it is not a blocking error.
// flush: 'sync' so this runs at the moment the composable sets the error, still
// inside activate() — otherwise the default post-flush timing could land after
// the `finally` above cleared autoAttempt and a silent attempt would toast.
watch(serverPlay.error, (message) => {
  if (!message) return
  if (autoAttemptId !== 0) return
  toast.add({
    title: message,
    description: 'Showing the standard embed instead.',
    color: 'warning',
    icon: 'i-lucide-alert-triangle',
  })
}, {flush: 'sync'})

// ── Add to playlist ──────────────────────────────────────────────────────────
// Hub items are stored in playlists as media_id = "hub:<embed_id>" so the rest
// of the app (playlist render, player) can recognize + play them as embeds.
const playlists = ref<Playlist[]>([])
const playlistOpen = ref(false)
const addingToPlaylist = ref(false)
const playlistTarget = ref<HubEmbed | null>(null)

async function openAddToPlaylist(item: HubEmbed) {
  playlistTarget.value = item
  playlistOpen.value = true
  try {
    playlists.value = (await playlistApi.list()) ?? []
  } catch (e: unknown) {
    toast.add({title: e instanceof Error ? e.message : 'Failed to load playlists', color: 'error', icon: 'i-lucide-alert-circle'})
  }
}

async function addToPlaylist(playlistId: string) {
  if (!playlistTarget.value) return
  addingToPlaylist.value = true
  try {
    await playlistApi.addItem(playlistId, `hub:${playlistTarget.value.embed_id}`)
    toast.add({title: 'Added to playlist', color: 'success', icon: 'i-lucide-check'})
    playlistOpen.value = false
  } catch (e: unknown) {
    toast.add({title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x'})
  } finally {
    addingToPlaylist.value = false
  }
}

// ── Formatting ───────────────────────────────────────────────────────────────
function formatViews(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1).replace(/\.0$/, '') + 'M'
  if (n >= 1_000) return (n / 1_000).toFixed(1).replace(/\.0$/, '') + 'K'
  return String(n)
}

onMounted(async () => {
  try {
    // Absent key => default on, so a blocked viewer gets working playback
    // without first discovering a setting.
    preferServerPlay.value = localStorage.getItem(PREFER_SERVER_KEY) !== '0'
  } catch { /* storage unavailable — keep the default */ }
  await loadServerSettings()
  if (!allowed.value) return
  try {
    categories.value = (await hubApi.categories()) ?? []
  } catch {
    // non-fatal: filter dropdown just stays minimal
  }
  await fetchPage()
  // Wait for the grid to lay out (card aspect ratios give a deterministic height
  // even before images load) before restoring the saved scroll offset.
  await nextTick()
  restoreScrollIfMatch()
  window.addEventListener('scroll', onScroll, {passive: true})
})

onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer)
  if (urlSyncTimer) clearTimeout(urlSyncTimer)
  if (hoverTimer) clearInterval(hoverTimer)
  if (scrollSaveRaf) cancelAnimationFrame(scrollSaveRaf)
  if (typeof window !== 'undefined') window.removeEventListener('scroll', onScroll)
})
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 py-6">
    <!-- Gate: feature off OR user not permitted for mature content -->
    <div v-if="!allowed" class="py-20 text-center">
      <UIcon name="i-lucide-lock" class="size-10 text-muted mx-auto mb-4"/>
      <h1 class="text-lg font-semibold mb-2">Hub is unavailable</h1>
      <p class="text-sm text-muted max-w-md mx-auto">
        <template v-if="!hubEnabled">This feature is not enabled on this server.</template>
        <template v-else>
          The Hub contains 18+ content. Log in and enable mature-content viewing in your
          profile to access it.
        </template>
      </p>
    </div>

    <template v-else>
      <!-- Header -->
      <div class="flex flex-wrap items-center gap-3 mb-5">
        <div class="flex items-center gap-2">
          <h1 class="text-xl font-semibold">Hub</h1>
          <UBadge color="warning" variant="subtle" size="sm">BETA</UBadge>
        </div>
        <div class="flex-1"/>
        <UInput
            v-model="search"
            icon="i-lucide-search"
            placeholder="Search titles & tags…"
            class="w-full sm:w-64"
        />
        <USelect v-model="category" :items="categoryItems" class="w-44"/>
        <USelect v-model="sort" :items="sortItems" class="w-40"/>
      </div>

      <!-- Error -->
      <UAlert
          v-if="error"
          color="error"
          variant="soft"
          icon="i-lucide-alert-circle"
          :title="error"
          class="mb-4"
      />

      <!-- Loading (first page) -->
      <div v-if="loading" class="flex justify-center py-20">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-muted"/>
      </div>

      <!-- Empty -->
      <div v-else-if="items.length === 0" class="py-20 text-center">
        <UIcon name="i-lucide-clapperboard" class="size-10 text-muted mx-auto mb-4"/>
        <p class="text-sm text-muted">
          No embeds found. An administrator can import the catalog from the admin panel.
        </p>
      </div>

      <!-- Result summary -->
      <p v-else-if="total > 0" class="text-xs text-muted mb-3">
        {{ total.toLocaleString() }} result<span v-if="total !== 1">s</span>
        · page {{ page.toLocaleString() }} of {{ totalPages.toLocaleString() }}
      </p>

      <!-- Grid -->
      <div v-if="!loading && items.length > 0" class="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-4">
        <div
            v-for="item in items"
            :key="item.embed_id"
            class="group text-left rounded-lg overflow-hidden bg-muted/40 hover:ring-2 hover:ring-primary transition"
            @mouseenter="startHover(item)"
            @mouseleave="stopHover"
        >
          <div class="relative aspect-video bg-black/40 overflow-hidden">
            <!-- Play button covers the thumbnail (kept separate from the add-to-
                 playlist button so we never nest interactive elements). -->
            <button type="button" class="absolute inset-0 w-full h-full" aria-label="Play" @click="openEmbed(item)">
              <img
                  v-if="!brokenThumbs[item.embed_id]"
                  :src="cardThumb(item)"
                  :alt="item.title"
                  loading="lazy"
                  decoding="async"
                  referrerpolicy="no-referrer"
                  class="w-full h-full object-cover"
                  @error="onThumbError(item)"
              >
              <!-- Deliberate placeholder: an invisible image left the card as an
                   empty hole, which read as a broken page rather than one absent
                   thumbnail. -->
              <div v-else class="w-full h-full flex items-center justify-center bg-muted/60">
                <UIcon name="i-lucide-image-off" class="size-6 text-muted"/>
              </div>
              <div class="absolute inset-0 flex items-center justify-center opacity-0 group-hover:opacity-100 transition">
                <UIcon name="i-lucide-play" class="size-10 text-white drop-shadow"/>
              </div>
            </button>
            <span
                v-if="item.duration_secs > 0"
                class="absolute bottom-1 right-1 text-[11px] font-medium bg-black/70 text-white rounded px-1.5 py-0.5 pointer-events-none"
            >{{ formatDuration(item.duration_secs) }}</span>
            <button
                v-if="authStore.isLoggedIn"
                type="button"
                class="absolute top-1 right-1 rounded-full bg-black/70 text-white p-1.5 opacity-0 group-hover:opacity-100 transition hover:bg-primary"
                aria-label="Add to playlist"
                @click.stop="openAddToPlaylist(item)"
            >
              <UIcon name="i-lucide-list-plus" class="size-4"/>
            </button>
          </div>
          <div class="p-2">
            <p class="text-sm font-medium line-clamp-2 leading-snug">{{ item.title }}</p>
            <p class="text-xs text-muted mt-1 truncate">
              <span v-if="item.pornstar">{{ item.pornstar }} · </span>{{ formatViews(item.views) }} views
            </p>
          </div>
        </div>
      </div>

      <!-- Pagination -->
      <div v-if="!loading && totalPages > 1" class="flex justify-center mt-8">
        <UPagination
            v-model:page="page"
            :total="total"
            :items-per-page="limit"
            :sibling-count="1"
            show-edges
            @update:page="onPageChange"
        />
      </div>

      <!-- Player modal: single sandboxed iframe, mounted only while open -->
      <UModal
          v-model:open="modalOpen"
          :title="active?.title ?? 'Hub'"
          :ui="{ content: 'max-w-4xl' }"
      >
        <template #body>
          <div v-if="active">
            <div class="aspect-video w-full bg-black rounded overflow-hidden">
              <!-- Server-side playback. The iframe below stays the default and the
                   fallback: every failure path in useHubProxyPlayback clears
                   `active`, which restores it. -->
              <video
                  v-if="serverPlay.active.value"
                  ref="videoRef"
                  class="w-full h-full"
                  controls
                  autoplay
                  playsinline
              />
              <iframe
                  v-else
                  :src="active.embed_url"
                  class="w-full h-full"
                  frameborder="0"
                  scrolling="no"
                  referrerpolicy="no-referrer"
                  allow="autoplay; fullscreen; encrypted-media; picture-in-picture"
                  sandbox="allow-scripts allow-same-origin allow-popups allow-presentation"
                  allowfullscreen
              />
            </div>
            <div class="mt-3">
              <div class="flex items-center justify-between gap-2">
                <p class="text-sm font-medium">{{ active.title }}</p>
                <div class="flex items-center gap-2">
                  <UButton
                      v-if="canServerPlay"
                      :icon="serverPlay.active.value ? 'i-lucide-square-play' : 'i-lucide-server'"
                      :label="serverPlay.active.value ? 'Show embed' : 'Play on server'"
                      :loading="serverPlay.loading.value"
                      variant="outline"
                      color="primary"
                      size="xs"
                      @click="toggleServerPlay"
                  />
                  <UButton
                      v-if="authStore.isLoggedIn"
                      icon="i-lucide-list-plus"
                      label="Add to Playlist"
                      variant="outline"
                      color="neutral"
                      size="xs"
                      @click="openAddToPlaylist(active)"
                  />
                </div>
              </div>
              <p class="text-xs text-muted mt-1">
                <span v-if="active.pornstar">{{ active.pornstar }} · </span>
                {{ formatViews(active.views) }} views
                <span v-if="active.duration_secs > 0"> · {{ formatDuration(active.duration_secs) }}</span>
              </p>
              <div v-if="active.categories.length" class="flex flex-wrap gap-1.5 mt-2">
                <UBadge
                    v-for="c in active.categories.slice(0, 8)"
                    :key="c"
                    color="neutral"
                    variant="subtle"
                    size="sm"
                >{{ c }}</UBadge>
              </div>
            </div>
          </div>
        </template>
      </UModal>

      <!-- Add-to-playlist picker -->
      <UModal v-model:open="playlistOpen" title="Add to Playlist" :ui="{ content: 'max-w-sm' }">
        <template #body>
          <div class="space-y-2">
            <p v-if="playlistTarget" class="text-xs text-muted truncate">{{ playlistTarget.title }}</p>
            <div v-if="playlists.length === 0" class="py-6 text-center text-sm text-muted">
              No playlists yet. Create one from the Playlists tab.
            </div>
            <div v-else class="flex flex-col gap-1 max-h-72 overflow-y-auto">
              <UButton
                  v-for="pl in playlists"
                  :key="pl.id"
                  :label="pl.name"
                  icon="i-lucide-list-music"
                  variant="ghost"
                  color="neutral"
                  block
                  class="justify-start"
                  :loading="addingToPlaylist"
                  :disabled="addingToPlaylist"
                  @click="addToPlaylist(pl.id)"
              />
            </div>
          </div>
        </template>
      </UModal>
    </template>
  </div>
</template>

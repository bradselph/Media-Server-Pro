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

const SORTS = ['views', 'duration', 'title', 'newest'] as const
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
  {label: 'Longest', value: 'duration'},
  {label: 'Title A–Z', value: 'title'},
  {label: 'Newest', value: 'newest'},
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
const hoverId = ref('')
const hoverFrame = ref(0)
let hoverTimer: ReturnType<typeof setInterval> | null = null

function startHover(item: HubEmbed) {
  hoverId.value = item.embed_id
  hoverFrame.value = 0
  if (hoverTimer) clearInterval(hoverTimer)
  if (item.preview_urls.length > 1) {
    hoverTimer = setInterval(() => {
      hoverFrame.value++
    }, 700)
  }
}

function stopHover() {
  hoverId.value = ''
  if (hoverTimer) {
    clearInterval(hoverTimer)
    hoverTimer = null
  }
}

function cardThumb(item: HubEmbed): string {
  if (hoverId.value === item.embed_id && item.preview_urls.length > 0) {
    return item.preview_urls[hoverFrame.value % item.preview_urls.length]
  }
  return item.thumb_url
}

// ── Player modal (click-to-load iframe) ──────────────────────────────────────
const modalOpen = ref(false)
const active = ref<HubEmbed | null>(null)

function openEmbed(item: HubEmbed) {
  active.value = item
  modalOpen.value = true
  // Record the play. The grid modal already has the full embed data, so this
  // fetch exists purely to trigger the server-side hub_view tracking in
  // GET /api/hub/embeds/:id (the same call the full player makes). Fire-and-
  // forget — a tracking failure must never block playback.
  void hubApi.get(item.embed_id).catch(() => {})
  trackEvent('hub_play', {embed_id: item.embed_id, title: item.title})
}

watch(modalOpen, (open) => {
  if (!open) active.value = null
})

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

function onThumbError(e: Event) {
  // Third-party CDN URLs can 404; hide the broken image so the card stays clean.
  const el = e.target as HTMLImageElement
  el.style.visibility = 'hidden'
}

onMounted(async () => {
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
                  :src="cardThumb(item)"
                  :alt="item.title"
                  loading="lazy"
                  referrerpolicy="no-referrer"
                  class="w-full h-full object-cover"
                  @error="onThumbError"
              >
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
              <iframe
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

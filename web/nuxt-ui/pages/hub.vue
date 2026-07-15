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
import type {HubEmbed} from '~/types/api'
import {useHubApi} from '~/composables/useApiEndpoints'

definePageMeta({title: 'Hub'})

const hubApi = useHubApi()
const {settings: serverSettings, load: loadServerSettings} = useServerSettings()
const canViewMature = useCanViewMature()

const hubEnabled = computed(() => serverSettings.value?.hub?.enabled === true)
const allowed = computed(() => hubEnabled.value && canViewMature.value)

// ── Query state ──────────────────────────────────────────────────────────────
const search = ref('')
const category = ref('')
const sort = ref<'views' | 'duration' | 'title' | 'newest'>('views')
const categories = ref<string[]>([])

const items = ref<HubEmbed[]>([])
const total = ref(0)
const limit = 60
const offset = ref(0)
const loading = ref(false)
const loadingMore = ref(false)
const error = ref('')

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

async function fetchPage(reset: boolean) {
  if (!allowed.value) return
  if (reset) {
    offset.value = 0
    loading.value = true
  } else {
    loadingMore.value = true
  }
  error.value = ''
  try {
    const res = await hubApi.list({
      limit,
      offset: offset.value,
      search: search.value.trim() || undefined,
      category: category.value || undefined,
      sort: sort.value,
    })
    const batch = res?.items ?? []
    total.value = res?.total ?? 0
    items.value = reset ? batch : [...items.value, ...batch]
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to load Hub'
  } finally {
    loading.value = false
    loadingMore.value = false
  }
}

function loadMore() {
  offset.value += limit
  fetchPage(false)
}

const hasMore = computed(() => items.value.length < total.value)

// Debounced search + immediate filter/sort changes.
let searchTimer: ReturnType<typeof setTimeout> | null = null
watch(search, () => {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => fetchPage(true), 350)
})
watch([category, sort], () => fetchPage(true))

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
}

watch(modalOpen, (open) => {
  if (!open) active.value = null
})

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
  fetchPage(true)
})

onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer)
  if (hoverTimer) clearInterval(hoverTimer)
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

      <!-- Grid -->
      <div v-else class="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-4">
        <button
            v-for="item in items"
            :key="item.embed_id"
            type="button"
            class="group text-left rounded-lg overflow-hidden bg-muted/40 hover:ring-2 hover:ring-primary transition"
            @click="openEmbed(item)"
            @mouseenter="startHover(item)"
            @mouseleave="stopHover"
        >
          <div class="relative aspect-video bg-black/40 overflow-hidden">
            <img
                :src="cardThumb(item)"
                :alt="item.title"
                loading="lazy"
                referrerpolicy="no-referrer"
                class="w-full h-full object-cover"
                @error="onThumbError"
            >
            <span
                v-if="item.duration_secs > 0"
                class="absolute bottom-1 right-1 text-[11px] font-medium bg-black/70 text-white rounded px-1.5 py-0.5"
            >{{ formatDuration(item.duration_secs) }}</span>
            <div class="absolute inset-0 flex items-center justify-center opacity-0 group-hover:opacity-100 transition">
              <UIcon name="i-lucide-play" class="size-10 text-white drop-shadow"/>
            </div>
          </div>
          <div class="p-2">
            <p class="text-sm font-medium line-clamp-2 leading-snug">{{ item.title }}</p>
            <p class="text-xs text-muted mt-1 truncate">
              <span v-if="item.pornstar">{{ item.pornstar }} · </span>{{ formatViews(item.views) }} views
            </p>
          </div>
        </button>
      </div>

      <!-- Load more -->
      <div v-if="!loading && hasMore" class="flex justify-center mt-6">
        <UButton
            :loading="loadingMore"
            variant="soft"
            color="neutral"
            label="Load more"
            @click="loadMore"
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
              <p class="text-sm font-medium">{{ active.title }}</p>
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
    </template>
  </div>
</template>

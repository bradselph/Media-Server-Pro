<script setup lang="ts">
import type { MediaItem, MediaListParams, MediaListResponse, MediaCategory } from '~/types/api'

definePageMeta({
  title: 'Media Library',
})

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const mediaApi = useMediaApi()

// ── Pagination limits ──
const PAGINATION_LIMITS = [12, 24, 48, 96] as const

function normalizeLimit(value: number, fallback: number): number {
  const n = Number.isFinite(value) && value > 0 ? Math.floor(value) : fallback
  if ((PAGINATION_LIMITS as readonly number[]).includes(n)) return n
  const next = PAGINATION_LIMITS.find(m => m >= n)
  return next ?? PAGINATION_LIMITS[PAGINATION_LIMITS.length - 1]
}

// ── URL-driven filter/sort/pagination state ──
const defaultLimit = 24

const page = computed(() => Math.max(1, Number(route.query.page) || 1))
const limit = computed(() => normalizeLimit(Number(route.query.limit) || defaultLimit, defaultLimit))
const mediaType = computed(() => (route.query.type as string) || 'all')
const sortBy = computed(() => (route.query.sort as string) || 'date')
const sortOrder = computed(() => (route.query.order as string) || 'desc')
const category = computed(() => (route.query.category as string) || 'all')
const search = computed(() => (route.query.q as string) || '')

// Local search input with debounce
const searchInput = ref(search.value)

// Sync search input from route on external navigation
watch(search, (val) => {
  if (val !== searchInput.value) {
    searchInput.value = val
  }
})

/**
 * Update URL query params. Replaces history entry so Back button
 * doesn't step through every filter change.
 */
function updateParams(updates: Record<string, string | number | null>) {
  const query = { ...route.query }
  for (const [key, value] of Object.entries(updates)) {
    if (value === null || value === '') {
      delete query[key]
    } else {
      query[key] = String(value)
    }
  }
  // Clean defaults out of URL to keep it tidy
  if (query.page === '1') delete query.page
  if (query.type === 'all') delete query.type
  if (query.sort === 'date') delete query.sort
  if (query.order === 'desc') delete query.order
  if (query.category === 'all') delete query.category
  if (query.limit === String(defaultLimit)) delete query.limit
  router.replace({ query })
}

// Debounced search: sync typed input to URL param after 400ms
let searchTimeout: ReturnType<typeof setTimeout> | null = null
watch(searchInput, (val) => {
  if (searchTimeout) clearTimeout(searchTimeout)
  searchTimeout = setTimeout(() => {
    updateParams({ q: val || null, page: null })
  }, 400)
})

onUnmounted(() => {
  if (searchTimeout) clearTimeout(searchTimeout)
})

// ── Filter/sort controls ──
const showFilters = ref(true)

const typeOptions = [
  { label: 'All Media', value: 'all' },
  { label: 'Videos', value: 'video' },
  { label: 'Audio', value: 'audio' },
]

const sortOptions = [
  { label: 'Date Added', value: 'date' },
  { label: 'Name', value: 'name' },
  { label: 'File Size', value: 'size' },
  { label: 'Duration', value: 'duration' },
  { label: 'Views', value: 'views' },
]

const orderOptions = [
  { label: 'Descending', value: 'desc' },
  { label: 'Ascending', value: 'asc' },
]

const limitOptions = PAGINATION_LIMITS.map(n => ({
  label: `${n} per page`,
  value: n,
}))

// ── Data fetching ──
const mediaData = ref<MediaListResponse | null>(null)
const mediaLoading = ref(true)
const mediaError = ref<string | null>(null)
const categories = ref<MediaCategory[]>([])

/** Build API params from current URL state. */
function buildParams(): MediaListParams {
  const params: MediaListParams = {
    page: page.value,
    limit: limit.value,
  }
  if (mediaType.value !== 'all') params.type = mediaType.value
  if (sortBy.value) params.sort = sortBy.value
  if (sortOrder.value) params.sort_order = sortOrder.value
  if (category.value !== 'all') params.category = category.value
  if (search.value) params.search = search.value
  return params
}

async function fetchMedia() {
  mediaLoading.value = true
  mediaError.value = null
  try {
    mediaData.value = await mediaApi.list(buildParams())
  } catch (err: unknown) {
    mediaError.value = err instanceof Error ? err.message : 'Failed to load media'
  } finally {
    mediaLoading.value = false
  }
}

async function fetchCategories() {
  try {
    categories.value = await mediaApi.getCategories()
  } catch {
    // Non-critical; ignore
  }
}

// Fetch on mount and whenever filter/sort/page params change
watch(
  () => [page.value, limit.value, mediaType.value, sortBy.value, sortOrder.value, category.value, search.value],
  () => fetchMedia(),
  { immediate: true },
)

onMounted(() => {
  fetchCategories()
})

// ── Computed from media data ──
const items = computed(() => mediaData.value?.items ?? [])
const totalPages = computed(() => mediaData.value?.total_pages ?? 1)
const totalItems = computed(() => mediaData.value?.total_items ?? 0)
const isScanning = computed(() => mediaData.value?.scanning ?? false)

// ── Category options for the filter ──
const categoryOptions = computed(() => {
  const opts = [{ label: 'All Categories', value: 'all' }]
  for (const c of categories.value) {
    opts.push({ label: c.display_name || c.name, value: c.name })
  }
  return opts
})

// ── Helpers ──

/** Format a media item name for display (strip extension, replace separators). */
function formatTitle(name: string): string {
  // Remove file extension
  const dotIndex = name.lastIndexOf('.')
  const base = dotIndex > 0 ? name.slice(0, dotIndex) : name
  // Replace underscores and hyphens with spaces
  return base.replace(/[_-]/g, ' ')
}

/** Format duration from seconds to HH:MM:SS or MM:SS. */
function formatDuration(seconds: number): string {
  if (!seconds || seconds <= 0) return ''
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.floor(seconds % 60)
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
  return `${m}:${String(s).padStart(2, '0')}`
}

/** Format file size in human-readable form. */
function formatSize(bytes: number): string {
  if (!bytes || bytes <= 0) return ''
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let size = bytes
  while (size >= 1024 && i < units.length - 1) {
    size /= 1024
    i++
  }
  return `${size.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

/** Navigate to the player for a given media item. */
function playMedia(item: MediaItem) {
  router.push(`/player?id=${encodeURIComponent(item.id)}`)
}

/** Set page via URL params. */
function setPage(newPage: number) {
  updateParams({ page: newPage })
}

/** Refresh media data. */
function handleRefresh() {
  fetchMedia()
  fetchCategories()
}

/** Thumbnail error fallback tracker. */
const failedThumbnails = reactive(new Set<string>())

function onThumbnailError(id: string) {
  failedThumbnails.add(id)
}
</script>

<template>
  <UContainer class="py-6 space-y-6">
    <!-- Header -->
    <div class="space-y-1">
      <div class="flex items-center justify-between flex-wrap gap-4">
        <div>
          <h1 class="text-2xl font-bold text-(--ui-text-highlighted)">
            Media Library
          </h1>
          <p class="text-sm text-(--ui-text-muted)">
            Video and Music Streaming Server
          </p>
        </div>
        <div class="flex items-center gap-2">
          <UButton
            variant="ghost"
            icon="i-lucide-refresh-cw"
            :loading="mediaLoading"
            label="Refresh"
            size="sm"
            @click="handleRefresh"
          />
        </div>
      </div>
    </div>

    <!-- Search + Filter Toggle -->
    <div class="flex items-center gap-3 flex-wrap">
      <UInput
        v-model="searchInput"
        icon="i-lucide-search"
        placeholder="Search your media library..."
        class="flex-1 min-w-[200px]"
        size="lg"
        :trailing-icon="searchInput ? 'i-lucide-x' : undefined"
        @keydown.escape="searchInput = ''"
      />
      <UButton
        :icon="showFilters ? 'i-lucide-filter-x' : 'i-lucide-filter'"
        :label="showFilters ? 'Hide Filters' : 'Filters'"
        variant="soft"
        @click="showFilters = !showFilters"
      />
    </div>

    <!-- Filter Panel -->
    <div v-if="showFilters" class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-5 gap-3">
      <!-- Media Type -->
      <div class="space-y-1">
        <label class="text-xs font-medium text-(--ui-text-muted) uppercase tracking-wide">Type</label>
        <USelect
          :model-value="mediaType"
          :items="typeOptions"
          value-key="value"
          @update:model-value="(v: string) => updateParams({ type: v, page: null })"
        />
      </div>

      <!-- Sort By -->
      <div class="space-y-1">
        <label class="text-xs font-medium text-(--ui-text-muted) uppercase tracking-wide">Sort By</label>
        <USelect
          :model-value="sortBy"
          :items="sortOptions"
          value-key="value"
          @update:model-value="(v: string) => updateParams({ sort: v, page: null })"
        />
      </div>

      <!-- Sort Order -->
      <div class="space-y-1">
        <label class="text-xs font-medium text-(--ui-text-muted) uppercase tracking-wide">Order</label>
        <USelect
          :model-value="sortOrder"
          :items="orderOptions"
          value-key="value"
          @update:model-value="(v: string) => updateParams({ order: v, page: null })"
        />
      </div>

      <!-- Category -->
      <div v-if="categories.length > 0" class="space-y-1">
        <label class="text-xs font-medium text-(--ui-text-muted) uppercase tracking-wide">Category</label>
        <USelect
          :model-value="category"
          :items="categoryOptions"
          value-key="value"
          @update:model-value="(v: string) => updateParams({ category: v, page: null })"
        />
      </div>

      <!-- Results count -->
      <div class="flex items-end pb-1">
        <p class="text-sm text-(--ui-text-muted)">
          <UIcon name="i-lucide-database" class="size-4 inline" />
          {{ totalItems.toLocaleString() }} items
        </p>
      </div>
    </div>

    <!-- Error State -->
    <UAlert
      v-if="mediaError"
      color="error"
      icon="i-lucide-alert-triangle"
      title="Could not load your library"
      :description="mediaError"
    >
      <template #actions>
        <UButton
          label="Try again"
          icon="i-lucide-refresh-cw"
          variant="soft"
          color="error"
          @click="fetchMedia"
        />
      </template>
    </UAlert>

    <!-- Loading Skeleton -->
    <div
      v-else-if="mediaLoading && !mediaData"
      class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4"
    >
      <UCard v-for="i in limit" :key="i" class="overflow-hidden">
        <template #header>
          <USkeleton class="aspect-video w-full" />
        </template>
        <div class="space-y-2">
          <USkeleton class="h-4 w-3/4" />
          <USkeleton class="h-3 w-1/2" />
        </div>
      </UCard>
    </div>

    <!-- Scanning State -->
    <div
      v-else-if="items.length === 0 && isScanning"
      class="flex flex-col items-center justify-center py-16 space-y-4"
    >
      <UIcon name="i-lucide-loader-2" class="size-12 text-(--ui-text-muted) animate-spin" />
      <p class="text-lg text-(--ui-text-muted)">
        Scanning your library... give it a moment.
      </p>
    </div>

    <!-- Empty State -->
    <div
      v-else-if="items.length === 0"
      class="flex flex-col items-center justify-center py-16 space-y-4"
    >
      <UIcon name="i-lucide-film" class="size-16 text-(--ui-text-dimmed)" />
      <h3 class="text-lg font-semibold text-(--ui-text-highlighted)">
        No media found
      </h3>
      <p class="text-sm text-(--ui-text-muted) text-center max-w-md">
        <template v-if="search">
          No results for "{{ search }}". Try a different search term.
        </template>
        <template v-else>
          Add media files to your library or adjust your filters to get started.
        </template>
      </p>
      <div class="flex gap-2">
        <UButton
          v-if="search || mediaType !== 'all' || category !== 'all'"
          label="Clear Filters"
          icon="i-lucide-x"
          variant="soft"
          @click="updateParams({ q: null, type: null, category: null, page: null }); searchInput = ''"
        />
        <UButton
          v-if="!authStore.isAuthenticated"
          label="Sign in"
          icon="i-lucide-log-in"
          to="/login"
        />
      </div>
    </div>

    <!-- Media Grid -->
    <div v-else>
      <div
        class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4"
        :class="{ 'opacity-60 transition-opacity duration-300': mediaLoading }"
      >
        <UCard
          v-for="item in items"
          :key="item.id"
          class="overflow-hidden cursor-pointer group hover:ring-2 hover:ring-(--ui-primary) transition-all duration-200"
          @click="playMedia(item)"
        >
          <template #header>
            <!-- Thumbnail -->
            <div class="aspect-video bg-(--ui-bg-muted) relative overflow-hidden">
              <img
                v-if="item.thumbnail_url && !failedThumbnails.has(item.id)"
                :src="mediaApi.getThumbnailUrl(item.id)"
                :alt="formatTitle(item.name)"
                class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-300"
                loading="lazy"
                @error="onThumbnailError(item.id)"
              >
              <div
                v-else
                class="w-full h-full flex items-center justify-center"
              >
                <UIcon
                  :name="item.type === 'video' ? 'i-lucide-film' : 'i-lucide-music'"
                  class="size-10 text-(--ui-text-dimmed)"
                />
              </div>

              <!-- Type badge overlay -->
              <UBadge
                :color="item.type === 'video' ? 'info' : 'success'"
                variant="solid"
                size="xs"
                class="absolute top-2 left-2"
                :label="item.type === 'video' ? 'Video' : 'Audio'"
              />

              <!-- Mature badge -->
              <UBadge
                v-if="item.is_mature"
                color="error"
                variant="solid"
                size="xs"
                class="absolute top-2 right-2"
                label="18+"
              />

              <!-- Duration overlay -->
              <span
                v-if="item.duration && item.duration > 0"
                class="absolute bottom-2 right-2 bg-black/75 text-white text-xs px-1.5 py-0.5 rounded font-mono"
              >
                {{ formatDuration(item.duration) }}
              </span>

              <!-- Play overlay on hover -->
              <div class="absolute inset-0 flex items-center justify-center bg-black/0 group-hover:bg-black/30 transition-colors duration-200">
                <UIcon
                  name="i-lucide-play"
                  class="size-12 text-white opacity-0 group-hover:opacity-100 transition-opacity duration-200 drop-shadow-lg"
                />
              </div>
            </div>
          </template>

          <!-- Card body -->
          <div class="space-y-1">
            <p class="text-sm font-medium text-(--ui-text-highlighted) truncate" :title="formatTitle(item.name)">
              {{ formatTitle(item.name) }}
            </p>
            <div class="flex items-center gap-3 text-xs text-(--ui-text-muted)">
              <span v-if="item.views > 0" class="inline-flex items-center gap-1">
                <UIcon name="i-lucide-eye" class="size-3" />
                {{ item.views.toLocaleString() }}
              </span>
              <span v-if="item.size > 0" class="inline-flex items-center gap-1">
                <UIcon name="i-lucide-hard-drive" class="size-3" />
                {{ formatSize(item.size) }}
              </span>
              <span v-if="item.category" class="inline-flex items-center gap-1">
                <UIcon name="i-lucide-folder" class="size-3" />
                {{ item.category }}
              </span>
            </div>
          </div>
        </UCard>
      </div>

      <!-- Pagination -->
      <div
        v-if="totalPages > 1"
        class="flex flex-col sm:flex-row items-center justify-between gap-4 mt-8 pt-6 border-t border-(--ui-border)"
      >
        <p class="text-sm text-(--ui-text-muted)">
          Page {{ page }} of {{ totalPages }}
          <span class="ml-2">({{ totalItems.toLocaleString() }} items)</span>
        </p>

        <div class="flex items-center gap-4">
          <!-- Per page selector -->
          <div class="flex items-center gap-2">
            <label class="text-xs text-(--ui-text-muted)">Per page:</label>
            <USelect
              :model-value="limit"
              :items="limitOptions"
              value-key="value"
              class="w-32"
              size="sm"
              @update:model-value="(v: number) => updateParams({ limit: normalizeLimit(v, defaultLimit), page: null })"
            />
          </div>

          <!-- Pagination buttons -->
          <UPagination
            :model-value="page"
            :total="totalItems"
            :items-per-page="limit"
            @update:model-value="setPage"
          />
        </div>
      </div>
    </div>
  </UContainer>
</template>

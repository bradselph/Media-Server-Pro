<script setup lang="ts">
import type { MediaItem, MediaCategory, Suggestion } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'

definePageMeta({ title: 'Media Library' })

const mediaApi = useMediaApi()
const suggestionsApi = useSuggestionsApi()
const authStore = useAuthStore()
const router = useRouter()
const toast = useToast()

// Recommendations (only for logged-in users)
const continueWatching = ref<Suggestion[]>([])
const trending = ref<Suggestion[]>([])
const recommended = ref<Suggestion[]>([])

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

const params = reactive({
  page: 1,
  limit: authStore.user?.preferences?.items_per_page ?? 24,
  search: '',
  type: 'all',
  category: 'all',
  sort_by: 'name',
  sort_order: 'asc' as 'asc' | 'desc',
})

async function loadRecommendations() {
  if (!authStore.isLoggedIn) return
  try {
    const [cw, tr, rec] = await Promise.allSettled([
      suggestionsApi.getContinueWatching(),
      suggestionsApi.getTrending(),
      suggestionsApi.getPersonalized(12),
    ])
    if (cw.status === 'fulfilled') continueWatching.value = cw.value ?? []
    if (tr.status === 'fulfilled') trending.value = tr.value ?? []
    if (rec.status === 'fulfilled') recommended.value = rec.value ?? []
  } catch { /* non-critical */ }
}

// When the user logs in mid-session (logged-out → logged-in), reload
// recommendations and refresh the grid with their preference-based limit.
// This watcher is NOT immediate — loadRecommendations() is called from onMounted
// when the user is already logged in at page load, and we avoid a double
// load() by only reloading when the limit preference actually changed.
watch(() => authStore.isLoggedIn, (loggedIn) => {
  if (loggedIn) {
    loadRecommendations()
    const pref = authStore.user?.preferences?.items_per_page
    if (pref && pref !== params.limit) {
      params.limit = pref
      params.page = 1
      load()
    }
  }
}, { immediate: false })

let searchTimer: ReturnType<typeof setTimeout> | null = null

function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => { params.page = 1; load() }, 300)
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const apiParams = {
      ...params,
      type: params.type === 'all' ? '' : params.type,
      category: params.category === 'all' ? '' : params.category,
    }
    const res = await mediaApi.list(apiParams)
    items.value = res.items ?? []
    total.value = res.total_items ?? res.total ?? 0
    scanning.value = res.scanning ?? false
    initializing.value = res.initializing ?? false
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

onMounted(() => {
  // Apply the user's items_per_page preference before the first load so we
  // don't need a second request if it differs from the reactive default.
  const pref = authStore.user?.preferences?.items_per_page
  if (pref && pref !== params.limit) params.limit = pref
  loadCategories()
  load()
  // Fetch recommendations for already-logged-in users (page refresh).
  // When the user logs in mid-session, the watch above handles this instead.
  if (authStore.isLoggedIn) loadRecommendations()
})

// View mode
const viewMode = ref<'grid' | 'list'>('grid')

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

function formatDuration(secs?: number): string {
  if (!secs) return ''
  const h = Math.floor(secs / 3600), m = Math.floor((secs % 3600) / 60), s = Math.floor(secs % 60)
  return h > 0 ? `${h}:${String(m).padStart(2,'0')}:${String(s).padStart(2,'0')}` : `${m}:${String(s).padStart(2,'0')}`
}
</script>

<template>
  <UContainer class="py-6 space-y-6">
    <!-- Recommendations (logged-in only) -->
    <template v-if="authStore.isLoggedIn">
      <!-- Continue Watching -->
      <div v-if="continueWatching.length > 0 && authStore.user?.preferences?.show_continue_watching !== false" class="space-y-2">
        <h2 class="text-sm font-semibold text-muted flex items-center gap-2">
          <UIcon name="i-lucide-play-circle" class="size-4 text-primary" />
          Continue Watching
        </h2>
        <div class="flex gap-3 overflow-x-auto pb-2">
          <NuxtLink
            v-for="s in continueWatching"
            :key="s.media_id"
            :to="`/player?id=${encodeURIComponent(s.media_id)}`"
            class="group shrink-0 w-40"
          >
            <div class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-1.5">
              <img
                :src="mediaApi.getThumbnailUrl(s.media_id)"
                :alt="s.title"
                class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
                loading="lazy"
              />
            </div>
            <p class="text-xs font-medium truncate group-hover:text-primary transition-colors" :title="s.title">{{ s.title }}</p>
          </NuxtLink>
        </div>
      </div>

      <!-- Trending -->
      <div v-if="trending.length > 0 && authStore.user?.preferences?.show_trending !== false" class="space-y-2">
        <h2 class="text-sm font-semibold text-muted flex items-center gap-2">
          <UIcon name="i-lucide-trending-up" class="size-4 text-primary" />
          Trending
        </h2>
        <div class="flex gap-3 overflow-x-auto pb-2">
          <NuxtLink
            v-for="s in trending"
            :key="s.media_id"
            :to="`/player?id=${encodeURIComponent(s.media_id)}`"
            class="group shrink-0 w-40"
          >
            <div class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-1.5">
              <img
                :src="mediaApi.getThumbnailUrl(s.media_id)"
                :alt="s.title"
                class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
                loading="lazy"
              />
            </div>
            <p class="text-xs font-medium truncate group-hover:text-primary transition-colors" :title="s.title">{{ s.title }}</p>
          </NuxtLink>
        </div>
      </div>

      <!-- Recommended For You -->
      <div v-if="recommended.length > 0 && authStore.user?.preferences?.show_recommended !== false" class="space-y-2">
        <h2 class="text-sm font-semibold text-muted flex items-center gap-2">
          <UIcon name="i-lucide-sparkles" class="size-4 text-primary" />
          Recommended For You
        </h2>
        <div class="flex gap-3 overflow-x-auto pb-2">
          <NuxtLink
            v-for="s in recommended"
            :key="s.media_id"
            :to="`/player?id=${encodeURIComponent(s.media_id)}`"
            class="group shrink-0 w-40"
          >
            <div class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-1.5">
              <img
                :src="mediaApi.getThumbnailUrl(s.media_id)"
                :alt="s.title"
                class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
                loading="lazy"
              />
            </div>
            <p class="text-xs font-medium truncate group-hover:text-primary transition-colors" :title="s.title">{{ s.title }}</p>
          </NuxtLink>
        </div>
      </div>
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
        :items="[{ label: 'All Types', value: 'all' }, { label: 'Video', value: 'video' }, { label: 'Audio', value: 'audio' }, { label: 'Image', value: 'image' }]"
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
        :items="[{ label: 'Name', value: 'name' }, { label: 'Date Added', value: 'date_added' }, { label: 'Size', value: 'size' }, { label: 'Duration', value: 'duration' }, { label: 'Views', value: 'views' }]"
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
      >
        <div class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-2">
          <img
            v-if="item.type !== 'audio'"
            :src="mediaApi.getThumbnailUrl(item.id)"
            :alt="getDisplayTitle(item)"
            :class="['w-full h-full object-cover transition-transform duration-200 group-hover:scale-105', item.is_mature && !canViewMature ? 'blur-lg scale-110' : '']"
            loading="lazy"
          />
          <div v-else class="w-full h-full flex items-center justify-center">
            <UIcon name="i-lucide-music" class="size-8 text-muted" />
          </div>
          <!-- Mature gate overlay (guests + users with show_mature disabled) -->
          <div
            v-if="item.is_mature && !canViewMature"
            class="absolute inset-0 flex flex-col items-center justify-center bg-black/60 gap-1.5 px-2 text-center"
          >
            <UIcon name="i-lucide-lock" class="size-5 text-white" />
            <p class="text-white text-xs font-semibold leading-tight">
              {{ authStore.isLoggedIn ? 'Enable mature content\nin profile settings' : 'Sign in to view' }}
            </p>
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
        </div>
        <p class="text-sm font-medium text-default truncate group-hover:text-primary transition-colors" :title="getDisplayTitle(item)">
          {{ getDisplayTitle(item) }}
        </p>
        <p v-if="item.category && !(item.is_mature && !canViewMature)" class="text-xs text-muted truncate">{{ item.category }}</p>
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
            <div class="relative w-16 h-9 rounded overflow-hidden bg-muted shrink-0">
              <img
                :src="mediaApi.getThumbnailUrl(row.original.id)"
                :alt="getDisplayTitle(row.original)"
                :class="['w-full h-full object-cover', row.original.is_mature && !canViewMature ? 'blur-md' : '']"
                loading="lazy"
              />
              <div v-if="row.original.is_mature && !canViewMature" class="absolute inset-0 flex items-center justify-center bg-black/50">
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
          <span class="text-sm text-muted">{{ new Date(row.original.date_added).toLocaleDateString() }}</span>
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

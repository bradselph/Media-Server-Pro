<script setup lang="ts">
import type { CategoryStats, CategoryBrowseItem } from '~/types/api'
import { useCategoryBrowseApi } from '~/composables/useApiEndpoints'
import { getDisplayTitle } from '~/utils/mediaTitle'
import { formatDuration } from '~/utils/format'

definePageMeta({ layout: 'default', title: 'Browse by Category' })

const browseApi = useCategoryBrowseApi()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const toast = useToast()

const stats = ref<CategoryStats | null>(null)
const items = ref<CategoryBrowseItem[]>([])
const selectedCategory = ref<string>((route.query.category as string) || '')
const loading = ref(false)
const error = ref('')
const ITEMS_PER_PAGE = 60
const categoryPage = ref(1)

// Map category to a lucide icon
const categoryIcon: Record<string, string> = {
  'TV Shows': 'i-lucide-tv',
  'Anime': 'i-lucide-star',
  'Movies': 'i-lucide-film',
  'Documentaries': 'i-lucide-book-open',
  'Music': 'i-lucide-music',
  'Podcasts': 'i-lucide-mic',
  'Audiobooks': 'i-lucide-headphones',
  'Uncategorized': 'i-lucide-folder',
}

function iconFor(cat: string) {
  return categoryIcon[cat] ?? 'i-lucide-folder'
}

async function loadStats() {
  try {
    stats.value = await browseApi.getStats()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load categories', color: 'error', icon: 'i-lucide-x' })
  }
}

async function loadCategory(cat: string) {
  selectedCategory.value = cat
  categoryPage.value = 1
  router.replace({ query: { category: cat } })
  loading.value = true
  error.value = ''
  try {
    const res = await browseApi.getByCategory(cat)
    items.value = res?.items ?? []
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : 'Failed to load'
    items.value = []
  } finally {
    loading.value = false
  }
}

const paginatedItems = computed(() => {
  const start = (categoryPage.value - 1) * ITEMS_PER_PAGE
  return items.value.slice(start, start + ITEMS_PER_PAGE)
})

const totalCategoryPages = computed(() => Math.ceil(items.value.length / ITEMS_PER_PAGE))

// Sort key for grouping TV Shows by show name, Music by artist
const grouped = computed(() => {
  if (selectedCategory.value === 'TV Shows' || selectedCategory.value === 'Anime') {
    const map = new Map<string, CategoryBrowseItem[]>()
    for (const item of items.value) {
      const key = item.detected_info?.show_name || 'Unknown Show'
      const existing = map.get(key) ?? []
      map.set(key, [...existing, item])
    }
    return [...map.entries()].sort((a, b) => a[0].localeCompare(b[0]))
  }
  if (selectedCategory.value === 'Music') {
    const map = new Map<string, CategoryBrowseItem[]>()
    for (const item of items.value) {
      const key = item.detected_info?.artist || 'Unknown Artist'
      const existing = map.get(key) ?? []
      map.set(key, [...existing, item])
    }
    return [...map.entries()].sort((a, b) => a[0].localeCompare(b[0]))
  }
  return null // flat list for other categories
})

const availableCategories = computed(() => {
  if (!stats.value?.by_category) return []
  return Object.entries(stats.value.by_category)
    .filter(([, count]) => count > 0)
    .sort((a, b) => b[1] - a[1])
})

let hasFetched = false
async function loadAll() {
  hasFetched = true
  await loadStats()
  if (selectedCategory.value) {
    await loadCategory(selectedCategory.value)
  }
}

onMounted(() => {
  if (!authStore.isLoading && authStore.user) loadAll()
})
watch(() => authStore.user, (user) => {
  if (user && !hasFetched) loadAll()
})
</script>

<template>
  <UContainer class="py-6 max-w-6xl">
    <div class="flex items-center gap-2 mb-6">
      <UIcon name="i-lucide-layers" class="size-5 text-primary" />
      <h1 class="text-xl font-semibold">Browse by Category</h1>
    </div>

    <!-- Category tiles -->
    <div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3 mb-8">
      <button
        v-for="[cat, count] in availableCategories"
        :key="cat"
        class="flex items-center gap-3 px-4 py-3 rounded-xl border transition-colors text-left"
        :class="selectedCategory === cat
          ? 'border-primary bg-primary/10 text-primary'
          : 'border-default hover:border-primary/50 hover:bg-elevated/60'"
        @click="loadCategory(cat)"
      >
        <UIcon :name="iconFor(cat)" class="size-5 shrink-0" />
        <div>
          <p class="text-sm font-medium">{{ cat }}</p>
          <p class="text-xs text-muted">{{ count }} item{{ count !== 1 ? 's' : '' }}</p>
        </div>
      </button>
    </div>

    <!-- Items panel -->
    <div v-if="selectedCategory">
      <div class="flex items-center gap-2 mb-4">
        <UIcon :name="iconFor(selectedCategory)" class="size-4 text-primary" />
        <h2 class="text-lg font-semibold">{{ selectedCategory }}</h2>
        <UBadge v-if="items.length > 0" :label="String(items.length)" color="neutral" variant="subtle" size="xs" />
      </div>

      <div v-if="loading" class="flex justify-center py-12">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-8 text-primary" />
      </div>

      <UAlert v-else-if="error" :title="error" color="error" icon="i-lucide-alert-circle" class="mb-4" />

      <div v-else-if="items.length === 0" class="text-center py-12 text-muted">
        <UIcon name="i-lucide-inbox" class="size-10 mb-3 mx-auto opacity-40" />
        <p>No items in this category yet.</p>
        <p class="text-sm mt-1">Files are categorized automatically during the next library scan.</p>
      </div>

      <!-- Grouped view (TV Shows / Music) -->
      <template v-else-if="grouped">
        <div v-for="[group, groupItems] in grouped" :key="group" class="mb-8">
          <h3 class="text-sm font-semibold text-muted mb-2 flex items-center gap-1.5">
            <UIcon
              :name="selectedCategory === 'Music' ? 'i-lucide-music-2' : 'i-lucide-clapperboard'"
              class="size-3.5"
            />
            {{ group }}
            <span class="text-xs font-normal">({{ groupItems.length }})</span>
          </h3>
          <div class="flex gap-3 overflow-x-auto pb-2">
            <NuxtLink
              v-for="item in groupItems"
              :key="item.id"
              :to="`/player?id=${encodeURIComponent(item.id)}`"
              class="group shrink-0 w-36"
            >
              <div class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-1.5 media-card-lift scanline-thumb">
                <img
                  v-if="item.thumbnail_url"
                  :src="item.thumbnail_url"
                  :alt="item.name"
                  class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
                  loading="lazy"
                />
                <div v-else class="w-full h-full flex items-center justify-center">
                  <UIcon :name="iconFor(selectedCategory)" class="size-6 text-muted" />
                </div>
                <!-- Episode badge for TV shows -->
                <div
                  v-if="item.detected_info?.season != null && item.detected_info?.episode != null"
                  class="absolute top-1 left-1"
                >
                  <UBadge
                    :label="`S${String(item.detected_info.season).padStart(2,'0')}E${String(item.detected_info.episode).padStart(2,'0')}`"
                    color="neutral"
                    variant="solid"
                    size="xs"
                    class="bg-black/70 text-white border-0"
                  />
                </div>
                <div v-if="item.duration" class="absolute bottom-1 right-1 bg-black/70 text-white text-[10px] font-mono px-1 rounded">
                  {{ formatDuration(item.duration) }}
                </div>
              </div>
              <p class="text-xs font-medium truncate group-hover:text-primary transition-colors" :title="item.detected_info?.title || getDisplayTitle(item)">
                {{ item.detected_info?.title || getDisplayTitle(item) }}
              </p>
            </NuxtLink>
          </div>
        </div>
      </template>

      <!-- Flat grid (Movies, Documentaries, etc.) -->
      <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
        <NuxtLink
          v-for="item in paginatedItems"
          :key="item.id"
          :to="`/player?id=${encodeURIComponent(item.id)}`"
          class="group"
        >
          <div class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-1.5">
            <img
              v-if="item.thumbnail_url"
              :src="item.thumbnail_url"
              :alt="item.name"
              class="w-full h-full object-cover group-hover:scale-105 transition-transform duration-200"
              loading="lazy"
            />
            <div v-else class="w-full h-full flex items-center justify-center">
              <UIcon :name="iconFor(selectedCategory)" class="size-6 text-muted" />
            </div>
            <!-- Season/episode badge if detected -->
            <div
              v-if="item.detected_info?.season != null && item.detected_info?.episode != null"
              class="absolute top-1 left-1"
            >
              <UBadge
                :label="`S${String(item.detected_info.season).padStart(2,'0')}E${String(item.detected_info.episode).padStart(2,'0')}`"
                color="neutral"
                variant="solid"
                size="xs"
                class="bg-black/70 text-white border-0"
              />
            </div>
            <!-- Year badge if detected (and no episode info) -->
            <div
              v-else-if="item.detected_info?.year"
              class="absolute top-1 left-1 bg-black/70 text-white text-xs px-1.5 py-0.5 rounded"
            >
              {{ item.detected_info.year }}
            </div>
            <div v-if="item.duration" class="absolute bottom-1 right-1 bg-black/70 text-white text-[10px] font-mono px-1 rounded">
              {{ formatDuration(item.duration) }}
            </div>
          </div>
          <p class="text-xs font-medium truncate group-hover:text-primary transition-colors" :title="item.detected_info?.title || getDisplayTitle(item)">
            {{ item.detected_info?.title || getDisplayTitle(item) }}
          </p>
        </NuxtLink>
      </div>

      <!-- Pagination for flat grid -->
      <div v-if="!grouped && totalCategoryPages > 1" class="flex justify-center pt-4">
        <UPagination v-model:page="categoryPage" :total="items.length" :items-per-page="ITEMS_PER_PAGE" />
      </div>
    </div>

    <!-- Prompt when nothing selected yet -->
    <div v-else-if="!loading && availableCategories.length > 0" class="text-center py-12 text-muted">
      <UIcon name="i-lucide-layers" class="size-10 mb-3 mx-auto opacity-40" />
      <p>Select a category above to browse its content.</p>
    </div>

    <div v-else-if="!loading && availableCategories.length === 0" class="text-center py-12 text-muted">
      <UIcon name="i-lucide-folder-search" class="size-10 mb-3 mx-auto opacity-40" />
      <p class="font-medium">No categories found</p>
      <p class="text-sm mt-1">Run the categorizer from the Admin panel to organize your library.</p>
      <UButton
        v-if="authStore.isAdmin"
        to="/admin?tab=discovery"
        label="Open Categorizer"
        icon="i-lucide-tag"
        size="sm"
        variant="outline"
        class="mt-3"
      />
    </div>
  </UContainer>
</template>

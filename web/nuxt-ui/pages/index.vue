<script setup lang="ts">
import type { MediaItem, MediaCategory } from '~/types/api'

definePageMeta({ title: 'Media Library' })

const mediaApi = useMediaApi()
const authStore = useAuthStore()
const router = useRouter()

// State
const items = ref<MediaItem[]>([])
const categories = ref<MediaCategory[]>([])
const total = ref(0)
const loading = ref(true)

const params = reactive({
  page: 1,
  limit: 24,
  search: '',
  type: '',
  category: '',
  sort_by: 'name',
  sort_order: 'asc' as 'asc' | 'desc',
})

let searchTimer: ReturnType<typeof setTimeout> | null = null

function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => { params.page = 1; load() }, 300)
}

async function load() {
  loading.value = true
  try {
    const res = await mediaApi.list(params)
    items.value = res.items ?? []
    total.value = res.total ?? 0
  } catch {}
  finally { loading.value = false }
}

async function loadCategories() {
  try { categories.value = (await mediaApi.getCategories()) ?? [] }
  catch {}
}

watch([() => params.type, () => params.category, () => params.sort_by, () => params.sort_order], () => {
  params.page = 1
  load()
})

onMounted(() => { loadCategories(); load() })

// View mode
const viewMode = ref<'grid' | 'list'>('grid')

const totalPages = computed(() => Math.ceil(total.value / params.limit))

function formatDuration(secs?: number): string {
  if (!secs) return ''
  const h = Math.floor(secs / 3600), m = Math.floor((secs % 3600) / 60), s = Math.floor(secs % 60)
  return h > 0 ? `${h}:${String(m).padStart(2,'0')}:${String(s).padStart(2,'0')}` : `${m}:${String(s).padStart(2,'0')}`
}
</script>

<template>
  <UContainer class="py-6 space-y-6">
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
        :items="[{ label: 'All Types', value: '' }, { label: 'Video', value: 'video' }, { label: 'Audio', value: 'audio' }, { label: 'Image', value: 'image' }]"
        class="w-36"
      />
      <USelect
        v-if="categories.length > 0"
        v-model="params.category"
        :items="[{ label: 'All Categories', value: '' }, ...categories.map(c => ({ label: `${c.name} (${c.count})`, value: c.name }))]"
        class="w-48"
      />
      <USelect
        v-model="params.sort_by"
        :items="[{ label: 'Name', value: 'name' }, { label: 'Date Added', value: 'date_added' }, { label: 'Size', value: 'size' }, { label: 'Duration', value: 'duration' }, { label: 'Views', value: 'views' }]"
        class="w-36"
      />
      <UButton
        :icon="params.sort_order === 'asc' ? 'i-lucide-arrow-up-az' : 'i-lucide-arrow-down-az'"
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
            :variant="viewMode === 'grid' ? 'solid' : 'ghost'"
            :color="viewMode === 'grid' ? 'primary' : 'neutral'"
            size="xs"
            @click="viewMode = 'grid'"
          />
          <UButton
            icon="i-lucide-list"
            :variant="viewMode === 'list' ? 'solid' : 'ghost'"
            :color="viewMode === 'list' ? 'primary' : 'neutral'"
            size="xs"
            @click="viewMode = 'list'"
          />
        </UButtonGroup>
      </div>
    </div>

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
        :to="`/player?id=${encodeURIComponent(item.id)}`"
        class="group block"
      >
        <div class="relative aspect-video rounded-lg overflow-hidden bg-muted mb-2">
          <img
            v-if="item.thumbnail_url || item.id"
            :src="mediaApi.getThumbnailUrl(item.id)"
            :alt="item.name"
            class="w-full h-full object-cover transition-transform duration-200 group-hover:scale-105"
            loading="lazy"
          />
          <div v-else class="w-full h-full flex items-center justify-center">
            <UIcon
              :name="item.type === 'audio' ? 'i-lucide-music' : 'i-lucide-film'"
              class="size-8 text-muted"
            />
          </div>
          <!-- Duration badge -->
          <div
            v-if="item.duration"
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
          <!-- Mature badge -->
          <div v-if="item.is_mature" class="absolute top-1 right-1">
            <UBadge label="18+" color="error" variant="solid" size="xs" />
          </div>
        </div>
        <p class="text-sm font-medium text-default truncate group-hover:text-primary transition-colors" :title="item.name">
          {{ item.name }}
        </p>
        <p v-if="item.category" class="text-xs text-muted truncate">{{ item.category }}</p>
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
          { key: 'name', label: 'Name' },
          { key: 'type', label: 'Type' },
          { key: 'duration', label: 'Duration' },
          { key: 'category', label: 'Category' },
          { key: 'views', label: 'Views' },
          { key: 'date_added', label: 'Added' },
        ]"
      >
        <template #name-cell="{ row }">
          <NuxtLink :to="`/player?id=${encodeURIComponent(row.original.id)}`" class="flex items-center gap-3 hover:text-primary">
            <div class="w-16 h-9 rounded overflow-hidden bg-muted shrink-0">
              <img
                :src="mediaApi.getThumbnailUrl(row.original.id)"
                :alt="row.original.name"
                class="w-full h-full object-cover"
                loading="lazy"
              />
            </div>
            <span class="font-medium truncate max-w-xs">{{ row.original.name }}</span>
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

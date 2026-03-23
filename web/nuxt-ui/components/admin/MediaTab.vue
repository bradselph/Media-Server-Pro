<script setup lang="ts">
import type { MediaItem, AdminMediaListParams } from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

const items = ref<MediaItem[]>([])
const loading = ref(true)
const scanning = ref(false)
const totalItems = ref(0)
const totalPages = ref(1)

const params = reactive<AdminMediaListParams>({
  page: 1, limit: 20, search: '', sort: 'name', sort_order: 'asc', type: '', is_mature: '',
})

let searchTimer: ReturnType<typeof setTimeout> | null = null

function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => { params.page = 1; load() }, 300)
}

async function load() {
  loading.value = true
  try {
    const res = await adminApi.listMedia(params)
    items.value = res.items ?? []
    totalItems.value = res.total_items ?? 0
    totalPages.value = res.total_pages ?? 1
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load media', color: 'error', icon: 'i-lucide-x' })
  } finally {
    loading.value = false
  }
}

async function handleScan() {
  scanning.value = true
  try {
    await adminApi.scanMedia()
    toast.add({ title: 'Media scan triggered', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Scan failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    scanning.value = false
  }
}

function formatBytes(bytes?: number): string {
  if (!bytes) return '—'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / k ** i).toFixed(1)} ${sizes[i]}`
}

function formatDuration(secs?: number): string {
  if (!secs) return '—'
  const h = Math.floor(secs / 3600)
  const m = Math.floor((secs % 3600) / 60)
  const s = Math.floor(secs % 60)
  return h > 0 ? `${h}:${String(m).padStart(2,'0')}:${String(s).padStart(2,'0')}` : `${m}:${String(s).padStart(2,'0')}`
}

function sortBy(col: string) {
  if (params.sort === col) {
    params.sort_order = params.sort_order === 'asc' ? 'desc' : 'asc'
  } else {
    params.sort = col
    params.sort_order = 'asc'
  }
  params.page = 1
  load()
}

watch([() => params.type, () => params.is_mature], () => { params.page = 1; load() })

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <!-- Toolbar -->
    <div class="flex flex-wrap gap-2 items-center justify-between">
      <div class="flex flex-wrap gap-2">
        <UInput
          v-model="params.search"
          icon="i-lucide-search"
          placeholder="Search media…"
          class="w-52"
          @input="onSearchInput"
        />
        <USelect
          v-model="params.type"
          :items="[
            { label: 'All Types', value: '' },
            { label: 'Video', value: 'video' },
            { label: 'Audio', value: 'audio' },
            { label: 'Image', value: 'image' },
          ]"
          class="w-36"
        />
        <USelect
          v-model="params.is_mature"
          :items="[
            { label: 'All Content', value: '' },
            { label: 'SFW Only', value: 'false' },
            { label: 'Mature Only', value: 'true' },
          ]"
          class="w-40"
        />
      </div>
      <div class="flex gap-2">
        <UButton
          icon="i-lucide-scan"
          label="Scan Library"
          :loading="scanning"
          variant="outline"
          color="neutral"
          @click="handleScan"
        />
        <UButton icon="i-lucide-refresh-cw" variant="ghost" color="neutral" @click="load" />
      </div>
    </div>

    <p class="text-sm text-(--ui-text-muted)">
      {{ totalItems.toLocaleString() }} items
    </p>

    <!-- Table -->
    <UCard>
      <div v-if="loading" class="flex justify-center py-8">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-primary" />
      </div>
      <UTable
        v-else
        :data="items"
        :columns="[
          { key: 'name', label: 'Name' },
          { key: 'type', label: 'Type' },
          { key: 'size', label: 'Size' },
          { key: 'duration', label: 'Duration' },
          { key: 'category', label: 'Category' },
          { key: 'views', label: 'Views' },
          { key: 'is_mature', label: 'Mature' },
          { key: 'actions', label: '' },
        ]"
      >
        <template #name-cell="{ row }">
          <div class="max-w-xs truncate text-sm font-medium" :title="row.original.name">
            {{ row.original.name }}
          </div>
        </template>
        <template #type-cell="{ row }">
          <UBadge :label="row.original.type" color="neutral" variant="subtle" size="xs" />
        </template>
        <template #size-cell="{ row }">
          <span class="text-sm">{{ formatBytes(row.original.size) }}</span>
        </template>
        <template #duration-cell="{ row }">
          <span class="text-sm font-mono">{{ formatDuration(row.original.duration) }}</span>
        </template>
        <template #category-cell="{ row }">
          <span class="text-sm text-(--ui-text-muted)">{{ row.original.category || '—' }}</span>
        </template>
        <template #views-cell="{ row }">
          <span class="text-sm">{{ (row.original.views ?? 0).toLocaleString() }}</span>
        </template>
        <template #is_mature-cell="{ row }">
          <UBadge
            v-if="row.original.is_mature"
            label="Mature"
            color="error"
            variant="subtle"
            size="xs"
          />
          <span v-else class="text-(--ui-text-muted) text-sm">—</span>
        </template>
        <template #actions-cell="{ row }">
          <div class="flex items-center gap-1 justify-end">
            <UButton
              icon="i-lucide-image"
              size="xs"
              variant="ghost"
              color="neutral"
              title="Generate thumbnail"
              @click="adminApi.generateThumbnail(row.original.id).then(() => toast.add({ title: 'Thumbnail queued', color: 'success', icon: 'i-lucide-check' }))"
            />
            <UButton
              icon="i-lucide-trash-2"
              size="xs"
              variant="ghost"
              color="error"
              title="Delete"
              @click="adminApi.deleteMedia(row.original.id).then(load)"
            />
          </div>
        </template>
      </UTable>
      <p v-if="!loading && items.length === 0" class="text-center py-6 text-(--ui-text-muted) text-sm">
        No media found.
      </p>
    </UCard>

    <!-- Pagination -->
    <div v-if="totalPages > 1" class="flex justify-center">
      <UPagination
        v-model:page="params.page"
        :total="totalItems"
        :items-per-page="params.limit"
        @update:page="load"
      />
    </div>
  </div>
</template>

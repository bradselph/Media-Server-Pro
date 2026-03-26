<script setup lang="ts">
import type { MediaItem, AdminMediaListParams } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'

const adminApi = useAdminApi()
const toast = useToast()

const items = ref<MediaItem[]>([])
const loading = ref(true)
const scanning = ref(false)
const totalItems = ref(0)
const totalPages = ref(1)

// Bulk selection
const selectedIds = ref(new Set<string>())
const allPageSelected = computed(() =>
  items.value.length > 0 && items.value.every(item => selectedIds.value.has(item.id)),
)
function toggleSelectAll() {
  if (allPageSelected.value) {
    items.value.forEach(item => selectedIds.value.delete(item.id))
  } else {
    items.value.forEach(item => selectedIds.value.add(item.id))
  }
  selectedIds.value = new Set(selectedIds.value)
}
function toggleSelect(id: string) {
  const next = new Set(selectedIds.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selectedIds.value = next
}
const bulkRunning = ref(false)
async function runBulk(action: 'delete' | 'update', data?: { category?: string; is_mature?: boolean }) {
  const ids = [...selectedIds.value]
  if (!ids.length) return
  bulkRunning.value = true
  try {
    const res = await adminApi.bulkMedia(ids, action, data)
    const msg = action === 'delete' ? `Deleted ${res?.success ?? ids.length} items` : `Updated ${res?.success ?? ids.length} items`
    toast.add({ title: msg, color: 'success', icon: 'i-lucide-check' })
    selectedIds.value = new Set()
    load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Bulk action failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    bulkRunning.value = false
  }
}

// Edit modal
const editTarget = ref<MediaItem | null>(null)
const editOpen = computed({
  get: () => !!editTarget.value,
  set: (v: boolean) => { if (!v) editTarget.value = null },
})
const editForm = reactive({ name: '', category: '', is_mature: false })
const editSaving = ref(false)

function openEdit(item: MediaItem) {
  editTarget.value = item
  editForm.name = item.name
  editForm.category = item.category ?? ''
  editForm.is_mature = item.is_mature ?? false
}

async function saveEdit() {
  if (!editTarget.value) return
  editSaving.value = true
  try {
    await adminApi.updateMedia(editTarget.value.id, {
      name: editForm.name,
      category: editForm.category,
      is_mature: editForm.is_mature,
    })
    toast.add({ title: 'Media updated', color: 'success', icon: 'i-lucide-check' })
    editTarget.value = null
    load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Update failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    editSaving.value = false
  }
}

const params = reactive<AdminMediaListParams>({
  page: 1, limit: 20, search: '', sort: 'name', sort_order: 'asc', type: 'all', is_mature: 'all',
})

let searchTimer: ReturnType<typeof setTimeout> | null = null

function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => { params.page = 1; load() }, 300)
}

async function load() {
  loading.value = true
  try {
    const apiParams = {
      ...params,
      type: params.type === 'all' ? '' : params.type,
      is_mature: params.is_mature === 'all' ? '' : params.is_mature,
    }
    const res = await adminApi.listMedia(apiParams)
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
            { label: 'All Types', value: 'all' },
            { label: 'Video', value: 'video' },
            { label: 'Audio', value: 'audio' },
            { label: 'Image', value: 'image' },
          ]"
          class="w-36"
        />
        <USelect
          v-model="params.is_mature"
          :items="[
            { label: 'All Content', value: 'all' },
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
        <UButton icon="i-lucide-refresh-cw" aria-label="Refresh media list" variant="ghost" color="neutral" @click="load" />
      </div>
    </div>

    <div class="flex items-center justify-between">
      <p class="text-sm text-muted">
        {{ totalItems.toLocaleString() }} items
      </p>
      <span v-if="selectedIds.size > 0" class="text-sm text-primary font-medium">
        {{ selectedIds.size }} selected
      </span>
    </div>

    <!-- Bulk action bar -->
    <div v-if="selectedIds.size > 0" class="flex flex-wrap items-center gap-2 p-3 bg-elevated rounded-lg border border-default">
      <span class="text-sm font-medium">{{ selectedIds.size }} item{{ selectedIds.size !== 1 ? 's' : '' }} selected</span>
      <UButton
        icon="i-lucide-shield"
        label="Mark Mature"
        size="xs"
        variant="outline"
        color="warning"
        :loading="bulkRunning"
        @click="runBulk('update', { is_mature: true })"
      />
      <UButton
        icon="i-lucide-shield-off"
        label="Unmark Mature"
        size="xs"
        variant="outline"
        color="neutral"
        :loading="bulkRunning"
        @click="runBulk('update', { is_mature: false })"
      />
      <UButton
        icon="i-lucide-trash-2"
        label="Delete Selected"
        size="xs"
        variant="outline"
        color="error"
        :loading="bulkRunning"
        @click="runBulk('delete')"
      />
      <UButton
        icon="i-lucide-x"
        label="Clear"
        size="xs"
        variant="ghost"
        color="neutral"
        @click="selectedIds = new Set()"
      />
    </div>

    <!-- Table -->
    <UCard>
      <div v-if="loading" class="flex justify-center py-8">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-primary" />
      </div>
      <UTable
        v-else
        :data="items"
        :columns="[
          { accessorKey: '_select', header: '' },
          { accessorKey: 'name', header: 'Name' },
          { accessorKey: 'type', header: 'Type' },
          { accessorKey: 'size', header: 'Size' },
          { accessorKey: 'duration', header: 'Duration' },
          { accessorKey: 'category', header: 'Category' },
          { accessorKey: 'views', header: 'Views' },
          { accessorKey: 'is_mature', header: 'Mature' },
          { accessorKey: 'actions', header: '' },
        ]"
      >
        <template #_select-header>
          <UCheckbox
            :model-value="allPageSelected"
            aria-label="Select all"
            @update:model-value="toggleSelectAll"
          />
        </template>
        <template #_select-cell="{ row }">
          <UCheckbox
            :model-value="selectedIds.has(row.original.id)"
            aria-label="Select row"
            @update:model-value="toggleSelect(row.original.id)"
          />
        </template>

        <template #name-cell="{ row }">
          <div class="max-w-xs truncate text-sm font-medium" :title="getDisplayTitle(row.original)">
            {{ getDisplayTitle(row.original) }}
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
          <span class="text-sm text-muted">{{ row.original.category || '—' }}</span>
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
          <span v-else class="text-muted text-sm">—</span>
        </template>
        <template #actions-cell="{ row }">
          <div class="flex items-center gap-1 justify-end">
            <UButton
              icon="i-lucide-pencil"
              size="xs"
              variant="ghost"
              color="neutral"
              title="Edit"
              @click="openEdit(row.original)"
            />
            <UButton
              icon="i-lucide-image"
              size="xs"
              variant="ghost"
              color="neutral"
              title="Generate thumbnail"
              @click="adminApi.generateThumbnail(row.original.id).then(() => toast.add({ title: 'Thumbnail queued', color: 'success', icon: 'i-lucide-check' })).catch((e: unknown) => toast.add({ title: e instanceof Error ? e.message : 'Thumbnail failed', color: 'error', icon: 'i-lucide-x' }))"
            />
            <UButton
              icon="i-lucide-trash-2"
              size="xs"
              variant="ghost"
              color="error"
              title="Delete"
              @click="adminApi.deleteMedia(row.original.id).then(load).catch((e: unknown) => toast.add({ title: e instanceof Error ? e.message : 'Delete failed', color: 'error', icon: 'i-lucide-x' }))"
            />
          </div>
        </template>
      </UTable>
      <p v-if="!loading && items.length === 0" class="text-center py-6 text-muted text-sm">
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

    <!-- Edit media modal -->
    <UModal v-model:open="editOpen" title="Edit Media">
      <template #body>
        <div class="space-y-4">
          <UFormField label="Name">
            <UInput v-model="editForm.name" class="w-full" />
          </UFormField>
          <UFormField label="Category">
            <UInput v-model="editForm.category" placeholder="e.g. Entertainment" class="w-full" />
          </UFormField>
          <UFormField label="Mature content">
            <UCheckbox v-model="editForm.is_mature" label="Mark as 18+ content" />
          </UFormField>
        </div>
      </template>
      <template #footer>
        <UButton label="Save" :loading="editSaving" color="primary" @click="saveEdit" />
        <UButton label="Cancel" variant="ghost" color="neutral" @click="editOpen = false" />
      </template>
    </UModal>
  </div>
</template>

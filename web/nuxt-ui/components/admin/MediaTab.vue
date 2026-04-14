<script setup lang="ts">
import type { MediaItem, AdminMediaListParams, ThumbnailStats } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'
import { formatBytes, formatDuration } from '~/utils/format'

const adminApi = useAdminApi()
const hlsApi = useHlsApi()
const toast = useToast()

const thumbStats = ref<ThumbnailStats | null>(null)
async function loadThumbStats() {
  try { thumbStats.value = await adminApi.getThumbnailStats() }
  catch { /* optional — suppress if endpoint unavailable */ }
}

const items = ref<MediaItem[]>([])
const loading = ref(true)
const scanning = ref(false)
const totalItems = ref(0)
const totalPages = ref(1)

// Confirmation refs
const confirmDeleteId = ref<string | null>(null)
const confirmBulkDelete = ref(false)

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
  if (action === 'delete') {
    confirmBulkDelete.value = true
    return
  }
  await executeBulk(action, data)
}
async function executeBulk(action: 'delete' | 'update', data?: { category?: string; is_mature?: boolean }) {
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
const editForm = reactive({ name: '', category: '', is_mature: false, tags: '', description: '' })
const editSaving = ref(false)

function openEdit(item: MediaItem) {
  editTarget.value = item
  editForm.name = item.name
  editForm.category = item.category ?? ''
  editForm.is_mature = item.is_mature ?? false
  editForm.tags = (item.tags ?? []).join(', ')
  editForm.description = (item.metadata?.description ?? '') as string
}

async function saveEdit() {
  if (!editTarget.value) return
  editSaving.value = true
  try {
    await adminApi.updateMedia(editTarget.value.id, {
      name: editForm.name,
      category: editForm.category,
      is_mature: editForm.is_mature,
      tags: editForm.tags.split(',').map((t: string) => t.trim()).filter(Boolean),
      metadata: { ...editTarget.value.metadata, description: editForm.description },
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
// Generation counter — prevents stale out-of-order responses from overwriting fresh results.
let loadSeq = 0

function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => { params.page = 1; load() }, 300)
}

async function load() {
  const seq = ++loadSeq
  loading.value = true
  try {
    const apiParams = {
      ...params,
      type: params.type === 'all' ? '' : params.type,
      is_mature: params.is_mature === 'all' ? '' : params.is_mature,
    }
    const res = await adminApi.listMedia(apiParams)
    if (seq !== loadSeq) return
    items.value = res.items ?? []
    totalItems.value = res.total_items ?? 0
    totalPages.value = res.total_pages ?? 1
  } catch (e: unknown) {
    if (seq !== loadSeq) return
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load media', color: 'error', icon: 'i-lucide-x' })
  } finally {
    if (seq === loadSeq) loading.value = false
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

// Per-row loading guards to prevent duplicate operations
const rowBusy = ref<Set<string>>(new Set())

async function generateThumbnail(id: string) {
  if (rowBusy.value.has(`thumb-${id}`)) return
  const next = new Set(rowBusy.value); next.add(`thumb-${id}`); rowBusy.value = next
  try {
    await adminApi.generateThumbnail(id)
    toast.add({ title: 'Thumbnail queued', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Thumbnail failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    const cleared = new Set(rowBusy.value); cleared.delete(`thumb-${id}`); rowBusy.value = cleared
  }
}

async function generateHLS(id: string) {
  if (rowBusy.value.has(`hls-${id}`)) return
  const next = new Set(rowBusy.value); next.add(`hls-${id}`); rowBusy.value = next
  try {
    await hlsApi.generate(id)
    toast.add({ title: 'HLS generation started', color: 'info', icon: 'i-lucide-info' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'HLS generation failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    const cleared = new Set(rowBusy.value); cleared.delete(`hls-${id}`); rowBusy.value = cleared
  }
}

function confirmDelete(id: string) {
  confirmDeleteId.value = id
}
async function executeDelete() {
  const id = confirmDeleteId.value
  if (!id) return
  confirmDeleteId.value = null
  await deleteMediaItem(id)
}
async function deleteMediaItem(id: string) {
  if (rowBusy.value.has(`del-${id}`)) return
  const next = new Set(rowBusy.value); next.add(`del-${id}`); rowBusy.value = next
  try {
    await adminApi.deleteMedia(id)
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Delete failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    const cleared = new Set(rowBusy.value); cleared.delete(`del-${id}`); rowBusy.value = cleared
  }
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

watch([() => params.type, () => params.is_mature], () => {
  params.page = 1
  selectedIds.value = new Set()
  load()
})

// Clear selection when the page changes so that invisible items from previous
// pages are never silently included in bulk operations.
watch(() => params.page, () => {
  selectedIds.value = new Set()
})

const route = useRoute()
const mediaApi = useMediaApi()
onMounted(async () => {
  await load()
  loadThumbStats()
  // Auto-open edit modal when linked from player page (?edit=mediaId)
  const editId = route.query.edit as string | undefined
  if (editId) {
    const item = items.value.find(i => i.id === editId)
    if (item) openEdit(item)
    else {
      // Item may not be on page 1; fetch it directly via public media endpoint
      try {
        const found = await mediaApi.getById(editId)
        if (found) openEdit(found)
      } catch { /* best-effort */ }
    }
  }
})
onUnmounted(() => { if (searchTimer) clearTimeout(searchTimer) })
</script>

<template>
  <div class="space-y-4">
    <!-- Thumbnail stats -->
    <div v-if="thumbStats" class="grid grid-cols-2 sm:grid-cols-4 gap-3">
      <UCard :ui="{ body: 'p-3' }">
        <p class="text-xl font-bold text-highlighted">{{ thumbStats.total_thumbnails.toLocaleString() }}</p>
        <p class="text-xs text-muted">Thumbnails</p>
      </UCard>
      <UCard :ui="{ body: 'p-3' }">
        <p class="text-xl font-bold text-highlighted">{{ thumbStats.total_size_mb.toFixed(1) }} MB</p>
        <p class="text-xs text-muted">Thumbnail Size</p>
      </UCard>
      <UCard :ui="{ body: 'p-3' }">
        <p class="text-xl font-bold" :class="thumbStats.pending_generation > 0 ? 'text-warning' : 'text-highlighted'">{{ thumbStats.pending_generation.toLocaleString() }}</p>
        <p class="text-xs text-muted">Pending Generation</p>
      </UCard>
      <UCard :ui="{ body: 'p-3' }">
        <p class="text-xl font-bold" :class="thumbStats.generation_errors > 0 ? 'text-error' : 'text-highlighted'">{{ thumbStats.generation_errors.toLocaleString() }}</p>
        <p class="text-xs text-muted">Generation Errors</p>
      </UCard>
    </div>

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
              :loading="rowBusy.has(`thumb-${row.original.id}`)"
              @click="generateThumbnail(row.original.id)"
            />
            <UButton
              v-if="row.original.type !== 'audio'"
              icon="i-lucide-video"
              size="xs"
              variant="ghost"
              color="neutral"
              title="Generate HLS"
              :loading="rowBusy.has(`hls-${row.original.id}`)"
              @click="generateHLS(row.original.id)"
            />
            <UButton
              icon="i-lucide-trash-2"
              size="xs"
              variant="ghost"
              color="error"
              title="Delete"
              :loading="rowBusy.has(`del-${row.original.id}`)"
              @click="confirmDelete(row.original.id)"
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

    <!-- Single-item delete confirmation -->
    <UModal
      :open="!!confirmDeleteId"
      title="Delete Media Item"
      @update:open="val => { if (!val) confirmDeleteId = null }"
    >
      <template #body>
        <p>Are you sure you want to delete this media item? This action cannot be undone.</p>
      </template>
      <template #footer>
        <UButton variant="ghost" color="neutral" label="Cancel" @click="confirmDeleteId = null" />
        <UButton color="error" label="Delete" :loading="!!confirmDeleteId && rowBusy.has(`del-${confirmDeleteId}`)" @click="executeDelete" />
      </template>
    </UModal>

    <!-- Bulk delete confirmation -->
    <UModal
      :open="confirmBulkDelete"
      title="Delete Selected Items"
      @update:open="val => { if (!val) confirmBulkDelete = false }"
    >
      <template #body>
        <p>Are you sure you want to delete <strong>{{ selectedIds.size }}</strong> selected item{{ selectedIds.size !== 1 ? 's' : '' }}? This action cannot be undone.</p>
      </template>
      <template #footer>
        <UButton variant="ghost" color="neutral" label="Cancel" @click="confirmBulkDelete = false" />
        <UButton color="error" label="Delete" :loading="bulkRunning" @click="confirmBulkDelete = false; executeBulk('delete')" />
      </template>
    </UModal>

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
          <UFormField label="Tags" hint="Comma-separated (e.g. action, sci-fi)">
            <UInput v-model="editForm.tags" placeholder="e.g. action, comedy" class="w-full" />
          </UFormField>
          <UFormField label="Description">
            <UTextarea v-model="editForm.description" placeholder="Short description (stored in metadata)" :rows="3" class="w-full" />
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

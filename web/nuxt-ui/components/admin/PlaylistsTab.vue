<script setup lang="ts">
import type { Playlist, AdminPlaylistStats } from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

const playlists = ref<Playlist[]>([])
const loading = ref(true)
const deleteTarget = ref<Playlist | null>(null)
const deleting = ref(false)
const stats = ref<AdminPlaylistStats | null>(null)

// Search, filter, sort, pagination
const search = ref('')
const visibilityFilter = ref<'all' | 'public' | 'private'>('all')
const sortKey = ref<'name' | 'user_id' | 'items' | 'is_public' | 'created_at'>('name')
const sortDir = ref<'asc' | 'desc'>('asc')
const page = ref(1)
const perPage = ref(25)

const PER_PAGE_OPTIONS = [
  { label: '25', value: 25 },
  { label: '50', value: 50 },
  { label: '100', value: 100 },
]

function doSort(key: typeof sortKey.value) {
  if (sortKey.value === key) {
    sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
  } else {
    sortKey.value = key
    sortDir.value = 'asc'
  }
}

function sortIndicator(key: string) {
  if (sortKey.value !== key) return ''
  return sortDir.value === 'asc' ? ' \u2191' : ' \u2193'
}

const filtered = computed(() => {
  let result = playlists.value
  if (search.value) {
    const q = search.value.toLowerCase()
    result = result.filter(p =>
      p.name.toLowerCase().includes(q) || (p.user_id ?? '').toLowerCase().includes(q),
    )
  }
  if (visibilityFilter.value === 'public') result = result.filter(p => p.is_public)
  else if (visibilityFilter.value === 'private') result = result.filter(p => !p.is_public)

  result = [...result].sort((a, b) => {
    let cmp = 0
    switch (sortKey.value) {
      case 'name': cmp = a.name.localeCompare(b.name); break
      case 'user_id': cmp = (a.user_id ?? '').localeCompare(b.user_id ?? ''); break
      case 'items': cmp = (a.items?.length ?? 0) - (b.items?.length ?? 0); break
      case 'is_public': cmp = Number(a.is_public) - Number(b.is_public); break
      case 'created_at': cmp = (a.created_at ?? '').localeCompare(b.created_at ?? ''); break
    }
    return sortDir.value === 'asc' ? cmp : -cmp
  })
  return result
})

const totalFiltered = computed(() => filtered.value.length)
const totalPages = computed(() => Math.max(1, Math.ceil(totalFiltered.value / perPage.value)))
const paged = computed(() => {
  const start = (page.value - 1) * perPage.value
  return filtered.value.slice(start, start + perPage.value)
})

watch([search, visibilityFilter, sortKey, sortDir, perPage], () => { page.value = 1 })

// Bulk selection
const selected = ref<Set<string>>(new Set())
const bulkDeleting = ref(false)
const confirmBulkDelete = ref(false)
const allSelected = computed(() => paged.value.length > 0 && paged.value.every(p => selected.value.has(p.id)))

function toggleSelect(id: string) {
  const s = new Set(selected.value)
  s.has(id) ? s.delete(id) : s.add(id)
  selected.value = s
}

function toggleAll() {
  if (allSelected.value) {
    selected.value = new Set()
  } else {
    selected.value = new Set(paged.value.map(p => p.id))
  }
}

function bulkDelete() {
  if (!selected.value.size) return
  confirmBulkDelete.value = true
}
async function executeBulkDelete() {
  if (!selected.value.size || bulkDeleting.value) return
  bulkDeleting.value = true
  try {
    const ids = Array.from(selected.value)
    const res = await adminApi.bulkDeletePlaylists(ids)
    const plural = res.success === 1 ? '' : 's'
    const failedMsg = res.failed ? `, ${res.failed} failed` : ''
    toast.add({ title: `Deleted ${res.success} playlist${plural}${failedMsg}`, color: res.failed ? 'warning' : 'success', icon: 'i-lucide-check' })
    selected.value = new Set()
    confirmBulkDelete.value = false
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Bulk delete failed', color: 'error', icon: 'i-lucide-x' })
  } finally { bulkDeleting.value = false }
}

async function load() {
  loading.value = true
  try {
    const [res, s] = await Promise.allSettled([
      adminApi.listAllPlaylists({ limit: 10000 }),
      adminApi.getPlaylistStats(),
    ])
    if (res.status === 'fulfilled') {
      playlists.value = res.value?.items ?? []
    }
    if (s.status === 'fulfilled') stats.value = s.value
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load playlists', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { loading.value = false }
}

async function handleDelete() {
  if (!deleteTarget.value) return
  deleting.value = true
  try {
    await adminApi.deletePlaylist(deleteTarget.value.id)
    toast.add({ title: 'Playlist deleted', color: 'success', icon: 'i-lucide-check' })
    deleteTarget.value = null
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    deleting.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <!-- Stats banner -->
    <div class="grid grid-cols-3 gap-3">
      <UCard :ui="{ body: 'p-3' }">
        <p class="text-xl font-bold text-highlighted">{{ stats?.total_playlists ?? playlists.length }}</p>
        <p class="text-xs text-muted mt-1">Total Playlists</p>
      </UCard>
      <UCard :ui="{ body: 'p-3' }">
        <p class="text-xl font-bold text-highlighted">{{ stats?.public_playlists ?? playlists.filter(p => p.is_public).length }}</p>
        <p class="text-xs text-muted mt-1">Public</p>
      </UCard>
      <UCard :ui="{ body: 'p-3' }">
        <p class="text-xl font-bold text-highlighted">{{ stats?.total_items ?? playlists.reduce((s, p) => s + (p.items?.length ?? 0), 0) }}</p>
        <p class="text-xs text-muted mt-1">Total Items</p>
      </UCard>
    </div>

    <!-- Toolbar: search, filter, per-page -->
    <div class="flex flex-wrap gap-2 items-center justify-between">
      <div class="flex flex-wrap gap-2 items-center">
        <UInput
          v-model="search"
          icon="i-lucide-search"
          placeholder="Search name or owner…"
          class="w-56"
        />
        <USelect
          v-model="visibilityFilter"
          :items="[
            { label: 'All Visibility', value: 'all' },
            { label: 'Public', value: 'public' },
            { label: 'Private', value: 'private' },
          ]"
          class="w-40"
        />
        <USelect
          v-model="perPage"
          :items="PER_PAGE_OPTIONS"
          class="w-24"
        />
      </div>
      <div class="flex gap-2 items-center">
        <UButton
          v-if="selected.size > 0"
          :loading="bulkDeleting"
          icon="i-lucide-trash-2"
          :label="`Delete Selected (${selected.size})`"
          color="error"
          variant="outline"
          size="sm"
          @click="bulkDelete"
        />
        <span class="text-sm text-muted">{{ totalFiltered }} playlist{{ totalFiltered !== 1 ? 's' : '' }}</span>
        <UButton icon="i-lucide-refresh-cw" aria-label="Refresh playlists" variant="ghost" color="neutral" @click="load" />
      </div>
    </div>

    <UCard>
      <div v-if="loading" class="flex justify-center py-8">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-6" />
      </div>
      <div v-else class="overflow-x-auto">
        <table class="min-w-full text-sm">
          <thead>
            <tr class="border-b border-default text-left text-xs text-muted">
              <th class="px-3 py-2 w-8">
                <UCheckbox :model-value="allSelected" @update:model-value="toggleAll" />
              </th>
              <th class="px-3 py-2 cursor-pointer hover:text-default select-none" @click="doSort('name')">
                Name{{ sortIndicator('name') }}
              </th>
              <th class="px-3 py-2 cursor-pointer hover:text-default select-none" @click="doSort('user_id')">
                Owner{{ sortIndicator('user_id') }}
              </th>
              <th class="px-3 py-2 cursor-pointer hover:text-default select-none text-right" @click="doSort('items')">
                Items{{ sortIndicator('items') }}
              </th>
              <th class="px-3 py-2 cursor-pointer hover:text-default select-none" @click="doSort('is_public')">
                Visibility{{ sortIndicator('is_public') }}
              </th>
              <th class="px-3 py-2 cursor-pointer hover:text-default select-none" @click="doSort('created_at')">
                Created{{ sortIndicator('created_at') }}
              </th>
              <th class="px-3 py-2 w-10" />
            </tr>
          </thead>
          <tbody class="divide-y divide-default">
            <tr v-for="p in paged" :key="p.id" class="hover:bg-muted/30">
              <td class="px-3 py-2">
                <UCheckbox :model-value="selected.has(p.id)" @update:model-value="toggleSelect(p.id)" />
              </td>
              <td class="px-3 py-2">
                <div>
                  <p class="font-medium">{{ p.name }}</p>
                  <p v-if="p.description" class="text-xs text-muted truncate max-w-xs">{{ p.description }}</p>
                </div>
              </td>
              <td class="px-3 py-2 font-mono text-xs">{{ p.user_id?.slice(0, 8) }}…</td>
              <td class="px-3 py-2 text-right">{{ p.items?.length ?? 0 }}</td>
              <td class="px-3 py-2">
                <UBadge
                  :label="p.is_public ? 'Public' : 'Private'"
                  :color="p.is_public ? 'success' : 'neutral'"
                  variant="subtle"
                  size="xs"
                />
              </td>
              <td class="px-3 py-2 text-muted">
                {{ p.created_at ? new Date(p.created_at).toLocaleDateString() : '—' }}
              </td>
              <td class="px-3 py-2 text-right">
                <UButton
                  icon="i-lucide-trash-2"
                  size="xs"
                  variant="ghost"
                  color="error"
                  @click="deleteTarget = p"
                />
              </td>
            </tr>
          </tbody>
        </table>
        <p v-if="paged.length === 0" class="text-center py-6 text-muted text-sm">
          No playlists found.
        </p>
      </div>
    </UCard>

    <!-- Pagination -->
    <div v-if="totalPages > 1" class="flex justify-center">
      <UPagination
        v-model:page="page"
        :total="totalFiltered"
        :items-per-page="perPage"
      />
    </div>

    <!-- Bulk delete confirmation -->
    <UModal
      :open="confirmBulkDelete"
      title="Delete Selected Playlists"
      @update:open="val => { if (!val) confirmBulkDelete = false }"
    >
      <template #body>
        <p>Are you sure you want to delete <strong>{{ selected.size }}</strong> selected playlist{{ selected.size !== 1 ? 's' : '' }}? This action cannot be undone.</p>
      </template>
      <template #footer>
        <UButton variant="ghost" color="neutral" label="Cancel" @click="confirmBulkDelete = false" />
        <UButton :loading="bulkDeleting" color="error" label="Delete" @click="executeBulkDelete()" />
      </template>
    </UModal>

    <!-- Delete confirmation -->
    <UModal
      v-if="deleteTarget"
      :open="!!deleteTarget"
      title="Delete Playlist"
      @update:open="val => { if (!val) deleteTarget = null }"
    >
      <template #body>
        Are you sure you want to delete <strong>{{ deleteTarget?.name }}</strong>? This cannot be undone.
      </template>
      <template #footer>
        <UButton variant="ghost" color="neutral" label="Cancel" @click="deleteTarget = null" />
        <UButton :loading="deleting" color="error" label="Delete" @click="handleDelete" />
      </template>
    </UModal>
  </div>
</template>

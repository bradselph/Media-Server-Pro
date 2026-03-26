<script setup lang="ts">
import type { Playlist, AdminPlaylistStats } from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

const playlists = ref<Playlist[]>([])
const loading = ref(true)
const deleteTarget = ref<Playlist | null>(null)
const deleting = ref(false)
const stats = ref<AdminPlaylistStats | null>(null)

async function load() {
  loading.value = true
  try {
    const [res, s] = await Promise.allSettled([
      adminApi.listAllPlaylists(),
      adminApi.getPlaylistStats(),
    ])
    if (res.status === 'fulfilled') {
      playlists.value = Array.isArray(res.value) ? res.value : (res.value?.items ?? [])
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

    <div class="flex justify-end">
      <UButton icon="i-lucide-refresh-cw" aria-label="Refresh playlists" variant="ghost" color="neutral" @click="load" />
    </div>

    <UCard>
      <div v-if="loading" class="flex justify-center py-8">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-6" />
      </div>
      <UTable
        v-else
        :data="playlists"
        :columns="[
          { accessorKey: 'name', header: 'Name' },
          { accessorKey: 'user_id', header: 'Owner' },
          { accessorKey: 'items', header: 'Items' },
          { accessorKey: 'is_public', header: 'Visibility' },
          { accessorKey: 'created_at', header: 'Created' },
          { accessorKey: 'actions', header: '' },
        ]"
      >
        <template #name-cell="{ row }">
          <div>
            <p class="font-medium text-sm">{{ row.original.name }}</p>
            <p v-if="row.original.description" class="text-xs text-muted truncate max-w-xs">
              {{ row.original.description }}
            </p>
          </div>
        </template>
        <template #user_id-cell="{ row }">
          <span class="text-sm font-mono">{{ row.original.user_id?.slice(0, 8) }}…</span>
        </template>
        <template #items-cell="{ row }">
          <span class="text-sm">{{ row.original.items?.length ?? 0 }}</span>
        </template>
        <template #is_public-cell="{ row }">
          <UBadge
            :label="row.original.is_public ? 'Public' : 'Private'"
            :color="row.original.is_public ? 'success' : 'neutral'"
            variant="subtle"
            size="xs"
          />
        </template>
        <template #created_at-cell="{ row }">
          <span class="text-sm text-muted">
            {{ new Date(row.original.created_at).toLocaleDateString() }}
          </span>
        </template>
        <template #actions-cell="{ row }">
          <UButton
            icon="i-lucide-trash-2"
            size="xs"
            variant="ghost"
            color="error"
            @click="deleteTarget = row.original"
          />
        </template>
      </UTable>
      <p v-if="!loading && playlists.length === 0" class="text-center py-6 text-muted text-sm">
        No playlists found.
      </p>
    </UCard>

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

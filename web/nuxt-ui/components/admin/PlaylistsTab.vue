<script setup lang="ts">
import type { Playlist } from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

const playlists = ref<Playlist[]>([])
const loading = ref(true)
const deleteTarget = ref<Playlist | null>(null)
const deleting = ref(false)

async function load() {
  loading.value = true
  try { playlists.value = (await adminApi.listAllPlaylists()) ?? [] }
  catch {}
  finally { loading.value = false }
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
    <UCard :ui="{ body: 'py-3 px-4' }">
      <div class="flex items-center gap-4 text-sm">
        <UIcon name="i-lucide-list-music" class="size-4 text-primary" />
        <span><strong>{{ playlists.length }}</strong> playlists total</span>
        <span class="text-muted">·</span>
        <span><strong>{{ playlists.reduce((sum, p) => sum + (p.items?.length ?? 0), 0) }}</strong> items total</span>
      </div>
    </UCard>

    <div class="flex justify-end">
      <UButton icon="i-lucide-refresh-cw" variant="ghost" color="neutral" @click="load" />
    </div>

    <UCard>
      <div v-if="loading" class="flex justify-center py-8">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-6" />
      </div>
      <UTable
        v-else
        :data="playlists"
        :columns="[
          { key: 'name', label: 'Name' },
          { key: 'user_id', label: 'Owner' },
          { key: 'items', label: 'Items' },
          { key: 'is_public', label: 'Visibility' },
          { key: 'created_at', label: 'Created' },
          { key: 'actions', label: '' },
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

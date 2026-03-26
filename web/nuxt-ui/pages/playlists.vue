<script setup lang="ts">
import type { Playlist } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'

definePageMeta({ layout: 'default', title: 'Playlists', middleware: 'auth' })

const playlistApi = usePlaylistApi()
const mediaApi = useMediaApi()
const authStore = useAuthStore()
const router = useRouter()
const toast = useToast()

watchEffect(() => {
  if (!authStore.isLoading && !authStore.isLoggedIn) router.replace('/login')
})

// List
const playlists = ref<Playlist[]>([])
const loading = ref(true)

async function load() {
  loading.value = true
  try { playlists.value = (await playlistApi.list()) ?? [] }
  catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load playlists', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { loading.value = false }
}

// Create
const createOpen = ref(false)
const newName = ref('')
const newDesc = ref('')
const newPublic = ref(false)
const creating = ref(false)

async function createPlaylist() {
  if (!newName.value.trim()) return
  creating.value = true
  try {
    const pl = await playlistApi.create({ name: newName.value.trim(), description: newDesc.value, is_public: newPublic.value })
    playlists.value.unshift(pl)
    createOpen.value = false
    newName.value = ''; newDesc.value = ''; newPublic.value = false
    toast.add({ title: 'Playlist created', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { creating.value = false }
}

// Delete
const deleteTarget = ref<Playlist | null>(null)
const deleteOpen = computed({
  get: () => !!deleteTarget.value,
  set: (v: boolean) => { if (!v) deleteTarget.value = null },
})
const deleting = ref(false)

async function confirmDelete() {
  if (!deleteTarget.value) return
  deleting.value = true
  try {
    await playlistApi.delete(deleteTarget.value.id)
    playlists.value = playlists.value.filter(p => p.id !== deleteTarget.value!.id)
    toast.add({ title: 'Playlist deleted', color: 'success', icon: 'i-lucide-check' })
    deleteTarget.value = null
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { deleting.value = false }
}

// Edit playlist
const editTarget = ref<Playlist | null>(null)
const editOpen = computed({
  get: () => !!editTarget.value,
  set: (v: boolean) => { if (!v) editTarget.value = null },
})
const editName = ref('')
const editDesc = ref('')
const editPublic = ref(false)
const editSaving = ref(false)

function openEdit(pl: Playlist) {
  editTarget.value = pl
  editName.value = pl.name
  editDesc.value = pl.description ?? ''
  editPublic.value = pl.is_public ?? false
}

async function saveEdit() {
  if (!editTarget.value || !editName.value.trim()) return
  editSaving.value = true
  try {
    const updated = await playlistApi.update(editTarget.value.id, {
      name: editName.value.trim(),
      description: editDesc.value,
      is_public: editPublic.value,
    })
    playlists.value = playlists.value.map(p => p.id === updated.id ? updated : p)
    if (activePlaylist.value?.id === updated.id) activePlaylist.value = updated
    editTarget.value = null
    toast.add({ title: 'Playlist updated', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    editSaving.value = false
  }
}

// Copy playlist
const copyingId = ref<string | null>(null)

async function copyPlaylist(pl: Playlist) {
  copyingId.value = pl.id
  try {
    const copy = await playlistApi.copy(pl.id, `${pl.name} (copy)`)
    playlists.value.unshift(copy)
    toast.add({ title: 'Playlist duplicated', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    copyingId.value = null
  }
}

// Clear all items from a playlist
const clearingId = ref<string | null>(null)

async function clearPlaylist(pl: Playlist) {
  clearingId.value = pl.id
  try {
    await playlistApi.clear(pl.id)
    if (activePlaylist.value?.id === pl.id) {
      activePlaylist.value = { ...activePlaylist.value, items: [] }
    }
    playlists.value = playlists.value.map(p =>
      p.id === pl.id ? { ...p, items: [] } : p,
    )
    toast.add({ title: 'Playlist cleared', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    clearingId.value = null
  }
}

// View / edit single playlist
const activePlaylist = ref<Playlist | null>(null)
const activeLoading = ref(false)

async function openPlaylist(pl: Playlist) {
  activeLoading.value = true
  activePlaylist.value = pl
  try {
    activePlaylist.value = await playlistApi.get(pl.id)
  } catch { /* keep the partial data */ }
  finally { activeLoading.value = false }
}

async function removeItem(playlistId: string, mediaId: string, itemId?: string) {
  try {
    if (itemId) {
      await playlistApi.removePlaylistItemById(playlistId, itemId)
    } else {
      await playlistApi.removeItem(playlistId, mediaId)
    }
    if (activePlaylist.value) {
      activePlaylist.value = {
        ...activePlaylist.value,
        items: (activePlaylist.value.items ?? []).filter(i => itemId ? i.id !== itemId : i.media_id !== mediaId),
      }
    }
    toast.add({ title: 'Removed from playlist', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function moveItem(idx: number, direction: -1 | 1) {
  if (!activePlaylist.value) return
  const items = activePlaylist.value.items ?? []
  const target = idx + direction
  if (target < 0 || target >= items.length) return
  const positions = items.map((_, j) => j)
  positions[idx] = target
  positions[target] = idx
  try {
    await playlistApi.reorder(activePlaylist.value.id, positions)
    const newItems = [...items]
    ;[newItems[idx], newItems[target]] = [newItems[target], newItems[idx]]
    activePlaylist.value = { ...activePlaylist.value, items: newItems }
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to reorder', color: 'error', icon: 'i-lucide-x' })
  }
}

onMounted(load)
</script>

<template>
  <UContainer class="py-6 max-w-4xl space-y-6">
    <!-- Loading -->
    <div v-if="authStore.isLoading" class="flex justify-center py-16">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-8 text-primary" />
    </div>

    <template v-else-if="authStore.user">
      <!-- Header -->
      <div class="flex items-center justify-between flex-wrap gap-3">
        <h1 class="text-2xl font-bold text-highlighted flex items-center gap-2">
          <UIcon name="i-lucide-list-music" class="size-6 text-primary" />
          My Playlists
        </h1>
        <UButton
          v-if="authStore.user.permissions?.can_create_playlists !== false"
          icon="i-lucide-plus"
          label="New Playlist"
          @click="createOpen = true"
        />
      </div>

      <!-- Playlist detail view -->
      <template v-if="activePlaylist">
        <UButton
          icon="i-lucide-arrow-left"
          label="Back to playlists"
          variant="ghost"
          color="neutral"
          size="sm"
          @click="activePlaylist = null"
        />
        <UCard>
          <template #header>
            <div class="flex items-center justify-between gap-3 flex-wrap">
              <div>
                <h2 class="font-semibold text-lg">{{ activePlaylist.name }}</h2>
                <p v-if="activePlaylist.description" class="text-sm text-muted mt-0.5">{{ activePlaylist.description }}</p>
              </div>
              <div class="flex items-center gap-2 flex-wrap">
                <UBadge :label="activePlaylist.is_public ? 'Public' : 'Private'" :color="activePlaylist.is_public ? 'success' : 'neutral'" variant="subtle" size="xs" />
                <UButton icon="i-lucide-pencil" label="Edit" size="xs" variant="outline" color="neutral" @click="openEdit(activePlaylist)" />
                <UButton icon="i-lucide-copy" label="Duplicate" size="xs" variant="outline" color="neutral" :loading="copyingId === activePlaylist.id" @click="copyPlaylist(activePlaylist)" />
                <UButton icon="i-lucide-trash-2" label="Clear Items" size="xs" variant="outline" color="warning" :loading="clearingId === activePlaylist.id" @click="clearPlaylist(activePlaylist)" />
                <UButton icon="i-lucide-download" label="Export M3U" size="xs" variant="outline" color="neutral" :to="playlistApi.exportPlaylist(activePlaylist.id, 'm3u8')" />
              </div>
            </div>
          </template>
          <div v-if="activeLoading" class="flex justify-center py-6">
            <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
          </div>
          <div v-else-if="!activePlaylist.items || activePlaylist.items.length === 0" class="text-center py-8 text-muted text-sm">
            This playlist is empty.
          </div>
          <div v-else class="divide-y divide-default">
            <div
              v-for="(item, idx) in activePlaylist.items"
              :key="item.id ?? item.media_id"
              class="flex items-center gap-3 py-2"
            >
              <span class="text-sm text-muted w-6 text-right shrink-0">{{ idx + 1 }}</span>
              <div class="w-16 h-9 rounded overflow-hidden bg-muted shrink-0">
                <img
                  :src="mediaApi.getThumbnailUrl(item.media_id)"
                  :alt="item.title"
                  class="w-full h-full object-cover"
                  loading="lazy"
                />
              </div>
              <NuxtLink
                :to="`/player?id=${encodeURIComponent(item.media_id)}`"
                class="flex-1 min-w-0 text-sm font-medium truncate hover:text-primary transition-colors"
              >
                {{ item.title || item.media_id }}
              </NuxtLink>
              <div class="flex items-center gap-1 shrink-0">
                <UButton
                  icon="i-lucide-chevron-up"
                  aria-label="Move up"
                  size="xs"
                  variant="ghost"
                  color="neutral"
                  :disabled="idx === 0"
                  @click="moveItem(idx, -1)"
                />
                <UButton
                  icon="i-lucide-chevron-down"
                  aria-label="Move down"
                  size="xs"
                  variant="ghost"
                  color="neutral"
                  :disabled="idx === (activePlaylist!.items?.length ?? 0) - 1"
                  @click="moveItem(idx, 1)"
                />
                <UButton
                  icon="i-lucide-x"
                  aria-label="Remove from playlist"
                  size="xs"
                  variant="ghost"
                  color="neutral"
                  @click="removeItem(activePlaylist!.id, item.media_id, item.id)"
                />
              </div>
            </div>
          </div>
        </UCard>
      </template>

      <!-- Playlist grid -->
      <template v-else>
        <div v-if="loading" class="flex justify-center py-12">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-8 text-primary" />
        </div>
        <div v-else-if="playlists.length === 0" class="text-center py-12 text-muted">
          <UIcon name="i-lucide-list-music" class="size-12 mx-auto mb-3 opacity-30" />
          <p class="text-sm">No playlists yet. Create your first one!</p>
        </div>
        <div v-else class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <UCard
            v-for="pl in playlists"
            :key="pl.id"
            class="cursor-pointer hover:ring-1 hover:ring-primary transition-all"
            @click="openPlaylist(pl)"
          >
            <div class="flex items-start justify-between gap-2">
              <div class="min-w-0">
                <p class="font-semibold truncate">{{ pl.name }}</p>
                <p v-if="pl.description" class="text-xs text-muted truncate mt-0.5">{{ pl.description }}</p>
                <div class="flex items-center gap-2 mt-2">
                  <UBadge :label="pl.is_public ? 'Public' : 'Private'" :color="pl.is_public ? 'success' : 'neutral'" variant="subtle" size="xs" />
                  <span class="text-xs text-muted">{{ (pl.items?.length ?? 0) }} items</span>
                  <span class="text-xs text-muted">· {{ new Date(pl.modified_at).toLocaleDateString() }}</span>
                </div>
              </div>
              <div class="flex items-center gap-1">
                <UButton icon="i-lucide-pencil" aria-label="Edit playlist" size="xs" variant="ghost" color="neutral" @click.stop="openEdit(pl)" />
                <UButton icon="i-lucide-copy" aria-label="Duplicate playlist" size="xs" variant="ghost" color="neutral" :loading="copyingId === pl.id" @click.stop="copyPlaylist(pl)" />
                <UButton icon="i-lucide-trash-2" aria-label="Delete playlist" size="xs" variant="ghost" color="error" @click.stop="deleteTarget = pl" />
              </div>
            </div>
          </UCard>
        </div>
      </template>

      <!-- Create modal -->
      <UModal v-model:open="createOpen" title="New Playlist">
        <template #body>
          <div class="space-y-3">
            <UFormField label="Name" required>
              <UInput v-model="newName" placeholder="Playlist name" autofocus />
            </UFormField>
            <UFormField label="Description">
              <UInput v-model="newDesc" placeholder="Optional description" />
            </UFormField>
            <div class="flex items-center gap-2">
              <USwitch v-model="newPublic" />
              <span class="text-sm">Make public</span>
            </div>
          </div>
        </template>
        <template #footer>
          <UButton variant="ghost" color="neutral" label="Cancel" @click="createOpen = false" />
          <UButton :loading="creating" label="Create" :disabled="!newName.trim()" @click="createPlaylist" />
        </template>
      </UModal>

      <!-- Edit playlist modal -->
      <UModal v-model:open="editOpen" title="Edit Playlist">
        <template #body>
          <div class="space-y-3">
            <UFormField label="Name" required>
              <UInput v-model="editName" placeholder="Playlist name" autofocus />
            </UFormField>
            <UFormField label="Description">
              <UInput v-model="editDesc" placeholder="Optional description" />
            </UFormField>
            <div class="flex items-center gap-2">
              <USwitch v-model="editPublic" />
              <span class="text-sm">Make public</span>
            </div>
          </div>
        </template>
        <template #footer>
          <UButton variant="ghost" color="neutral" label="Cancel" @click="editOpen = false" />
          <UButton :loading="editSaving" label="Save" :disabled="!editName.trim()" @click="saveEdit" />
        </template>
      </UModal>

      <!-- Delete confirm modal -->
      <UModal v-model:open="deleteOpen" title="Delete Playlist" description="This will permanently delete the playlist and all its items.">
        <template #footer>
          <UButton variant="ghost" color="neutral" label="Cancel" @click="deleteTarget = null" />
          <UButton :loading="deleting" color="error" label="Delete" @click="confirmDelete" />
        </template>
      </UModal>
    </template>
  </UContainer>
</template>

<script setup lang="ts">
import type { MediaItem, Playlist, SmartPlaylist, SmartPlaylistRules, SmartCondition } from '~/types/api'

definePageMeta({ layout: 'default', title: 'Playlists' })

const playlistApi = usePlaylistApi()
const smartPlaylistsApi = useSmartPlaylistsApi()
const mediaApi = useMediaApi()
const authStore = useAuthStore()
const toast = useToast()

let mounted = true
onUnmounted(() => { mounted = false })

// List
const playlists = ref<Playlist[]>([])
const loading = ref(true)
// Track whether a fetch has been initiated to prevent duplicate loads when
// the auth store resolves after the component mounts (SPA navigation race).
let hasFetched = false

async function load() {
  hasFetched = true
  loading.value = true
  try {
    const result = await playlistApi.list()
    if (!mounted) return
    playlists.value = result ?? []
  } catch (e: unknown) {
    if (!mounted) return
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load playlists', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { if (mounted) loading.value = false }
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
  const targetId = deleteTarget.value.id
  deleting.value = true
  try {
    await playlistApi.delete(targetId)
    playlists.value = playlists.value.filter(p => p.id !== targetId)
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
    // The backend may return a partial fallback shape without an `id` if it
    // could not re-fetch the updated playlist (rare race condition). In that
    // case reload all playlists so the local state stays consistent.
    if (!updated?.id) {
      await load()
    } else {
      playlists.value = playlists.value.map(p => p.id === updated.id ? updated : p)
      if (activePlaylist.value?.id === updated.id) activePlaylist.value = updated
    }
    editTarget.value = null
    toast.add({ title: 'Playlist updated', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    editSaving.value = false
  }
}

// Copy playlist — separate loading refs to avoid racing between personal and public copies
const copyingPersonalId = ref<string | null>(null)
const copyingPublicId = ref<string | null>(null)

async function copyPlaylist(pl: Playlist) {
  copyingPersonalId.value = pl.id
  try {
    const copy = await playlistApi.copy(pl.id, `${pl.name} (copy)`)
    if (!mounted) return
    playlists.value.unshift(copy)
    toast.add({ title: 'Playlist duplicated', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    if (!mounted) return
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    if (mounted) copyingPersonalId.value = null
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
      const plId = activePlaylist.value.id
      const updatedItems = (activePlaylist.value.items ?? []).filter(i => itemId ? i.id !== itemId : i.media_id !== mediaId)
      activePlaylist.value = { ...activePlaylist.value, items: updatedItems }
      // Keep sidebar item count in sync
      playlists.value = playlists.value.map(p => p.id === plId ? { ...p, items: updatedItems } : p)
    }
    toast.add({ title: 'Removed from playlist', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

const reordering = ref(false)

async function moveItem(idx: number, direction: -1 | 1) {
  if (!activePlaylist.value || reordering.value) return
  const items = activePlaylist.value.items ?? []
  const target = idx + direction
  if (target < 0 || target >= items.length) return
  const positions = items.map((_, j) => j)
  positions[idx] = target
  positions[target] = idx
  reordering.value = true
  try {
    await playlistApi.reorder(activePlaylist.value.id, positions)
    const newItems = [...items]
    ;[newItems[idx], newItems[target]] = [newItems[target], newItems[idx]]
    activePlaylist.value = { ...activePlaylist.value, items: newItems }
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to reorder', color: 'error', icon: 'i-lucide-x' })
  } finally {
    reordering.value = false
  }
}

// Public playlists
const publicPlaylists = ref<Playlist[]>([])
const publicLoading = ref(false)
const showPublic = ref(false)

let publicLastFetched = 0
const PUBLIC_CACHE_MS = 60_000 // refresh after 60s

async function loadPublicPlaylists(force = false) {
  if (!force && publicPlaylists.value.length > 0 && Date.now() - publicLastFetched < PUBLIC_CACHE_MS) return
  publicLoading.value = true
  try {
    publicPlaylists.value = (await playlistApi.listPublic()) ?? []
    publicLastFetched = Date.now()
  } catch { publicPlaylists.value = [] }
  finally { publicLoading.value = false }
}

async function copyPublicPlaylist(pl: Playlist) {
  copyingPublicId.value = pl.id
  try {
    const copy = await playlistApi.copy(pl.id, `${pl.name} (copy)`)
    if (!mounted) return
    playlists.value.unshift(copy)
    toast.add({ title: 'Playlist saved to your library', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    if (!mounted) return
    toast.add({ title: e instanceof Error ? e.message : 'Failed to copy playlist', color: 'error', icon: 'i-lucide-x' })
  } finally {
    if (mounted) copyingPublicId.value = null
  }
}

// Bulk delete
const selectMode = ref(false)
const selectedIds = ref<Set<string>>(new Set())
const bulkDeleteOpen = ref(false)
const bulkDeleting = ref(false)

function toggleSelect(id: string) {
  const next = new Set(selectedIds.value)
  if (next.has(id)) { next.delete(id) } else { next.add(id) }
  selectedIds.value = next
}

function exitSelectMode() {
  selectMode.value = false
  selectedIds.value = new Set()
}

async function confirmBulkDelete() {
  const ids = [...selectedIds.value]
  if (ids.length === 0) return
  bulkDeleting.value = true
  try {
    const result = await playlistApi.bulkDelete(ids)
    playlists.value = playlists.value.filter(p => !ids.includes(p.id))
    toast.add({ title: `Deleted ${result?.deleted ?? ids.length} playlist(s)`, color: 'success', icon: 'i-lucide-check' })
    exitSelectMode()
    bulkDeleteOpen.value = false
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to delete', color: 'error', icon: 'i-lucide-x' })
  } finally { bulkDeleting.value = false }
}

// On mount: load immediately if auth has settled, otherwise wait for the user
// to become available. This handles SPA navigations where the auth plugin may
// still be resolving when the component mounts (isLoading=true) — without
// this guard, the page shows a blank body until the user manually refreshes.
onMounted(() => {
  if (!authStore.isLoading) {
    if (authStore.user) load()
    else { showPublic.value = true; loadPublicPlaylists() }
  }
})

watch(() => authStore.user, (user) => {
  if (user && !hasFetched) load()
  else if (!user && !authStore.isLoading) { showPublic.value = true; loadPublicPlaylists() }
})

watch(() => authStore.isLoading, (loading) => {
  if (!loading && !authStore.user && !hasFetched) { showPublic.value = true; loadPublicPlaylists() }
})

// ── Smart Playlists ──────────────────────────────────────────────────────────

const smartPlaylists = ref<SmartPlaylist[]>([])
const spLoading = ref(false)
let spHasFetched = false

async function loadSmartPlaylists() {
  spHasFetched = true
  spLoading.value = true
  try {
    smartPlaylists.value = (await smartPlaylistsApi.list()) ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load smart playlists', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { spLoading.value = false }
}

// Create smart playlist
const spCreateOpen = ref(false)
const spNewName = ref('')
const spNewDesc = ref('')
const spNewRules = ref<SmartPlaylistRules>(defaultSmartRules())
const spCreating = ref(false)

function defaultSmartRules(): SmartPlaylistRules {
  return {
    match: 'all',
    conditions: [],
    order_by: 'date_added',
    order_dir: 'desc',
    limit: 50,
  }
}

async function createSmartPlaylist() {
  if (!spNewName.value.trim()) return
  spCreating.value = true
  try {
    const pl = await smartPlaylistsApi.create({
      name: spNewName.value.trim(),
      description: spNewDesc.value,
      rules: JSON.stringify(spNewRules.value),
    })
    smartPlaylists.value.unshift(pl)
    spCreateOpen.value = false
    spNewName.value = ''
    spNewDesc.value = ''
    spNewRules.value = defaultSmartRules()
    toast.add({ title: 'Smart playlist created', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { spCreating.value = false }
}

// Delete smart playlist
const spDeleteTarget = ref<SmartPlaylist | null>(null)
const spDeleteOpen = computed({
  get: () => !!spDeleteTarget.value,
  set: (v: boolean) => { if (!v) spDeleteTarget.value = null },
})
const spDeleting = ref(false)

async function confirmDeleteSmart() {
  if (!spDeleteTarget.value) return
  const targetId = spDeleteTarget.value.id
  spDeleting.value = true
  try {
    await smartPlaylistsApi.delete(targetId)
    smartPlaylists.value = smartPlaylists.value.filter(p => p.id !== targetId)
    toast.add({ title: 'Smart playlist deleted', color: 'success', icon: 'i-lucide-check' })
    spDeleteTarget.value = null
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { spDeleting.value = false }
}

// Edit smart playlist
const spEditTarget = ref<SmartPlaylist | null>(null)
const spEditOpen = computed({
  get: () => !!spEditTarget.value,
  set: (v: boolean) => { if (!v) spEditTarget.value = null },
})
const spEditName = ref('')
const spEditDesc = ref('')
const spEditRules = ref<SmartPlaylistRules>(defaultSmartRules())
const spEditSaving = ref(false)

function openEditSmart(pl: SmartPlaylist) {
  spEditTarget.value = pl
  spEditName.value = pl.name
  spEditDesc.value = pl.description ?? ''
  try {
    spEditRules.value = JSON.parse(pl.rules) ?? defaultSmartRules()
  } catch {
    spEditRules.value = defaultSmartRules()
  }
}

async function saveEditSmart() {
  if (!spEditTarget.value || !spEditName.value.trim()) return
  spEditSaving.value = true
  try {
    const updated = await smartPlaylistsApi.update(spEditTarget.value.id, {
      name: spEditName.value.trim(),
      description: spEditDesc.value,
      rules: JSON.stringify(spEditRules.value),
    })
    smartPlaylists.value = smartPlaylists.value.map(p => p.id === updated.id ? updated : p)
    spEditTarget.value = null
    toast.add({ title: 'Smart playlist updated', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { spEditSaving.value = false }
}

// Preview smart playlist
const spPreviewTarget = ref<SmartPlaylist | null>(null)
const spPreviewOpen = computed({
  get: () => !!spPreviewTarget.value,
  set: (v: boolean) => { if (!v) spPreviewTarget.value = null },
})
const spPreviewItems = ref<MediaItem[]>([])
const spPreviewLoading = ref(false)
let spPreviewGen = 0

async function loadSmartPreview(sp: SmartPlaylist) {
  const thisGen = ++spPreviewGen
  spPreviewTarget.value = sp
  spPreviewItems.value = []
  spPreviewLoading.value = true
  try {
    const items = await smartPlaylistsApi.preview(sp.id)
    if (!mounted || thisGen !== spPreviewGen) return
    spPreviewItems.value = items ?? []
  } catch (e: unknown) {
    if (!mounted || thisGen !== spPreviewGen) return
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load preview', color: 'error', icon: 'i-lucide-x' })
  } finally { if (mounted && thisGen === spPreviewGen) spPreviewLoading.value = false }
}

// Load smart playlists when authenticated
watch(() => authStore.user, (user) => {
  if (user && !spHasFetched) loadSmartPlaylists()
})

onMounted(() => {
  if (!authStore.isLoading && authStore.user && !spHasFetched) {
    loadSmartPlaylists()
  }
})
</script>

<template>
  <UContainer class="py-6 max-w-4xl space-y-6">
    <!-- Auth resolving -->
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
        <div class="flex items-center gap-2 flex-wrap">
          <template v-if="selectMode">
            <UButton
              v-if="selectedIds.size > 0"
              icon="i-lucide-trash-2"
              :label="`Delete (${selectedIds.size})`"
              color="error"
              size="sm"
              @click="bulkDeleteOpen = true"
            />
            <UButton
              icon="i-lucide-x"
              label="Cancel"
              variant="ghost"
              color="neutral"
              size="sm"
              @click="exitSelectMode()"
            />
          </template>
          <UButton
            v-else
            icon="i-lucide-check-square"
            label="Select"
            variant="ghost"
            color="neutral"
            size="sm"
            @click="selectMode = true"
          />
          <UButton
            v-if="!selectMode && authStore.user.permissions?.can_create_playlists !== false"
            icon="i-lucide-plus"
            label="New Playlist"
            @click="createOpen = true"
          />
        </div>
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
                <UButton icon="i-lucide-copy" label="Duplicate" size="xs" variant="outline" color="neutral" :loading="copyingPersonalId === activePlaylist.id" @click="copyPlaylist(activePlaylist)" />
                <UButton icon="i-lucide-trash-2" label="Clear Items" size="xs" variant="outline" color="warning" :loading="clearingId === activePlaylist.id" @click="clearPlaylist(activePlaylist)" />
                <UDropdownMenu :items="[[
                  { label: 'Export M3U8', icon: 'i-lucide-file-music', to: playlistApi.exportPlaylist(activePlaylist.id, 'm3u8'), target: '_blank' },
                  { label: 'Export M3U', icon: 'i-lucide-file-music', to: playlistApi.exportPlaylist(activePlaylist.id, 'm3u'), target: '_blank' },
                  { label: 'Export JSON', icon: 'i-lucide-file-json', to: playlistApi.exportPlaylist(activePlaylist.id, 'json'), target: '_blank' },
                ]]">
                  <UButton icon="i-lucide-download" label="Export" size="xs" variant="outline" color="neutral" trailing-icon="i-lucide-chevron-down" />
                </UDropdownMenu>
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
                :to="`/player?id=${encodeURIComponent(item.media_id)}&playlist_id=${encodeURIComponent(activePlaylist?.id ?? '')}&playlist_idx=${idx}`"
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
                  :disabled="idx === 0 || reordering"
                  @click="moveItem(idx, -1)"
                />
                <UButton
                  icon="i-lucide-chevron-down"
                  aria-label="Move down"
                  size="xs"
                  variant="ghost"
                  color="neutral"
                  :disabled="idx === (activePlaylist?.items?.length ?? 0) - 1 || reordering"
                  @click="moveItem(idx, 1)"
                />
                <UButton
                  icon="i-lucide-x"
                  aria-label="Remove from playlist"
                  size="xs"
                  variant="ghost"
                  color="neutral"
                  @click="activePlaylist && removeItem(activePlaylist.id, item.media_id, item.id)"
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
            :class="[
              'transition-all',
              selectMode ? 'cursor-pointer' : 'cursor-pointer hover:ring-1 hover:ring-primary',
              selectMode && selectedIds.has(pl.id) ? 'ring-2 ring-primary' : '',
            ]"
            @click="selectMode ? toggleSelect(pl.id) : openPlaylist(pl)"
          >
            <div class="flex items-start justify-between gap-2">
              <div class="flex items-start gap-2 min-w-0">
                <UCheckbox
                  v-if="selectMode"
                  :model-value="selectedIds.has(pl.id)"
                  class="mt-0.5 shrink-0"
                  @click.stop
                  @update:model-value="toggleSelect(pl.id)"
                />
                <div class="min-w-0">
                  <p class="font-semibold truncate">{{ pl.name }}</p>
                  <p v-if="pl.description" class="text-xs text-muted truncate mt-0.5">{{ pl.description }}</p>
                  <div class="flex items-center gap-2 mt-2">
                    <UBadge :label="pl.is_public ? 'Public' : 'Private'" :color="pl.is_public ? 'success' : 'neutral'" variant="subtle" size="xs" />
                    <span class="text-xs text-muted">{{ (pl.items?.length ?? 0) }} items</span>
                    <span v-if="pl.modified_at" class="text-xs text-muted">· {{ new Date(pl.modified_at).toLocaleDateString() }}</span>
                  </div>
                </div>
              </div>
              <div v-if="!selectMode" class="flex items-center gap-1">
                <UButton icon="i-lucide-pencil" aria-label="Edit playlist" size="xs" variant="ghost" color="neutral" @click.stop="openEdit(pl)" />
                <UButton icon="i-lucide-copy" aria-label="Duplicate playlist" size="xs" variant="ghost" color="neutral" :loading="copyingPersonalId === pl.id" @click.stop="copyPlaylist(pl)" />
                <UButton icon="i-lucide-trash-2" aria-label="Delete playlist" size="xs" variant="ghost" color="error" @click.stop="deleteTarget = pl" />
              </div>
            </div>
          </UCard>
        </div>
      </template>

      <!-- Smart Playlists Section -->
      <div class="mt-8 pt-6 border-t border-default">
        <div class="flex items-center justify-between flex-wrap gap-3 mb-4">
          <h2 class="text-xl font-bold text-highlighted flex items-center gap-2">
            <UIcon name="i-lucide-filter" class="size-5 text-primary" />
            Smart Playlists
          </h2>
          <UButton
            v-if="authStore.user.permissions?.can_create_playlists !== false && !selectMode"
            icon="i-lucide-plus"
            label="New Smart Playlist"
            @click="spCreateOpen = true"
          />
        </div>

        <div v-if="spLoading" class="flex justify-center py-8">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-primary" />
        </div>
        <div v-else-if="smartPlaylists.length === 0" class="text-center py-8 text-muted">
          <UIcon name="i-lucide-filter" class="size-10 mx-auto mb-2 opacity-30" />
          <p class="text-sm">No smart playlists yet.</p>
        </div>
        <div v-else class="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <UCard
            v-for="sp in smartPlaylists"
            :key="sp.id"
            :ui="{ body: 'p-3' }"
          >
            <div class="space-y-2">
              <div class="flex items-start justify-between gap-2">
                <div class="flex-1 min-w-0">
                  <p class="font-semibold text-sm truncate">{{ sp.name }}</p>
                  <p v-if="sp.description" class="text-xs text-muted truncate mt-0.5">{{ sp.description }}</p>
                  <UBadge label="Auto" color="primary" variant="subtle" size="xs" class="mt-1" />
                </div>
                <div class="flex items-center gap-1 shrink-0">
                  <UButton
                    icon="i-lucide-eye"
                    aria-label="Preview"
                    size="xs"
                    variant="ghost"
                    color="neutral"
                    @click="loadSmartPreview(sp)"
                  />
                  <UButton
                    icon="i-lucide-pencil"
                    aria-label="Edit"
                    size="xs"
                    variant="ghost"
                    color="neutral"
                    @click="openEditSmart(sp)"
                  />
                  <UButton
                    icon="i-lucide-trash-2"
                    aria-label="Delete"
                    size="xs"
                    variant="ghost"
                    color="error"
                    @click="spDeleteTarget = sp"
                  />
                </div>
              </div>
            </div>
          </UCard>
        </div>
      </div>

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

      <!-- Bulk delete confirm modal -->
      <UModal
        v-model:open="bulkDeleteOpen"
        title="Delete Selected Playlists"
        :description="`Permanently delete ${selectedIds.size} playlist(s) and all their items?`"
      >
        <template #footer>
          <UButton variant="ghost" color="neutral" label="Cancel" @click="bulkDeleteOpen = false" />
          <UButton :loading="bulkDeleting" color="error" :label="`Delete ${selectedIds.size}`" @click="confirmBulkDelete" />
        </template>
      </UModal>

      <!-- Create smart playlist modal -->
      <UModal v-model:open="spCreateOpen" title="New Smart Playlist" size="lg">
        <template #body>
          <div class="space-y-4 max-h-96 overflow-y-auto">
            <UFormField label="Name" required>
              <UInput v-model="spNewName" placeholder="Smart playlist name" autofocus />
            </UFormField>
            <UFormField label="Description">
              <UInput v-model="spNewDesc" placeholder="Optional description" />
            </UFormField>
            <div>
              <label class="text-sm font-medium">Rules</label>
              <div class="space-y-2 mt-2">
                <div class="flex items-center gap-2">
                  <span class="text-xs text-muted">Match</span>
                  <USelect
                    v-model="spNewRules.match"
                    :options="['all', 'any']"
                    size="sm"
                  />
                </div>
                <div class="space-y-2 pl-2 border-l-2 border-default">
                  <div v-for="(cond, idx) in spNewRules.conditions" :key="idx" class="flex items-end gap-2">
                    <USelect
                      v-model="cond.field"
                      :options="['type', 'category', 'tags', 'duration', 'date_added_days', 'views', 'is_mature']"
                      size="sm"
                      class="flex-1"
                    />
                    <USelect
                      v-model="cond.op"
                      :options="['eq', 'gte', 'lte', 'includes']"
                      size="sm"
                      class="w-20"
                    />
                    <UInput
                      v-model="cond.value"
                      placeholder="Value"
                      size="sm"
                      class="flex-1"
                    />
                    <UButton
                      icon="i-lucide-x"
                      size="xs"
                      variant="ghost"
                      color="error"
                      @click="spNewRules.conditions = spNewRules.conditions.filter((_, i) => i !== idx)"
                    />
                  </div>
                  <UButton
                    icon="i-lucide-plus"
                    label="Add condition"
                    variant="outline"
                    color="neutral"
                    size="sm"
                    @click="spNewRules.conditions.push({ field: 'type', op: 'eq', value: '' })"
                  />
                </div>
                <div class="flex items-center gap-2 pt-2">
                  <span class="text-xs text-muted">Order by</span>
                  <USelect
                    v-model="spNewRules.order_by"
                    :options="['date_added', 'name', 'duration', 'views']"
                    size="sm"
                  />
                  <USelect
                    v-model="spNewRules.order_dir"
                    :options="['asc', 'desc']"
                    size="sm"
                    class="w-24"
                  />
                </div>
                <div class="flex items-center gap-2">
                  <span class="text-xs text-muted">Limit</span>
                  <UInput
                    v-model.number="spNewRules.limit"
                    type="number"
                    min="1"
                    max="200"
                    size="sm"
                    class="w-20"
                  />
                </div>
              </div>
            </div>
          </div>
        </template>
        <template #footer>
          <UButton variant="ghost" color="neutral" label="Cancel" @click="spCreateOpen = false" />
          <UButton :loading="spCreating" label="Create" :disabled="!spNewName.trim()" @click="createSmartPlaylist" />
        </template>
      </UModal>

      <!-- Edit smart playlist modal -->
      <UModal v-model:open="spEditOpen" title="Edit Smart Playlist" size="lg">
        <template #body>
          <div class="space-y-4 max-h-96 overflow-y-auto">
            <UFormField label="Name" required>
              <UInput v-model="spEditName" placeholder="Smart playlist name" autofocus />
            </UFormField>
            <UFormField label="Description">
              <UInput v-model="spEditDesc" placeholder="Optional description" />
            </UFormField>
            <div>
              <label class="text-sm font-medium">Rules</label>
              <div class="space-y-2 mt-2">
                <div class="flex items-center gap-2">
                  <span class="text-xs text-muted">Match</span>
                  <USelect
                    v-model="spEditRules.match"
                    :options="['all', 'any']"
                    size="sm"
                  />
                </div>
                <div class="space-y-2 pl-2 border-l-2 border-default">
                  <div v-for="(cond, idx) in spEditRules.conditions" :key="idx" class="flex items-end gap-2">
                    <USelect
                      v-model="cond.field"
                      :options="['type', 'category', 'tags', 'duration', 'date_added_days', 'views', 'is_mature']"
                      size="sm"
                      class="flex-1"
                    />
                    <USelect
                      v-model="cond.op"
                      :options="['eq', 'gte', 'lte', 'includes']"
                      size="sm"
                      class="w-20"
                    />
                    <UInput
                      v-model="cond.value"
                      placeholder="Value"
                      size="sm"
                      class="flex-1"
                    />
                    <UButton
                      icon="i-lucide-x"
                      size="xs"
                      variant="ghost"
                      color="error"
                      @click="spEditRules.conditions = spEditRules.conditions.filter((_, i) => i !== idx)"
                    />
                  </div>
                  <UButton
                    icon="i-lucide-plus"
                    label="Add condition"
                    variant="outline"
                    color="neutral"
                    size="sm"
                    @click="spEditRules.conditions.push({ field: 'type', op: 'eq', value: '' })"
                  />
                </div>
                <div class="flex items-center gap-2 pt-2">
                  <span class="text-xs text-muted">Order by</span>
                  <USelect
                    v-model="spEditRules.order_by"
                    :options="['date_added', 'name', 'duration', 'views']"
                    size="sm"
                  />
                  <USelect
                    v-model="spEditRules.order_dir"
                    :options="['asc', 'desc']"
                    size="sm"
                    class="w-24"
                  />
                </div>
                <div class="flex items-center gap-2">
                  <span class="text-xs text-muted">Limit</span>
                  <UInput
                    v-model.number="spEditRules.limit"
                    type="number"
                    min="1"
                    max="200"
                    size="sm"
                    class="w-20"
                  />
                </div>
              </div>
            </div>
          </div>
        </template>
        <template #footer>
          <UButton variant="ghost" color="neutral" label="Cancel" @click="spEditOpen = false" />
          <UButton :loading="spEditSaving" label="Save" :disabled="!spEditName.trim()" @click="saveEditSmart" />
        </template>
      </UModal>

      <!-- Delete smart playlist confirm modal -->
      <UModal v-model:open="spDeleteOpen" title="Delete Smart Playlist" description="This smart playlist will be permanently deleted.">
        <template #footer>
          <UButton variant="ghost" color="neutral" label="Cancel" @click="spDeleteTarget = null" />
          <UButton :loading="spDeleting" color="error" label="Delete" @click="confirmDeleteSmart" />
        </template>
      </UModal>

      <!-- Preview smart playlist modal -->
      <UModal v-model:open="spPreviewOpen" title="Smart Playlist Preview" size="lg">
        <template #body>
          <div class="max-h-96 overflow-y-auto">
            <div v-if="spPreviewLoading" class="flex justify-center py-6">
              <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
            </div>
            <div v-else-if="spPreviewItems.length === 0" class="text-center py-8 text-muted text-sm">
              <p>No items match this playlist's rules.</p>
            </div>
            <div v-else class="divide-y divide-default">
              <div
                v-for="item in spPreviewItems"
                :key="item.id"
                class="py-2 flex items-center gap-3"
              >
                <div class="w-12 h-8 rounded overflow-hidden bg-muted shrink-0">
                  <img
                    :src="mediaApi.getThumbnailUrl(item.id)"
                    :alt="item.name"
                    class="w-full h-full object-cover"
                    loading="lazy"
                  />
                </div>
                <div class="flex-1 min-w-0">
                  <p class="text-sm font-medium truncate">{{ item.name }}</p>
                  <div class="flex items-center gap-2 text-xs text-muted mt-0.5">
                    <span>{{ item.type }}</span>
                    <span v-if="item.duration">· {{ Math.round(item.duration) }}s</span>
                    <span v-if="item.category">· {{ item.category }}</span>
                  </div>
                </div>
                <NuxtLink
                  :to="`/player?id=${encodeURIComponent(item.id)}`"
                  class="shrink-0"
                >
                  <UButton
                    icon="i-lucide-play"
                    size="xs"
                    variant="ghost"
                    color="primary"
                  />
                </NuxtLink>
              </div>
            </div>
          </div>
        </template>
      </UModal>

      <!-- Public Playlist Browsing -->
      <div class="pt-2">
        <button
          class="flex items-center gap-2 text-sm font-medium text-muted hover:text-default transition-colors w-full"
          @click="showPublic = !showPublic; showPublic && loadPublicPlaylists()"
        >
          <UIcon name="i-lucide-globe" class="size-4" />
          Public Playlists
          <UIcon :name="showPublic ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'" class="size-4 ml-auto" />
        </button>

        <div v-if="showPublic" class="mt-3 space-y-2">
          <div v-if="publicLoading" class="flex justify-center py-6">
            <UIcon name="i-lucide-loader-2" class="animate-spin size-5 text-primary" />
          </div>
          <div v-else-if="publicPlaylists.length === 0" class="text-center py-6 text-muted text-sm">
            <UIcon name="i-lucide-globe" class="size-8 mx-auto mb-2 opacity-40" />
            <p>No public playlists yet.</p>
          </div>
          <div v-else class="grid sm:grid-cols-2 gap-3">
            <UCard
              v-for="pl in publicPlaylists"
              :key="pl.id"
              :ui="{ body: 'p-3' }"
            >
              <div class="flex items-start gap-3">
                <div class="flex-1 min-w-0">
                  <p class="font-medium text-sm truncate">{{ pl.name }}</p>
                  <p class="text-xs text-muted mt-0.5">{{ (pl.items ?? []).length }} items</p>
                  <p v-if="pl.description" class="text-xs text-muted truncate mt-0.5">{{ pl.description }}</p>
                </div>
                <div class="flex gap-1 shrink-0">
                  <UButton
                    icon="i-lucide-play"
                    size="xs"
                    variant="ghost"
                    color="primary"
                    aria-label="Play playlist"
                    :to="pl.items?.[0] ? `/player?id=${encodeURIComponent(pl.items[0].media_id)}&playlist_id=${encodeURIComponent(pl.id)}&playlist_idx=0` : undefined"
                    :disabled="!pl.items?.length"
                  />
                  <UButton
                    icon="i-lucide-copy"
                    size="xs"
                    variant="ghost"
                    color="neutral"
                    aria-label="Copy to my playlists"
                    :loading="copyingPublicId === pl.id"
                    @click="copyPublicPlaylist(pl)"
                  />
                </div>
              </div>
            </UCard>
          </div>
        </div>
      </div>
    </template>

    <!-- Guest view: show public playlists only -->
    <template v-else>
      <div class="flex items-center justify-between flex-wrap gap-3">
        <h1 class="text-2xl font-bold text-highlighted flex items-center gap-2">
          <UIcon name="i-lucide-globe" class="size-6 text-primary" />
          Public Playlists
        </h1>
        <UButton icon="i-lucide-log-in" label="Log in to create playlists" variant="outline" color="neutral" to="/login" />
      </div>

      <div v-if="publicLoading" class="flex justify-center py-16">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-8 text-primary" />
      </div>
      <div v-else-if="publicPlaylists.length === 0" class="text-center py-16 text-muted text-sm">
        <UIcon name="i-lucide-globe" class="size-12 mx-auto mb-3 opacity-40" />
        <p class="text-base">No public playlists yet.</p>
        <p class="mt-1 text-xs">Log in to create and share your own playlists.</p>
      </div>
      <div v-else class="grid sm:grid-cols-2 gap-3">
        <UCard v-for="pl in publicPlaylists" :key="pl.id" :ui="{ body: 'p-3' }">
          <div class="flex items-start gap-3">
            <div class="flex-1 min-w-0">
              <p class="font-medium text-sm truncate">{{ pl.name }}</p>
              <p class="text-xs text-muted mt-0.5">{{ (pl.items ?? []).length }} items</p>
              <p v-if="pl.description" class="text-xs text-muted truncate mt-0.5">{{ pl.description }}</p>
            </div>
            <UButton
              icon="i-lucide-play"
              size="xs"
              variant="ghost"
              color="primary"
              aria-label="Play playlist"
              :to="pl.items?.[0] ? `/player?id=${encodeURIComponent(pl.items[0].media_id)}&playlist_id=${encodeURIComponent(pl.id)}&playlist_idx=0` : undefined"
              :disabled="!pl.items?.length"
            />
          </div>
        </UCard>
      </div>
    </template>
  </UContainer>
</template>

<script setup lang="ts">
import type { MediaCollection, MediaCollectionItem, MediaItem } from '~/types/api'
import { useCollectionsApi } from '~/composables/useApiEndpoints'
import { getDisplayTitle } from '~/utils/mediaTitle'

const collectionsApi = useCollectionsApi()
const adminApi = useAdminApi()
const mediaApi = useMediaApi()
const toast = useToast()

const collections = ref<MediaCollection[]>([])
const loading = ref(false)
const deletingId = ref<string | null>(null)
const confirmDeleteId = ref<string | null>(null)

// Create / Edit modal
const formOpen = ref(false)
const editTarget = ref<MediaCollection | null>(null)
const form = reactive({ name: '', description: '', cover_media_id: '' })
const saving = ref(false)

// Detail view (view a collection's items)
const detailOpen = ref(false)
const detailCollection = ref<MediaCollection | null>(null)
const detailItems = ref<MediaCollectionItem[]>([])
const detailLoading = ref(false)
const removingItemId = ref<string | null>(null)

// Add items modal
const addItemsOpen = ref(false)
const addItemsTarget = ref<string | null>(null)
const mediaSearch = ref('')
const mediaResults = ref<MediaItem[]>([])
const mediaSearching = ref(false)
const addingIds = ref(new Set<string>())
let searchTimer: ReturnType<typeof setTimeout> | null = null

async function load() {
  loading.value = true
  try {
    collections.value = (await collectionsApi.list()) ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load collections', color: 'error', icon: 'i-lucide-x' })
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editTarget.value = null
  form.name = ''
  form.description = ''
  form.cover_media_id = ''
  formOpen.value = true
}

function openEdit(col: MediaCollection) {
  editTarget.value = col
  form.name = col.name
  form.description = col.description ?? ''
  form.cover_media_id = col.cover_media_id ?? ''
  formOpen.value = true
}

async function save() {
  if (!form.name.trim()) {
    toast.add({ title: 'Name is required', color: 'error', icon: 'i-lucide-x' })
    return
  }
  saving.value = true
  try {
    const data = { name: form.name, description: form.description, cover_media_id: form.cover_media_id || undefined }
    if (editTarget.value) {
      await collectionsApi.update(editTarget.value.id, data)
      toast.add({ title: 'Collection updated', color: 'success', icon: 'i-lucide-check' })
    } else {
      await collectionsApi.create(data)
      toast.add({ title: 'Collection created', color: 'success', icon: 'i-lucide-check' })
    }
    formOpen.value = false
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Save failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    saving.value = false
  }
}

async function deleteCollection(id: string) {
  if (deletingId.value) return
  deletingId.value = id
  try {
    await collectionsApi.delete(id)
    collections.value = collections.value.filter(c => c.id !== id)
    toast.add({ title: 'Collection deleted', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Delete failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    deletingId.value = null
  }
}

async function openDetail(col: MediaCollection) {
  detailCollection.value = col
  detailOpen.value = true
  detailLoading.value = true
  try {
    const full = await collectionsApi.get(col.id)
    detailItems.value = full.items ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load items', color: 'error', icon: 'i-lucide-x' })
  } finally {
    detailLoading.value = false
  }
}

async function removeItem(mediaId: string) {
  if (!detailCollection.value || removingItemId.value) return
  removingItemId.value = mediaId
  try {
    await collectionsApi.removeItem(detailCollection.value.id, mediaId)
    detailItems.value = detailItems.value.filter(i => i.media_id !== mediaId)
    toast.add({ title: 'Item removed', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    removingItemId.value = null
  }
}

function openAddItems(collectionId: string) {
  addItemsTarget.value = collectionId
  mediaSearch.value = ''
  mediaResults.value = []
  // Load current items so alreadyInCollection reflects actual membership
  if (detailCollection.value?.id !== collectionId) {
    collectionsApi.get(collectionId).then(full => {
      detailItems.value = full.items ?? []
    }).catch(() => {})
  }
  addItemsOpen.value = true
}

async function searchMedia() {
  if (!mediaSearch.value.trim()) { mediaResults.value = []; return }
  mediaSearching.value = true
  try {
    const res = await adminApi.listMedia({ page: 1, limit: 20, search: mediaSearch.value })
    mediaResults.value = res.items ?? []
  } catch { mediaResults.value = [] }
  finally { mediaSearching.value = false }
}

function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(searchMedia, 300)
}

async function addItem(mediaId: string) {
  if (!addItemsTarget.value || addingIds.value.has(mediaId)) return
  const next = new Set(addingIds.value)
  next.add(mediaId)
  addingIds.value = next
  try {
    const pos = detailItems.value.length
    await collectionsApi.addItems(addItemsTarget.value, [mediaId], pos)
    toast.add({ title: 'Added to collection', color: 'success', icon: 'i-lucide-check' })
    // Refresh detail if open for same collection
    if (detailCollection.value?.id === addItemsTarget.value) {
      const full = await collectionsApi.get(addItemsTarget.value)
      detailItems.value = full.items ?? []
    }
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to add', color: 'error', icon: 'i-lucide-x' })
  } finally {
    const cleared = new Set(addingIds.value)
    cleared.delete(mediaId)
    addingIds.value = cleared
  }
}

const alreadyInCollection = computed(() => new Set(detailItems.value.map(i => i.media_id)))

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <!-- Header -->
    <div class="flex items-center justify-between flex-wrap gap-3">
      <div>
        <h2 class="text-lg font-semibold text-highlighted">Collections</h2>
        <p class="text-sm text-muted">Group media into named series or collections for browsing and navigation.</p>
      </div>
      <div class="flex gap-2">
        <UButton icon="i-lucide-plus" label="New Collection" size="sm" color="primary" @click="openCreate" />
        <UButton icon="i-lucide-refresh-cw" aria-label="Refresh" size="sm" variant="ghost" color="neutral" :loading="loading" @click="load" />
      </div>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-primary" />
    </div>

    <!-- Empty -->
    <div v-else-if="collections.length === 0" class="text-center py-12 text-muted">
      <UIcon name="i-lucide-layers" class="size-8 mb-2 opacity-50" />
      <p>No collections yet.</p>
      <p class="text-xs mt-1">Create a collection to group related media into a series.</p>
    </div>

    <!-- Collections grid -->
    <div v-else class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
      <UCard
        v-for="col in collections"
        :key="col.id"
        :ui="{ body: 'p-4' }"
        class="cursor-pointer hover:ring-2 hover:ring-primary/50 transition-all"
        @click="openDetail(col)"
      >
        <div class="flex items-start justify-between gap-2">
          <div class="flex-1 min-w-0">
            <p class="font-semibold text-highlighted truncate">{{ col.name }}</p>
            <p v-if="col.description" class="text-xs text-muted mt-0.5 line-clamp-2">{{ col.description }}</p>
          </div>
          <div class="flex gap-1 shrink-0">
            <UButton
              icon="i-lucide-pencil"
              size="xs"
              variant="ghost"
              color="neutral"
              @click.stop="openEdit(col)"
            />
            <UButton
              icon="i-lucide-trash-2"
              size="xs"
              variant="ghost"
              color="error"
              :loading="deletingId === col.id"
              @click.stop="confirmDeleteId = col.id"
            />
          </div>
        </div>
        <div class="mt-2 flex items-center gap-2">
          <UButton
            icon="i-lucide-plus"
            label="Add Media"
            size="xs"
            variant="outline"
            color="neutral"
            @click.stop="openAddItems(col.id); detailCollection = col"
          />
        </div>
      </UCard>
    </div>

    <!-- Create / Edit modal -->
    <UModal
      v-model:open="formOpen"
      :title="editTarget ? 'Edit Collection' : 'New Collection'"
    >
      <template #body>
        <div class="space-y-4">
          <UFormField label="Name" required>
            <UInput v-model="form.name" placeholder="e.g. Marvel Cinematic Universe" class="w-full" />
          </UFormField>
          <UFormField label="Description">
            <UTextarea v-model="form.description" placeholder="Optional description" :rows="2" class="w-full" />
          </UFormField>
          <UFormField label="Cover Media ID" hint="Optional: media ID whose thumbnail is used as the collection cover">
            <UInput v-model="form.cover_media_id" placeholder="Media UUID" class="w-full font-mono text-xs" />
          </UFormField>
        </div>
      </template>
      <template #footer>
        <UButton :label="editTarget ? 'Save' : 'Create'" color="primary" :loading="saving" @click="save" />
        <UButton label="Cancel" variant="ghost" color="neutral" @click="formOpen = false" />
      </template>
    </UModal>

    <!-- Delete confirmation -->
    <UModal
      :open="!!confirmDeleteId"
      title="Delete Collection"
      @update:open="val => { if (!val) confirmDeleteId = null }"
    >
      <template #body>
        <p class="text-sm">Are you sure you want to delete <strong>{{ collections.find(c => c.id === confirmDeleteId)?.name }}</strong>? All items will be removed from the collection. This cannot be undone.</p>
      </template>
      <template #footer>
        <UButton color="error" label="Delete" :loading="deletingId === confirmDeleteId" @click="deleteCollection(confirmDeleteId!); confirmDeleteId = null" />
        <UButton label="Cancel" variant="ghost" color="neutral" @click="confirmDeleteId = null" />
      </template>
    </UModal>

    <!-- Collection detail / item management -->
    <UModal
      v-model:open="detailOpen"
      :title="detailCollection?.name ?? 'Collection'"
      :ui="{ content: 'max-w-2xl' }"
    >
      <template #body>
        <div class="space-y-3">
          <div class="flex justify-end">
            <UButton
              icon="i-lucide-plus"
              label="Add Media"
              size="sm"
              variant="outline"
              color="neutral"
              @click="addItemsTarget = detailCollection?.id ?? null; addItemsOpen = true; mediaSearch = ''; mediaResults = []"
            />
          </div>
          <div v-if="detailLoading" class="flex justify-center py-6">
            <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
          </div>
          <div v-else-if="detailItems.length === 0" class="text-center py-6 text-muted text-sm">
            No items in this collection yet.
          </div>
          <div v-else class="divide-y divide-default max-h-96 overflow-y-auto">
            <div
              v-for="(item, idx) in detailItems"
              :key="item.media_id"
              class="flex items-center gap-3 py-2.5"
            >
              <span class="text-xs text-muted w-5 text-right shrink-0">{{ idx + 1 }}</span>
              <div class="w-16 aspect-video rounded overflow-hidden bg-muted shrink-0">
                <img
                  :src="`/thumbnail?id=${encodeURIComponent(item.media_id)}`"
                  class="w-full h-full object-cover"
                  loading="lazy"
                  @error="($event.target as HTMLImageElement).style.display = 'none'"
                />
              </div>
              <span class="flex-1 text-sm truncate">{{ item.media_name || item.media_id }}</span>
              <NuxtLink
                :to="`/player?id=${encodeURIComponent(item.media_id)}`"
                class="shrink-0"
                @click="detailOpen = false"
              >
                <UButton icon="i-lucide-play" size="xs" variant="ghost" color="neutral" />
              </NuxtLink>
              <UButton
                icon="i-lucide-x"
                size="xs"
                variant="ghost"
                color="error"
                :loading="removingItemId === item.media_id"
                @click="removeItem(item.media_id)"
              />
            </div>
          </div>
        </div>
      </template>
      <template #footer>
        <UButton label="Close" variant="ghost" color="neutral" @click="detailOpen = false" />
      </template>
    </UModal>

    <!-- Add items search modal -->
    <UModal
      v-model:open="addItemsOpen"
      title="Add Media to Collection"
      :ui="{ content: 'max-w-lg' }"
    >
      <template #body>
        <div class="space-y-3">
          <UInput
            v-model="mediaSearch"
            icon="i-lucide-search"
            placeholder="Search media by name…"
            class="w-full"
            :loading="mediaSearching"
            @input="onSearchInput"
          />
          <div v-if="mediaResults.length === 0 && mediaSearch.length > 0 && !mediaSearching" class="text-center py-4 text-muted text-sm">
            No results.
          </div>
          <div v-else class="divide-y divide-default max-h-80 overflow-y-auto">
            <div
              v-for="item in mediaResults"
              :key="item.id"
              class="flex items-center gap-3 py-2"
            >
              <div class="w-12 aspect-video rounded overflow-hidden bg-muted shrink-0">
                <img
                  :src="mediaApi.getThumbnailUrl(item.id)"
                  class="w-full h-full object-cover"
                  loading="lazy"
                  @error="($event.target as HTMLImageElement).style.display = 'none'"
                />
              </div>
              <span class="flex-1 text-sm truncate">{{ getDisplayTitle(item) }}</span>
              <UBadge
                v-if="alreadyInCollection.has(item.id)"
                label="In collection"
                color="success"
                variant="subtle"
                size="xs"
              />
              <UButton
                v-else
                icon="i-lucide-plus"
                size="xs"
                variant="outline"
                color="primary"
                :loading="addingIds.has(item.id)"
                @click="addItem(item.id)"
              />
            </div>
          </div>
        </div>
      </template>
      <template #footer>
        <UButton label="Done" color="primary" @click="addItemsOpen = false" />
      </template>
    </UModal>
  </div>
</template>

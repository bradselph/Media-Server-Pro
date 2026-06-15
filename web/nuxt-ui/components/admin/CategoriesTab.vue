<script setup lang="ts">
import type {MediaCategory, MediaCategoryItem, MediaItem} from '~/types/api'
import {useCategoriesApi} from '~/composables/useApiEndpoints'
import {getDisplayTitle} from '~/utils/mediaTitle'
import {useAdminFeedback} from '~/composables/useAdminFeedback'

const categoriesApi = useCategoriesApi()
const adminApi = useAdminApi()
const mediaApi = useMediaApi()
const {notifyError, notifySuccess} = useAdminFeedback()

const categories = ref<MediaCategory[]>([])
const loading = ref(false)
const deletingId = ref<string | null>(null)
const confirmDeleteId = ref<string | null>(null)

// Create / Edit modal
const formOpen = ref(false)
const editTarget = ref<MediaCategory | null>(null)
const form = reactive({name: '', description: '', cover_media_id: ''})
const saving = ref(false)

// Detail view (view a category's items)
const detailOpen = ref(false)
const detailCategory = ref<MediaCategory | null>(null)
const detailItems = ref<MediaCategoryItem[]>([])
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
// Nonce to discard stale background fetches from openAddItems (incremented by addItem on success)
let addItemsFetchNonce = 0

async function load() {
  loading.value = true
  try {
    categories.value = (await categoriesApi.list()) ?? []
  } catch (e: unknown) {
    notifyError(e, 'Failed to load categories')
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

function openEdit(cat: MediaCategory) {
  editTarget.value = cat
  form.name = cat.name
  form.description = cat.description ?? ''
  form.cover_media_id = cat.cover_media_id ?? ''
  formOpen.value = true
}

async function save() {
  if (!form.name.trim()) {
    notifyError('Name is required')
    return
  }
  if (form.name.length > 255) {
    notifyError('Name too long (max 255 characters)')
    return
  }
  saving.value = true
  try {
    const data = {name: form.name, description: form.description, cover_media_id: form.cover_media_id || undefined}
    if (editTarget.value) {
      await categoriesApi.update(editTarget.value.id, data)
      notifySuccess('Category updated')
    } else {
      await categoriesApi.create(data)
      notifySuccess('Category created')
    }
    await load()
    formOpen.value = false
    form.name = ''
    form.description = ''
    form.cover_media_id = ''
  } catch (e: unknown) {
    notifyError(e, 'Save failed')
  } finally {
    saving.value = false
  }
}

async function deleteCategory(id: string) {
  if (deletingId.value) return
  deletingId.value = id
  try {
    await categoriesApi.delete(id)
    categories.value = categories.value.filter(c => c.id !== id)
    notifySuccess('Category deleted')
  } catch (e: unknown) {
    notifyError(e, 'Delete failed')
  } finally {
    deletingId.value = null
  }
}

async function openDetail(cat: MediaCategory) {
  detailCategory.value = cat
  detailOpen.value = true
  detailLoading.value = true
  try {
    const full = await categoriesApi.get(cat.id)
    detailItems.value = full.items ?? []
  } catch (e: unknown) {
    notifyError(e, 'Failed to load items')
  } finally {
    detailLoading.value = false
  }
}

async function removeItem(mediaId: string) {
  if (!detailCategory.value || removingItemId.value) return
  removingItemId.value = mediaId
  try {
    await categoriesApi.removeItem(detailCategory.value.id, mediaId)
    detailItems.value = detailItems.value.filter(i => i.media_id !== mediaId)
    notifySuccess('Item removed')
  } catch (e: unknown) {
    notifyError(e, 'Failed')
  } finally {
    removingItemId.value = null
  }
}

function openAddItems(categoryId: string) {
  addItemsTarget.value = categoryId
  mediaSearch.value = ''
  mediaResults.value = []
  // Load current items so alreadyInCategory reflects actual membership.
  // Capture nonce so a stale response that arrives after addItem's own refresh
  // doesn't overwrite the fresher detailItems state.
  if (detailCategory.value?.id !== categoryId) {
    const nonce = ++addItemsFetchNonce
    categoriesApi.get(categoryId).then(full => {
      if (addItemsFetchNonce === nonce) {
        detailItems.value = full.items ?? []
      }
    }).catch((err: unknown) => {
      console.warn('[categories] Failed to pre-load category items:', err)
    })
  }
  addItemsOpen.value = true
}

async function searchMedia() {
  if (!mediaSearch.value.trim()) {
    mediaResults.value = [];
    return
  }
  mediaSearching.value = true
  try {
    const res = await adminApi.listMedia({page: 1, limit: 20, search: mediaSearch.value})
    mediaResults.value = res.items ?? []
  } catch {
    mediaResults.value = []
  } finally {
    mediaSearching.value = false
  }
}

function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(searchMedia, 300)
}

onUnmounted(() => {
  if (searchTimer) {
    clearTimeout(searchTimer)
    searchTimer = null
  }
})

async function addItem(mediaId: string) {
  if (!addItemsTarget.value || addingIds.value.has(mediaId)) return
  const next = new Set(addingIds.value)
  next.add(mediaId)
  addingIds.value = next
  try {
    const pos = detailItems.value.length
    await categoriesApi.addItems(addItemsTarget.value, [mediaId], pos)
    notifySuccess('Added to category')
    // Always refresh detailItems for the current add target. Bump nonce so any
    // concurrent background fetch from openAddItems doesn't clobber this result.
    if (addItemsTarget.value) {
      addItemsFetchNonce++
      const full = await categoriesApi.get(addItemsTarget.value)
      detailItems.value = full.items ?? []
    }
  } catch (e: unknown) {
    notifyError(e, 'Failed to add')
  } finally {
    const cleared = new Set(addingIds.value)
    cleared.delete(mediaId)
    addingIds.value = cleared
  }
}

const alreadyInCategory = computed(() => new Set(detailItems.value.map(i => i.media_id)))

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <!-- Header -->
    <div class="flex items-center justify-between flex-wrap gap-3">
      <div>
        <h2 class="text-lg font-semibold text-highlighted">Categories</h2>
        <p class="text-sm text-muted">Group media into named, curated categories for browsing and navigation.</p>
      </div>
      <div class="flex gap-2">
        <UButton icon="i-lucide-plus" label="New Category" size="sm" color="primary" @click="openCreate"/>
        <UButton icon="i-lucide-refresh-cw" aria-label="Refresh" size="sm" variant="ghost" color="neutral"
                 :loading="loading" @click="load"/>
      </div>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-primary"/>
    </div>

    <!-- Empty -->
    <div v-else-if="categories.length === 0" class="text-center py-12 text-muted">
      <UIcon name="i-lucide-layers" class="size-8 mb-2 opacity-50"/>
      <p>No categories yet.</p>
      <p class="text-xs mt-1">Create a category to group related media together.</p>
    </div>

    <!-- Categories grid -->
    <div v-else class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
      <UCard
          v-for="cat in categories"
          :key="cat.id"
          :ui="{ body: 'p-4' }"
          class="cursor-pointer hover:ring-2 hover:ring-primary/50 transition-all"
          @click="openDetail(cat)"
      >
        <div class="flex items-start justify-between gap-2">
          <div class="flex-1 min-w-0">
            <p class="font-semibold text-highlighted truncate">{{ cat.name }}</p>
            <p class="text-xs text-muted mt-0.5">{{ cat.item_count ?? 0 }} {{ (cat.item_count ?? 0) === 1 ? 'item' : 'items' }}</p>
            <p v-if="cat.description" class="text-xs text-muted mt-0.5 line-clamp-2">{{ cat.description }}</p>
          </div>
          <div class="flex gap-1 shrink-0">
            <UButton
                icon="i-lucide-pencil"
                size="xs"
                variant="ghost"
                color="neutral"
                @click.stop="openEdit(cat)"
            />
            <UButton
                icon="i-lucide-trash-2"
                size="xs"
                variant="ghost"
                color="error"
                :loading="deletingId === cat.id"
                @click.stop="confirmDeleteId = cat.id"
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
              @click.stop="openAddItems(cat.id); detailCategory = cat"
          />
        </div>
      </UCard>
    </div>

    <!-- Create / Edit modal -->
    <UModal
        v-model:open="formOpen"
        :title="editTarget ? 'Edit Category' : 'New Category'"
    >
      <template #body>
        <div class="space-y-4">
          <UFormField label="Name" required>
            <UInput v-model="form.name" placeholder="e.g. Marvel Cinematic Universe" class="w-full" maxlength="255"/>
          </UFormField>
          <UFormField label="Description">
            <UTextarea v-model="form.description" placeholder="Optional description" :rows="2" class="w-full"
                       maxlength="2000"/>
          </UFormField>
          <UFormField label="Cover Media ID" hint="Optional: media ID whose thumbnail is used as the category cover">
            <UInput v-model="form.cover_media_id" placeholder="Media UUID" class="w-full font-mono text-xs"/>
          </UFormField>
        </div>
      </template>
      <template #footer>
        <UButton :label="editTarget ? 'Save' : 'Create'" color="primary" :loading="saving" @click="save"/>
        <UButton label="Cancel" variant="ghost" color="neutral" @click="formOpen = false"/>
      </template>
    </UModal>

    <!-- Delete confirmation -->
    <UModal
        :open="!!confirmDeleteId"
        title="Delete Category"
        @update:open="val => { if (!val) confirmDeleteId = null }"
    >
      <template #body>
        <p class="text-sm">Are you sure you want to delete
          <strong>{{ categories.find(c => c.id === confirmDeleteId)?.name }}</strong>? All items will be removed from
          the category. This cannot be undone.</p>
      </template>
      <template #footer>
        <UButton color="error" label="Delete" :loading="deletingId === confirmDeleteId"
                 @click="deleteCategory(confirmDeleteId!); confirmDeleteId = null"/>
        <UButton label="Cancel" variant="ghost" color="neutral" @click="confirmDeleteId = null"/>
      </template>
    </UModal>

    <!-- Category detail / item management -->
    <UModal
        v-model:open="detailOpen"
        :title="detailCategory?.name ?? 'Category'"
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
                @click="addItemsTarget = detailCategory?.id ?? null; addItemsOpen = true; mediaSearch = ''; mediaResults = []"
            />
          </div>
          <div v-if="detailLoading" class="flex justify-center py-6">
            <UIcon name="i-lucide-loader-2" class="animate-spin size-5"/>
          </div>
          <div v-else-if="detailItems.length === 0" class="text-center py-6 text-muted text-sm">
            No items in this category yet.
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
                <UButton icon="i-lucide-play" size="xs" variant="ghost" color="neutral"/>
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
        <UButton label="Close" variant="ghost" color="neutral" @click="detailOpen = false"/>
      </template>
    </UModal>

    <!-- Add items search modal -->
    <UModal
        v-model:open="addItemsOpen"
        title="Add Media to Category"
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
          <div v-if="mediaResults.length === 0 && mediaSearch.length > 0 && !mediaSearching"
               class="text-center py-4 text-muted text-sm">
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
                  v-if="alreadyInCategory.has(item.id)"
                  label="In category"
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
        <UButton label="Done" color="primary" @click="addItemsOpen = false"/>
      </template>
    </UModal>
  </div>
</template>

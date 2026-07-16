<script setup lang="ts">
import type {ExtractorItem, ExtractorStats} from '~/types/api'
import {useAdminFeedback} from '~/composables/useAdminFeedback'

const adminApi = useAdminApi()
const {notifyError, notifySuccess} = useAdminFeedback()

const extractorStats = ref<ExtractorStats | null>(null)
const extractorItems = ref<ExtractorItem[]>([])
const extractorLoading = ref(false)
const newExtractUrl = ref('')
const addingExtract = ref(false)
const inFlightIds = ref(new Set<string>())

async function loadExtractor() {
  extractorLoading.value = true
  try {
    const [stats, items] = await Promise.all([
      adminApi.getExtractorStats(),
      adminApi.listExtractorItems(),
    ])
    extractorStats.value = stats
    extractorItems.value = items ?? []
  } catch (e: unknown) {
    notifyError(e, 'Failed to load extractor', 'i-lucide-alert-circle')
  } finally {
    extractorLoading.value = false
  }
}

async function addExtractorUrl() {
  if (!newExtractUrl.value.trim()) return
  addingExtract.value = true
  try {
    await adminApi.addExtractorUrl(newExtractUrl.value.trim())
    newExtractUrl.value = ''
    notifySuccess('URL added to extractor')
    await loadExtractor()
  } catch (e: unknown) {
    notifyError(e, 'Failed')
  } finally {
    addingExtract.value = false
  }
}

async function deleteExtractorItem(id: string) {
  if (inFlightIds.value.has(id)) return
  inFlightIds.value.add(id)
  try {
    await adminApi.deleteExtractorItem(id)
    notifySuccess('Item removed')
    await loadExtractor()
  } catch (e: unknown) {
    notifyError(e, 'Failed')
  } finally {
    inFlightIds.value.delete(id)
  }
}

function statusColor(status: string): 'success' | 'warning' | 'error' | 'neutral' {
  if (status === 'online' || status === 'active' || status === 'completed') return 'success'
  if (status === 'pending') return 'warning'
  if (status === 'error' || status === 'offline') return 'error'
  return 'neutral'
}

onMounted(() => {
  loadExtractor().catch(err => notifyError(err, 'Failed to load extractor', 'i-lucide-alert-circle'))
})
</script>

<template>
  <div class="space-y-4">
    <h3 class="text-sm font-semibold text-muted uppercase tracking-wide flex items-center gap-2">
      <UIcon name="i-lucide-download-cloud" class="size-4"/>
      Extractor
    </h3>

    <!-- Stats -->
    <div v-if="extractorStats" class="grid grid-cols-3 gap-3">
      <UCard>
        <p class="text-2xl font-bold">{{ extractorStats.total_items }}</p>
        <p class="text-xs text-muted mt-1">Total Items</p>
      </UCard>
      <UCard>
        <p class="text-2xl font-bold">{{ extractorStats.active_items }}</p>
        <p class="text-xs text-muted mt-1">Active</p>
      </UCard>
      <UCard>
        <p class="text-2xl font-bold text-error">{{ extractorStats.error_items }}</p>
        <p class="text-xs text-muted mt-1">Errors</p>
      </UCard>
    </div>

    <!-- Add URL -->
    <UCard>
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-plus" class="size-4"/>
          Extract from URL
        </div>
      </template>
      <div class="flex gap-2">
        <UInput v-model="newExtractUrl" placeholder="URL to extract media from" class="flex-1"/>
        <UButton :loading="addingExtract" icon="i-lucide-download-cloud" label="Extract"
                 :disabled="!newExtractUrl.trim()" @click="addExtractorUrl"/>
      </div>
    </UCard>

    <!-- Items -->
    <UCard>
      <template #header>
        <div class="flex items-center justify-between">
          <span class="font-semibold">Items ({{ extractorItems.length }})</span>
          <UButton icon="i-lucide-refresh-cw" aria-label="Refresh extractor" variant="ghost" color="neutral" size="xs"
                   @click="loadExtractor"/>
        </div>
      </template>
      <div v-if="extractorLoading" class="flex justify-center py-6">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-5"/>
      </div>
      <div v-else-if="extractorItems.length === 0" class="text-center py-4 text-muted text-sm">No extractor items.
      </div>
      <UTable
          v-else
          :data="extractorItems"
          :columns="[
            { accessorKey: 'title', header: 'Title' },
            { accessorKey: 'status', header: 'Status' },
            { accessorKey: 'actions', header: '' },
          ]"
      >
        <template #title-cell="{ row }">
          <div class="max-w-sm">
            <p class="text-sm font-medium truncate">{{ row.original.title || '—' }}</p>
            <p class="text-xs text-muted truncate" :title="row.original.stream_url">{{ row.original.stream_url }}</p>
          </div>
        </template>
        <template #status-cell="{ row }">
          <div>
            <UBadge :label="row.original.status" :color="statusColor(row.original.status)" variant="subtle"
                    size="xs"/>
            <p v-if="row.original.error_message" class="text-xs text-error mt-0.5 truncate">
              {{ row.original.error_message }}</p>
          </div>
        </template>
        <template #actions-cell="{ row }">
          <UButton icon="i-lucide-trash-2" aria-label="Delete extractor item" size="xs" variant="ghost" color="error"
                   @click="deleteExtractorItem(row.original.id)"/>
        </template>
      </UTable>
    </UCard>
  </div>
</template>

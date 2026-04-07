<script setup lang="ts">
import type {
  CrawlerTarget, CrawlerDiscovery, CrawlerStats,
  ExtractorItem, ExtractorStats,
} from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

// ── Crawler ────────────────────────────────────────────────────────────────────
const crawlerStats = ref<CrawlerStats | null>(null)
const crawlerTargets = ref<CrawlerTarget[]>([])
const crawlerDiscoveries = ref<CrawlerDiscovery[]>([])
const crawlerLoading = ref(false)
const newCrawlUrl = ref('')
const newCrawlName = ref('')
const addingCrawl = ref(false)

async function loadCrawler() {
  crawlerLoading.value = true
  try {
    const [stats, targets, discoveries] = await Promise.all([
      adminApi.getCrawlerStats(),
      adminApi.listCrawlerTargets(),
      adminApi.getCrawlerDiscoveries(),
    ])
    crawlerStats.value = stats
    crawlerTargets.value = targets ?? []
    crawlerDiscoveries.value = discoveries ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load crawler', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { crawlerLoading.value = false }
}

async function addCrawlTarget() {
  if (!newCrawlUrl.value.trim()) return
  addingCrawl.value = true
  try {
    await adminApi.addCrawlerTarget(newCrawlUrl.value.trim(), newCrawlName.value.trim() || undefined)
    newCrawlUrl.value = ''; newCrawlName.value = ''
    toast.add({ title: 'Crawler target added', color: 'success', icon: 'i-lucide-check' })
    await loadCrawler()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { addingCrawl.value = false }
}

async function startCrawl(targetId: string) {
  try {
    await adminApi.startCrawl(targetId)
    toast.add({ title: 'Crawl started', color: 'success', icon: 'i-lucide-check' })
    await loadCrawler()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function deleteCrawlTarget(id: string) {
  try {
    await adminApi.deleteCrawlerTarget(id)
    toast.add({ title: 'Target deleted', color: 'success', icon: 'i-lucide-check' })
    await loadCrawler()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function approveDiscovery(id: string) {
  try {
    await adminApi.approveCrawlerDiscovery(id)
    toast.add({ title: 'Discovery approved', color: 'success', icon: 'i-lucide-check' })
    await loadCrawler()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function ignoreDiscovery(id: string) {
  try {
    await adminApi.ignoreCrawlerDiscovery(id)
    toast.add({ title: 'Discovery ignored', color: 'success', icon: 'i-lucide-check' })
    await loadCrawler()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function deleteDiscovery(id: string) {
  try {
    await adminApi.deleteCrawlerDiscovery(id)
    crawlerDiscoveries.value = crawlerDiscoveries.value.filter(d => d.id !== id)
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

// ── Extractor ──────────────────────────────────────────────────────────────────
const extractorStats = ref<ExtractorStats | null>(null)
const extractorItems = ref<ExtractorItem[]>([])
const extractorLoading = ref(false)
const newExtractUrl = ref('')
const addingExtract = ref(false)

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
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load extractor', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { extractorLoading.value = false }
}

async function addExtractorUrl() {
  if (!newExtractUrl.value.trim()) return
  addingExtract.value = true
  try {
    await adminApi.addExtractorUrl(newExtractUrl.value.trim())
    newExtractUrl.value = ''
    toast.add({ title: 'URL added to extractor', color: 'success', icon: 'i-lucide-check' })
    await loadExtractor()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { addingExtract.value = false }
}

async function deleteExtractorItem(id: string) {
  try {
    await adminApi.deleteExtractorItem(id)
    toast.add({ title: 'Item removed', color: 'success', icon: 'i-lucide-check' })
    await loadExtractor()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

function statusColor(status: string): 'success' | 'warning' | 'error' | 'neutral' {
  if (status === 'online' || status === 'active' || status === 'completed') return 'success'
  if (status === 'pending' || status === 'crawling') return 'warning'
  if (status === 'error' || status === 'offline') return 'error'
  return 'neutral'
}

onMounted(() => {
  Promise.all([loadCrawler(), loadExtractor()])
})
</script>

<template>
  <div class="space-y-6">
    <!-- ── Crawler ────────────────────────────────────────────────────────── -->
    <div class="space-y-4">
      <h3 class="text-sm font-semibold text-muted uppercase tracking-wide flex items-center gap-2">
        <UIcon name="i-lucide-globe" class="size-4" /> Crawler
      </h3>

      <!-- Stats -->
      <div v-if="crawlerStats" class="grid grid-cols-2 sm:grid-cols-4 gap-3">
        <UCard>
          <p class="text-2xl font-bold">{{ crawlerStats.total_targets }}</p>
          <p class="text-xs text-muted mt-1">Targets</p>
        </UCard>
        <UCard>
          <p class="text-2xl font-bold">{{ crawlerStats.enabled_targets }}</p>
          <p class="text-xs text-muted mt-1">Enabled</p>
        </UCard>
        <UCard>
          <p class="text-2xl font-bold">{{ crawlerStats.total_discoveries }}</p>
          <p class="text-xs text-muted mt-1">Discoveries</p>
        </UCard>
        <UCard>
          <p class="text-2xl font-bold">{{ crawlerStats.pending_discoveries }}</p>
          <p class="text-xs text-muted mt-1">Pending</p>
        </UCard>
      </div>

      <!-- Add target -->
      <UCard>
        <template #header>
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-plus" class="size-4" />
            Add Crawler Target
          </div>
        </template>
        <div class="flex flex-wrap gap-2">
          <UInput v-model="newCrawlUrl" placeholder="URL to crawl" class="flex-1 min-w-48" />
          <UInput v-model="newCrawlName" placeholder="Name (optional)" class="w-40" />
          <UButton :loading="addingCrawl" icon="i-lucide-plus" label="Add" :disabled="!newCrawlUrl.trim()" @click="addCrawlTarget" />
        </div>
      </UCard>

      <!-- Targets -->
      <UCard>
        <template #header>
          <div class="flex items-center justify-between">
            <span class="font-semibold">Targets ({{ crawlerTargets.length }})</span>
            <UButton icon="i-lucide-refresh-cw" aria-label="Refresh crawler" variant="ghost" color="neutral" size="xs" @click="loadCrawler" />
          </div>
        </template>
        <div v-if="crawlerLoading" class="flex justify-center py-6">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <div v-else-if="crawlerTargets.length === 0" class="text-center py-4 text-muted text-sm">No targets.</div>
        <div v-else class="divide-y divide-default">
          <div v-for="t in crawlerTargets" :key="t.id" class="flex items-center gap-3 py-2 flex-wrap">
            <div class="flex-1 min-w-0">
              <p class="font-medium text-sm">{{ t.name || t.url }}</p>
              <p v-if="t.name" class="text-xs text-muted truncate">{{ t.url }}</p>
              <div class="flex items-center gap-2 mt-1">
                <UBadge :label="t.enabled ? 'enabled' : 'disabled'" :color="t.enabled ? 'success' : 'neutral'" variant="subtle" size="xs" />
                <span v-if="t.site" class="text-xs text-muted">{{ t.site }}</span>
                <span v-if="t.last_crawled" class="text-xs text-muted">· Last crawled {{ new Date(t.last_crawled).toLocaleDateString() }}</span>
              </div>
            </div>
            <div class="flex gap-1">
              <UButton icon="i-lucide-play" aria-label="Start crawl" size="xs" variant="ghost" color="success" @click="startCrawl(t.id)" />
              <UButton icon="i-lucide-trash-2" aria-label="Delete target" size="xs" variant="ghost" color="error" @click="deleteCrawlTarget(t.id)" />
            </div>
          </div>
        </div>
      </UCard>

      <!-- Discoveries -->
      <UCard v-if="crawlerDiscoveries.length > 0">
        <template #header>
          <span class="font-semibold">Pending Discoveries ({{ crawlerDiscoveries.filter(d => d.status === 'pending').length }})</span>
        </template>
        <div class="divide-y divide-default max-h-64 overflow-y-auto">
          <div v-for="d in crawlerDiscoveries.filter(d => d.status === 'pending')" :key="d.id" class="flex items-center gap-3 py-2">
            <div class="flex-1 min-w-0">
              <p class="text-sm truncate">{{ d.title || d.url }}</p>
              <p v-if="d.title" class="text-xs text-muted truncate">{{ d.url }}</p>
            </div>
            <div class="flex gap-1">
              <UButton icon="i-lucide-check" aria-label="Approve discovery" size="xs" variant="ghost" color="success" @click="approveDiscovery(d.id)" />
              <UButton icon="i-lucide-ban" aria-label="Ignore discovery" size="xs" variant="ghost" color="warning" title="Ignore" @click="ignoreDiscovery(d.id)" />
              <UButton icon="i-lucide-trash-2" aria-label="Delete discovery" size="xs" variant="ghost" color="error" title="Delete" @click="deleteDiscovery(d.id)" />
            </div>
          </div>
        </div>
      </UCard>
    </div>

    <USeparator />

    <!-- ── Extractor ──────────────────────────────────────────────────────── -->
    <div class="space-y-4">
      <h3 class="text-sm font-semibold text-muted uppercase tracking-wide flex items-center gap-2">
        <UIcon name="i-lucide-download-cloud" class="size-4" /> Extractor
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
            <UIcon name="i-lucide-plus" class="size-4" />
            Extract from URL
          </div>
        </template>
        <div class="flex gap-2">
          <UInput v-model="newExtractUrl" placeholder="URL to extract media from" class="flex-1" />
          <UButton :loading="addingExtract" icon="i-lucide-download-cloud" label="Extract" :disabled="!newExtractUrl.trim()" @click="addExtractorUrl" />
        </div>
      </UCard>

      <!-- Items -->
      <UCard>
        <template #header>
          <div class="flex items-center justify-between">
            <span class="font-semibold">Items ({{ extractorItems.length }})</span>
            <UButton icon="i-lucide-refresh-cw" aria-label="Refresh extractor" variant="ghost" color="neutral" size="xs" @click="loadExtractor" />
          </div>
        </template>
        <div v-if="extractorLoading" class="flex justify-center py-6">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <div v-else-if="extractorItems.length === 0" class="text-center py-4 text-muted text-sm">No extractor items.</div>
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
              <p class="text-xs text-muted truncate" :title="row.original.url">{{ row.original.url }}</p>
            </div>
          </template>
          <template #status-cell="{ row }">
            <div>
              <UBadge :label="row.original.status" :color="statusColor(row.original.status)" variant="subtle" size="xs" />
              <p v-if="row.original.error" class="text-xs text-error mt-0.5 truncate">{{ row.original.error }}</p>
            </div>
          </template>
          <template #actions-cell="{ row }">
            <UButton icon="i-lucide-trash-2" aria-label="Delete extractor item" size="xs" variant="ghost" color="error" @click="deleteExtractorItem(row.original.id)" />
          </template>
        </UTable>
      </UCard>
    </div>
  </div>
</template>

<script setup lang="ts">
import type {
  RemoteSourceState, RemoteStats, RemoteMediaItem,
  CrawlerTarget, CrawlerDiscovery, CrawlerStats,
  ExtractorItem, ExtractorStats,
  SlaveNode, ReceiverStats, ReceiverDuplicate, ReceiverMedia,
} from '~/types/api'

const adminApi = useAdminApi()
const mediaApi = useMediaApi()
const toast = useToast()

const subTab = ref('remote')
const subTabs = [
  { label: 'Remote Sources', value: 'remote', icon: 'i-lucide-server' },
  { label: 'Crawler', value: 'crawler', icon: 'i-lucide-globe' },
  { label: 'Extractor', value: 'extractor', icon: 'i-lucide-download-cloud' },
  { label: 'Receiver', value: 'receiver', icon: 'i-lucide-radio-tower' },
]

// ── Remote Sources ─────────────────────────────────────────────────────────────
const remoteStats = ref<RemoteStats | null>(null)
const remoteSources = ref<RemoteSourceState[]>([])
const remoteLoading = ref(false)
const newRemoteName = ref('')
const newRemoteUrl = ref('')
const newRemoteUser = ref('')
const newRemotePass = ref('')
const addingRemote = ref(false)

async function loadRemote() {
  remoteLoading.value = true
  try {
    const [stats, sources] = await Promise.all([
      adminApi.getRemoteStats(),
      adminApi.getRemoteSources(),
    ])
    remoteStats.value = stats
    remoteSources.value = sources ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load remote sources', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { remoteLoading.value = false }
}

async function addRemoteSource() {
  if (!newRemoteName.value.trim() || !newRemoteUrl.value.trim()) return
  addingRemote.value = true
  try {
    await adminApi.createRemoteSource({
      name: newRemoteName.value.trim(),
      url: newRemoteUrl.value.trim(),
      username: newRemoteUser.value || undefined,
      password: newRemotePass.value || undefined,
    })
    newRemoteName.value = ''; newRemoteUrl.value = ''; newRemoteUser.value = ''; newRemotePass.value = ''
    toast.add({ title: 'Remote source added', color: 'success', icon: 'i-lucide-check' })
    await loadRemote()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { addingRemote.value = false }
}

async function syncRemote(name: string) {
  try {
    await adminApi.syncRemoteSource(name)
    toast.add({ title: 'Sync started', color: 'success', icon: 'i-lucide-check' })
    await loadRemote()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function deleteRemote(name: string) {
  try {
    await adminApi.deleteRemoteSource(name)
    toast.add({ title: 'Source removed', color: 'success', icon: 'i-lucide-check' })
    await loadRemote()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function cleanRemoteCache() {
  try {
    const res = await adminApi.cleanRemoteCache()
    toast.add({ title: `Cleaned ${(res as { removed: number }).removed ?? 0} cached items`, color: 'success', icon: 'i-lucide-check' })
    await loadRemote()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

// ── Remote Media Browser ───────────────────────────────────────────────────────
const remoteMedia = ref<RemoteMediaItem[]>([])
const remoteMediaLoading = ref(false)
const remoteMediaSource = ref<string | null>(null)
const showRemoteMedia = ref(false)

async function loadAllRemoteMedia() {
  remoteMediaLoading.value = true
  showRemoteMedia.value = true
  remoteMediaSource.value = null
  try {
    remoteMedia.value = (await adminApi.getRemoteMedia()) ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load remote media', color: 'error', icon: 'i-lucide-x' })
  } finally { remoteMediaLoading.value = false }
}

async function loadSourceMedia(name: string) {
  remoteMediaLoading.value = true
  showRemoteMedia.value = true
  remoteMediaSource.value = name
  try {
    remoteMedia.value = (await adminApi.getRemoteSourceMedia(name)) ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load source media', color: 'error', icon: 'i-lucide-x' })
  } finally { remoteMediaLoading.value = false }
}

async function cacheRemoteItem(url: string, sourceName: string) {
  try {
    await adminApi.cacheRemoteMedia(url, sourceName)
    toast.add({ title: 'Item cached locally', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Cache failed', color: 'error', icon: 'i-lucide-x' })
  }
}

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

// ── Receiver / Slaves ──────────────────────────────────────────────────────────
const receiverStats = ref<ReceiverStats | null>(null)
const slaves = ref<SlaveNode[]>([])
const duplicates = ref<ReceiverDuplicate[]>([])
const slaveMedia = ref<ReceiverMedia[]>([])
const receiverLoading = ref(false)
const slaveMediaLoading = ref(false)
const showSlaveMedia = ref(false)

async function loadReceiver() {
  receiverLoading.value = true
  try {
    const [stats, slaveList, dups] = await Promise.all([
      adminApi.getReceiverStats(),
      adminApi.listSlaves(),
      adminApi.listDuplicates('pending'),
    ])
    receiverStats.value = stats
    slaves.value = slaveList ?? []
    duplicates.value = dups ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load receiver', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { receiverLoading.value = false }
}

async function loadSlaveMedia() {
  slaveMediaLoading.value = true
  try {
    slaveMedia.value = (await adminApi.getSlaveMedia()) ?? []
    showSlaveMedia.value = true
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load slave media', color: 'error', icon: 'i-lucide-x' })
  } finally { slaveMediaLoading.value = false }
}

async function removeSlave(id: string) {
  try {
    await adminApi.removeReceiverSlave(id)
    toast.add({ title: 'Slave removed', color: 'success', icon: 'i-lucide-check' })
    await loadReceiver()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function resolveDuplicate(id: string, action: string) {
  try {
    await adminApi.resolveDuplicate(id, action)
    duplicates.value = duplicates.value.filter(d => d.id !== id)
    toast.add({ title: `Duplicate ${action}d`, color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

// Tab-switching triggers lazy load
watch(subTab, (tab) => {
  if (tab === 'remote' && !remoteStats.value) loadRemote()
  else if (tab === 'crawler' && !crawlerStats.value) loadCrawler()
  else if (tab === 'extractor' && !extractorStats.value) loadExtractor()
  else if (tab === 'receiver' && !receiverStats.value) loadReceiver()
}, { immediate: true })

function formatBytes(bytes?: number): string {
  if (!bytes) return '—'
  const k = 1024; const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / k ** i).toFixed(1)} ${sizes[i]}`
}

function statusColor(status: string): 'success' | 'warning' | 'error' | 'neutral' {
  if (status === 'online' || status === 'active' || status === 'completed') return 'success'
  if (status === 'pending' || status === 'crawling') return 'warning'
  if (status === 'error' || status === 'offline') return 'error'
  return 'neutral'
}
</script>

<template>
  <div class="space-y-4">
    <UTabs v-model="subTab" :items="subTabs" orientation="horizontal" class="w-full">
      <template #content="{ item }">
        <div class="pt-3 space-y-4">

          <!-- ── Remote Sources ──────────────────────────────────────────── -->
          <template v-if="item.value === 'remote'">
            <!-- Stats -->
            <div v-if="remoteStats" class="grid grid-cols-2 sm:grid-cols-4 gap-3">
              <UCard>
                <p class="text-2xl font-bold">{{ remoteStats.source_count }}</p>
                <p class="text-xs text-muted mt-1">Sources</p>
              </UCard>
              <UCard>
                <p class="text-2xl font-bold">{{ remoteStats.total_media_count }}</p>
                <p class="text-xs text-muted mt-1">Media Items</p>
              </UCard>
              <UCard>
                <p class="text-2xl font-bold">{{ remoteStats.cached_item_count }}</p>
                <p class="text-xs text-muted mt-1">Cached</p>
              </UCard>
              <UCard>
                <p class="text-2xl font-bold">{{ formatBytes(remoteStats.cache_size) }}</p>
                <p class="text-xs text-muted mt-1">Cache Size</p>
              </UCard>
            </div>

            <!-- Add source -->
            <UCard>
              <template #header>
                <div class="font-semibold flex items-center gap-2">
                  <UIcon name="i-lucide-plus" class="size-4" />
                  Add Remote Source
                </div>
              </template>
              <div class="grid grid-cols-1 sm:grid-cols-2 gap-2">
                <UInput v-model="newRemoteName" placeholder="Name" />
                <UInput v-model="newRemoteUrl" placeholder="URL (http://...)" />
                <UInput v-model="newRemoteUser" placeholder="Username (optional)" />
                <UInput v-model="newRemotePass" type="password" placeholder="Password (optional)" />
              </div>
              <div class="flex justify-end mt-3 gap-2">
                <UButton
                  :loading="addingRemote"
                  icon="i-lucide-plus"
                  label="Add Source"
                  :disabled="!newRemoteName.trim() || !newRemoteUrl.trim()"
                  @click="addRemoteSource"
                />
                <UButton icon="i-lucide-trash-2" label="Clean Cache" color="warning" variant="outline" @click="cleanRemoteCache" />
              </div>
            </UCard>

            <!-- Sources list -->
            <UCard>
              <template #header>
                <div class="flex items-center justify-between">
                  <span class="font-semibold">Sources ({{ remoteSources.length }})</span>
                  <UButton icon="i-lucide-refresh-cw" aria-label="Refresh sources" variant="ghost" color="neutral" size="xs" @click="loadRemote" />
                </div>
              </template>
              <div v-if="remoteLoading" class="flex justify-center py-6">
                <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
              </div>
              <div v-else-if="remoteSources.length === 0" class="text-center py-6 text-muted text-sm">
                No remote sources configured.
              </div>
              <div v-else class="divide-y divide-default">
                <div v-for="s in remoteSources" :key="s.source.name" class="flex items-center gap-3 py-2 flex-wrap">
                  <div class="flex-1 min-w-0">
                    <p class="font-medium text-sm">{{ s.source.name }}</p>
                    <p class="text-xs text-muted truncate">{{ s.source.url }}</p>
                    <div class="flex items-center gap-2 mt-1">
                      <UBadge :label="s.status" :color="statusColor(s.status)" variant="subtle" size="xs" />
                      <span class="text-xs text-muted">{{ s.media_count }} items</span>
                      <span v-if="s.last_sync" class="text-xs text-muted">· synced {{ new Date(s.last_sync).toLocaleDateString() }}</span>
                      <span v-if="s.error" class="text-xs text-error truncate">{{ s.error }}</span>
                    </div>
                  </div>
                  <div class="flex gap-1">
                    <UButton icon="i-lucide-list" aria-label="Browse media" size="xs" variant="ghost" color="neutral" title="Browse media" @click="loadSourceMedia(s.source.name)" />
                    <UButton icon="i-lucide-refresh-cw" aria-label="Sync source" size="xs" variant="ghost" color="neutral" @click="syncRemote(s.source.name)" />
                    <UButton icon="i-lucide-trash-2" aria-label="Delete source" size="xs" variant="ghost" color="error" @click="deleteRemote(s.source.name)" />
                  </div>
                </div>
              </div>
            </UCard>
          </template>

            <!-- Remote media browser -->
            <div class="flex justify-end gap-2">
              <UButton
                icon="i-lucide-film"
                :label="showRemoteMedia ? 'Hide Remote Media' : 'Browse All Remote Media'"
                size="sm"
                variant="outline"
                color="neutral"
                :loading="remoteMediaLoading && remoteMediaSource === null"
                @click="showRemoteMedia && remoteMediaSource === null ? showRemoteMedia = false : loadAllRemoteMedia()"
              />
            </div>
            <UCard v-if="showRemoteMedia">
              <template #header>
                <div class="flex items-center justify-between">
                  <span class="font-semibold">
                    Remote Media
                    <span v-if="remoteMediaSource" class="text-muted font-normal text-sm"> — {{ remoteMediaSource }}</span>
                    <span class="text-muted font-normal text-sm ml-2">({{ remoteMedia.length }})</span>
                  </span>
                  <div class="flex gap-2">
                    <UButton icon="i-lucide-refresh-cw" aria-label="Refresh" size="xs" variant="ghost" color="neutral" :loading="remoteMediaLoading" @click="remoteMediaSource ? loadSourceMedia(remoteMediaSource) : loadAllRemoteMedia()" />
                    <UButton icon="i-lucide-x" aria-label="Close" size="xs" variant="ghost" color="neutral" @click="showRemoteMedia = false" />
                  </div>
                </div>
              </template>
              <div v-if="remoteMediaLoading" class="flex justify-center py-6">
                <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
              </div>
              <div v-else-if="remoteMedia.length === 0" class="text-center py-4 text-muted text-sm">No remote media found.</div>
              <div v-else class="divide-y divide-default max-h-80 overflow-y-auto">
                <div v-for="m in remoteMedia" :key="m.id" class="flex items-center gap-3 py-2">
                  <div class="flex-1 min-w-0">
                    <p class="text-sm font-medium truncate" :title="m.name">{{ m.name }}</p>
                    <p class="text-xs text-muted">{{ m.source_name }} · {{ m.content_type }} · {{ formatBytes(m.size) }}</p>
                  </div>
                  <UButton
                    tag="a"
                    :href="mediaApi.getRemoteStreamUrl(m.url, m.source_name)"
                    icon="i-lucide-play"
                    aria-label="Stream"
                    size="xs"
                    variant="ghost"
                    color="primary"
                    title="Stream"
                    target="_blank"
                  />
                  <UButton
                    icon="i-lucide-hard-drive"
                    aria-label="Cache locally"
                    size="xs"
                    variant="ghost"
                    color="neutral"
                    title="Cache locally"
                    @click="cacheRemoteItem(m.url, m.source_name)"
                  />
                </div>
              </div>
            </UCard>
          </template>

          <!-- ── Crawler ────────────────────────────────────────────────── -->
          <template v-else-if="item.value === 'crawler'">
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
                      <UBadge :label="t.status" :color="statusColor(t.status)" variant="subtle" size="xs" />
                      <span class="text-xs text-muted">{{ t.discoveries }} discoveries</span>
                      <span v-if="t.last_crawl" class="text-xs text-muted">· {{ new Date(t.last_crawl).toLocaleDateString() }}</span>
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
          </template>

          <!-- ── Extractor ──────────────────────────────────────────────── -->
          <template v-else-if="item.value === 'extractor'">
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
          </template>

          <!-- ── Receiver / Slaves ───────────────────────────────────────── -->
          <template v-else-if="item.value === 'receiver'">
            <!-- Stats -->
            <div v-if="receiverStats" class="grid grid-cols-2 sm:grid-cols-4 gap-3">
              <UCard>
                <p class="text-2xl font-bold">{{ receiverStats.slave_count }}</p>
                <p class="text-xs text-muted mt-1">Slaves</p>
              </UCard>
              <UCard>
                <p class="text-2xl font-bold text-success">{{ receiverStats.online_slaves }}</p>
                <p class="text-xs text-muted mt-1">Online</p>
              </UCard>
              <UCard>
                <p class="text-2xl font-bold">{{ receiverStats.media_count }}</p>
                <p class="text-xs text-muted mt-1">Media</p>
              </UCard>
              <UCard>
                <p class="text-2xl font-bold text-warning">{{ receiverStats.duplicate_count }}</p>
                <p class="text-xs text-muted mt-1">Duplicates</p>
              </UCard>
            </div>

            <!-- Slaves list -->
            <UCard>
              <template #header>
                <div class="flex items-center justify-between">
                  <span class="font-semibold">Slave Nodes ({{ slaves.length }})</span>
                  <UButton icon="i-lucide-refresh-cw" aria-label="Refresh slaves" variant="ghost" color="neutral" size="xs" @click="loadReceiver" />
                </div>
              </template>
              <div v-if="receiverLoading" class="flex justify-center py-6">
                <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
              </div>
              <div v-else-if="slaves.length === 0" class="text-center py-8 text-muted text-sm">
                <UIcon name="i-lucide-radio-tower" class="size-10 mx-auto mb-2 opacity-30" />
                <p>No slave nodes registered.</p>
                <p class="text-xs mt-1">Slaves register automatically when a receiver instance connects.</p>
              </div>
              <div v-else class="divide-y divide-default">
                <div v-for="slave in slaves" :key="slave.id" class="flex items-center gap-3 py-2 flex-wrap">
                  <div class="flex-1 min-w-0">
                    <p class="font-medium text-sm">{{ slave.name }}</p>
                    <p class="text-xs text-muted truncate">{{ slave.base_url }}</p>
                    <div class="flex items-center gap-2 mt-1">
                      <UBadge :label="slave.status" :color="statusColor(slave.status)" variant="subtle" size="xs" />
                      <span class="text-xs text-muted">{{ slave.media_count }} media</span>
                      <span class="text-xs text-muted">· last seen {{ new Date(slave.last_seen).toLocaleString() }}</span>
                    </div>
                  </div>
                  <UButton icon="i-lucide-trash-2" aria-label="Remove slave" size="xs" variant="ghost" color="error" @click="removeSlave(slave.id)" />
                </div>
              </div>
            </UCard>

            <!-- Slave media browser -->
            <div class="flex justify-end">
              <UButton
                icon="i-lucide-database"
                :label="showSlaveMedia ? 'Hide Media' : 'Browse Slave Media'"
                size="sm"
                variant="outline"
                color="neutral"
                :loading="slaveMediaLoading"
                @click="showSlaveMedia ? showSlaveMedia = false : loadSlaveMedia()"
              />
            </div>
            <UCard v-if="showSlaveMedia">
              <template #header>
                <div class="flex items-center justify-between">
                  <span class="font-semibold">Slave Media ({{ slaveMedia.length }})</span>
                  <UButton icon="i-lucide-refresh-cw" aria-label="Refresh slave media" variant="ghost" color="neutral" size="xs" @click="loadSlaveMedia" />
                </div>
              </template>
              <div v-if="slaveMediaLoading" class="flex justify-center py-4">
                <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
              </div>
              <div v-else-if="slaveMedia.length === 0" class="text-center py-4 text-muted text-sm">No media from slave nodes.</div>
              <div v-else class="divide-y divide-default max-h-64 overflow-y-auto">
                <div v-for="m in slaveMedia" :key="m.id" class="flex items-center gap-3 py-2">
                  <div class="flex-1 min-w-0">
                    <p class="text-sm font-medium truncate">{{ m.name }}</p>
                    <p class="text-xs text-muted">{{ m.type }} · {{ formatBytes(m.size) }}</p>
                  </div>
                  <span class="text-xs text-muted font-mono shrink-0">{{ m.slave_id.slice(0, 8) }}…</span>
                </div>
              </div>
            </UCard>

            <!-- Duplicates -->
            <UCard v-if="duplicates.length > 0">
              <template #header><span class="font-semibold">Pending Duplicates ({{ duplicates.length }})</span></template>
              <div class="divide-y divide-default">
                <div v-for="d in duplicates" :key="d.id" class="flex items-center gap-3 py-2 flex-wrap">
                  <div class="flex-1 min-w-0">
                    <p class="text-sm font-medium">{{ d.item_a_name }}</p>
                    <p class="text-xs text-muted">vs. {{ d.item_b_name }}</p>
                  </div>
                  <div class="flex gap-1">
                    <UButton label="Keep A" size="xs" variant="outline" color="success" @click="resolveDuplicate(d.id, 'keep_a')" />
                    <UButton label="Keep B" size="xs" variant="outline" color="neutral" @click="resolveDuplicate(d.id, 'keep_b')" />
                    <UButton label="Keep Both" size="xs" variant="ghost" color="neutral" @click="resolveDuplicate(d.id, 'keep_both')" />
                  </div>
                </div>
              </div>
            </UCard>
          </template>

        </div>
      </template>
    </UTabs>
  </div>
</template>

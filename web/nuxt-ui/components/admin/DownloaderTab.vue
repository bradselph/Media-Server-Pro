<script setup lang="ts">
import type { DownloaderJob, ImportableFile, DownloaderHealth, DownloaderSettings, DownloaderDetectResult, DownloaderProgress, DownloaderStreamInfo } from '~/types/api'
import { formatBytes, formatUptime } from '~/utils/format'
import { asRecord } from '~/utils/typeGuards'

const adminApi = useAdminApi()
const toast = useToast()

// ── Download server config ─────────────────────────────────────────────────────

const fullConfig = ref<Record<string, unknown>>({})
const downloadEnabled = ref(true)
const downloadRequireAuth = ref(true)
const configSaving = ref(false)

async function loadDownloadConfig() {
  try {
    const cfg = await adminApi.getConfig()
    if (cfg) {
      fullConfig.value = cfg
      const dl = asRecord(cfg.download)
      downloadEnabled.value = dl?.enabled !== false
      downloadRequireAuth.value = dl?.require_auth !== false
    }
  } catch { /* non-critical */ }
}

async function saveDownloadConfig(key: 'enabled' | 'require_auth', value: boolean) {
  configSaving.value = true
  try {
    const updated = {
      ...fullConfig.value,
      download: { ...asRecord(fullConfig.value.download), [key]: value },
    }
    await adminApi.updateConfig(updated)
    fullConfig.value = updated
    if (key === 'enabled') downloadEnabled.value = value
    else downloadRequireAuth.value = value
    toast.add({ title: 'Download settings saved', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to save', color: 'error', icon: 'i-lucide-x' })
    // Reload config from server to revert UI to actual state
    loadDownloadConfig()
  } finally {
    configSaving.value = false
  }
}

// ── WebSocket ─────────────────────────────────────────────────────────────────

const wsConnected = ref(false)
const wsClientId = ref<string | null>(null)
const activeProgress = ref(new Map<string, DownloaderProgress>())
let wsRef: WebSocket | null = null
let wsReconnectTimer: ReturnType<typeof setTimeout> | null = null
let wsBackoff = 1000
let destroyed = false

function connectWS() {
  if (destroyed) return
  if (wsRef?.readyState === WebSocket.OPEN || wsRef?.readyState === WebSocket.CONNECTING) return
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const ws = new WebSocket(`${proto}//${location.host}/ws/admin/downloader`)
  wsRef = ws

  ws.onopen = () => { wsConnected.value = true; wsBackoff = 1000 }

  ws.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data)
      if (msg.type === 'connected' && msg.clientId) {
        wsClientId.value = msg.clientId
        return
      }
      if (msg.downloadId) {
        const next = new Map(activeProgress.value)
        next.set(msg.downloadId, msg as DownloaderProgress)
        activeProgress.value = next
        if (msg.status === 'complete' || msg.status === 'error' || msg.status === 'cancelled') {
          setTimeout(() => {
            if (destroyed) return
            const m = new Map(activeProgress.value)
            m.delete(msg.downloadId)
            activeProgress.value = m
            load()
            loadImportable()
          }, 8000)
        }
      }
    } catch { /* ignore non-JSON */ }
  }

  ws.onclose = () => {
    wsConnected.value = false
    wsClientId.value = null
    wsRef = null
    if (destroyed) return
    wsReconnectTimer = setTimeout(() => {
      wsBackoff = Math.min(wsBackoff * 2, 30000)
      connectWS()
    }, wsBackoff)
  }
  ws.onerror = () => { ws.close() }
}

// ── Auto-refresh interval ────────────────────────────────────────────────────
// Poll downloads + importable every 5s so the UI stays fresh even when the WS
// is disconnected or misses a message (matches React behaviour).
let autoRefreshInterval: ReturnType<typeof setInterval> | null = null

function startAutoRefresh() {
  if (autoRefreshInterval) return
  autoRefreshInterval = setInterval(() => {
    if (destroyed || document.hidden) return
    load()
    loadImportable()
  }, 5000)
}

onMounted(() => {
  connectWS()
  loadHealth()
  loadSettings()
  loadDownloadConfig()
  load()
  loadImportable()
  startAutoRefresh()
})

onUnmounted(() => {
  destroyed = true
  if (wsReconnectTimer) clearTimeout(wsReconnectTimer)
  if (autoRefreshInterval) clearInterval(autoRefreshInterval)
  wsRef?.close()
})

// ── Health ────────────────────────────────────────────────────────────────────
const health = ref<DownloaderHealth | null>(null)
const settings = ref<DownloaderSettings | null>(null)
const showSettings = ref(false)
const isOnline = computed(() => health.value?.online ?? false)

async function loadHealth() {
  try { health.value = await adminApi.getDownloaderHealth() }
  catch { health.value = null }
}

async function loadSettings() {
  try { settings.value = await adminApi.getDownloaderSettings() }
  catch { settings.value = null }
}

// ── Downloads list ────────────────────────────────────────────────────────────

const downloads = ref<DownloaderJob[]>([])
const loading = ref(true)

async function load() {
  try {
    const result = await adminApi.listDownloaderJobs()
    downloads.value = result ?? []
    // Only show the spinner on the very first load
    if (loading.value) loading.value = false
  } catch (e: unknown) {
    if (loading.value) {
      toast.add({ title: e instanceof Error ? e.message : 'Failed to load downloads', color: 'error', icon: 'i-lucide-alert-circle' })
      loading.value = false
    }
  }
}

async function cancelDownload(id: string) {
  try {
    await adminApi.cancelDownloaderJob(id)
    toast.add({ title: 'Download cancelled', color: 'info', icon: 'i-lucide-info' })
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function deleteDownload(filename: string) {
  try {
    await adminApi.deleteDownloaderJob(filename)
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

// ── URL Detection + Download ──────────────────────────────────────────────────

const newUrl = ref('')
const detecting = ref(false)
const detected = ref<DownloaderDetectResult | null>(null)
const downloading = ref(false)

// Filter ad streams out — only show real content
const filteredStreams = computed(() =>
  (detected.value?.streams ?? []).filter(s => !s.isAd),
)

function streamLabel(s: DownloaderStreamInfo): string {
  return s.quality || s.resolution || s.type || 'Stream'
}

async function detect() {
  if (!newUrl.value.trim()) return
  detecting.value = true
  detected.value = null
  try {
    detected.value = await adminApi.detectDownload(newUrl.value.trim())
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Detection failed', color: 'error', icon: 'i-lucide-x' })
  } finally { detecting.value = false }
}

async function startDownload(streamUrl?: string) {
  if (!detected.value) return
  downloading.value = true
  const clientId = wsClientId.value ?? `admin-${Date.now()}`
  try {
    const result = await adminApi.createDownloaderJob({
      url: streamUrl ?? detected.value.url,
      title: detected.value.title,
      clientId,
      isYouTube: detected.value.isYouTube,
      isYouTubeMusic: detected.value.isYouTubeMusic,
      relayId: detected.value.relayId,
    })
    toast.add({ title: 'Download started', color: 'success', icon: 'i-lucide-check' })
    if (result?.downloadId) {
      const next = new Map(activeProgress.value)
      next.set(result.downloadId, { downloadId: result.downloadId, status: 'queued', title: detected.value.title })
      activeProgress.value = next
    }
    detected.value = null
    newUrl.value = ''
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Download failed', color: 'error', icon: 'i-lucide-x' })
  } finally { downloading.value = false }
}

// ── Importable files ──────────────────────────────────────────────────────────

const importable = ref<ImportableFile[]>([])
const importableLoading = ref(false)
const importingFile = ref<string | null>(null)
const deleteSource = ref(true)
const triggerScan = ref(true)

async function loadImportable() {
  try {
    const result = await adminApi.listImportable()
    importable.value = result ?? []
    if (importableLoading.value) importableLoading.value = false
  } catch {
    if (importableLoading.value) importableLoading.value = false
  }
}

async function importFile(filename: string) {
  importingFile.value = filename
  try {
    const result = await adminApi.importFile(filename, deleteSource.value, triggerScan.value)
    const deleteNote = result?.sourceDeleted === false ? ' (source file could not be removed)' : ''
    toast.add({ title: `Imported to ${result?.destination ?? 'library'}${deleteNote}`, color: 'success', icon: 'i-lucide-check' })
    await Promise.allSettled([load(), loadImportable()])
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Import failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    importingFile.value = null
  }
}


function progressBarColor(status: DownloaderProgress['status']) {
  if (status === 'error') return 'error'
  if (status === 'complete') return 'success'
  return 'primary'
}
</script>

<template>
  <div class="space-y-4">
    <!-- Download config toggles -->
    <UCard :ui="{ body: 'p-4' }">
      <div class="divide-y divide-default">
        <div class="flex items-center justify-between gap-4 pb-3">
          <div>
            <p class="font-medium text-sm text-highlighted">Downloader enabled</p>
            <p class="text-xs text-muted mt-0.5">Allow admins to queue and manage media downloads</p>
          </div>
          <USwitch
            :model-value="downloadEnabled"
            :disabled="configSaving"
            aria-label="Downloader enabled"
            @update:model-value="saveDownloadConfig('enabled', $event)"
          />
        </div>
        <div class="flex items-center justify-between gap-4 pt-3">
          <div>
            <p class="font-medium text-sm text-highlighted">Require authentication</p>
            <p class="text-xs text-muted mt-0.5">Only authenticated users can trigger downloads</p>
          </div>
          <USwitch
            :model-value="downloadRequireAuth"
            :disabled="configSaving || !downloadEnabled"
            aria-label="Require authentication for downloads"
            @update:model-value="saveDownloadConfig('require_auth', $event)"
          />
        </div>
      </div>
    </UCard>

    <!-- Downloader health -->
    <UCard :ui="{ body: 'py-3 px-4' }">
      <div class="space-y-2">
        <div class="flex items-center gap-3 text-sm flex-wrap">
          <div class="flex items-center gap-1.5">
            <UIcon
              :name="isOnline ? 'i-lucide-check-circle' : 'i-lucide-x-circle'"
              :class="isOnline ? 'text-success' : 'text-error'"
              class="size-4"
            />
            <span class="font-medium">{{ isOnline ? 'Online' : 'Offline' }}</span>
          </div>
          <span v-if="health?.activeDownloads != null" class="text-muted">{{ health.activeDownloads }} active</span>
          <span v-if="health?.queuedDownloads != null" class="text-muted">· {{ health.queuedDownloads }} queued</span>
          <span v-if="health?.uptime != null" class="text-muted">· Up {{ formatUptime(health.uptime) }}</span>
          <div class="flex items-center gap-1.5 ml-auto">
            <UIcon :name="wsConnected ? 'i-lucide-wifi' : 'i-lucide-wifi-off'" class="size-3.5" :class="wsConnected ? 'text-success' : 'text-muted'" />
            <span class="text-xs text-muted">{{ wsConnected ? 'WS connected' : 'WS disconnected' }}</span>
          </div>
          <UButton icon="i-lucide-refresh-cw" size="xs" variant="ghost" color="neutral" @click="loadHealth" />
        </div>

        <!-- Dependencies -->
        <div v-if="health?.online && health.dependencies && Object.keys(health.dependencies).length > 0" class="flex flex-wrap gap-2">
          <UBadge
            v-for="(ver, name) in health.dependencies"
            :key="String(name)"
            :label="`${name}: ${ver || '—'}`"
            color="neutral"
            variant="subtle"
            size="xs"
          />
        </div>

        <!-- Error -->
        <p v-if="health?.error" class="text-xs text-error">{{ health.error }}</p>
      </div>
    </UCard>

    <!-- Settings -->
    <UCard v-if="settings && showSettings">
      <template #header>
        <div class="flex items-center justify-between">
          <span class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-settings" class="size-4" />
            Downloader Settings
          </span>
          <UButton icon="i-lucide-x" size="xs" variant="ghost" color="neutral" @click="showSettings = false" />
        </div>
      </template>
      <div class="grid grid-cols-2 sm:grid-cols-3 gap-3 text-sm">
        <div v-if="settings.maxConcurrent != null">
          <p class="text-xs text-muted">Max Concurrent</p>
          <p class="font-medium">{{ settings.maxConcurrent }}</p>
        </div>
        <div v-if="settings.downloadsDir">
          <p class="text-xs text-muted">Downloads Dir</p>
          <p class="font-mono text-xs truncate" :title="settings.downloadsDir">{{ settings.downloadsDir }}</p>
        </div>
        <div>
          <p class="text-xs text-muted">Server Storage</p>
          <UBadge :label="settings.allowServerStorage ? 'Allowed' : 'Browser only'" :color="settings.allowServerStorage ? 'success' : 'neutral'" variant="subtle" size="xs" />
        </div>
        <div v-if="settings.videoFormat">
          <p class="text-xs text-muted">Video Format</p>
          <p class="font-medium">{{ settings.videoFormat }}</p>
        </div>
        <div v-if="settings.audioFormat">
          <p class="text-xs text-muted">Audio Format</p>
          <p class="font-medium">{{ settings.audioFormat }} {{ settings.audioQuality ? `(${settings.audioQuality})` : '' }}</p>
        </div>
        <div v-if="settings.proxy">
          <p class="text-xs text-muted">Proxy</p>
          <UBadge :label="settings.proxy.enabled ? 'Enabled' : 'Disabled'" :color="settings.proxy.enabled ? 'info' : 'neutral'" variant="subtle" size="xs" />
        </div>
      </div>
      <div v-if="settings.supportedSites?.length" class="mt-3">
        <p class="text-xs text-muted mb-1">Supported Sites ({{ settings.supportedSites.length }})</p>
        <div class="flex flex-wrap gap-1 max-h-24 overflow-y-auto">
          <UBadge v-for="site in settings.supportedSites" :key="site" :label="site" color="neutral" variant="subtle" size="xs" />
        </div>
      </div>
    </UCard>
    <div v-else-if="settings" class="flex justify-end">
      <UButton icon="i-lucide-settings" label="Show Settings" size="sm" variant="ghost" color="neutral" @click="showSettings = true" />
    </div>

    <!-- New Download — detect first, then choose stream -->
    <UCard>
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-cloud-download" class="size-4" />
          New Download
        </div>
      </template>
      <div class="space-y-3">
        <p class="text-xs text-muted">
          Detect the URL first to see available streams. With server storage enabled, completed downloads are saved to the configured import directory.
        </p>
        <div class="flex flex-wrap gap-2">
          <UInput v-model="newUrl" placeholder="URL to download…" class="flex-1 min-w-64" :disabled="!isOnline" @keyup.enter="detect" />
          <UButton :loading="detecting" icon="i-lucide-search" label="Detect" variant="outline" color="neutral" :disabled="!newUrl.trim() || !isOnline" @click="detect" />
        </div>
        <p v-if="!isOnline" class="text-xs text-warning">Downloader is offline — detection and downloads are unavailable.</p>

        <!-- Stream options from detect -->
        <template v-if="detected">
          <UCard :ui="{ body: 'p-3' }">
            <p class="text-sm font-medium mb-2">{{ detected.title || 'Detected Streams' }}</p>

            <!-- Multiple streams to choose from (ad streams filtered) -->
            <div v-if="filteredStreams.length > 0" class="space-y-1.5">
              <div
                v-for="(s, i) in filteredStreams"
                :key="i"
                class="flex items-center gap-2 rounded bg-muted px-2 py-1.5"
              >
                <span class="flex-1 text-xs">
                  {{ streamLabel(s) }}{{ s.size ? ` · ${formatBytes(s.size)}` : '' }}
                </span>
                <UButton
                  :loading="downloading"
                  icon="i-lucide-download"
                  label="Download"
                  size="xs"
                  variant="outline"
                  color="primary"
                  :disabled="!isOnline"
                  @click="startDownload(s.url)"
                />
              </div>
            </div>

            <p v-if="detected.streams.length > 0 && filteredStreams.length === 0" class="text-xs text-muted">
              No non-ad streams detected.
            </p>

            <!-- YouTube best quality / single stream -->
            <UButton
              v-if="detected.isYouTube || filteredStreams.length === 0"
              :loading="downloading"
              icon="i-lucide-download"
              :label="detected.isYouTube ? 'Download (best quality)' : 'Download'"
              color="primary"
              :disabled="!isOnline"
              :class="filteredStreams.length > 0 ? 'mt-2' : ''"
              @click="startDownload()"
            />
          </UCard>
        </template>
      </div>
    </UCard>

    <!-- Active downloads with real-time progress -->
    <UCard v-if="activeProgress.size > 0">
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-loader-2" class="size-4 animate-spin text-primary" />
          Active Downloads
          <UBadge :label="String(activeProgress.size)" color="info" variant="subtle" size="xs" />
        </div>
      </template>
      <div class="space-y-3">
        <div v-for="[id, dl] in activeProgress" :key="id" class="space-y-1">
          <div class="flex items-center justify-between gap-2 text-sm">
            <span class="truncate font-medium flex-1">{{ dl.title || dl.filename || id }}</span>
            <div class="flex items-center gap-2 shrink-0">
              <span class="text-xs text-muted">{{ dl.status }}{{ dl.speed ? ` · ${dl.speed}` : '' }}{{ dl.eta ? ` · ETA ${dl.eta}` : '' }}</span>
              <UButton
                v-if="dl.status === 'downloading' || dl.status === 'queued'"
                icon="i-lucide-x"
                size="xs"
                variant="ghost"
                color="error"
                @click="cancelDownload(id)"
              />
            </div>
          </div>
          <UProgress :value="dl.progress ?? 0" :color="progressBarColor(dl.status)" size="xs" />
          <p v-if="dl.error" class="text-xs text-error">{{ dl.error }}</p>
        </div>
      </div>
    </UCard>

    <!-- Importable files — move to media library -->
    <UCard>
      <template #header>
        <div class="flex items-center justify-between">
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-package-check" class="size-4" />
            Import to Library
            <UBadge :label="String(importable.length)" color="neutral" variant="subtle" size="xs" />
          </div>
          <div class="flex items-center gap-3 text-sm">
            <label class="flex items-center gap-1.5 cursor-pointer">
              <UCheckbox v-model="deleteSource" />
              <span>Delete source</span>
            </label>
            <label class="flex items-center gap-1.5 cursor-pointer">
              <UCheckbox v-model="triggerScan" />
              <span>Scan library</span>
            </label>
            <UButton icon="i-lucide-refresh-cw" size="xs" variant="ghost" color="neutral" :loading="importableLoading" @click="loadImportable" />
          </div>
        </div>
      </template>
      <p class="text-xs text-muted mb-3">
        Files below have been downloaded to the server's configured downloads directory and are ready to be moved to the media library.
      </p>
      <div v-if="importableLoading" class="flex justify-center py-4">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
      </div>
      <div v-else-if="importable.length === 0" class="text-center py-4 text-muted text-sm">
        No files ready to import. Complete a download with server storage enabled to populate this list.
      </div>
      <div v-else class="divide-y divide-default">
        <div v-for="f in importable" :key="f.name" class="flex items-center justify-between py-2 gap-3">
          <div class="min-w-0 flex-1">
            <p class="text-sm font-medium truncate" :title="f.name">{{ f.name }}</p>
            <p class="text-xs text-muted">
              {{ formatBytes(f.size) }} ·
              {{ f.isAudio ? 'Audio' : 'Video' }} ·
              {{ f.modified ? new Date(f.modified * 1000).toLocaleDateString() : '' }}
            </p>
          </div>
          <UButton
            icon="i-lucide-import"
            label="Import"
            size="xs"
            variant="outline"
            color="primary"
            :loading="importingFile === f.name"
            @click="importFile(f.name)"
          />
        </div>
      </div>
    </UCard>

    <!-- Downloaded files list (server files) -->
    <UCard>
      <template #header>
        <div class="flex items-center justify-between">
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-folder-open" class="size-4" />
            Server Files
          </div>
          <UButton icon="i-lucide-refresh-cw" size="xs" variant="ghost" color="neutral" @click="load" />
        </div>
      </template>
      <div v-if="loading" class="flex justify-center py-8">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-6" />
      </div>
      <UTable
        v-else
        :data="downloads"
        :columns="[
          { accessorKey: 'filename', header: 'Filename' },
          { accessorKey: 'size', header: 'Size' },
          { accessorKey: 'created', header: 'Created' },
          { accessorKey: 'actions', header: '' },
        ]"
      >
        <template #filename-cell="{ row }">
          <div class="max-w-xs">
            <p class="text-sm font-medium truncate" :title="row.original.filename">{{ row.original.filename || '—' }}</p>
            <p v-if="row.original.url" class="text-xs text-muted truncate" :title="row.original.url">{{ row.original.url }}</p>
          </div>
        </template>
        <template #size-cell="{ row }">
          <span class="text-sm">{{ formatBytes(row.original.size) }}</span>
        </template>
        <template #created-cell="{ row }">
          <span class="text-sm text-muted">{{ row.original.created ? new Date(row.original.created * 1000).toLocaleString() : '—' }}</span>
        </template>
        <template #actions-cell="{ row }">
          <UButton
            icon="i-lucide-trash-2"
            aria-label="Delete download"
            size="xs"
            variant="ghost"
            color="error"
            @click="deleteDownload(row.original.filename)"
          />
        </template>
      </UTable>
      <p v-if="!loading && downloads.length === 0" class="text-center py-6 text-muted text-sm">
        No downloads.
      </p>
    </UCard>
  </div>
</template>

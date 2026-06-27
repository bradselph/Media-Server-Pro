<script setup lang="ts">
import type {
  DownloaderDetectResult,
  DownloaderHealth,
  DownloaderJob,
  DownloaderProgress,
  DownloaderQueue,
  DownloaderSettings,
  DownloaderStreamInfo,
  ImportableFile,
  ImportDestination
} from '~/types/api'
import {formatBytes, formatUptime} from '~/utils/format'
import {asRecord} from '~/utils/typeGuards'
import {useAdminFeedback} from '~/composables/useAdminFeedback'

const adminApi = useAdminApi()
const toast = useToast()
const {notifyError, notifySuccess, notifyInfo} = useAdminFeedback()

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
  } catch { /* non-critical */
  }
}

async function saveDownloadConfig(key: 'enabled' | 'require_auth', value: boolean) {
  configSaving.value = true
  try {
    const updated = {
      ...fullConfig.value,
      download: {...asRecord(fullConfig.value.download), [key]: value},
    }
    await adminApi.updateConfig(updated)
    fullConfig.value = updated
    if (key === 'enabled') downloadEnabled.value = value
    else downloadRequireAuth.value = value
    notifySuccess('Download settings saved')
  } catch (e: unknown) {
    notifyError(e, 'Failed to save')
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

  ws.onopen = () => {
    wsConnected.value = true;
    wsBackoff = 1000
  }

  ws.onmessage = (event) => {
    let msg: Record<string, unknown>
    try {
      msg = JSON.parse(event.data)
    } catch {
      return
    }
    if (msg.type === 'connected' && msg.clientId) {
      wsClientId.value = msg.clientId as string
      return
    }
    if (msg.downloadId && typeof msg.status === 'string') {
      const next = new Map(activeProgress.value)
      next.set(msg.downloadId as string, msg as unknown as DownloaderProgress)
      activeProgress.value = next
      if (msg.status === 'complete' || msg.status === 'completed' || msg.status === 'error' || msg.status === 'cancelled') {
        setTimeout(() => {
          if (destroyed) return
          const m = new Map(activeProgress.value)
          m.delete(msg.downloadId as string)
          activeProgress.value = m
          load()
          loadImportable()
        }, 8000)
      }
    }
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
  ws.onerror = () => {
    ws.close()
  }
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
    loadQueue()
  }, 5000)
}

onMounted(() => {
  loadAutoImportPref()
  connectWS()
  loadHealth()
  loadSettings()
  loadDownloadConfig()
  load()
  loadImportable()
  loadQueue()
  loadDestinations()
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
  try {
    health.value = await adminApi.getDownloaderHealth()
  } catch {
    health.value = null
  }
}

async function loadSettings() {
  try {
    settings.value = await adminApi.getDownloaderSettings()
  } catch {
    settings.value = null
  }
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
      notifyError(e, 'Failed to load downloads', 'i-lucide-alert-circle')
      loading.value = false
    }
  }
}

async function cancelDownload(id: string) {
  try {
    await adminApi.cancelDownloaderJob(id)
    notifyInfo('Download cancelled')
    await load()
  } catch (e: unknown) {
    notifyError(e, 'Failed')
  }
}

async function deleteDownload(filename: string) {
  try {
    await adminApi.deleteDownloaderJob(filename)
    await load()
  } catch (e: unknown) {
    notifyError(e, 'Failed')
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
  const urlStr = newUrl.value.trim()
  if (!urlStr) return
  try {
    const u = new URL(urlStr)
    if (!['http:', 'https:'].includes(u.protocol)) {
      notifyError('URL must use http or https')
      return
    }
  } catch {
    notifyError('Invalid URL')
    return
  }
  detecting.value = true
  detected.value = null
  try {
    detected.value = await adminApi.detectDownload(urlStr)
  } catch (e: unknown) {
    notifyError(e, 'Detection failed')
  } finally {
    detecting.value = false
  }
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
      ...downloadOptions(),
    })
    notifySuccess('Download started')
    if (result?.downloadId) {
      const next = new Map(activeProgress.value)
      next.set(result.downloadId, {downloadId: result.downloadId, status: 'queued', title: detected.value.title})
      activeProgress.value = next
    }
    detected.value = null
    newUrl.value = ''
    await load()
  } catch (e: unknown) {
    notifyError(e, 'Download failed')
  } finally {
    downloading.value = false
  }
}

// ── Importable files ──────────────────────────────────────────────────────────

const importable = ref<ImportableFile[]>([])
const importableLoading = ref(false)
const importingFile = ref<string | null>(null)
const deleteSource = ref(true)
const triggerScan = ref(true)
// Auto-import: when on, every file that becomes importable is sent straight to
// the library (default destination, source removed + rescan) so a download
// submitted with this checked imports itself once it finishes downloading and
// converting. Persisted per-browser. `autoImportAttempted` remembers names we've
// already handled so a file that fails to import isn't retried on every poll.
const autoImport = ref(false)
const autoImportAttempted = new Set<string>()

// ── Import destination prompt ───────────────────────────────────────────────
const destinations = ref<ImportDestination[]>([])
const selectedDestKey = ref<string>('')
const newSubfolder = ref<string>('')
const importModalOpen = ref(false)
const pendingFile = ref<ImportableFile | null>(null)
const loadingDestinations = ref(false)

// Read-only destinations (e.g. a HiDrive share mounted --read-only) are shown but
// disabled — importing there would fail, so the picker surfaces that up-front.
const destinationItems = computed(() =>
    destinations.value.map(d => ({
      label: d.writable ? d.label : `${d.label} (read-only)`,
      value: d.key,
      disabled: !d.writable,
    }))
)

// The selected destination is importable only if it exists and is writable.
const selectedDestWritable = computed(() =>
    destinations.value.find(d => d.key === selectedDestKey.value)?.writable === true
)

// Prefer the server-flagged default when it's writable, else the first writable
// destination, else fall back so the select is never left blank.
function pickDefaultDestination(): ImportDestination | undefined {
  const d = destinations.value
  return d.find(x => x.isDefault && x.writable)
      ?? d.find(x => x.writable)
      ?? d.find(x => x.isDefault)
      ?? d[0]
}

async function loadDestinations() {
  try {
    const result = await adminApi.listImportDestinations()
    destinations.value = result ?? []
  } catch {
    destinations.value = []
  }
}

async function loadImportable() {
  try {
    const result = await adminApi.listImportable()
    importable.value = result ?? []
    if (importableLoading.value) importableLoading.value = false
    if (autoImport.value) void runAutoImport()
  } catch {
    if (importableLoading.value) importableLoading.value = false
  }
}

// Sweep the importable list into the library while auto-import is on. Imports to
// the default destination (empty destination => backend's defaultImportDir),
// removing the source and triggering a rescan, then refreshes so cleared entries
// disappear. Each name is marked attempted before the call so a failing file is
// not retried every 5s poll, and the in-flight guard keeps it from racing a
// manual import or another sweep.
async function runAutoImport() {
  if (!autoImport.value || importingFile.value !== null) return
  const pending = importable.value.filter(f => !autoImportAttempted.has(f.name))
  if (!pending.length) return
  for (const f of pending) {
    if (!autoImport.value) break
    autoImportAttempted.add(f.name)
    importingFile.value = f.name
    try {
      const result = await adminApi.importFile(f.name, true, true)
      notifySuccess(`Auto-imported ${f.name} → ${result?.destination ?? 'library'}`)
    } catch (e: unknown) {
      notifyError(e, `Auto-import failed for ${f.name}`)
    } finally {
      importingFile.value = null
    }
  }
  // Re-fetch directly (not via loadImportable) so the refresh doesn't recurse
  // back into another sweep mid-pass.
  try {
    importable.value = (await adminApi.listImportable()) ?? []
  } catch { /* keep current list */ }
  await load()
}

// Restore the persisted auto-import preference on mount (hoisted so onMounted can
// call it before its own definition). Setting the ref fires the watcher below.
function loadAutoImportPref() {
  try {
    autoImport.value = localStorage.getItem('downloader.autoImport') === '1'
  } catch { /* localStorage unavailable (private mode) — leave default off */ }
}

// Persist the toggle, and when it's switched on, forget prior attempts and sweep
// whatever is already importable so pending files import immediately.
watch(autoImport, (on) => {
  try {
    localStorage.setItem('downloader.autoImport', on ? '1' : '0')
  } catch { /* ignore */ }
  if (on) {
    autoImportAttempted.clear()
    void runAutoImport()
  }
})

// Open the per-file prompt so the operator picks where the download is stored.
// Always starts from a clean subfolder field and a freshly-chosen default so a
// previous import's choices never leak into the next one.
async function openImport(f: ImportableFile) {
  pendingFile.value = f
  newSubfolder.value = ''
  if (!destinations.value.length) {
    loadingDestinations.value = true
    try {
      await loadDestinations()
    } finally {
      loadingDestinations.value = false
    }
  }
  if (!destinations.value.length) {
    toast.add({
      title: 'No import destinations available',
      description: 'The downloader may be disabled, or no library folders are configured.',
      color: 'warning',
      icon: 'i-lucide-alert-triangle',
    })
    pendingFile.value = null
    return
  }
  const def = pickDefaultDestination()
  if (def) selectedDestKey.value = def.key
  importModalOpen.value = true
}

// Reset all per-import modal state so the next open starts fresh.
function closeImportModal() {
  importModalOpen.value = false
  newSubfolder.value = ''
  pendingFile.value = null
}

async function confirmImport() {
  const f = pendingFile.value
  if (!f) return
  importingFile.value = f.name
  try {
    const result = await adminApi.importFile(f.name, deleteSource.value, triggerScan.value, selectedDestKey.value, newSubfolder.value.trim())
    const deleteNote = result?.sourceDeleted === false ? ' (source file could not be removed)' : ''
    notifySuccess(`Imported to ${result?.destination ?? 'library'}${deleteNote}`)
    // Close only on success — on error the modal stays open so the admin sees the
    // failure in context and can retry or change the destination.
    closeImportModal()
    await Promise.allSettled([load(), loadImportable()])
  } catch (e: unknown) {
    notifyError(e, 'Import failed')
  } finally {
    importingFile.value = null
  }
}


function progressBarColor(status: DownloaderProgress['status']) {
  if (status === 'error') return 'error'
  if (status === 'complete' || status === 'completed') return 'success'
  return 'primary'
}

// ── Download options (downloader v1.5.0) ──────────────────────────────────────
// Apply to both detect-based downloads and batch downloads.
const audioOnly = ref(false)
const audioFormat = ref('mp3')
const videoFormat = ref('')

// Audio formats advertised by the downloader; fall back to a standard set.
const audioFormatOptions = computed<string[]>(() =>
    settings.value?.audioFormats?.length ? settings.value.audioFormats : ['mp3', 'm4a', 'aac', 'opus', 'flac', 'wav'],
)

// Per-job option object spread into download/batch params. Hoisted so
// startDownload (declared above) can call it; the refs are read at call time.
function downloadOptions(): { audioOnly?: boolean; audioFormat?: string; format?: string } {
  if (audioOnly.value) return {audioOnly: true, audioFormat: audioFormat.value}
  const f = videoFormat.value.trim()
  return f ? {format: f} : {}
}

// ── Batch download (v1.5.0) ───────────────────────────────────────────────────
const batchUrls = ref('')
const batchRunning = ref(false)

async function startBatch() {
  const urls = batchUrls.value.split('\n').map(u => u.trim()).filter(Boolean)
  if (!urls.length) {
    notifyError('Enter at least one URL (one per line)')
    return
  }
  batchRunning.value = true
  const clientId = wsClientId.value ?? `admin-${Date.now()}`
  const opts = downloadOptions()
  try {
    const result = await adminApi.batchDownloaderJobs({
      clientId,
      urls: urls.map(url => ({url, ...opts})),
    })
    const rejected = result?.rejected?.length ?? 0
    if (result?.queued) {
      notifySuccess(`Queued ${result.queued} download${result.queued === 1 ? '' : 's'}${rejected ? `, ${rejected} rejected` : ''}`)
      batchUrls.value = ''
    } else {
      notifyError(`Nothing queued${rejected ? ` — ${rejected} rejected (unsupported or invalid)` : ''}`)
    }
    await Promise.allSettled([load(), loadQueue()])
  } catch (e: unknown) {
    notifyError(e, 'Batch download failed')
  } finally {
    batchRunning.value = false
  }
}

// ── Downloader queue (server-side active + queued jobs, v1.5.0) ────────────────
const queue = ref<DownloaderQueue | null>(null)

async function loadQueue() {
  if (!isOnline.value) {
    queue.value = null
    return
  }
  try {
    queue.value = await adminApi.getDownloaderQueue()
  } catch {
    queue.value = null
  }
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
            <UIcon :name="wsConnected ? 'i-lucide-wifi' : 'i-lucide-wifi-off'" class="size-3.5"
                   :class="wsConnected ? 'text-success' : 'text-muted'"/>
            <span class="text-xs text-muted">{{ wsConnected ? 'WS connected' : 'WS disconnected' }}</span>
          </div>
          <UButton icon="i-lucide-refresh-cw" size="xs" variant="ghost" color="neutral" @click="loadHealth"/>
        </div>

        <!-- Dependencies -->
        <div v-if="health?.online && health.dependencies && Object.keys(health.dependencies).length > 0"
             class="flex flex-wrap gap-2">
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
            <UIcon name="i-lucide-settings" class="size-4"/>
            Downloader Settings
          </span>
          <UButton icon="i-lucide-x" size="xs" variant="ghost" color="neutral" @click="showSettings = false"/>
        </div>
      </template>
      <div class="grid grid-cols-2 sm:grid-cols-3 gap-3 text-sm">
        <div v-if="settings.downloadsDir">
          <p class="text-xs text-muted">Downloads Dir</p>
          <p class="font-mono text-xs truncate" :title="settings.downloadsDir">{{ settings.downloadsDir }}</p>
        </div>
        <div>
          <p class="text-xs text-muted">Server Storage</p>
          <UBadge :label="settings.allowServerStorage ? 'Allowed' : 'Browser only'"
                  :color="settings.allowServerStorage ? 'success' : 'neutral'" variant="subtle" size="xs"/>
        </div>
        <div v-if="settings.audioFormat">
          <p class="text-xs text-muted">Audio Format</p>
          <p class="font-medium">{{ settings.audioFormat }}</p>
        </div>
        <div v-if="settings.browserRelayConfigured != null">
          <p class="text-xs text-muted">Browser Relay</p>
          <UBadge :label="settings.browserRelayConfigured ? 'Configured' : 'Not configured'"
                  :color="settings.browserRelayConfigured ? 'success' : 'neutral'" variant="subtle" size="xs"/>
        </div>
        <div v-if="settings.proxyPoolSize != null">
          <p class="text-xs text-muted">Proxy Pool</p>
          <UBadge
              :label="settings.proxyPoolSize > 0 ? `${settings.proxyPoolSize} entr${settings.proxyPoolSize === 1 ? 'y' : 'ies'}` : 'None'"
              :color="settings.proxyPoolSize > 0 ? 'success' : 'neutral'"
              variant="subtle"
              size="xs"
          />
        </div>
      </div>
      <div v-if="settings.supportedSites?.length" class="mt-3">
        <p class="text-xs text-muted mb-1">Supported Sites ({{ settings.supportedSites.length }})</p>
        <div class="flex flex-wrap gap-1 max-h-24 overflow-y-auto">
          <UBadge v-for="site in settings.supportedSites" :key="site" :label="site" color="neutral" variant="subtle"
                  size="xs"/>
        </div>
      </div>
    </UCard>
    <div v-else-if="settings" class="flex justify-end">
      <UButton icon="i-lucide-settings" label="Show Settings" size="sm" variant="ghost" color="neutral"
               @click="showSettings = true"/>
    </div>

    <!-- New Download — detect first, then choose stream -->
    <UCard>
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-cloud-download" class="size-4"/>
          New Download
        </div>
      </template>
      <div class="space-y-3">
        <p class="text-xs text-muted">
          Detect the URL first to see available streams. With server storage enabled, completed downloads are saved to
          the configured import directory.
        </p>
        <div class="flex flex-wrap gap-2">
          <UInput v-model="newUrl" placeholder="URL to download…" class="flex-1 min-w-64" :disabled="!isOnline"
                  @keyup.enter="detect"/>
          <UButton :loading="detecting" icon="i-lucide-search" label="Detect" variant="outline" color="neutral"
                   :disabled="!newUrl.trim() || !isOnline" @click="detect"/>
        </div>
        <p v-if="!isOnline" class="text-xs text-warning">Downloader is offline — detection and downloads are
          unavailable.</p>

        <!-- Download options (v1.5.0) — applied to detect-based and batch downloads -->
        <div class="flex flex-wrap items-center gap-3 rounded bg-muted px-3 py-2">
          <USwitch v-model="audioOnly" :disabled="!isOnline" label="Audio only" size="sm"/>
          <USelect
              v-if="audioOnly"
              v-model="audioFormat"
              :items="audioFormatOptions"
              size="sm"
              class="w-28"
              aria-label="Audio format"
          />
          <UInput
              v-else
              v-model="videoFormat"
              placeholder="yt-dlp -f format (optional)"
              size="sm"
              class="flex-1 min-w-48"
              aria-label="Video format selector"
          />
        </div>

        <!-- Auto-import: send finished downloads straight to the library -->
        <div class="flex items-center gap-2 rounded bg-muted px-3 py-2">
          <USwitch v-model="autoImport" label="Auto-import when complete" size="sm"
                   aria-label="Auto-import completed downloads into the library"/>
          <UTooltip text="Finished downloads are imported to the default library folder and removed from the downloader automatically.">
            <UIcon name="i-lucide-info" class="size-4 text-muted"/>
          </UTooltip>
        </div>

        <!-- Stream options from detect -->
        <template v-if="detected">
          <UCard :ui="{ body: 'p-3' }">
            <p class="text-sm font-medium mb-2">{{ detected.title || 'Detected Streams' }}</p>

            <div v-if="detected.engine === 'ytdlp' || detected.adminUnlocked"
                 class="flex flex-wrap items-center gap-1.5 mb-2">
              <UBadge v-if="detected.engine === 'ytdlp'" label="yt-dlp engine" color="info" variant="subtle" size="xs"
                      icon="i-lucide-wand-2"/>
              <UBadge v-if="detected.adminUnlocked" label="Any-site unlocked" color="success" variant="subtle" size="xs"
                      icon="i-lucide-unlock"/>
            </div>

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

    <!-- Batch download (v1.5.0) — queue many URLs at once -->
    <UCard :ui="{ body: 'p-4' }">
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-list-plus" class="size-4"/>
          Batch download
        </div>
      </template>
      <div class="space-y-2">
        <p class="text-xs text-muted">
          Paste one URL per line to queue many downloads at once. The audio-only / format options above apply to every
          URL.
        </p>
        <UTextarea
            v-model="batchUrls"
            :rows="4"
            placeholder="One URL per line…"
            :disabled="!isOnline"
            class="w-full font-mono text-xs"
        />
        <div class="flex justify-end">
          <UButton
              :loading="batchRunning"
              icon="i-lucide-list-plus"
              label="Queue batch"
              color="primary"
              :disabled="!isOnline || !batchUrls.trim()"
              @click="startBatch"
          />
        </div>
      </div>
    </UCard>

    <!-- Downloader queue: server-side active + queued jobs (v1.5.0) -->
    <UCard v-if="queue && (queue.active.length > 0 || queue.queued.length > 0)">
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-list-ordered" class="size-4"/>
          Queue
          <UBadge :label="`${queue.processing}/${queue.maxConcurrent} processing`" color="info" variant="subtle"
                  size="xs"/>
        </div>
      </template>
      <div class="space-y-3">
        <div v-if="queue.active.length > 0">
          <p class="text-xs font-medium text-muted mb-1">Active</p>
          <div class="space-y-1">
            <div v-for="a in queue.active" :key="a.downloadId"
                 class="flex items-center gap-2 text-xs rounded bg-muted px-2 py-1">
              <UBadge :label="a.type" color="primary" variant="subtle" size="xs"/>
              <span class="font-mono truncate">{{ a.downloadId }}</span>
            </div>
          </div>
        </div>
        <div v-if="queue.queued.length > 0">
          <p class="text-xs font-medium text-muted mb-1">Queued ({{ queue.queued.length }})</p>
          <div class="space-y-1">
            <div v-for="q in queue.queued" :key="q.downloadId"
                 class="flex items-center gap-2 text-xs rounded bg-muted px-2 py-1">
              <UBadge v-if="q.audioOnly" label="audio" color="neutral" variant="subtle" size="xs"/>
              <span class="flex-1 truncate">{{ q.title || q.url }}</span>
            </div>
          </div>
        </div>
      </div>
    </UCard>

    <!-- Active downloads with real-time progress -->
    <UCard v-if="activeProgress.size > 0">
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-loader-2" class="size-4 animate-spin text-primary"/>
          Active Downloads
          <UBadge :label="String(activeProgress.size)" color="info" variant="subtle" size="xs"/>
        </div>
      </template>
      <div class="space-y-3">
        <div v-for="[id, dl] in activeProgress" :key="id" class="space-y-1">
          <div class="flex items-center justify-between gap-2 text-sm">
            <span class="truncate font-medium flex-1">{{ dl.title || dl.filename || id }}</span>
            <div class="flex items-center gap-2 shrink-0">
              <span class="text-xs text-muted">{{ dl.status }}{{
                  dl.speed ? ` · ${dl.speed}` : ''
                }}{{ dl.eta ? ` · ETA ${dl.eta}` : '' }}</span>
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
          <UProgress :model-value="dl.progress ?? 0" :color="progressBarColor(dl.status)" size="xs"/>
          <p v-if="dl.error" class="text-xs text-error">{{ dl.error }}</p>
        </div>
      </div>
    </UCard>

    <!-- Importable files — move to media library -->
    <UCard>
      <template #header>
        <div class="flex items-center justify-between">
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-package-check" class="size-4"/>
            Import to Library
            <UBadge :label="String(importable.length)" color="neutral" variant="subtle" size="xs"/>
          </div>
          <div class="flex items-center gap-3 text-sm">
            <label class="flex items-center gap-1.5 cursor-pointer">
              <UCheckbox v-model="deleteSource"/>
              <span>Delete source</span>
            </label>
            <label class="flex items-center gap-1.5 cursor-pointer">
              <UCheckbox v-model="triggerScan"/>
              <span>Scan library</span>
            </label>
            <UButton icon="i-lucide-refresh-cw" size="xs" variant="ghost" color="neutral" :loading="importableLoading"
                     @click="loadImportable"/>
          </div>
        </div>
      </template>
      <p class="text-xs text-muted mb-3">
        Files below have been downloaded to the server's configured downloads directory and are ready to be moved to the
        media library.
      </p>
      <div v-if="importableLoading" class="flex justify-center py-4">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-5"/>
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
              :disabled="importingFile !== null"
              @click="openImport(f)"
          />
        </div>
      </div>
    </UCard>

    <!-- Import destination prompt -->
    <UModal v-model:open="importModalOpen" :ui="{ content: 'max-w-md' }">
      <template #content>
        <UCard>
          <template #header>
            <div class="font-semibold flex items-center gap-2">
              <UIcon name="i-lucide-import" class="size-4"/>
              Import to library
            </div>
          </template>
          <div class="space-y-3">
            <p v-if="pendingFile" class="text-sm truncate" :title="pendingFile.name">
              <span class="text-muted">File:</span> {{ pendingFile.name }}
            </p>
            <UFormField label="Destination" hint="Where to store this file in the library">
              <USelect
                  v-model="selectedDestKey"
                  :items="destinationItems"
                  :loading="loadingDestinations"
                  placeholder="Select a destination"
                  class="w-full"
              />
            </UFormField>
            <p v-if="selectedDestKey && !selectedDestWritable" class="text-xs text-error flex items-center gap-1">
              <UIcon name="i-lucide-lock" class="size-3.5 shrink-0"/>
              This destination is read-only. Pick a writable location or remount it read-write.
            </p>
            <UFormField label="New sub-folder" hint="Optional — creates a folder under the destination">
              <UInput
                  v-model="newSubfolder"
                  placeholder="e.g. New Series"
                  class="w-full"
              />
            </UFormField>
            <div class="flex items-center gap-4 text-sm">
              <label class="flex items-center gap-1.5 cursor-pointer">
                <UCheckbox v-model="deleteSource"/>
                <span>Delete source</span>
              </label>
              <label class="flex items-center gap-1.5 cursor-pointer">
                <UCheckbox v-model="triggerScan"/>
                <span>Scan library</span>
              </label>
            </div>
          </div>
          <template #footer>
            <div class="flex justify-end gap-2">
              <UButton variant="ghost" color="neutral" label="Cancel" @click="closeImportModal"/>
              <UButton
                  color="primary"
                  label="Import"
                  icon="i-lucide-check"
                  :loading="importingFile !== null"
                  :disabled="!selectedDestKey || !selectedDestWritable || importingFile !== null"
                  @click="confirmImport"
              />
            </div>
          </template>
        </UCard>
      </template>
    </UModal>

    <!-- Downloaded files list (server files) -->
    <UCard>
      <template #header>
        <div class="flex items-center justify-between">
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-folder-open" class="size-4"/>
            Server Files
          </div>
          <UButton icon="i-lucide-refresh-cw" size="xs" variant="ghost" color="neutral" @click="load"/>
        </div>
      </template>
      <div v-if="loading" class="flex justify-center py-8">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-6"/>
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
            <p class="text-sm font-medium truncate" :title="row.original.filename">{{
                row.original.filename || '—'
              }}</p>
            <p v-if="row.original.url" class="text-xs text-muted truncate" :title="row.original.url">{{
                row.original.url
              }}</p>
          </div>
        </template>
        <template #size-cell="{ row }">
          <span class="text-sm">{{ formatBytes(row.original.size) }}</span>
        </template>
        <template #created-cell="{ row }">
          <span class="text-sm text-muted">{{
              row.original.created ? new Date(row.original.created * 1000).toLocaleString() : '—'
            }}</span>
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

<script setup lang="ts">
import type { AdminStats, SystemInfo, StreamSession, UploadProgress, ModuleHealth, ServerSettings, MediaStats } from '~/types/api'
import { formatBytes, formatUptime } from '~/utils/format'

const STREAM_COLUMNS = [
  { accessorKey: 'user_id', header: 'User' },
  { accessorKey: 'media_id', header: 'Media ID' },
  { accessorKey: 'quality', header: 'Quality' },
  { accessorKey: 'bytes_sent', header: 'Sent' },
  { accessorKey: 'ip_address', header: 'Client IP' },
  { accessorKey: 'started_at', header: 'Since' },
]

const UPLOAD_COLUMNS = [
  { accessorKey: 'filename', header: 'File' },
  { accessorKey: 'user_id', header: 'User' },
  { accessorKey: 'progress', header: 'Progress' },
  { accessorKey: 'status', header: 'Status' },
]

const adminApi = useAdminApi()
const mediaApi = useMediaApi()
const settingsApi = useSettingsApi()
const toast = useToast()

const settings = ref<ServerSettings | null>(null)

const stats = ref<AdminStats | null>(null)
const system = ref<SystemInfo | null>(null)
const mediaStats = ref<MediaStats | null>(null)
const streams = ref<StreamSession[]>([])
const uploads = ref<UploadProgress[]>([])
const statsLoading = ref(true)


const diskPct = computed(() => {
  if (!stats.value) return 0
  return Math.round(((stats.value.disk_usage ?? 0) / ((stats.value.disk_total || 1))) * 100)
})
const diskColor = computed(() => diskPct.value > 90 ? 'error' : diskPct.value > 70 ? 'warning' : 'success')

const memPct = computed(() => {
  if (!system.value) return 0
  return Math.round(((system.value.memory_used ?? 0) / ((system.value.memory_total || 1))) * 100)
})

function moduleStatusColor(status: ModuleHealth['status']) {
  return status === 'healthy' ? 'success' : status === 'degraded' ? 'warning' : 'error'
}

async function loadAll() {
  statsLoading.value = true
  try {
    const [s, sys, str, upl, sett, ms] = await Promise.allSettled([
      adminApi.getStats(),
      adminApi.getSystemInfo(),
      adminApi.getActiveStreams(),
      adminApi.getActiveUploads(),
      settingsApi.get(),
      mediaApi.getStats(),
    ])
    if (s.status === 'fulfilled') stats.value = s.value
    if (sys.status === 'fulfilled') system.value = sys.value
    if (str.status === 'fulfilled') streams.value = str.value ?? []
    if (upl.status === 'fulfilled') uploads.value = upl.value ?? []
    if (sett.status === 'fulfilled') settings.value = sett.value
    if (ms.status === 'fulfilled') mediaStats.value = ms.value
  } finally {
    statsLoading.value = false
  }
}

const actionBusy = ref<Set<string>>(new Set())

async function handleAction(fn: () => Promise<unknown>, successMsg: string, actionKey?: string) {
  const key = actionKey ?? successMsg
  if (actionBusy.value.has(key)) return
  const next = new Set(actionBusy.value); next.add(key); actionBusy.value = next
  try {
    await fn()
    toast.add({ title: successMsg, color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Action failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    const cleared = new Set(actionBusy.value); cleared.delete(key); actionBusy.value = cleared
  }
}

onMounted(loadAll)
// Auto-refresh every 30s — skip when tab is hidden to avoid wasteful background requests
const interval = setInterval(() => { if (!document.hidden) loadAll() }, 30_000)
onUnmounted(() => clearInterval(interval))
</script>

<template>
  <div class="space-y-6">
    <!-- Loading -->
    <div v-if="statsLoading && !stats" class="flex justify-center py-12">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-8 text-primary" />
    </div>

    <template v-else-if="stats">
      <!-- Stat cards -->
      <div class="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-7 gap-3">
        <UCard
          v-for="item in [
            { label: 'Videos', value: stats.total_videos ?? 0, icon: 'i-lucide-film' },
            { label: 'Audio', value: stats.total_audio ?? 0, icon: 'i-lucide-music' },
            { label: 'Users', value: stats.total_users ?? 0, icon: 'i-lucide-users' },
            { label: 'Sessions', value: stats.active_sessions ?? 0, icon: 'i-lucide-activity' },
            { label: 'Views', value: stats.total_views ?? 0, icon: 'i-lucide-eye' },
            { label: 'HLS Running', value: stats.hls_jobs_running ?? 0, icon: 'i-lucide-radio' },
            { label: 'HLS Done', value: stats.hls_jobs_completed ?? 0, icon: 'i-lucide-check-circle' },
          ]"
          :key="item.label"
          :ui="{ body: 'p-4' }"
        >
          <div class="flex items-start gap-2">
            <UIcon :name="item.icon" class="size-4 text-muted mt-0.5" />
            <div>
              <p class="text-xl font-bold text-highlighted">{{ item.value.toLocaleString() }}</p>
              <p class="text-xs text-muted">{{ item.label }}</p>
            </div>
          </div>
        </UCard>
      </div>

      <!-- Disk + Library -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-2 font-semibold">
            <UIcon name="i-lucide-hard-drive" class="size-4" />
            Storage
          </div>
        </template>
        <div class="space-y-2">
          <div class="flex justify-between text-sm">
            <span class="text-muted">Disk Usage</span>
            <span>{{ formatBytes(stats.disk_usage ?? 0) }} / {{ formatBytes(stats.disk_total ?? 0) }}</span>
          </div>
          <UProgress :value="diskPct" :color="diskColor" size="sm" />
          <p class="text-xs text-muted">{{ diskPct }}% used · {{ formatBytes(stats.disk_free ?? 0) }} free</p>
          <template v-if="mediaStats">
            <div class="border-t border-default pt-2 mt-2 grid grid-cols-2 gap-2 text-sm">
              <div><span class="text-muted">Library Size:</span> {{ formatBytes(mediaStats.total_size) }}</div>
              <div v-if="mediaStats.last_scan">
                <span class="text-muted">Last Scan:</span>
                {{ new Date(mediaStats.last_scan).toLocaleString() }}
              </div>
            </div>
          </template>
        </div>
      </UCard>

      <!-- Live streams -->
      <UCard>
        <template #header>
          <div class="flex items-center gap-2 font-semibold">
            <UIcon name="i-lucide-radio" class="size-4" />
            Live Streams
            <UBadge :label="String(streams.length)" color="neutral" variant="subtle" size="xs" />
          </div>
        </template>
        <p v-if="streams.length === 0" class="text-muted text-sm">No active streams.</p>
        <UTable
          v-else
          :data="streams"
          :columns="STREAM_COLUMNS"
          class="text-sm"
        >
          <template #media_id-cell="{ row }">
            <span class="font-mono text-xs" :title="row.original.media_id">
              {{ row.original.media_id?.slice(0, 8) }}…
            </span>
          </template>
          <template #bytes_sent-cell="{ row }">
            {{ formatBytes(row.original.bytes_sent ?? 0) }}
          </template>
          <template #started_at-cell="{ row }">
            {{ row.original.started_at ? new Date(row.original.started_at).toLocaleTimeString() : '—' }}
          </template>
        </UTable>
      </UCard>

      <!-- Uploads -->
      <UCard v-if="uploads.length > 0">
        <template #header>
          <div class="flex items-center gap-2 font-semibold">
            <UIcon name="i-lucide-upload" class="size-4" />
            Active Uploads
          </div>
        </template>
        <UTable
          :data="uploads"
          :columns="UPLOAD_COLUMNS"
          class="text-sm"
        >
          <template #progress-cell="{ row }">
            {{ row.original.progress != null ? `${Math.round(row.original.progress)}%` : '—' }}
          </template>
        </UTable>
      </UCard>
    </template>

    <!-- System info -->
    <UCard v-if="system">
      <template #header>
        <div class="flex items-center gap-2 font-semibold">
          <UIcon name="i-lucide-cpu" class="size-4" />
          System Info
        </div>
      </template>
      <div class="grid grid-cols-2 sm:grid-cols-3 gap-4 text-sm mb-4">
        <div><span class="text-muted">Version:</span> <span class="font-mono">{{ system.version }}</span></div>
        <div v-if="system.build_date"><span class="text-muted">Built:</span> {{ new Date(system.build_date).toLocaleDateString() }}</div>
        <div><span class="text-muted">OS/Arch:</span> {{ system.os }}/{{ system.arch }}</div>
        <div><span class="text-muted">Go:</span> {{ system.go_version }}</div>
        <div><span class="text-muted">Uptime:</span> {{ formatUptime(system.uptime) }}</div>
        <div><span class="text-muted">CPUs:</span> {{ system.cpu_count }}</div>
        <div>
          <span class="text-muted">Memory:</span>
          {{ formatBytes(system.memory_used) }} / {{ formatBytes(system.memory_total) }}
          <UProgress :value="memPct" size="xs" class="mt-1" />
        </div>
      </div>
      <div v-if="system.modules?.length" class="space-y-1">
        <p class="text-sm font-medium text-highlighted mb-2">Module Health</p>
        <div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-2">
          <div
            v-for="m in system.modules"
            :key="m.name"
            class="flex items-center justify-between text-xs bg-muted rounded px-2 py-1"
          >
            <span class="text-muted truncate">{{ m.name }}</span>
            <UBadge :label="m.status" :color="moduleStatusColor(m.status)" size="xs" variant="subtle" />
          </div>
        </div>
      </div>
    </UCard>

    <!-- Features panel -->
    <UCard v-if="settings">
      <template #header>
        <div class="flex items-center gap-2 font-semibold">
          <UIcon name="i-lucide-toggle-right" class="size-4" />
          Feature Flags
        </div>
      </template>
      <div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-2">
        <div
          v-for="[name, enabled] in Object.entries(settings.features)"
          :key="name"
          class="flex items-center justify-between text-xs rounded px-2 py-1.5"
          :class="enabled ? 'bg-success/10' : 'bg-muted'"
        >
          <span class="text-muted truncate">{{ name.replace(/^enable/, '').replace(/([A-Z])/g, ' $1').trim() }}</span>
          <UIcon :name="enabled ? 'i-lucide-check' : 'i-lucide-minus'" :class="enabled ? 'text-success' : 'text-muted'" class="size-3 shrink-0 ml-1" />
        </div>
      </div>
    </UCard>

    <!-- Server controls -->
    <UCard>
      <template #header>
        <div class="flex items-center gap-2 font-semibold">
          <UIcon name="i-lucide-settings-2" class="size-4" />
          Server Controls
        </div>
      </template>
      <div class="flex flex-wrap gap-2">
        <UButton
          icon="i-lucide-trash-2"
          label="Clear Cache"
          variant="outline"
          color="neutral"
          :loading="actionBusy.has('clear-cache')"
          @click="handleAction(adminApi.clearCache, 'Cache cleared', 'clear-cache')"
        />
        <UButton
          icon="i-lucide-scan"
          label="Scan Media"
          variant="outline"
          color="neutral"
          :loading="actionBusy.has('scan-media')"
          @click="handleAction(adminApi.scanMedia, 'Media scan triggered', 'scan-media')"
        />
        <UButton
          icon="i-lucide-rotate-cw"
          label="Restart Server"
          variant="outline"
          color="warning"
          :loading="actionBusy.has('restart')"
          @click="handleAction(adminApi.restartServer, 'Server restarting…', 'restart')"
        />
        <UButton
          icon="i-lucide-power"
          label="Shutdown"
          variant="outline"
          color="error"
          :loading="actionBusy.has('shutdown')"
          @click="handleAction(adminApi.shutdownServer, 'Server shutting down…', 'shutdown')"
        />
      </div>
    </UCard>
  </div>
</template>

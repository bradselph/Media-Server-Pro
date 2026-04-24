<script setup lang="ts">
import type { HLSJob, HLSStats, HLSCapabilities, HLSValidationResult } from '~/types/api'
import { formatBytes } from '~/utils/format'
import { asRecord } from '~/utils/typeGuards'

const adminApi = useAdminApi()
const hlsApi = useHlsApi()
const toast = useToast()

const jobs = ref<HLSJob[]>([])
const stats = ref<HLSStats | null>(null)
const caps = ref<HLSCapabilities | null>(null)
const loading = ref(true)
const validating = ref<string | null>(null)
const validationResult = ref<HLSValidationResult | null>(null)
const jobRefreshing = ref<string | null>(null)

const fullConfig = ref<Record<string, unknown>>({})
const autoGenerate = ref(false)
const pregenIntervalHours = ref(1)
const configSaving = ref(false)

let destroyed = false
let loadSeq = 0

const INTERVAL_OPTIONS = [
  { label: '15 minutes', value: 0 },
  { label: '1 hour', value: 1 },
  { label: '2 hours', value: 2 },
  { label: '4 hours', value: 4 },
  { label: '6 hours', value: 6 },
  { label: '12 hours', value: 12 },
  { label: '24 hours', value: 24 },
]

async function load() {
  const seq = ++loadSeq
  loading.value = true
  try {
    const [j, s, cfg, c] = await Promise.allSettled([
      adminApi.listHLSJobs(),
      adminApi.getHLSStats(),
      adminApi.getConfig(),
      hlsApi.getCapabilities(),
    ])
    if (destroyed || seq !== loadSeq) return
    if (j.status === 'fulfilled') jobs.value = j.value ?? []
    if (s.status === 'fulfilled') stats.value = s.value
    if (c.status === 'fulfilled') caps.value = c.value
    if (cfg.status === 'fulfilled' && cfg.value) {
      fullConfig.value = cfg.value
      const hls = asRecord(cfg.value.hls)
      autoGenerate.value = hls?.auto_generate === true
      const intervalValue = typeof hls?.pre_generate_interval_hours === 'number'
        ? hls.pre_generate_interval_hours
        : 1
      const validValues = INTERVAL_OPTIONS.map(o => o.value)
      pregenIntervalHours.value = validValues.includes(intervalValue) ? intervalValue : 1
    }
  } finally {
    if (!destroyed) loading.value = false
  }
}

async function saveHLSConfig() {
  configSaving.value = true
  try {
    const updated = {
      ...fullConfig.value,
      hls: {
        ...asRecord(fullConfig.value.hls),
        auto_generate: autoGenerate.value,
        pre_generate_interval_hours: pregenIntervalHours.value,
      },
    }
    await adminApi.updateConfig(updated)
    if (destroyed) return
    fullConfig.value = updated
    toast.add({ title: 'HLS settings saved', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    if (destroyed) return
    toast.add({ title: e instanceof Error ? e.message : 'Failed to save', color: 'error', icon: 'i-lucide-x' })
    // Reload config from server to revert UI to actual state
    await load()
  } finally {
    if (!destroyed) configSaving.value = false
  }
}

async function copyJobId(id: string) {
  if (!id) {
    toast.add({ title: 'ID not available', color: 'error', icon: 'i-lucide-x' })
    return
  }
  try {
    await navigator.clipboard.writeText(id)
    toast.add({ title: 'ID copied', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: 'Failed to copy ID', color: 'error', icon: 'i-lucide-x' })
  }
}

async function deleteJob(id: string) {
  try {
    await adminApi.deleteHLSJob(id)
    if (destroyed) return
    toast.add({ title: 'Job deleted', color: 'success', icon: 'i-lucide-check' })
    await load()
  } catch (e: unknown) {
    if (destroyed) return
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function cleanInactive() {
  try {
    await adminApi.cleanHLSInactive()
    if (destroyed) return
    toast.add({ title: 'Cleaned inactive HLS jobs', color: 'success', icon: 'i-lucide-check' })
    await load()
  } catch (e: unknown) {
    if (destroyed) return
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}


function statusColor(status: HLSJob['status']): 'neutral' | 'info' | 'success' | 'error' | 'warning' {
  const map = { pending: 'neutral', running: 'info', completed: 'success', failed: 'error', canceled: 'warning' } as const
  return map[status] ?? 'neutral'
}

async function cleanStaleLocks() {
  try {
    await adminApi.cleanHLSStaleLocks()
    toast.add({ title: 'Stale locks cleaned', color: 'success', icon: 'i-lucide-check' })
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function refreshJobStatus(id: string) {
  jobRefreshing.value = id
  try {
    const updated = await hlsApi.getStatus(id)
    if (destroyed) return
    const idx = jobs.value.findIndex(j => j.id === id)
    if (idx !== -1) jobs.value = jobs.value.map((j, i) => i === idx ? updated : j)
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to refresh status', color: 'error', icon: 'i-lucide-x' })
  } finally { if (!destroyed) jobRefreshing.value = null }
}

async function validateJob(id: string) {
  validating.value = id
  validationResult.value = null
  try {
    validationResult.value = await adminApi.validateHLS(id)
    const ok = validationResult.value.valid
    toast.add({ title: ok ? 'HLS output is valid' : 'HLS validation failed', color: ok ? 'success' : 'warning', icon: ok ? 'i-lucide-check' : 'i-lucide-alert-triangle' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Validation failed', color: 'error', icon: 'i-lucide-x' })
  } finally { if (!destroyed) validating.value = null }
}

// Pagination
const jobsPerPage = 20
const jobsPage = ref(1)
const pagedJobs = computed(() => {
  const start = (jobsPage.value - 1) * jobsPerPage
  return jobs.value.slice(start, start + jobsPerPage)
})
const jobsTotalPages = computed(() => Math.ceil(jobs.value.length / jobsPerPage))

onMounted(() => {
  destroyed = false
  load()
})

onUnmounted(() => {
  destroyed = true
})
</script>

<template>
  <div class="space-y-4">
    <!-- Config -->
    <UCard :ui="{ body: 'p-4' }">
      <div class="space-y-4">
        <div class="flex items-center justify-between gap-4">
          <div>
            <p class="font-medium text-sm text-highlighted">Auto-generate HLS on scan</p>
            <p class="text-xs text-muted mt-0.5">Automatically create HLS variants when new media is discovered</p>
          </div>
          <USwitch v-model="autoGenerate" :disabled="configSaving || loading" aria-label="Auto-generate HLS on scan" />
        </div>

        <div v-if="autoGenerate" class="flex items-center justify-between gap-4 pt-3 border-t border-default">
          <div>
            <p class="font-medium text-sm text-highlighted">Pre-generation interval</p>
            <p class="text-xs text-muted mt-0.5">How often the scheduler scans for videos without HLS</p>
          </div>
          <USelect
            v-model="pregenIntervalHours"
            :items="INTERVAL_OPTIONS"
            value-key="value"
            :disabled="configSaving || loading"
            class="w-36"
            size="sm"
          />
        </div>

        <div class="flex justify-end pt-1">
          <UButton
            icon="i-lucide-save"
            label="Save HLS Settings"
            size="sm"
            :loading="configSaving"
            :disabled="loading"
            @click="saveHLSConfig"
          />
        </div>
      </div>
    </UCard>

    <!-- Capabilities -->
    <UCard v-if="caps" :ui="{ body: 'p-3' }">
      <div class="flex flex-wrap items-center gap-4 text-sm">
        <div class="flex items-center gap-1.5">
          <UIcon
            :name="caps.healthy ? 'i-lucide-check-circle' : 'i-lucide-alert-triangle'"
            :class="caps.healthy ? 'text-success' : 'text-warning'"
            class="size-4"
          />
          <span class="font-medium">{{ caps.healthy ? 'HLS Ready' : 'HLS Unavailable' }}</span>
        </div>
        <div class="flex items-center gap-1.5">
          <UBadge :label="caps.ffmpeg_found ? 'ffmpeg ✓' : 'ffmpeg ✗'" :color="caps.ffmpeg_found ? 'success' : 'error'" variant="subtle" size="xs" />
          <UBadge :label="caps.ffprobe_found ? 'ffprobe ✓' : 'ffprobe ✗'" :color="caps.ffprobe_found ? 'success' : 'error'" variant="subtle" size="xs" />
        </div>
        <div v-if="caps.qualities.length" class="flex items-center gap-1 flex-wrap">
          <span class="text-muted">Qualities:</span>
          <UBadge v-for="q in caps.qualities" :key="q" :label="q" color="neutral" variant="subtle" size="xs" />
        </div>
        <span class="text-muted text-xs">Max concurrent: {{ caps.max_concurrent }}</span>
        <span v-if="caps.message" class="text-muted text-xs ml-auto">{{ caps.message }}</span>
      </div>
    </UCard>

    <!-- Stats -->
    <div v-if="stats" class="grid grid-cols-2 sm:grid-cols-4 gap-3">
      <UCard v-for="item in [
        { label: 'Total', value: stats.total_jobs, icon: 'i-lucide-list' },
        { label: 'Running', value: stats.running_jobs, icon: 'i-lucide-play-circle', color: 'text-info' },
        { label: 'Completed', value: stats.completed_jobs, icon: 'i-lucide-check-circle', color: 'text-success' },
        { label: 'Disk Used', value: formatBytes(stats.cache_size_bytes), icon: 'i-lucide-hard-drive' },
      ]" :key="item.label" :ui="{ body: 'p-4' }">
        <div class="flex items-center gap-2">
          <UIcon :name="item.icon" class="size-4 text-muted" :class="item.color" />
          <div>
            <p class="text-lg font-bold text-highlighted">{{ item.value }}</p>
            <p class="text-xs text-muted">{{ item.label }}</p>
          </div>
        </div>
      </UCard>
    </div>

    <!-- Actions -->
    <div class="flex gap-2 flex-wrap">
      <UButton icon="i-lucide-refresh-cw" label="Refresh" variant="outline" color="neutral" @click="load" />
      <UButton icon="i-lucide-lock-open" label="Clean Stale Locks" variant="outline" color="warning" @click="cleanStaleLocks" />
      <UButton icon="i-lucide-trash-2" label="Clean Inactive" variant="outline" color="warning" @click="cleanInactive" />
    </div>

    <!-- Jobs table -->
    <UCard>
      <div v-if="loading" class="flex justify-center py-8">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-primary" />
      </div>
      <UTable
        v-else
        :data="pagedJobs"
        :columns="[
          { accessorKey: 'id', header: 'ID' },
          { accessorKey: 'status', header: 'Status' },
          { accessorKey: 'progress', header: 'Progress' },
          { accessorKey: 'qualities', header: 'Qualities' },
          { accessorKey: 'started_at', header: 'Started' },
          { accessorKey: 'actions', header: '' },
        ]"
      >
        <template #id-cell="{ row }">
          <button
            class="font-mono text-xs hover:text-primary cursor-pointer"
            :title="`${row.original.id} (click to copy)`"
            :disabled="!row.original.id"
            @click="row.original.id && copyJobId(row.original.id)"
          >{{ row.original.id?.slice(0, 12) }}…</button>
        </template>
        <template #status-cell="{ row }">
          <UBadge :label="row.original.status" :color="statusColor(row.original.status)" variant="subtle" size="xs" />
        </template>
        <template #progress-cell="{ row }">
          <div v-if="row.original.status === 'running'" class="flex items-center gap-2 min-w-20">
            <UProgress :value="row.original.progress" size="xs" class="flex-1" />
            <span class="text-xs">{{ row.original.progress }}%</span>
          </div>
          <span v-else class="text-sm text-muted">—</span>
        </template>
        <template #qualities-cell="{ row }">
          <div class="flex flex-wrap gap-1">
            <UBadge
              v-for="q in (row.original.qualities ?? [])"
              :key="q"
              :label="q"
              color="neutral"
              variant="outline"
              size="xs"
            />
          </div>
        </template>
        <template #started_at-cell="{ row }">
          <span class="text-sm text-muted">
            {{ row.original.started_at ? new Date(row.original.started_at).toLocaleString() : '—' }}
          </span>
        </template>
        <template #actions-cell="{ row }">
          <div class="flex gap-1">
            <UButton
              v-if="row.original.status === 'running' || row.original.status === 'pending'"
              icon="i-lucide-refresh-cw"
              aria-label="Refresh job status"
              size="xs"
              variant="ghost"
              color="neutral"
              :loading="jobRefreshing === row.original.id"
              @click="refreshJobStatus(row.original.id)"
            />
            <UButton
              v-if="row.original.status === 'completed'"
              icon="i-lucide-shield-check"
              aria-label="Validate HLS output"
              size="xs"
              variant="ghost"
              color="neutral"
              :loading="validating === row.original.id"
              @click="validateJob(row.original.id)"
            />
            <UButton
              icon="i-lucide-trash-2"
              :aria-label="`Delete HLS job ${row.original.id}`"
              size="xs"
              variant="ghost"
              color="error"
              :disabled="row.original.status === 'running'"
              @click="deleteJob(row.original.id)"
            />
          </div>
        </template>
      </UTable>
      <p v-if="!loading && jobs.length === 0" class="text-center py-6 text-muted text-sm">
        No HLS jobs found.
      </p>
      <div v-if="jobsTotalPages > 1" class="flex items-center justify-between pt-3 border-t border-default">
        <p class="text-xs text-muted">{{ jobs.length }} jobs · Page {{ jobsPage }}/{{ jobsTotalPages }}</p>
        <div class="flex gap-1">
          <UButton icon="i-lucide-chevron-left" size="xs" variant="ghost" color="neutral" :disabled="jobsPage <= 1" @click="jobsPage--" />
          <UButton icon="i-lucide-chevron-right" size="xs" variant="ghost" color="neutral" :disabled="jobsPage >= jobsTotalPages" @click="jobsPage++" />
        </div>
      </div>
    </UCard>
    <!-- Validation result -->
    <UCard v-if="validationResult">
      <template #header>
        <div class="flex items-center gap-2 font-semibold">
          <UIcon
            :name="validationResult.valid ? 'i-lucide-check-circle' : 'i-lucide-alert-triangle'"
            :class="validationResult.valid ? 'text-success' : 'text-warning'"
            class="size-4"
          />
          HLS Validation — {{ validationResult.valid ? 'Valid' : 'Invalid' }}
          <span class="text-muted font-normal text-xs ml-2">{{ validationResult.variant_count }} variant(s), {{ validationResult.segment_count }} segment(s)</span>
          <UButton icon="i-lucide-x" size="xs" variant="ghost" color="neutral" class="ml-auto" @click="validationResult = null" />
        </div>
      </template>
      <p class="font-mono text-xs text-muted">{{ validationResult.job_id }}</p>
      <ul v-if="validationResult.errors?.length" class="mt-2 space-y-1">
        <li v-for="err in validationResult.errors" :key="err" class="text-xs text-error">{{ err }}</li>
      </ul>
    </UCard>
  </div>
</template>

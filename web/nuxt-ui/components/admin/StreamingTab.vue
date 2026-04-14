<script setup lang="ts">
import type { HLSJob, HLSStats } from '~/types/api'
import { formatBytes } from '~/utils/format'
import { asRecord } from '~/utils/typeGuards'

const adminApi = useAdminApi()
const toast = useToast()

const jobs = ref<HLSJob[]>([])
const stats = ref<HLSStats | null>(null)
const loading = ref(true)

const fullConfig = ref<Record<string, unknown>>({})
const autoGenerate = ref(false)
const pregenIntervalHours = ref(1)
const configSaving = ref(false)

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
  loading.value = true
  try {
    const [j, s, cfg] = await Promise.allSettled([
      adminApi.listHLSJobs(),
      adminApi.getHLSStats(),
      adminApi.getConfig(),
    ])
    if (j.status === 'fulfilled') jobs.value = j.value ?? []
    if (s.status === 'fulfilled') stats.value = s.value
    if (cfg.status === 'fulfilled' && cfg.value) {
      fullConfig.value = cfg.value
      const hls = asRecord(cfg.value.hls)
      autoGenerate.value = hls?.auto_generate === true
      pregenIntervalHours.value = typeof hls?.pre_generate_interval_hours === 'number'
        ? hls.pre_generate_interval_hours
        : 1
    }
  } finally {
    loading.value = false
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
    fullConfig.value = updated
    toast.add({ title: 'HLS settings saved', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to save', color: 'error', icon: 'i-lucide-x' })
    // Reload config from server to revert UI to actual state
    load()
  } finally {
    configSaving.value = false
  }
}

function copyJobId(id: string) {
  navigator.clipboard.writeText(id)
  toast.add({ title: 'ID copied', color: 'success', icon: 'i-lucide-check' })
}

async function deleteJob(id: string) {
  try {
    await adminApi.deleteHLSJob(id)
    toast.add({ title: 'Job deleted', color: 'success', icon: 'i-lucide-check' })
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function cleanInactive() {
  try {
    await adminApi.cleanHLSInactive()
    toast.add({ title: 'Cleaned inactive HLS jobs', color: 'success', icon: 'i-lucide-check' })
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}


function statusColor(status: HLSJob['status']): 'neutral' | 'info' | 'success' | 'error' | 'warning' {
  const map = { pending: 'neutral', running: 'info', completed: 'success', failed: 'error', canceled: 'warning' } as const
  return map[status] ?? 'neutral'
}

// Pagination
const jobsPerPage = 20
const jobsPage = ref(1)
const pagedJobs = computed(() => {
  const start = (jobsPage.value - 1) * jobsPerPage
  return jobs.value.slice(start, start + jobsPerPage)
})
const jobsTotalPages = computed(() => Math.ceil(jobs.value.length / jobsPerPage))

onMounted(load)
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
    <div class="flex gap-2">
      <UButton icon="i-lucide-refresh-cw" label="Refresh" variant="outline" color="neutral" @click="load" />
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
            @click="copyJobId(row.original.id ?? '')"
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
          <UButton
            icon="i-lucide-trash-2"
            :aria-label="`Delete HLS job ${row.original.id}`"
            size="xs"
            variant="ghost"
            color="error"
            :disabled="row.original.status === 'running'"
            @click="deleteJob(row.original.id)"
          />
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
  </div>
</template>

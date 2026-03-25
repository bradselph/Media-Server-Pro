<script setup lang="ts">
import type { HLSJob, HLSStats } from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

const jobs = ref<HLSJob[]>([])
const stats = ref<HLSStats | null>(null)
const loading = ref(true)

async function load() {
  loading.value = true
  try {
    const [j, s] = await Promise.allSettled([adminApi.listHLSJobs(), adminApi.getHLSStats()])
    if (j.status === 'fulfilled') jobs.value = j.value ?? []
    if (s.status === 'fulfilled') stats.value = s.value
  } finally {
    loading.value = false
  }
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

function formatBytes(bytes?: number): string {
  if (!bytes) return '—'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / k ** i).toFixed(1)} ${sizes[i]}`
}

function statusColor(status: HLSJob['status']): 'neutral' | 'info' | 'success' | 'error' | 'warning' {
  const map = { pending: 'neutral', running: 'info', completed: 'success', failed: 'error', cancelled: 'warning' } as const
  return map[status] ?? 'neutral'
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
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
        :data="jobs"
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
          <span class="font-mono text-xs" :title="row.original.id">{{ row.original.id?.slice(0, 12) }}…</span>
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
    </UCard>
  </div>
</template>

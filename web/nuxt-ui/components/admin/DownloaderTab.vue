<script setup lang="ts">
import type { DownloaderJob } from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

const jobs = ref<DownloaderJob[]>([])
const loading = ref(true)
const newUrl = ref('')
const newFilename = ref('')
const adding = ref(false)

async function load() {
  loading.value = true
  try { jobs.value = (await adminApi.listDownloaderJobs()) ?? [] }
  catch {}
  finally { loading.value = false }
}

async function addJob() {
  if (!newUrl.value) return
  adding.value = true
  try {
    await adminApi.createDownloaderJob(newUrl.value, newFilename.value || undefined)
    toast.add({ title: 'Download started', color: 'success', icon: 'i-lucide-check' })
    newUrl.value = ''; newFilename.value = ''
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    adding.value = false
  }
}

async function cancelJob(id: string) {
  try {
    await adminApi.cancelDownloaderJob(id)
    toast.add({ title: 'Cancelled', color: 'warning', icon: 'i-lucide-x' })
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function deleteJob(id: string) {
  try {
    await adminApi.deleteDownloaderJob(id)
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

function formatBytes(bytes?: number): string {
  if (!bytes) return '—'
  const k = 1024; const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / k ** i).toFixed(1)} ${sizes[i]}`
}

function statusColor(status: DownloaderJob['status']) {
  return { pending: 'neutral', downloading: 'info', completed: 'success', failed: 'error', cancelled: 'warning' }[status] ?? 'neutral'
}

onMounted(load)

// Auto-refresh while any job is downloading
const interval = setInterval(() => {
  if (jobs.value.some(j => j.status === 'downloading')) load()
}, 5000)
onUnmounted(() => clearInterval(interval))
</script>

<template>
  <div class="space-y-4">
    <!-- Add new download -->
    <UCard>
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-cloud-download" class="size-4" />
          New Download
        </div>
      </template>
      <div class="flex flex-wrap gap-2">
        <UInput v-model="newUrl" placeholder="URL to download…" class="flex-1 min-w-64" />
        <UInput v-model="newFilename" placeholder="Filename (optional)" class="w-48" />
        <UButton :loading="adding" icon="i-lucide-plus" label="Add" @click="addJob" />
      </div>
    </UCard>

    <div class="flex gap-2 justify-end">
      <UButton icon="i-lucide-refresh-cw" variant="ghost" color="neutral" @click="load" />
    </div>

    <!-- Jobs table -->
    <UCard>
      <div v-if="loading" class="flex justify-center py-8">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-6" />
      </div>
      <UTable
        v-else
        :data="jobs"
        :columns="[
          { key: 'filename', label: 'File / URL' },
          { key: 'status', label: 'Status' },
          { key: 'progress', label: 'Progress' },
          { key: 'size', label: 'Size' },
          { key: 'created_at', label: 'Created' },
          { key: 'actions', label: '' },
        ]"
      >
        <template #filename-cell="{ row }">
          <div class="max-w-xs">
            <p class="text-sm font-medium truncate">{{ row.original.filename || '—' }}</p>
            <p class="text-xs text-(--ui-text-muted) truncate" :title="row.original.url">{{ row.original.url }}</p>
          </div>
        </template>
        <template #status-cell="{ row }">
          <UBadge :label="row.original.status" :color="statusColor(row.original.status)" variant="subtle" size="xs" />
        </template>
        <template #progress-cell="{ row }">
          <div v-if="row.original.status === 'downloading'" class="flex items-center gap-2 min-w-24">
            <UProgress :value="row.original.progress ?? 0" size="xs" class="flex-1" />
            <span class="text-xs">{{ Math.round(row.original.progress ?? 0) }}%</span>
          </div>
          <span v-else class="text-sm text-(--ui-text-muted)">—</span>
        </template>
        <template #size-cell="{ row }">
          <span class="text-sm">
            {{ row.original.downloaded ? `${formatBytes(row.original.downloaded)} / ` : '' }}{{ formatBytes(row.original.size) }}
          </span>
        </template>
        <template #created_at-cell="{ row }">
          <span class="text-sm text-(--ui-text-muted)">{{ new Date(row.original.created_at).toLocaleString() }}</span>
        </template>
        <template #actions-cell="{ row }">
          <div class="flex gap-1 justify-end">
            <UButton
              v-if="row.original.status === 'downloading' || row.original.status === 'pending'"
              icon="i-lucide-x"
              size="xs"
              variant="ghost"
              color="warning"
              title="Cancel"
              @click="cancelJob(row.original.id)"
            />
            <UButton
              v-if="row.original.status !== 'downloading'"
              icon="i-lucide-trash-2"
              size="xs"
              variant="ghost"
              color="error"
              @click="deleteJob(row.original.id)"
            />
          </div>
        </template>
      </UTable>
      <p v-if="!loading && jobs.length === 0" class="text-center py-6 text-(--ui-text-muted) text-sm">
        No downloads.
      </p>
    </UCard>
  </div>
</template>

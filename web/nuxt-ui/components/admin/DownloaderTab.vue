<script setup lang="ts">
import type { DownloaderJob } from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

const downloads = ref<DownloaderJob[]>([])
const loading = ref(true)
const newUrl = ref('')
const adding = ref(false)

async function load() {
  loading.value = true
  try { downloads.value = (await adminApi.listDownloaderJobs()) ?? [] }
  catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load downloads', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { loading.value = false }
}

async function addDownload() {
  if (!newUrl.value) return
  adding.value = true
  try {
    const clientId = `admin-${Date.now()}`
    await adminApi.createDownloaderJob({ url: newUrl.value, clientId })
    toast.add({ title: 'Download started', color: 'success', icon: 'i-lucide-check' })
    newUrl.value = ''
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    adding.value = false
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

function formatBytes(bytes?: number): string {
  if (!bytes) return '—'
  const k = 1024; const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / k ** i).toFixed(1)} ${sizes[i]}`
}

onMounted(load)
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
        <UButton :loading="adding" icon="i-lucide-plus" label="Add" @click="addDownload" />
      </div>
    </UCard>

    <div class="flex gap-2 justify-end">
      <UButton icon="i-lucide-refresh-cw" variant="ghost" color="neutral" @click="load" />
    </div>

    <!-- Downloads table -->
    <UCard>
      <div v-if="loading" class="flex justify-center py-8">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-6" />
      </div>
      <UTable
        v-else
        :data="downloads"
        :columns="[
          { key: 'filename', label: 'Filename' },
          { key: 'size', label: 'Size' },
          { key: 'created', label: 'Created' },
          { key: 'actions', label: '' },
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

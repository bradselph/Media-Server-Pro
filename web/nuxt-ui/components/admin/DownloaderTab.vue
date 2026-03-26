<script setup lang="ts">
import type { DownloaderJob, ImportableFile, DownloaderHealth } from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

// ── Health ────────────────────────────────────────────────────────────────────
const health = ref<DownloaderHealth | null>(null)

async function loadHealth() {
  try { health.value = await adminApi.getDownloaderHealth() }
  catch { health.value = null }
}

// ── Downloads list ────────────────────────────────────────────────────────────

const downloads = ref<DownloaderJob[]>([])
const loading = ref(true)
const newUrl = ref('')
const adding = ref(false)
// Track active download IDs so the user can cancel them
const activeDownloads = ref<{ id: string; url: string }[]>([])

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
    const result = await adminApi.createDownloaderJob({ url: newUrl.value, clientId })
    toast.add({ title: 'Download started', color: 'success', icon: 'i-lucide-check' })
    if (result?.downloadId) {
      activeDownloads.value = [...activeDownloads.value, { id: result.downloadId, url: newUrl.value }]
    }
    newUrl.value = ''
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    adding.value = false
  }
}

async function cancelDownload(id: string) {
  try {
    await adminApi.cancelDownloaderJob(id)
    activeDownloads.value = activeDownloads.value.filter(d => d.id !== id)
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

// ── Importable files ──────────────────────────────────────────────────────────

const importable = ref<ImportableFile[]>([])
const importableLoading = ref(false)
const importingFile = ref<string | null>(null)
const deleteSource = ref(true)
const triggerScan = ref(true)

async function loadImportable() {
  importableLoading.value = true
  try { importable.value = (await adminApi.listImportable()) ?? [] }
  catch { /* downloader may be offline; silently skip */ }
  finally { importableLoading.value = false }
}

async function importFile(filename: string) {
  importingFile.value = filename
  try {
    const result = await adminApi.importFile(filename, deleteSource.value, triggerScan.value)
    toast.add({ title: `Imported to ${result?.destination ?? 'library'}`, color: 'success', icon: 'i-lucide-check' })
    await Promise.allSettled([load(), loadImportable()])
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Import failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    importingFile.value = null
  }
}

function formatBytes(bytes?: number): string {
  if (!bytes) return '—'
  const k = 1024; const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / k ** i).toFixed(1)} ${sizes[i]}`
}

onMounted(() => {
  loadHealth()
  load()
  loadImportable()
})
</script>

<template>
  <div class="space-y-4">
    <!-- Downloader health -->
    <UCard v-if="health !== null" :ui="{ body: 'py-2 px-4' }">
      <div class="flex items-center gap-3 text-sm flex-wrap">
        <div class="flex items-center gap-1.5">
          <UIcon
            :name="health.online ? 'i-lucide-check-circle' : 'i-lucide-x-circle'"
            :class="health.online ? 'text-success' : 'text-error'"
            class="size-4"
          />
          <span class="font-medium">{{ health.online ? 'Online' : 'Offline' }}</span>
        </div>
        <span v-if="health.activeDownloads != null" class="text-muted">{{ health.activeDownloads }} active</span>
        <span v-if="health.queuedDownloads != null" class="text-muted">· {{ health.queuedDownloads }} queued</span>
        <span v-if="health.error" class="text-error text-xs">{{ health.error }}</span>
        <UButton icon="i-lucide-refresh-cw" size="xs" variant="ghost" color="neutral" class="ml-auto" @click="loadHealth" />
      </div>
    </UCard>

    <!-- Add new download -->
    <UCard>
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-cloud-download" class="size-4" />
          New Download
        </div>
      </template>
      <div class="flex flex-wrap gap-2">
        <UInput v-model="newUrl" placeholder="URL to download…" class="flex-1 min-w-64" @keyup.enter="addDownload" />
        <UButton :loading="adding" icon="i-lucide-plus" label="Add" @click="addDownload" />
      </div>
    </UCard>

    <!-- Active downloads (cancellable) -->
    <UCard v-if="activeDownloads.length > 0">
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-loader-2" class="size-4 animate-spin text-primary" />
          Active Downloads
          <UBadge :label="String(activeDownloads.length)" color="info" variant="subtle" size="xs" />
        </div>
      </template>
      <div class="divide-y divide-default">
        <div v-for="dl in activeDownloads" :key="dl.id" class="flex items-center justify-between py-2 gap-3">
          <p class="text-sm truncate text-muted min-w-0 flex-1" :title="dl.url">{{ dl.url }}</p>
          <UButton icon="i-lucide-x" label="Cancel" size="xs" variant="ghost" color="error" @click="cancelDownload(dl.id)" />
        </div>
      </div>
    </UCard>

    <!-- Importable files -->
    <UCard>
      <template #header>
        <div class="flex items-center justify-between">
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-package-check" class="size-4" />
            Ready to Import
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
      <div v-if="importableLoading" class="flex justify-center py-4">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
      </div>
      <div v-else-if="importable.length === 0" class="text-center py-4 text-muted text-sm">
        No files ready to import.
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

    <!-- Downloaded files list -->
    <UCard>
      <template #header>
        <div class="flex items-center justify-between">
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-folder-open" class="size-4" />
            Downloaded Files
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

<script setup lang="ts">
import type { RemoteSourceState, RemoteStats, RemoteMediaItem } from '~/types/api'
import { formatBytes } from '~/utils/format'

const adminApi = useAdminApi()
const mediaApi = useMediaApi()
const toast = useToast()

let destroyed = false
onUnmounted(() => { destroyed = true })

const remoteStats = ref<RemoteStats | null>(null)
const remoteSources = ref<RemoteSourceState[]>([])
const remoteLoading = ref(false)
const newRemoteName = ref('')
const newRemoteUrl = ref('')
const newRemoteUser = ref('')
const newRemotePass = ref('')
const addingRemote = ref(false)

const remoteMedia = ref<RemoteMediaItem[]>([])
const remoteMediaLoading = ref(false)
const remoteMediaSource = ref<string | null>(null)
const showRemoteMedia = ref(false)

function statusColor(status: string): 'success' | 'warning' | 'error' | 'neutral' {
  if (status === 'online' || status === 'active' || status === 'completed') return 'success'
  if (status === 'pending' || status === 'crawling') return 'warning'
  if (status === 'error' || status === 'offline') return 'error'
  return 'neutral'
}

async function loadRemote() {
  remoteLoading.value = true
  try {
    const [stats, sources] = await Promise.all([
      adminApi.getRemoteStats(),
      adminApi.getRemoteSources(),
    ])
    if (destroyed) return
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
    if (destroyed) return
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

async function loadAllRemoteMedia() {
  remoteMediaLoading.value = true
  showRemoteMedia.value = true
  remoteMediaSource.value = null
  try {
    const media = await adminApi.getRemoteMedia()
    if (destroyed) return
    remoteMedia.value = media ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load remote media', color: 'error', icon: 'i-lucide-x' })
  } finally { remoteMediaLoading.value = false }
}

async function loadSourceMedia(name: string) {
  remoteMediaLoading.value = true
  showRemoteMedia.value = true
  remoteMediaSource.value = name
  try {
    const media = await adminApi.getRemoteSourceMedia(name)
    if (destroyed) return
    remoteMedia.value = media ?? []
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

onMounted(loadRemote)
</script>

<template>
  <div class="space-y-4">
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
  </div>
</template>

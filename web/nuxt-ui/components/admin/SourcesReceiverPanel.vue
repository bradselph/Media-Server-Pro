<script setup lang="ts">
import type { SlaveNode, ReceiverStats, ReceiverDuplicate, ReceiverMedia } from '~/types/api'
import { formatBytes } from '~/utils/format'

const adminApi = useAdminApi()
const toast = useToast()

const receiverStats = ref<ReceiverStats | null>(null)
const slaves = ref<SlaveNode[]>([])
const duplicates = ref<ReceiverDuplicate[]>([])
const slaveMedia = ref<ReceiverMedia[]>([])
const receiverLoading = ref(false)
const slaveMediaLoading = ref(false)
const showSlaveMedia = ref(false)
const selectedSlaveMedia = ref<ReceiverMedia | null>(null)
const slaveMediaDetailLoading = ref(false)

function statusColor(status: string): 'success' | 'warning' | 'error' | 'neutral' {
  if (status === 'online' || status === 'active' || status === 'completed') return 'success'
  if (status === 'pending' || status === 'crawling') return 'warning'
  if (status === 'error' || status === 'offline') return 'error'
  return 'neutral'
}

async function openSlaveMediaDetail(id: string) {
  slaveMediaDetailLoading.value = true
  selectedSlaveMedia.value = null
  try {
    selectedSlaveMedia.value = await adminApi.getSlaveMediaItem(id)
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load media detail', color: 'error', icon: 'i-lucide-x' })
  } finally { slaveMediaDetailLoading.value = false }
}

async function loadReceiver() {
  receiverLoading.value = true
  try {
    const [stats, slaveList, dups] = await Promise.all([
      adminApi.getReceiverStats(),
      adminApi.listSlaves(),
      adminApi.listDuplicates('pending'),
    ])
    receiverStats.value = stats
    slaves.value = slaveList ?? []
    duplicates.value = dups ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load receiver', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { receiverLoading.value = false }
}

async function loadSlaveMedia() {
  slaveMediaLoading.value = true
  try {
    slaveMedia.value = (await adminApi.getSlaveMedia()) ?? []
    showSlaveMedia.value = true
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load slave media', color: 'error', icon: 'i-lucide-x' })
  } finally { slaveMediaLoading.value = false }
}

async function removeSlave(id: string) {
  try {
    await adminApi.removeReceiverSlave(id)
    toast.add({ title: 'Slave removed', color: 'success', icon: 'i-lucide-check' })
    await loadReceiver()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function resolveDuplicate(id: string, action: string) {
  try {
    await adminApi.resolveDuplicate(id, action)
    duplicates.value = duplicates.value.filter(d => d.id !== id)
    toast.add({ title: `Duplicate ${action}d`, color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

onMounted(loadReceiver)
</script>

<template>
  <div class="space-y-4">
    <!-- Stats -->
    <div v-if="receiverStats" class="grid grid-cols-2 sm:grid-cols-4 gap-3">
      <UCard>
        <p class="text-2xl font-bold">{{ receiverStats.slave_count }}</p>
        <p class="text-xs text-muted mt-1">Slaves</p>
      </UCard>
      <UCard>
        <p class="text-2xl font-bold text-success">{{ receiverStats.online_slaves }}</p>
        <p class="text-xs text-muted mt-1">Online</p>
      </UCard>
      <UCard>
        <p class="text-2xl font-bold">{{ receiverStats.media_count }}</p>
        <p class="text-xs text-muted mt-1">Media</p>
      </UCard>
      <UCard>
        <p class="text-2xl font-bold text-warning">{{ receiverStats.duplicate_count }}</p>
        <p class="text-xs text-muted mt-1">Duplicates</p>
      </UCard>
    </div>

    <!-- Slaves list -->
    <UCard>
      <template #header>
        <div class="flex items-center justify-between">
          <span class="font-semibold">Slave Nodes ({{ slaves.length }})</span>
          <UButton icon="i-lucide-refresh-cw" aria-label="Refresh slaves" variant="ghost" color="neutral" size="xs" @click="loadReceiver" />
        </div>
      </template>
      <div v-if="receiverLoading" class="flex justify-center py-6">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
      </div>
      <div v-else-if="slaves.length === 0" class="text-center py-8 text-muted text-sm">
        <UIcon name="i-lucide-radio-tower" class="size-10 mx-auto mb-2 opacity-30" />
        <p>No slave nodes registered.</p>
        <p class="text-xs mt-1">Slaves register automatically when a receiver instance connects.</p>
      </div>
      <div v-else class="divide-y divide-default">
        <div v-for="slave in slaves" :key="slave.id" class="flex items-center gap-3 py-2 flex-wrap">
          <div class="flex-1 min-w-0">
            <p class="font-medium text-sm">{{ slave.name }}</p>
            <p class="text-xs text-muted truncate">{{ slave.base_url }}</p>
            <div class="flex items-center gap-2 mt-1">
              <UBadge :label="slave.status" :color="statusColor(slave.status)" variant="subtle" size="xs" />
              <span class="text-xs text-muted">{{ slave.media_count }} media</span>
              <span v-if="slave.last_seen" class="text-xs text-muted">· last seen {{ new Date(slave.last_seen).toLocaleString() }}</span>
            </div>
          </div>
          <UButton icon="i-lucide-trash-2" aria-label="Remove slave" size="xs" variant="ghost" color="error" @click="removeSlave(slave.id)" />
        </div>
      </div>
    </UCard>

    <!-- Slave media browser -->
    <div class="flex justify-end">
      <UButton
        icon="i-lucide-database"
        :label="showSlaveMedia ? 'Hide Media' : 'Browse Slave Media'"
        size="sm"
        variant="outline"
        color="neutral"
        :loading="slaveMediaLoading"
        @click="showSlaveMedia ? showSlaveMedia = false : loadSlaveMedia()"
      />
    </div>
    <UCard v-if="showSlaveMedia">
      <template #header>
        <div class="flex items-center justify-between">
          <span class="font-semibold">Slave Media ({{ slaveMedia.length }})</span>
          <UButton icon="i-lucide-refresh-cw" aria-label="Refresh slave media" variant="ghost" color="neutral" size="xs" @click="loadSlaveMedia" />
        </div>
      </template>
      <div v-if="slaveMediaLoading" class="flex justify-center py-4">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
      </div>
      <div v-else-if="slaveMedia.length === 0" class="text-center py-4 text-muted text-sm">No media from slave nodes.</div>
      <div v-else class="divide-y divide-default max-h-64 overflow-y-auto">
        <button
          v-for="m in slaveMedia"
          :key="m.id"
          class="flex items-center gap-3 py-2 w-full text-left hover:bg-muted/40 transition-colors px-1 rounded"
          @click="openSlaveMediaDetail(m.id)"
        >
          <div class="flex-1 min-w-0">
            <p class="text-sm font-medium truncate">{{ m.name }}</p>
            <p class="text-xs text-muted">{{ m.type }} · {{ formatBytes(m.size) }}</p>
          </div>
          <span class="text-xs text-muted font-mono shrink-0">{{ m.slave_id.slice(0, 8) }}…</span>
        </button>
      </div>
    </UCard>

    <!-- Duplicates -->
    <UCard v-if="duplicates.length > 0">
      <template #header><span class="font-semibold">Pending Duplicates ({{ duplicates.length }})</span></template>
      <div class="divide-y divide-default">
        <div v-for="d in duplicates" :key="d.id" class="flex items-center gap-3 py-2 flex-wrap">
          <div class="flex-1 min-w-0">
            <p class="text-sm font-medium">{{ d.item_a_name }}</p>
            <p class="text-xs text-muted">vs. {{ d.item_b_name }}</p>
          </div>
          <div class="flex gap-1">
            <UButton label="Keep A" size="xs" variant="outline" color="success" @click="resolveDuplicate(d.id, 'keep_a')" />
            <UButton label="Keep B" size="xs" variant="outline" color="neutral" @click="resolveDuplicate(d.id, 'keep_b')" />
            <UButton label="Keep Both" size="xs" variant="ghost" color="neutral" @click="resolveDuplicate(d.id, 'keep_both')" />
          </div>
        </div>
      </div>
    </UCard>

    <!-- Slave media detail modal -->
    <UModal
      v-if="selectedSlaveMedia || slaveMediaDetailLoading"
      :open="!!(selectedSlaveMedia || slaveMediaDetailLoading)"
      :title="selectedSlaveMedia ? selectedSlaveMedia.name : 'Loading…'"
      @update:open="val => { if (!val) selectedSlaveMedia = null }"
    >
      <template #body>
        <div v-if="slaveMediaDetailLoading" class="flex justify-center py-4">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <div v-else-if="selectedSlaveMedia" class="space-y-2 text-sm">
          <div class="grid grid-cols-2 gap-2">
            <div><span class="text-muted">Type:</span> {{ selectedSlaveMedia.type }}</div>
            <div><span class="text-muted">Size:</span> {{ formatBytes(selectedSlaveMedia.size) }}</div>
            <div v-if="selectedSlaveMedia.duration"><span class="text-muted">Duration:</span> {{ selectedSlaveMedia.duration }}s</div>
            <div><span class="text-muted">Slave ID:</span> <span class="font-mono text-xs">{{ selectedSlaveMedia.slave_id }}</span></div>
            <div v-if="selectedSlaveMedia.created_at"><span class="text-muted">Added:</span> {{ new Date(selectedSlaveMedia.created_at).toLocaleString() }}</div>
          </div>
          <div>
            <span class="text-muted">Path:</span>
            <p class="font-mono text-xs mt-1 bg-muted rounded px-2 py-1 break-all">{{ selectedSlaveMedia.path }}</p>
          </div>
          <div v-if="selectedSlaveMedia.fingerprint">
            <span class="text-muted">Fingerprint:</span>
            <p class="font-mono text-xs mt-1 bg-muted rounded px-2 py-1 break-all">{{ selectedSlaveMedia.fingerprint }}</p>
          </div>
        </div>
      </template>
      <template #footer>
        <UButton variant="ghost" color="neutral" label="Close" @click="selectedSlaveMedia = null" />
      </template>
    </UModal>
  </div>
</template>

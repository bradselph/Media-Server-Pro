<script setup lang="ts">
import type {
  SlaveNode,
  ReceiverStats,
  ReceiverAdminSettings,
  ReceiverDuplicate,
  ReceiverMedia,
  FollowerSettings,
  FollowerStatus,
} from '~/types/api'
import { formatBytes } from '~/utils/format'

const adminApi = useAdminApi()
const toast = useToast()

const receiverStats = ref<ReceiverStats | null>(null)
const receiverSettings = ref<ReceiverAdminSettings | null>(null)
const slaves = ref<SlaveNode[]>([])
const duplicates = ref<ReceiverDuplicate[]>([])
const slaveMedia = ref<ReceiverMedia[]>([])
const receiverLoading = ref(false)
const slaveMediaLoading = ref(false)
const showSlaveMedia = ref(false)
const selectedSlaveMedia = ref<ReceiverMedia | null>(null)
const slaveMediaDetailLoading = ref(false)
const activeDetailRequestId = ref(0)
const revealedKeys = ref<Set<number>>(new Set())

// Follower (this-server-as-slave) state. Loaded alongside the receiver data
// so admins see both directions (incoming slaves + this server's outbound
// pairing) on a single page.
const followerSettings = ref<FollowerSettings | null>(null)
const followerStatus = ref<FollowerStatus | null>(null)
const followerLoading = ref(false)
const followerSaving = ref(false)
const followerTesting = ref(false)
const followerForm = reactive({
  master_url: '',
  api_key: '',
  slave_id: '',
  slave_name: '',
})

function toggleKeyReveal(idx: number) {
  const next = new Set(revealedKeys.value)
  if (next.has(idx)) next.delete(idx); else next.add(idx)
  revealedKeys.value = next
}

async function copyKey(key: string) {
  try {
    await navigator.clipboard.writeText(key)
    toast.add({ title: 'API key copied', color: 'success', icon: 'i-lucide-check' })
  } catch {
    toast.add({ title: 'Copy failed — select manually', color: 'warning', icon: 'i-lucide-alert-triangle' })
  }
}

function maskKey(key: string): string {
  if (!key) return ''
  if (key.length <= 8) return '•'.repeat(key.length)
  return key.slice(0, 4) + '•'.repeat(Math.max(4, key.length - 8)) + key.slice(-4)
}

let destroyed = false
let followerStatusTimer: ReturnType<typeof setInterval> | null = null
onUnmounted(() => {
  destroyed = true
  if (followerStatusTimer) clearInterval(followerStatusTimer)
})

function statusColor(status: string): 'success' | 'warning' | 'error' | 'neutral' {
  if (status === 'online' || status === 'active' || status === 'completed') return 'success'
  if (status === 'pending' || status === 'crawling') return 'warning'
  if (status === 'error' || status === 'offline') return 'error'
  return 'neutral'
}

async function openSlaveMediaDetail(id: string) {
  const myId = ++activeDetailRequestId.value
  slaveMediaDetailLoading.value = true
  selectedSlaveMedia.value = null
  try {
    const result = await adminApi.getSlaveMediaItem(id)
    if (!destroyed && activeDetailRequestId.value === myId) {
      selectedSlaveMedia.value = result
    }
  } catch (e: unknown) {
    if (!destroyed && activeDetailRequestId.value === myId) {
      toast.add({ title: e instanceof Error ? e.message : 'Failed to load media detail', color: 'error', icon: 'i-lucide-x' })
    }
  } finally {
    if (!destroyed && activeDetailRequestId.value === myId) {
      slaveMediaDetailLoading.value = false
    }
  }
}

async function loadReceiver() {
  receiverLoading.value = true
  try {
    const [stats, settings, slaveList, dups] = await Promise.all([
      adminApi.getReceiverStats(),
      adminApi.getReceiverSettings(),
      adminApi.listSlaves(),
      adminApi.listDuplicates('pending'),
    ])
    if (!destroyed) {
      receiverStats.value = stats
      receiverSettings.value = settings
      slaves.value = slaveList ?? []
      duplicates.value = dups ?? []
    }
  } catch (e: unknown) {
    if (!destroyed) {
      toast.add({ title: e instanceof Error ? e.message : 'Failed to load receiver', color: 'error', icon: 'i-lucide-alert-circle' })
    }
  } finally {
    if (!destroyed) receiverLoading.value = false
  }
}

async function loadSlaveMedia() {
  if (slaveMediaLoading.value) return
  slaveMediaLoading.value = true
  try {
    const media = (await adminApi.getSlaveMedia()) ?? []
    if (!destroyed) {
      slaveMedia.value = media
      showSlaveMedia.value = true
    }
  } catch (e: unknown) {
    if (!destroyed) {
      toast.add({ title: e instanceof Error ? e.message : 'Failed to load slave media', color: 'error', icon: 'i-lucide-x' })
    }
  } finally {
    if (!destroyed) slaveMediaLoading.value = false
  }
}

async function removeSlave(id: string) {
  try {
    await adminApi.removeReceiverSlave(id)
    if (!destroyed) {
      toast.add({ title: 'Slave removed', color: 'success', icon: 'i-lucide-check' })
      await loadReceiver()
    }
  } catch (e: unknown) {
    if (!destroyed) {
      toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
    }
  }
}

async function resolveDuplicate(id: string, action: string) {
  try {
    await adminApi.resolveDuplicate(id, action)
    if (!destroyed) {
      duplicates.value = duplicates.value.filter(d => d.id !== id)
      toast.add({ title: `Duplicate ${action}d`, color: 'success', icon: 'i-lucide-check' })
    }
  } catch (e: unknown) {
    if (!destroyed) {
      toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
    }
  }
}

async function loadFollower() {
  followerLoading.value = true
  try {
    const [settings, status] = await Promise.all([
      adminApi.getFollowerSettings(),
      adminApi.getFollowerStatus(),
    ])
    if (destroyed) return
    followerSettings.value = settings
    followerStatus.value = status
    followerForm.master_url = settings.master_url ?? ''
    followerForm.slave_id = settings.slave_id ?? ''
    followerForm.slave_name = settings.slave_name ?? ''
    // Never pre-populate the api key field — backend redacts it. Empty input
    // on save means "keep existing key", which is what admins usually want.
    followerForm.api_key = ''
  } catch (e: unknown) {
    if (!destroyed) {
      toast.add({ title: e instanceof Error ? e.message : 'Failed to load follower', color: 'error', icon: 'i-lucide-alert-circle' })
    }
  } finally {
    if (!destroyed) followerLoading.value = false
  }
}

async function refreshFollowerStatus() {
  try {
    const status = await adminApi.getFollowerStatus()
    if (!destroyed) followerStatus.value = status
  } catch {
    // Silent — status polling, don't toast on every transient failure.
  }
}

async function saveFollower() {
  if (followerSaving.value) return
  followerSaving.value = true
  try {
    // Backend auto-enables when master_url + api_key are populated, so no
    // explicit enabled field is sent — pairing turns on as soon as the
    // form is filled in.
    const result = await adminApi.updateFollowerSettings({
      enabled: true,
      master_url: followerForm.master_url.trim(),
      api_key: followerForm.api_key.trim() || undefined,
      slave_id: followerForm.slave_id.trim() || undefined,
      slave_name: followerForm.slave_name.trim() || undefined,
    })
    if (destroyed) return
    if (result.reload_status) followerStatus.value = result.reload_status
    if (result.reload_error) {
      toast.add({ title: `Saved, but reload failed: ${result.reload_error}`, color: 'warning', icon: 'i-lucide-alert-triangle' })
    } else {
      toast.add({ title: 'Follower settings saved', color: 'success', icon: 'i-lucide-check' })
    }
    // Re-fetch settings so api_key_configured reflects the new state.
    await loadFollower()
  } catch (e: unknown) {
    if (!destroyed) {
      toast.add({ title: e instanceof Error ? e.message : 'Failed to save', color: 'error', icon: 'i-lucide-x' })
    }
  } finally {
    if (!destroyed) followerSaving.value = false
  }
}

async function testFollower() {
  if (followerTesting.value) return
  if (!followerForm.master_url.trim() || !followerForm.api_key.trim()) {
    toast.add({ title: 'Enter master URL and API key first', color: 'warning', icon: 'i-lucide-alert-triangle' })
    return
  }
  followerTesting.value = true
  try {
    const result = await adminApi.testFollowerPairing(followerForm.master_url.trim(), followerForm.api_key.trim())
    if (destroyed) return
    if (result.ok) {
      toast.add({ title: 'Connection successful', color: 'success', icon: 'i-lucide-check' })
    } else {
      const detail = result.http_status ? ` (HTTP ${result.http_status})` : ''
      toast.add({ title: `Connection failed: ${result.error ?? 'unknown'}${detail}`, color: 'error', icon: 'i-lucide-x' })
    }
  } catch (e: unknown) {
    if (!destroyed) {
      toast.add({ title: e instanceof Error ? e.message : 'Test failed', color: 'error', icon: 'i-lucide-x' })
    }
  } finally {
    if (!destroyed) followerTesting.value = false
  }
}

onMounted(async () => {
  await Promise.all([loadReceiver(), loadFollower()])
  // Poll follower status every 10s so the admin sees connect/disconnect
  // transitions without manually refreshing.
  followerStatusTimer = setInterval(refreshFollowerStatus, 10000)
})
</script>

<template>
  <div class="space-y-4">
    <!-- This Server (follower / outbound pairing) -->
    <UCard>
      <template #header>
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-2">
            <UIcon name="i-lucide-link-2" class="size-4" />
            <span class="font-semibold">This Server &rarr; Another Master</span>
          </div>
          <UBadge
            v-if="followerStatus"
            :label="followerStatus.connected ? 'connected' : (followerStatus.enabled ? 'idle' : 'disabled')"
            :color="followerStatus.connected ? 'success' : (followerStatus.enabled ? 'warning' : 'neutral')"
            variant="subtle"
            size="xs"
          />
        </div>
      </template>
      <p class="text-xs text-muted mb-3">
        Push this server's media library to another Media Server Pro instance as a slave.
        No separate receiver binary needed — paste the other server's URL and a Receiver API key,
        and the two libraries sync automatically.
      </p>
      <div v-if="followerLoading" class="flex justify-center py-4">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
      </div>
      <div v-else class="space-y-3">
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <UFormField label="Master URL" hint="https://other-vps.example.com">
            <UInput
              v-model="followerForm.master_url"
              placeholder="https://other-vps.example.com"
              autocomplete="off"
            />
          </UFormField>
          <UFormField label="Receiver API Key" :hint="followerSettings?.api_key_configured ? 'Leave blank to keep existing key' : 'From the other server\'s admin → Receiver settings'">
            <UInput
              v-model="followerForm.api_key"
              type="password"
              :placeholder="followerSettings?.api_key_configured ? '••••••••' : 'paste API key'"
              autocomplete="new-password"
            />
          </UFormField>
          <UFormField label="Slave ID" hint="Leave blank to use hostname">
            <UInput v-model="followerForm.slave_id" placeholder="auto" />
          </UFormField>
          <UFormField label="Display Name" hint="Shown in the master's slave list">
            <UInput v-model="followerForm.slave_name" placeholder="auto" />
          </UFormField>
        </div>
        <p class="text-xs text-muted">
          Pairing auto-enables once both Master URL and Receiver API Key are saved.
          Clear either field to pause it.
        </p>
        <div class="flex items-center gap-3 flex-wrap">
          <UButton
            label="Test Connection"
            icon="i-lucide-zap"
            size="sm"
            variant="outline"
            color="neutral"
            :loading="followerTesting"
            @click="testFollower"
          />
          <UButton
            label="Save"
            icon="i-lucide-save"
            size="sm"
            color="primary"
            :loading="followerSaving"
            @click="saveFollower"
          />
          <UButton
            icon="i-lucide-refresh-cw"
            aria-label="Refresh status"
            size="sm"
            variant="ghost"
            color="neutral"
            @click="refreshFollowerStatus"
          />
        </div>
        <!-- Live status detail -->
        <div v-if="followerStatus" class="text-xs text-muted bg-muted/40 rounded px-3 py-2 space-y-1">
          <div v-if="followerStatus.last_connected_at">
            Last connected: {{ new Date(followerStatus.last_connected_at).toLocaleString() }}
          </div>
          <div v-if="followerStatus.last_catalog_push">
            Last catalog push: {{ new Date(followerStatus.last_catalog_push).toLocaleString() }}
            <span v-if="followerStatus.last_catalog_size != null">
              ({{ followerStatus.last_catalog_size }} items)
            </span>
          </div>
          <div v-if="followerStatus.last_error" class="text-error">
            Last error: {{ followerStatus.last_error }}
            <span v-if="followerStatus.last_error_at">
              ({{ new Date(followerStatus.last_error_at).toLocaleString() }})
            </span>
          </div>
          <div v-if="!followerStatus.last_connected_at && !followerStatus.last_error" class="text-muted">
            Not paired yet — fill in the form above and click Save to connect.
          </div>
        </div>
      </div>
    </UCard>

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

    <!-- Receiver API keys (for pairing other VPSes as slaves to this server) -->
    <UCard v-if="receiverSettings">
      <template #header>
        <div class="flex items-center gap-2">
          <UIcon name="i-lucide-key-round" class="size-4" />
          <span class="font-semibold">Receiver API Keys</span>
        </div>
      </template>
      <p class="text-xs text-muted mb-3">
        Copy a key into the Master URL + API Key form on another VPS so it pairs as a
        slave to this server. Keys are configured via <code>RECEIVER_API_KEYS</code>
        env var or the <code>receiver.api_keys</code> config field.
      </p>
      <div v-if="receiverSettings.api_keys.length === 0" class="text-sm text-muted text-center py-3">
        No API keys configured. Set <code>RECEIVER_API_KEYS=key1,key2</code> on this server and restart.
      </div>
      <div v-else class="space-y-2">
        <div
          v-for="(key, idx) in receiverSettings.api_keys"
          :key="idx"
          class="flex items-center gap-2 bg-muted/40 rounded px-3 py-2"
        >
          <code class="flex-1 text-xs font-mono break-all">
            {{ revealedKeys.has(idx) ? key : maskKey(key) }}
          </code>
          <UButton
            :icon="revealedKeys.has(idx) ? 'i-lucide-eye-off' : 'i-lucide-eye'"
            :aria-label="revealedKeys.has(idx) ? 'Hide key' : 'Show key'"
            size="xs"
            variant="ghost"
            color="neutral"
            @click="toggleKeyReveal(idx)"
          />
          <UButton
            icon="i-lucide-copy"
            aria-label="Copy key"
            size="xs"
            variant="ghost"
            color="neutral"
            @click="copyKey(key)"
          />
        </div>
      </div>
    </UCard>

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
            <p class="text-xs text-muted">{{ m.media_type }} · {{ formatBytes(m.size) }}</p>
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
      @update:open="val => { if (!val) { selectedSlaveMedia = null; slaveMediaDetailLoading = false } }"
    >
      <template #body>
        <div v-if="slaveMediaDetailLoading" class="flex justify-center py-4">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <div v-else-if="selectedSlaveMedia" class="space-y-2 text-sm">
          <div class="grid grid-cols-2 gap-2">
            <div><span class="text-muted">Type:</span> {{ selectedSlaveMedia.media_type }}</div>
            <div><span class="text-muted">Size:</span> {{ formatBytes(selectedSlaveMedia.size) }}</div>
            <div v-if="selectedSlaveMedia.duration"><span class="text-muted">Duration:</span> {{ selectedSlaveMedia.duration }}s</div>
            <div><span class="text-muted">Slave ID:</span> <span class="font-mono text-xs">{{ selectedSlaveMedia.slave_id }}</span></div>
          </div>
          <div>
            <span class="text-muted">Path:</span>
            <p class="font-mono text-xs mt-1 bg-muted rounded px-2 py-1 break-all">{{ selectedSlaveMedia.path }}</p>
          </div>
          <div v-if="selectedSlaveMedia.content_fingerprint">
            <span class="text-muted">Fingerprint:</span>
            <p class="font-mono text-xs mt-1 bg-muted rounded px-2 py-1 break-all">{{ selectedSlaveMedia.content_fingerprint }}</p>
          </div>
        </div>
      </template>
      <template #footer>
        <UButton variant="ghost" color="neutral" label="Close" @click="selectedSlaveMedia = null" />
      </template>
    </UModal>
  </div>
</template>

<script setup lang="ts">
import type { AuditLogEntry, IPListEntry, BannedIP, SecurityStats } from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

const subTab = ref('audit')
const subTabs = [
  { label: 'Audit Log', value: 'audit', icon: 'i-lucide-scroll-text' },
  { label: 'IP Whitelist', value: 'whitelist', icon: 'i-lucide-shield-check' },
  { label: 'IP Blacklist', value: 'blacklist', icon: 'i-lucide-shield-ban' },
  { label: 'Banned IPs', value: 'banned', icon: 'i-lucide-shield-x' },
  { label: 'Stats', value: 'stats', icon: 'i-lucide-bar-chart' },
]

// Audit log
const auditEntries = ref<AuditLogEntry[]>([])
const auditTotal = ref(0)
const auditPage = ref(1)
const auditLimit = 20
const auditLoading = ref(false)

async function loadAudit() {
  auditLoading.value = true
  try {
    // Backend reads `offset` (not `page`); offset = (page - 1) * limit
    const offset = (auditPage.value - 1) * auditLimit
    const res = await adminApi.getAuditLog({ offset, limit: auditLimit })
    // API returns a plain array, not a paginated object
    auditEntries.value = Array.isArray(res) ? res : []
    const count = auditEntries.value.length
    auditTotal.value = count === auditLimit
      ? auditPage.value * auditLimit + 1
      : (auditPage.value - 1) * auditLimit + count
  } catch {}
  finally { auditLoading.value = false }
}

// IP lists
const whitelist = ref<IPListEntry[]>([])
const blacklist = ref<IPListEntry[]>([])
const banned = ref<BannedIP[]>([])
const newIP = ref('')
const newComment = ref('')
const ipLoading = ref(false)

async function loadWhitelist() { whitelist.value = (await adminApi.getWhitelist()) ?? [] }
async function loadBlacklist() { blacklist.value = (await adminApi.getBlacklist()) ?? [] }
async function loadBanned() { banned.value = (await adminApi.getBannedIPs()) ?? [] }

async function addToList(type: 'whitelist' | 'blacklist') {
  if (!newIP.value) return
  ipLoading.value = true
  try {
    if (type === 'whitelist') await adminApi.addToWhitelist(newIP.value, newComment.value || undefined)
    else await adminApi.addToBlacklist(newIP.value, newComment.value || undefined)
    toast.add({ title: `IP added to ${type}`, color: 'success', icon: 'i-lucide-check' })
    newIP.value = ''; newComment.value = ''
    if (type === 'whitelist') loadWhitelist(); else loadBlacklist()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    ipLoading.value = false
  }
}

async function removeFromList(type: 'whitelist' | 'blacklist', ip: string) {
  try {
    if (type === 'whitelist') await adminApi.removeFromWhitelist(ip)
    else await adminApi.removeFromBlacklist(ip)
    toast.add({ title: 'IP removed', color: 'success', icon: 'i-lucide-check' })
    if (type === 'whitelist') loadWhitelist(); else loadBlacklist()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function unban(ip: string) {
  try {
    await adminApi.unbanIP(ip)
    toast.add({ title: 'IP unbanned', color: 'success', icon: 'i-lucide-check' })
    loadBanned()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

// Stats
const stats = ref<SecurityStats | null>(null)

watch(subTab, (v) => {
  if (v === 'audit') loadAudit()
  else if (v === 'whitelist') loadWhitelist()
  else if (v === 'blacklist') loadBlacklist()
  else if (v === 'banned') loadBanned()
  else if (v === 'stats') adminApi.getSecurityStats().then(s => { stats.value = s }).catch(() => {})
}, { immediate: true })
</script>

<template>
  <div class="space-y-4">
    <UTabs v-model="subTab" :items="subTabs" size="sm" />

    <!-- Audit log -->
    <div v-if="subTab === 'audit'" class="space-y-3">
      <div class="flex gap-2 justify-end">
        <UButton icon="i-lucide-refresh-cw" variant="ghost" color="neutral" size="sm" @click="loadAudit" />
      </div>
      <UCard>
        <div v-if="auditLoading" class="flex justify-center py-6">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <UTable
          v-else
          :data="auditEntries"
          :columns="[
            { accessorKey: 'timestamp', header: 'Time' },
            { accessorKey: 'username', header: 'User' },
            { accessorKey: 'action', header: 'Action' },
            { accessorKey: 'resource', header: 'Resource' },
            { accessorKey: 'ip_address', header: 'IP' },
            { accessorKey: 'success', header: 'Result' },
          ]"
          class="text-sm"
        >
          <template #timestamp-cell="{ row }">
            <span class="text-xs font-mono">{{ new Date(row.original.timestamp).toLocaleString() }}</span>
          </template>
          <template #success-cell="{ row }">
            <UBadge
              :label="row.original.success ? 'OK' : 'Fail'"
              :color="row.original.success ? 'success' : 'error'"
              variant="subtle"
              size="xs"
            />
          </template>
          <template #resource-cell="{ row }">
            <span class="font-mono text-xs">{{ row.original.resource }}</span>
          </template>
        </UTable>
        <p v-if="!auditLoading && auditEntries.length === 0" class="text-center py-4 text-muted text-sm">
          No audit log entries.
        </p>
      </UCard>
      <div v-if="auditTotal > auditLimit" class="flex justify-center">
        <UPagination v-model:page="auditPage" :total="auditTotal" :items-per-page="auditLimit" @update:page="loadAudit" />
      </div>
    </div>

    <!-- IP List management template -->
    <template v-if="subTab === 'whitelist' || subTab === 'blacklist'">
      <div class="space-y-4">
        <!-- Add IP -->
        <UCard>
          <template #header>
            <div class="font-semibold">Add IP to {{ subTab === 'whitelist' ? 'Whitelist' : 'Blacklist' }}</div>
          </template>
          <div class="flex flex-wrap gap-2">
            <UInput v-model="newIP" placeholder="IP address or CIDR" class="w-48" />
            <UInput v-model="newComment" placeholder="Comment (optional)" class="flex-1 min-w-40" />
            <UButton :loading="ipLoading" icon="i-lucide-plus" label="Add" @click="addToList(subTab as 'whitelist' | 'blacklist')" />
          </div>
        </UCard>

        <!-- List -->
        <UCard>
          <UTable
            :data="subTab === 'whitelist' ? whitelist : blacklist"
            :columns="[{ accessorKey: 'ip', header: 'IP' }, { accessorKey: 'comment', header: 'Comment' }, { accessorKey: 'added_at', header: 'Added' }, { accessorKey: 'actions', header: '' }]"
          >
            <template #ip-cell="{ row }"><span class="font-mono text-sm">{{ row.original.ip }}</span></template>
            <template #comment-cell="{ row }"><span class="text-sm text-muted">{{ row.original.comment || '—' }}</span></template>
            <template #added_at-cell="{ row }"><span class="text-sm">{{ new Date(row.original.added_at).toLocaleDateString() }}</span></template>
            <template #actions-cell="{ row }">
              <UButton
                icon="i-lucide-trash-2"
                size="xs"
                variant="ghost"
                color="error"
                @click="removeFromList(subTab as 'whitelist' | 'blacklist', row.original.ip)"
              />
            </template>
          </UTable>
          <p v-if="(subTab === 'whitelist' ? whitelist : blacklist).length === 0" class="text-center py-4 text-muted text-sm">
            No entries.
          </p>
        </UCard>
      </div>
    </template>

    <!-- Banned IPs -->
    <div v-if="subTab === 'banned'">
      <UCard>
        <UTable
          :data="banned"
          :columns="[{ accessorKey: 'ip', header: 'IP' }, { accessorKey: 'banned_at', header: 'Banned At' }, { accessorKey: 'reason', header: 'Reason' }, { accessorKey: 'actions', header: '' }]"
        >
          <template #ip-cell="{ row }"><span class="font-mono text-sm">{{ row.original.ip }}</span></template>
          <template #banned_at-cell="{ row }"><span class="text-sm">{{ new Date(row.original.banned_at).toLocaleString() }}</span></template>
          <template #reason-cell="{ row }"><span class="text-sm text-muted">{{ row.original.reason || '—' }}</span></template>
          <template #actions-cell="{ row }">
            <UButton icon="i-lucide-shield-off" size="xs" variant="ghost" color="warning" label="Unban" @click="unban(row.original.ip)" />
          </template>
        </UTable>
        <p v-if="banned.length === 0" class="text-center py-4 text-muted text-sm">No banned IPs.</p>
      </UCard>
    </div>

    <!-- Stats -->
    <div v-if="subTab === 'stats' && stats">
      <div class="grid grid-cols-2 sm:grid-cols-3 gap-3">
        <UCard v-for="item in [
          { label: 'Blocked Today', value: stats.total_blocks_today, icon: 'i-lucide-shield-x' },
          { label: 'Active Rate Limits', value: stats.active_rate_limits, icon: 'i-lucide-gauge' },
          { label: 'Banned IPs', value: stats.banned_ips, icon: 'i-lucide-ban' },
          { label: 'Whitelisted', value: stats.whitelisted_ips, icon: 'i-lucide-shield-check' },
          { label: 'Blacklisted', value: stats.blacklisted_ips, icon: 'i-lucide-shield-ban' },
        ]" :key="item.label" :ui="{ body: 'p-4' }">
          <div class="flex items-center gap-2">
            <UIcon :name="item.icon" class="size-4 text-muted" />
            <div>
              <p class="text-lg font-bold text-highlighted">{{ (item.value ?? 0).toLocaleString() }}</p>
              <p class="text-xs text-muted">{{ item.label }}</p>
            </div>
          </div>
        </UCard>
      </div>
    </div>
  </div>
</template>

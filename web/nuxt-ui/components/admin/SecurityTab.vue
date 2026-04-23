<script setup lang="ts">
import type { AuditLogEntry, IPListEntry, BannedIP, SecurityStats } from '~/types/api'
import { asRecord } from '~/utils/typeGuards'

const adminApi = useAdminApi()
const toast = useToast()

const subTab = ref('audit')
const subTabs = [
  { label: 'Audit Log', value: 'audit', icon: 'i-lucide-scroll-text' },
  { label: 'IP Whitelist', value: 'whitelist', icon: 'i-lucide-shield-check' },
  { label: 'IP Blacklist', value: 'blacklist', icon: 'i-lucide-shield-ban' },
  { label: 'Banned IPs', value: 'banned', icon: 'i-lucide-shield-x' },
  { label: 'Stats', value: 'stats', icon: 'i-lucide-bar-chart' },
  { label: 'Settings', value: 'settings', icon: 'i-lucide-settings' },
]

// Security config toggles
const fullConfig = ref<Record<string, unknown>>({})
const corsEnabled = ref(false)
const hstsEnabled = ref(false)
const httpsEnabled = ref(false)
const configSaving = ref(false)
const configLoading = ref(false)

async function loadSecurityConfig() {
  configLoading.value = true
  try {
    const cfg = await adminApi.getConfig()
    if (cfg) {
      fullConfig.value = cfg
      const sec = asRecord(cfg.security)
      const srv = asRecord(cfg.server)
      corsEnabled.value = sec?.cors_enabled === true
      hstsEnabled.value = sec?.hsts_enabled === true
      httpsEnabled.value = srv?.enable_https === true
    }
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load config', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally {
    configLoading.value = false
  }
}

async function saveSecurityToggle(
  section: 'security' | 'server',
  key: string,
  value: boolean,
) {
  configSaving.value = true
  try {
    const updated: Record<string, unknown> = { ...fullConfig.value }
    if (section === 'security') {
      updated.security = { ...asRecord(fullConfig.value.security), [key]: value }
    } else {
      updated.server = { ...asRecord(fullConfig.value.server), [key]: value }
    }
    await adminApi.updateConfig(updated)
    fullConfig.value = updated
    const sec = asRecord(updated.security)
    const srv = asRecord(updated.server)
    corsEnabled.value = sec?.cors_enabled === true
    hstsEnabled.value = sec?.hsts_enabled === true
    httpsEnabled.value = srv?.enable_https === true
    toast.add({ title: 'Security settings saved', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to save', color: 'error', icon: 'i-lucide-x' })
    // reload to revert UI state
    await loadSecurityConfig()
  } finally {
    configSaving.value = false
  }
}

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
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load audit log', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { auditLoading.value = false }
}

const exportingAudit = ref(false)
async function exportAuditLog() {
  exportingAudit.value = true
  try {
    const res = await fetch(adminApi.exportAuditLogUrl(), { credentials: 'include' })
    if (!res.ok) throw new Error(`Export failed: ${res.status}`)
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `audit-log-${new Date().toISOString().slice(0, 10)}.csv`
    a.click()
    URL.revokeObjectURL(url)
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Export failed', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { exportingAudit.value = false }
}

// IP lists
const whitelist = ref<IPListEntry[]>([])
const blacklist = ref<IPListEntry[]>([])
const banned = ref<BannedIP[]>([])
const newIP = ref('')
const newComment = ref('')
const ipLoading = ref(false)
const ipError = ref('')

// Basic IP/CIDR format validation (split into two regexes to stay under complexity limit)
const IP_V4_RE = /^(\d{1,3}\.){3}\d{1,3}(\/\d{1,2})?$/
const IP_V6_RE = /^[0-9a-fA-F:]+(\/\d{1,3})?$/
function isValidIPCIDR(ip: string): boolean {
  return IP_V4_RE.test(ip) || IP_V6_RE.test(ip)
}

async function loadWhitelist() {
  try { whitelist.value = (await adminApi.getWhitelist()) ?? [] }
  catch (e: unknown) { toast.add({ title: e instanceof Error ? e.message : 'Failed to load whitelist', color: 'error', icon: 'i-lucide-alert-circle' }) }
}
async function loadBlacklist() {
  try { blacklist.value = (await adminApi.getBlacklist()) ?? [] }
  catch (e: unknown) { toast.add({ title: e instanceof Error ? e.message : 'Failed to load blacklist', color: 'error', icon: 'i-lucide-alert-circle' }) }
}
async function loadBanned() {
  try { banned.value = (await adminApi.getBannedIPs()) ?? [] }
  catch (e: unknown) { toast.add({ title: e instanceof Error ? e.message : 'Failed to load banned IPs', color: 'error', icon: 'i-lucide-alert-circle' }) }
}

async function addToList(type: 'whitelist' | 'blacklist') {
  if (!newIP.value) return
  if (!isValidIPCIDR(newIP.value.trim())) {
    ipError.value = 'Invalid IP address or CIDR format'
    return
  }
  ipError.value = ''
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

const newBanIP = ref('')
const newBanDuration = ref('')
const banning = ref(false)

async function banIPAddress() {
  if (!newBanIP.value) return
  if (!isValidIPCIDR(newBanIP.value.trim())) {
    toast.add({ title: 'Invalid IP address or CIDR format', color: 'error', icon: 'i-lucide-x' })
    return
  }
  banning.value = true
  try {
    const dur = Number.parseInt(newBanDuration.value, 10)
    await adminApi.banIP(newBanIP.value, !Number.isNaN(dur) && dur > 0 ? dur : undefined)
    toast.add({ title: 'IP banned', color: 'success', icon: 'i-lucide-check' })
    newBanIP.value = ''
    newBanDuration.value = ''
    loadBanned()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    banning.value = false
  }
}

// Stats
const stats = ref<SecurityStats | null>(null)
const statsError = ref('')

async function loadStats() {
  statsError.value = ''
  try {
    stats.value = await adminApi.getSecurityStats()
  } catch (e: unknown) {
    statsError.value = e instanceof Error ? e.message : 'Failed to load security stats'
    toast.add({ title: statsError.value, color: 'error', icon: 'i-lucide-alert-circle' })
  }
}

watch(subTab, (v) => {
  if (v === 'audit') loadAudit()
  else if (v === 'whitelist') loadWhitelist()
  else if (v === 'blacklist') loadBlacklist()
  else if (v === 'banned') loadBanned()
  else if (v === 'stats') loadStats()
  else if (v === 'settings') loadSecurityConfig()
}, { immediate: true })
</script>

<template>
  <div class="space-y-4">
    <UTabs v-model="subTab" :items="subTabs" size="sm">
      <template #content="{ item }">
        <div class="pt-3">

    <!-- Audit log -->
    <div v-if="item.value === 'audit'" class="space-y-3">
      <div class="flex gap-2 justify-end">
        <UButton icon="i-lucide-download" label="Export CSV" size="sm" variant="outline" color="neutral" :loading="exportingAudit" @click="exportAuditLog" />
        <UButton icon="i-lucide-refresh-cw" aria-label="Refresh audit log" variant="ghost" color="neutral" size="sm" @click="loadAudit" />
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
            <span class="text-xs font-mono">{{ row.original.timestamp ? new Date(row.original.timestamp).toLocaleString() : '—' }}</span>
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
    <template v-if="item.value === 'whitelist' || item.value === 'blacklist'">
      <div class="space-y-4">
        <!-- Add IP -->
        <UCard>
          <template #header>
            <div class="font-semibold">Add IP to {{ subTab === 'whitelist' ? 'Whitelist' : 'Blacklist' }}</div>
          </template>
          <div class="flex flex-wrap gap-2">
            <UInput v-model="newIP" placeholder="IP address or CIDR" class="w-48" @input="ipError = ''" />
            <UInput v-model="newComment" placeholder="Comment (optional)" class="flex-1 min-w-40" />
            <UButton :loading="ipLoading" icon="i-lucide-plus" label="Add" @click="addToList(subTab as 'whitelist' | 'blacklist')" />
          </div>
          <p v-if="ipError" class="text-sm text-error mt-1">{{ ipError }}</p>
        </UCard>

        <!-- List -->
        <UCard>
          <UTable
            :data="subTab === 'whitelist' ? whitelist : blacklist"
            :columns="[{ accessorKey: 'ip', header: 'IP' }, { accessorKey: 'comment', header: 'Comment' }, { accessorKey: 'added_at', header: 'Added' }, { accessorKey: 'actions', header: '' }]"
          >
            <template #ip-cell="{ row }"><span class="font-mono text-sm">{{ row.original.ip }}</span></template>
            <template #comment-cell="{ row }"><span class="text-sm text-muted">{{ row.original.comment || '—' }}</span></template>
            <template #added_at-cell="{ row }"><span class="text-sm">{{ row.original.added_at ? new Date(row.original.added_at).toLocaleDateString() : '—' }}</span></template>
            <template #actions-cell="{ row }">
              <UButton
                icon="i-lucide-trash-2"
                aria-label="Remove IP from list"
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
    <div v-if="item.value === 'banned'" class="space-y-4">
      <UCard>
        <template #header><div class="font-semibold">Ban IP Address</div></template>
        <div class="flex flex-wrap gap-2">
          <UInput v-model="newBanIP" placeholder="IP address or CIDR" class="w-48" />
          <UInput v-model="newBanDuration" type="number" placeholder="Duration in minutes (blank = permanent)" class="flex-1 min-w-56" />
          <UButton :loading="banning" icon="i-lucide-shield-x" label="Ban" color="error" :disabled="!newBanIP" @click="banIPAddress" />
        </div>
      </UCard>
      <UCard>
        <UTable
          :data="banned"
          :columns="[{ accessorKey: 'ip', header: 'IP' }, { accessorKey: 'banned_at', header: 'Banned At' }, { accessorKey: 'reason', header: 'Reason' }, { accessorKey: 'actions', header: '' }]"
        >
          <template #ip-cell="{ row }"><span class="font-mono text-sm">{{ row.original.ip }}</span></template>
          <template #banned_at-cell="{ row }"><span class="text-sm">{{ row.original.banned_at ? new Date(row.original.banned_at).toLocaleString() : '—' }}</span></template>
          <template #reason-cell="{ row }"><span class="text-sm text-muted">{{ row.original.reason || '—' }}</span></template>
          <template #actions-cell="{ row }">
            <UButton icon="i-lucide-shield-off" size="xs" variant="ghost" color="warning" label="Unban" @click="unban(row.original.ip)" />
          </template>
        </UTable>
        <p v-if="banned.length === 0" class="text-center py-4 text-muted text-sm">No banned IPs.</p>
      </UCard>
    </div>

    <!-- Stats -->
    <UAlert v-if="item.value === 'stats' && statsError" :title="statsError" color="error" icon="i-lucide-alert-circle" class="mb-4" />
    <div v-if="item.value === 'stats' && stats" class="space-y-4">
      <div class="grid grid-cols-2 sm:grid-cols-3 gap-3">
        <UCard v-for="item in [
          { label: 'Blocked (Session)', value: stats.total_blocks_today, icon: 'i-lucide-shield-x' },
          { label: 'Rate Limited (Session)', value: stats.total_rate_limited, icon: 'i-lucide-gauge' },
          { label: 'Active Rate Limits', value: stats.active_rate_limits, icon: 'i-lucide-activity' },
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
      <!-- Feature flags -->
      <div class="flex flex-wrap gap-3">
        <div class="flex items-center gap-1.5 text-sm">
          <UIcon name="i-lucide-shield-check" class="size-4 text-muted" />
          <span class="text-muted">Whitelist</span>
          <UBadge :label="stats.whitelist_enabled ? 'On' : 'Off'" :color="stats.whitelist_enabled ? 'success' : 'neutral'" variant="subtle" size="xs" />
        </div>
        <div class="flex items-center gap-1.5 text-sm">
          <UIcon name="i-lucide-shield-ban" class="size-4 text-muted" />
          <span class="text-muted">Blacklist</span>
          <UBadge :label="stats.blacklist_enabled ? 'On' : 'Off'" :color="stats.blacklist_enabled ? 'success' : 'neutral'" variant="subtle" size="xs" />
        </div>
        <div class="flex items-center gap-1.5 text-sm">
          <UIcon name="i-lucide-gauge" class="size-4 text-muted" />
          <span class="text-muted">Rate Limiting</span>
          <UBadge :label="stats.rate_limit_enabled ? 'On' : 'Off'" :color="stats.rate_limit_enabled ? 'success' : 'neutral'" variant="subtle" size="xs" />
        </div>
      </div>
    </div>
    <!-- Security Settings -->
    <div v-if="item.value === 'settings'" class="space-y-4">
      <div v-if="configLoading" class="flex justify-center py-8">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-primary" />
      </div>
      <UCard v-else :ui="{ body: 'p-4' }">
        <div class="divide-y divide-default">
          <div class="flex items-center justify-between gap-4 py-3 first:pt-0">
            <div>
              <p class="font-medium text-sm text-highlighted">Enable HTTPS</p>
              <p class="text-xs text-muted mt-0.5">Serve the application over TLS (requires cert_file and key_file to be configured)</p>
            </div>
            <USwitch
              :model-value="httpsEnabled"
              :disabled="configSaving"
              aria-label="Enable HTTPS"
              @update:model-value="v => saveSecurityToggle('server', 'enable_https', v)"
            />
          </div>
          <div class="flex items-center justify-between gap-4 py-3">
            <div>
              <p class="font-medium text-sm text-highlighted">Enable HSTS</p>
              <p class="text-xs text-muted mt-0.5">Send Strict-Transport-Security header to force HTTPS on all future requests</p>
            </div>
            <USwitch
              :model-value="hstsEnabled"
              :disabled="configSaving"
              aria-label="Enable HSTS"
              @update:model-value="v => saveSecurityToggle('security', 'hsts_enabled', v)"
            />
          </div>
          <div class="flex items-center justify-between gap-4 py-3 last:pb-0">
            <div>
              <p class="font-medium text-sm text-highlighted">Enable CORS</p>
              <p class="text-xs text-muted mt-0.5">Allow cross-origin requests (configure allowed origins in cors_origins)</p>
            </div>
            <USwitch
              :model-value="corsEnabled"
              :disabled="configSaving"
              aria-label="Enable CORS"
              @update:model-value="v => saveSecurityToggle('security', 'cors_enabled', v)"
            />
          </div>
        </div>
      </UCard>
    </div>

        </div>
      </template>
    </UTabs>
  </div>
</template>

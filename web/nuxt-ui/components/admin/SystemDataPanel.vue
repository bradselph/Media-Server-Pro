<script setup lang="ts">
import type { BackupEntry, DatabaseStatus, QueryResult } from '~/types/api'
import { formatBytes } from '~/utils/format'
import { asRecord } from '~/utils/typeGuards'

const adminApi = useAdminApi()
const toast = useToast()

// ── Backups ────────────────────────────────────────────────────────────────────
const backups = ref<BackupEntry[]>([])
const backupsLoading = ref(false)
const creatingBackup = ref(false)
const backupType = ref<'full' | 'incremental'>('full')
const BACKUP_TYPE_OPTIONS = [
  { label: 'Full', value: 'full' },
  { label: 'Incremental', value: 'incremental' },
]

const backupFullConfig = ref<Record<string, unknown>>({})
const backupRetentionCount = ref(5)
const backupConfigSaving = ref(false)

async function loadBackupConfig() {
  try {
    const cfg = await adminApi.getConfig()
    if (cfg) {
      backupFullConfig.value = cfg
      const bk = asRecord(cfg.backup)
      backupRetentionCount.value = typeof bk?.retention_count === 'number' ? bk.retention_count : 5
    }
  } catch { /* non-critical */ }
}

async function saveBackupRetention() {
  backupConfigSaving.value = true
  try {
    const updated = {
      ...backupFullConfig.value,
      backup: { ...asRecord(backupFullConfig.value.backup), retention_count: backupRetentionCount.value },
    }
    await adminApi.updateConfig(updated)
    backupFullConfig.value = updated
    toast.add({ title: 'Backup settings saved', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to save', color: 'error', icon: 'i-lucide-x' })
  } finally { backupConfigSaving.value = false }
}

async function loadBackups() {
  backupsLoading.value = true
  try { backups.value = (await adminApi.listBackups()) ?? [] }
  catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load backups', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { backupsLoading.value = false }
}

async function createBackup() {
  creatingBackup.value = true
  try {
    await adminApi.createBackup(undefined, backupType.value)
    toast.add({ title: `${backupType.value === 'full' ? 'Full' : 'Incremental'} backup created`, color: 'success', icon: 'i-lucide-check' })
    await loadBackups()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Backup failed', color: 'error', icon: 'i-lucide-x' })
  } finally { creatingBackup.value = false }
}

// ── Database ───────────────────────────────────────────────────────────────────
const dbStatus = ref<DatabaseStatus | null>(null)
const dbStatusError = ref(false)
const dbQuery = ref('')
const dbQueryResult = ref<QueryResult | null>(null)
const dbQueryRunning = ref(false)
const dbQueryError = ref('')

async function loadDbStatus() {
  dbStatusError.value = false
  try { dbStatus.value = await adminApi.getDatabaseStatus() }
  catch { dbStatusError.value = true }
}

const SQL_ALLOWED_RE = /^\s*(SELECT|SHOW|DESCRIBE|EXPLAIN|PRAGMA)\b/i

async function runDbQuery() {
  if (!dbQuery.value.trim()) return
  if (!SQL_ALLOWED_RE.test(dbQuery.value.trim())) {
    dbQueryError.value = 'Only SELECT, SHOW, DESCRIBE, EXPLAIN, and PRAGMA queries are allowed.'
    return
  }
  dbQueryRunning.value = true
  dbQueryError.value = ''
  dbQueryResult.value = null
  try {
    dbQueryResult.value = await adminApi.executeQuery(dbQuery.value.trim())
  } catch (e: unknown) {
    dbQueryError.value = e instanceof Error ? e.message : 'Query failed'
  } finally { dbQueryRunning.value = false }
}

// Restore / delete confirmation
const confirmRestoreId = ref<string | null>(null)
const confirmDeleteId = ref<string | null>(null)

// Per-backup loading guard to prevent duplicate restore/delete operations
const backupBusy = ref<Set<string>>(new Set())

function requestRestore(id: string) {
  confirmRestoreId.value = id
}
async function executeRestore() {
  const id = confirmRestoreId.value
  if (!id) return
  await restoreBackup(id)
  confirmRestoreId.value = null
}
async function restoreBackup(id: string) {
  if (backupBusy.value.has(`restore-${id}`)) return
  const next = new Set(backupBusy.value); next.add(`restore-${id}`); backupBusy.value = next
  try {
    await adminApi.restoreBackup(id)
    toast.add({ title: 'Restore started', color: 'info', icon: 'i-lucide-info' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Restore failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    const cleared = new Set(backupBusy.value); cleared.delete(`restore-${id}`); backupBusy.value = cleared
  }
}

async function deleteBackup(id: string) {
  if (backupBusy.value.has(`delete-${id}`)) return
  const next = new Set(backupBusy.value); next.add(`delete-${id}`); backupBusy.value = next
  try {
    await adminApi.deleteBackup(id)
    await loadBackups()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Delete failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    const cleared = new Set(backupBusy.value); cleared.delete(`delete-${id}`); backupBusy.value = cleared
  }
}

onMounted(() => {
  loadBackups()
  loadBackupConfig()
  loadDbStatus()
})
</script>

<template>
  <div class="space-y-6">
    <!-- ── Backups ───────────────────────────────────────────────────────── -->
    <div class="space-y-3">
      <h3 class="text-sm font-semibold text-muted uppercase tracking-wide flex items-center gap-2">
        <UIcon name="i-lucide-archive" class="size-4" /> Backups
      </h3>

      <!-- Retention config -->
      <UCard :ui="{ body: 'p-4' }">
        <div class="flex flex-wrap items-end gap-4">
          <UFormField label="Retention count" description="Number of backup files to keep (older ones are deleted automatically)">
            <UInput
              v-model.number="backupRetentionCount"
              type="number"
              min="1"
              max="100"
              class="w-24"
            />
          </UFormField>
          <UButton
            :loading="backupConfigSaving"
            icon="i-lucide-save"
            label="Save"
            size="sm"
            @click="saveBackupRetention"
          />
        </div>
      </UCard>

      <div class="flex gap-2 items-center">
        <USelect v-model="backupType" :items="BACKUP_TYPE_OPTIONS" class="w-36" />
        <UButton icon="i-lucide-archive" :loading="creatingBackup" label="Create Backup" @click="createBackup" />
        <UButton icon="i-lucide-refresh-cw" aria-label="Refresh backups" variant="ghost" color="neutral" @click="loadBackups" />
      </div>

      <UCard>
        <div v-if="backupsLoading" class="flex justify-center py-4">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <UTable
          v-else
          :data="backups"
          :columns="[
            { accessorKey: 'filename', header: 'File' },
            { accessorKey: 'type', header: 'Type' },
            { accessorKey: 'file_count', header: 'Files' },
            { accessorKey: 'size', header: 'Size' },
            { accessorKey: 'created_at', header: 'Created' },
            { accessorKey: 'actions', header: '' },
          ]"
        >
          <template #file_count-cell="{ row }">
            <span class="text-sm tabular-nums">
              {{ row.original.files?.length ?? '—' }}
              <span v-if="row.original.errors?.length" class="text-error ml-1">({{ row.original.errors.length }} err)</span>
            </span>
          </template>
          <template #size-cell="{ row }">{{ formatBytes(row.original.size) }}</template>
          <template #created_at-cell="{ row }">
            <span class="text-sm">{{ row.original.created_at ? new Date(row.original.created_at).toLocaleString() : '—' }}</span>
          </template>
          <template #actions-cell="{ row }">
            <div class="flex gap-1 justify-end">
              <UButton
                icon="i-lucide-rotate-ccw"
                size="xs"
                variant="ghost"
                color="warning"
                title="Restore"
                aria-label="Restore backup"
                :loading="backupBusy.has(`restore-${row.original.id}`)"
                @click="requestRestore(row.original.id)"
              />
              <UButton
                icon="i-lucide-trash-2"
                size="xs"
                variant="ghost"
                color="error"
                aria-label="Delete backup"
                :loading="backupBusy.has(`delete-${row.original.id}`)"
                @click="confirmDeleteId = row.original.id"
              />
            </div>
          </template>
        </UTable>
        <p v-if="!backupsLoading && backups.length === 0" class="text-center py-4 text-muted text-sm">
          No backups found.
        </p>
      </UCard>
    </div>

    <!-- Restore confirmation -->
    <UModal
      :open="!!confirmRestoreId"
      title="Restore Backup"
      @update:open="val => { if (!val) confirmRestoreId = null }"
    >
      <template #body>
        <p>Are you sure you want to restore this backup? This will overwrite the current database.</p>
      </template>
      <template #footer>
        <UButton variant="ghost" color="neutral" label="Cancel" @click="confirmRestoreId = null" />
        <UButton color="warning" label="Restore" :loading="!!confirmRestoreId && backupBusy.has(`restore-${confirmRestoreId}`)" @click="executeRestore" />
      </template>
    </UModal>

    <!-- Delete confirmation -->
    <UModal
      :open="!!confirmDeleteId"
      title="Delete Backup"
      @update:open="val => { if (!val) confirmDeleteId = null }"
    >
      <template #body>
        <p>Are you sure you want to permanently delete this backup? This action cannot be undone.</p>
      </template>
      <template #footer>
        <UButton variant="ghost" color="neutral" label="Cancel" @click="confirmDeleteId = null" />
        <UButton color="error" label="Delete" :loading="!!confirmDeleteId && backupBusy.has(`delete-${confirmDeleteId}`)" @click="() => { const id = confirmDeleteId; confirmDeleteId = null; if (id) deleteBackup(id) }" />
      </template>
    </UModal>

    <USeparator />

    <!-- ── Database ──────────────────────────────────────────────────────── -->
    <div class="space-y-4">
      <h3 class="text-sm font-semibold text-muted uppercase tracking-wide flex items-center gap-2">
        <UIcon name="i-lucide-database" class="size-4" /> Database
      </h3>

      <!-- Connection status -->
      <UCard>
        <div v-if="dbStatusError" class="flex items-center gap-2 py-2 text-sm text-error">
          <UIcon name="i-lucide-alert-circle" class="size-4 shrink-0" />
          <span>Could not load database status.</span>
          <UButton variant="ghost" size="xs" label="Retry" @click="loadDbStatus" />
        </div>
        <div v-else-if="!dbStatus" class="flex justify-center py-4">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <div v-else class="space-y-3 text-sm">
          <div class="flex items-center gap-2">
            <UIcon
              :name="dbStatus.connected ? 'i-lucide-check-circle' : 'i-lucide-x-circle'"
              :class="dbStatus.connected ? 'text-success' : 'text-error'"
              class="size-4"
            />
            <span class="font-medium">{{ dbStatus.connected ? 'Connected' : 'Disconnected' }}</span>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <div><span class="text-muted">Host:</span> {{ dbStatus.host || '—' }}</div>
            <div><span class="text-muted">Database:</span> {{ dbStatus.database || '—' }}</div>
            <div><span class="text-muted">Type:</span> {{ dbStatus.repository_type || '—' }}</div>
            <div><span class="text-muted">Message:</span> {{ dbStatus.message || '—' }}</div>
          </div>
        </div>
      </UCard>

      <!-- Query executor (read-only) -->
      <UCard>
        <template #header>
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-terminal" class="size-4" />
            Query Executor
            <UBadge label="Read-only" color="neutral" variant="subtle" size="xs" />
          </div>
        </template>
        <div class="space-y-3">
          <UTextarea
            v-model="dbQuery"
            :rows="4"
            placeholder="SELECT * FROM users LIMIT 10"
            class="font-mono text-sm"
            @keydown.ctrl.enter="runDbQuery"
            @keydown.meta.enter="runDbQuery"
          />
          <div class="flex items-center gap-2">
            <UButton :loading="dbQueryRunning" icon="i-lucide-play" label="Run Query" size="sm" @click="runDbQuery" />
            <span class="text-xs text-muted">Ctrl+Enter to run · SELECT/SHOW only</span>
          </div>
          <UAlert v-if="dbQueryError" :title="dbQueryError" color="error" variant="soft" icon="i-lucide-x-circle" />
          <div v-if="dbQueryResult" class="space-y-1 text-sm">
            <p class="text-muted text-xs">{{ dbQueryResult.rows?.length ?? 0 }} row(s) returned</p>
            <div class="overflow-x-auto rounded border border-default">
              <table v-if="dbQueryResult.columns?.length" class="min-w-full text-xs font-mono">
                <thead class="bg-elevated">
                  <tr>
                    <th v-for="col in dbQueryResult.columns" :key="col" class="px-3 py-1.5 text-left font-medium text-muted whitespace-nowrap">{{ col }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-default">
                  <tr v-for="(row, ri) in dbQueryResult.rows" :key="ri" class="hover:bg-muted/30">
                    <td v-for="(col, colIdx) in dbQueryResult.columns" :key="col" class="px-3 py-1 whitespace-nowrap max-w-xs truncate" :title="String((row as unknown[])[colIdx] ?? '')">
                      {{ (row as unknown[])[colIdx] ?? '' }}
                    </td>
                  </tr>
                </tbody>
              </table>
              <p v-else class="px-3 py-2 text-muted">No rows returned.</p>
            </div>
          </div>
        </div>
      </UCard>
    </div>
  </div>
</template>

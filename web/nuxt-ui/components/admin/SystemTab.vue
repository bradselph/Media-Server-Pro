<script setup lang="ts">
import type { ScheduledTask, LogEntry, BackupEntry, DatabaseStatus } from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

const subTab = ref('settings')
const subTabs = [
  { label: 'Settings', value: 'settings', icon: 'i-lucide-settings' },
  { label: 'Tasks', value: 'tasks', icon: 'i-lucide-clock' },
  { label: 'Logs', value: 'logs', icon: 'i-lucide-scroll-text' },
  { label: 'Backups', value: 'backups', icon: 'i-lucide-archive' },
  { label: 'Database', value: 'database', icon: 'i-lucide-database' },
]

// Settings
const configText = ref('')
const configLoading = ref(false)
const configSaving = ref(false)
const pwCurrent = ref('')
const pwNew = ref('')
const pwConfirm = ref('')
const pwLoading = ref(false)

async function loadConfig() {
  configLoading.value = true
  try {
    const cfg = await adminApi.getConfig()
    configText.value = JSON.stringify(cfg, null, 2)
  } catch {}
  finally { configLoading.value = false }
}

async function saveConfig() {
  configSaving.value = true
  try {
    const parsed = JSON.parse(configText.value)
    await adminApi.updateConfig(parsed)
    toast.add({ title: 'Config saved', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    configSaving.value = false
  }
}

async function changeAdminPassword() {
  if (pwNew.value !== pwConfirm.value) {
    toast.add({ title: 'Passwords do not match', color: 'error', icon: 'i-lucide-x' })
    return
  }
  pwLoading.value = true
  try {
    await adminApi.changeOwnPassword(pwCurrent.value, pwNew.value)
    toast.add({ title: 'Password changed', color: 'success', icon: 'i-lucide-check' })
    pwCurrent.value = ''; pwNew.value = ''; pwConfirm.value = ''
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    pwLoading.value = false
  }
}

// Tasks
const tasks = ref<ScheduledTask[]>([])
const tasksLoading = ref(false)

async function loadTasks() {
  tasksLoading.value = true
  try { tasks.value = (await adminApi.listTasks()) ?? [] }
  catch {}
  finally { tasksLoading.value = false }
}

async function runTask(id: string) {
  try {
    await adminApi.runTask(id)
    toast.add({ title: 'Task triggered', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

// Logs
const logs = ref<LogEntry[]>([])
const logsLoading = ref(false)
const logLevel = ref('all')
const logModule = ref('')
const logsContainer = ref<HTMLElement | null>(null)

async function loadLogs() {
  logsLoading.value = true
  try {
    logs.value = (await adminApi.getLogs(logLevel.value === 'all' ? undefined : logLevel.value || undefined, logModule.value || undefined, 500)) ?? []
    await nextTick()
    if (logsContainer.value) logsContainer.value.scrollTop = logsContainer.value.scrollHeight
  } catch {}
  finally { logsLoading.value = false }
}

// Backups
const backups = ref<BackupEntry[]>([])
const backupsLoading = ref(false)
const creatingBackup = ref(false)

async function loadBackups() {
  backupsLoading.value = true
  try { backups.value = (await adminApi.listBackups()) ?? [] }
  catch {}
  finally { backupsLoading.value = false }
}

async function createBackup() {
  creatingBackup.value = true
  try {
    await adminApi.createBackup()
    toast.add({ title: 'Backup created', color: 'success', icon: 'i-lucide-check' })
    await loadBackups()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Backup failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    creatingBackup.value = false
  }
}

// Database
const dbStatus = ref<DatabaseStatus | null>(null)

async function loadDbStatus() {
  try { dbStatus.value = await adminApi.getDatabaseStatus() }
  catch {}
}

function formatBytes(bytes?: number): string {
  if (!bytes) return '—'
  const k = 1024; const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / k ** i).toFixed(1)} ${sizes[i]}`
}

// Load sub-tab data on switch
watch(subTab, (v) => {
  if (v === 'settings' && !configText.value) loadConfig()
  else if (v === 'tasks') loadTasks()
  else if (v === 'logs') loadLogs()
  else if (v === 'backups') loadBackups()
  else if (v === 'database') loadDbStatus()
}, { immediate: true })
</script>

<template>
  <div class="space-y-4">
    <UTabs v-model="subTab" :items="subTabs" size="sm" />

    <!-- Settings -->
    <div v-if="subTab === 'settings'" class="space-y-6">
      <UCard>
        <template #header><div class="font-semibold">Server Configuration (JSON)</div></template>
        <div v-if="configLoading" class="flex justify-center py-4">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <div v-else class="space-y-3">
          <UTextarea v-model="configText" :rows="20" class="font-mono text-xs" />
          <UButton :loading="configSaving" icon="i-lucide-save" label="Save Config" @click="saveConfig" />
        </div>
      </UCard>

      <UCard>
        <template #header><div class="font-semibold">Change Admin Password</div></template>
        <div class="space-y-3 max-w-sm">
          <UFormField label="Current Password">
            <UInput v-model="pwCurrent" type="password" placeholder="••••••••" />
          </UFormField>
          <UFormField label="New Password">
            <UInput v-model="pwNew" type="password" placeholder="••••••••" />
          </UFormField>
          <UFormField label="Confirm New Password">
            <UInput v-model="pwConfirm" type="password" placeholder="••••••••" />
          </UFormField>
          <UButton :loading="pwLoading" label="Change Password" @click="changeAdminPassword" />
        </div>
      </UCard>
    </div>

    <!-- Tasks -->
    <div v-if="subTab === 'tasks'">
      <UCard>
        <div v-if="tasksLoading" class="flex justify-center py-6">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <div v-else class="divide-y divide-default">
          <div
            v-for="task in tasks"
            :key="task.id"
            class="flex items-start justify-between py-3 gap-4"
          >
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2 flex-wrap">
                <span class="font-medium text-sm">{{ task.name }}</span>
                <UBadge
                  v-if="task.running"
                  label="Running"
                  color="info"
                  variant="subtle"
                  size="xs"
                />
                <UBadge
                  :label="task.enabled ? 'Enabled' : 'Disabled'"
                  :color="task.enabled ? 'success' : 'neutral'"
                  variant="subtle"
                  size="xs"
                />
              </div>
              <p class="text-xs text-muted mt-0.5">{{ task.description }}</p>
              <p class="text-xs text-muted">
                Schedule: {{ task.schedule }} · Next: {{ task.next_run ? new Date(task.next_run).toLocaleString() : '—' }}
              </p>
              <p v-if="task.last_error" class="text-xs text-error mt-0.5">{{ task.last_error }}</p>
            </div>
            <div class="flex gap-1 shrink-0">
              <UButton
                icon="i-lucide-play"
                size="xs"
                variant="ghost"
                color="neutral"
                :disabled="task.running"
                title="Run now"
                @click="runTask(task.id)"
              />
              <UButton
                :icon="task.enabled ? 'i-lucide-pause' : 'i-lucide-play-circle'"
                size="xs"
                variant="ghost"
                color="neutral"
                :title="task.enabled ? 'Disable' : 'Enable'"
                @click="task.enabled ? adminApi.disableTask(task.id).then(loadTasks) : adminApi.enableTask(task.id).then(loadTasks)"
              />
            </div>
          </div>
          <p v-if="tasks.length === 0" class="text-center py-4 text-muted text-sm">No tasks registered.</p>
        </div>
      </UCard>
    </div>

    <!-- Logs -->
    <div v-if="subTab === 'logs'" class="space-y-3">
      <div class="flex flex-wrap gap-2 items-center">
        <USelect
          v-model="logLevel"
          :items="[{ label: 'All levels', value: 'all' }, { label: 'Debug', value: 'debug' }, { label: 'Info', value: 'info' }, { label: 'Warn', value: 'warn' }, { label: 'Error', value: 'error' }]"
          class="w-36"
        />
        <UInput v-model="logModule" placeholder="Filter module…" class="w-48" @keyup.enter="loadLogs" />
        <UButton icon="i-lucide-refresh-cw" label="Refresh" variant="outline" color="neutral" @click="loadLogs" />
      </div>
      <div v-if="logsLoading" class="flex justify-center py-4">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
      </div>
      <div v-else ref="logsContainer" class="log-viewer">
        <div v-if="logs.length === 0" class="text-muted">No log entries.</div>
        <div
          v-for="(entry, i) in logs"
          :key="i"
          class="log-line"
          :class="entry.level?.toLowerCase()"
        >
          <span class="opacity-50">[{{ entry.timestamp }}]</span>
          <span class="font-semibold opacity-80"> [{{ entry.module }}]</span>
          {{ entry.message }}
        </div>
      </div>
    </div>

    <!-- Backups -->
    <div v-if="subTab === 'backups'" class="space-y-3">
      <div class="flex gap-2">
        <UButton icon="i-lucide-archive" :loading="creatingBackup" label="Create Backup" @click="createBackup" />
        <UButton icon="i-lucide-refresh-cw" variant="ghost" color="neutral" @click="loadBackups" />
      </div>
      <UCard>
        <div v-if="backupsLoading" class="flex justify-center py-4">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <UTable
          v-else
          :data="backups"
          :columns="[
            { key: 'filename', label: 'File' },
            { key: 'type', label: 'Type' },
            { key: 'size', label: 'Size' },
            { key: 'created_at', label: 'Created' },
            { key: 'actions', label: '' },
          ]"
        >
          <template #size-cell="{ row }">{{ formatBytes(row.original.size) }}</template>
          <template #created_at-cell="{ row }">
            <span class="text-sm">{{ new Date(row.original.created_at).toLocaleString() }}</span>
          </template>
          <template #actions-cell="{ row }">
            <div class="flex gap-1 justify-end">
              <UButton
                icon="i-lucide-rotate-ccw"
                size="xs"
                variant="ghost"
                color="warning"
                title="Restore"
                @click="adminApi.restoreBackup(row.original.id).then(() => toast.add({ title: 'Restore started', color: 'info', icon: 'i-lucide-info' }))"
              />
              <UButton
                icon="i-lucide-trash-2"
                size="xs"
                variant="ghost"
                color="error"
                @click="adminApi.deleteBackup(row.original.id).then(loadBackups)"
              />
            </div>
          </template>
        </UTable>
        <p v-if="!backupsLoading && backups.length === 0" class="text-center py-4 text-muted text-sm">
          No backups found.
        </p>
      </UCard>
    </div>

    <!-- Database -->
    <div v-if="subTab === 'database'">
      <UCard>
        <div v-if="!dbStatus" class="flex justify-center py-4">
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
    </div>
  </div>
</template>

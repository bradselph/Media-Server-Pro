<script setup lang="ts">
import type { ScheduledTask, LogEntry } from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

// ── Tasks ──────────────────────────────────────────────────────────────────────
const tasks = ref<ScheduledTask[]>([])
const tasksLoading = ref(false)
const togglingTasks = ref(new Set<string>())
let taskRefreshTimeout: ReturnType<typeof setTimeout> | null = null

async function loadTasks() {
  tasksLoading.value = true
  try { tasks.value = (await adminApi.listTasks()) ?? [] }
  catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load tasks', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { tasksLoading.value = false }
}

async function runTask(id: string) {
  try {
    await adminApi.runTask(id)
    toast.add({ title: 'Task triggered', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

async function toggleTask(task: ScheduledTask) {
  if (togglingTasks.value.has(task.id)) return
  togglingTasks.value.add(task.id)
  try {
    if (task.enabled) await adminApi.disableTask(task.id)
    else await adminApi.enableTask(task.id)
    await loadTasks()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    togglingTasks.value.delete(task.id)
  }
}

async function stopTask(id: string) {
  try {
    await adminApi.stopTask(id)
    toast.add({ title: 'Task stop requested', color: 'info', icon: 'i-lucide-info' })
    taskRefreshTimeout = setTimeout(loadTasks, 1000)
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  }
}

// ── Logs ───────────────────────────────────────────────────────────────────────
const logs = ref<LogEntry[]>([])
const logsLoading = ref(false)
const logLevel = ref('all')
const logModule = ref('')
const logsContainer = ref<HTMLElement | null>(null)
const autoRefreshLogs = ref(false)
let logRefreshInterval: ReturnType<typeof setInterval> | null = null

watch(autoRefreshLogs, (enabled) => {
  if (logRefreshInterval) { clearInterval(logRefreshInterval); logRefreshInterval = null }
  if (enabled) {
    logRefreshInterval = setInterval(() => { if (!document.hidden) loadLogs() }, 5_000)
  }
})

async function loadLogs() {
  logsLoading.value = true
  try {
    logs.value = (await adminApi.getLogs(logLevel.value === 'all' ? undefined : logLevel.value || undefined, logModule.value || undefined, 500)) ?? []
    await nextTick()
    if (logsContainer.value) logsContainer.value.scrollTop = logsContainer.value.scrollHeight
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load logs', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { logsLoading.value = false }
}

onMounted(() => {
  loadTasks()
  loadLogs()
})

onUnmounted(() => {
  if (logRefreshInterval) clearInterval(logRefreshInterval)
  if (taskRefreshTimeout) clearTimeout(taskRefreshTimeout)
})
</script>

<template>
  <div class="space-y-6">
    <!-- ── Tasks ────────────────────────────────────────────────────────── -->
    <div class="space-y-3">
      <h3 class="text-sm font-semibold text-muted uppercase tracking-wide flex items-center gap-2">
        <UIcon name="i-lucide-clock" class="size-4" /> Scheduled Tasks
      </h3>
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
                <UBadge v-if="task.running" label="Running" color="info" variant="subtle" size="xs" />
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
                aria-label="Run task now"
                @click="runTask(task.id)"
              />
              <UButton
                v-if="task.running"
                icon="i-lucide-square"
                size="xs"
                variant="ghost"
                color="warning"
                title="Stop"
                aria-label="Stop task"
                @click="stopTask(task.id)"
              />
              <UButton
                :icon="task.enabled ? 'i-lucide-pause' : 'i-lucide-play-circle'"
                size="xs"
                variant="ghost"
                color="neutral"
                :title="task.enabled ? 'Disable' : 'Enable'"
                :aria-label="task.enabled ? 'Disable task' : 'Enable task'"
                :disabled="togglingTasks.has(task.id)"
                @click="toggleTask(task)"
              />
            </div>
          </div>
          <p v-if="tasks.length === 0" class="text-center py-4 text-muted text-sm">No tasks registered.</p>
        </div>
      </UCard>
    </div>

    <USeparator />

    <!-- ── Logs ─────────────────────────────────────────────────────────── -->
    <div class="space-y-3">
      <h3 class="text-sm font-semibold text-muted uppercase tracking-wide flex items-center gap-2">
        <UIcon name="i-lucide-scroll-text" class="size-4" /> Server Logs
      </h3>
      <div class="flex flex-wrap gap-2 items-center">
        <USelect
          v-model="logLevel"
          :items="[{ label: 'All levels', value: 'all' }, { label: 'Debug', value: 'debug' }, { label: 'Info', value: 'info' }, { label: 'Warn', value: 'warn' }, { label: 'Error', value: 'error' }]"
          class="w-36"
        />
        <UInput v-model="logModule" placeholder="Filter module…" class="w-48" @keyup.enter="loadLogs" />
        <UButton icon="i-lucide-refresh-cw" label="Refresh" variant="outline" color="neutral" @click="loadLogs" />
        <UButton
          :icon="autoRefreshLogs ? 'i-lucide-pause' : 'i-lucide-play'"
          :label="autoRefreshLogs ? 'Auto (On)' : 'Auto (Off)'"
          :variant="autoRefreshLogs ? 'solid' : 'outline'"
          :color="autoRefreshLogs ? 'primary' : 'neutral'"
          size="sm"
          @click="autoRefreshLogs = !autoRefreshLogs"
        />
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
  </div>
</template>

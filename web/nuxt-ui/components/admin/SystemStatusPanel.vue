<script setup lang="ts">
import type { ServerStatus, ModuleHealth } from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

const serverStatus = ref<ServerStatus | null>(null)
const moduleStatuses = ref<ModuleHealth[]>([])
const statusLoading = ref(false)
const moduleDetail = ref<ModuleHealth | null>(null)
const moduleDetailLoading = ref(false)

function moduleStatusColor(status: ModuleHealth['status']): string {
  if (status === 'healthy') return 'success'
  if (status === 'degraded') return 'warning'
  return 'error'
}

async function loadStatus() {
  statusLoading.value = true
  try {
    const [srv, mods] = await Promise.allSettled([
      adminApi.getServerStatus(),
      adminApi.listModuleStatuses(),
    ])
    if (srv.status === 'fulfilled') serverStatus.value = srv.value
    if (mods.status === 'fulfilled') moduleStatuses.value = mods.value ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load server status', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally { statusLoading.value = false }
}

async function showModuleDetail(name: string) {
  moduleDetailLoading.value = true
  moduleDetail.value = null
  try {
    moduleDetail.value = await adminApi.getModuleHealth(name)
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load module health', color: 'error', icon: 'i-lucide-x' })
  } finally { moduleDetailLoading.value = false }
}

onMounted(loadStatus)
</script>

<template>
  <div class="space-y-4">
    <div class="flex justify-end">
      <UButton icon="i-lucide-refresh-cw" aria-label="Refresh status" variant="ghost" color="neutral" :loading="statusLoading" @click="loadStatus" />
    </div>
    <div v-if="statusLoading && !serverStatus" class="flex justify-center py-8">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-6" />
    </div>
    <template v-else>
      <!-- Server Status Card -->
      <UCard v-if="serverStatus">
        <template #header>
          <div class="flex items-center gap-2 font-semibold">
            <UIcon name="i-lucide-server" class="size-4" />
            Server Status
            <UBadge
              :label="serverStatus.running ? 'Running' : 'Stopped'"
              :color="serverStatus.running ? 'success' : 'error'"
              variant="subtle"
              size="xs"
            />
          </div>
        </template>
        <div class="grid grid-cols-2 sm:grid-cols-3 gap-4 text-sm">
          <div><span class="text-muted">Uptime:</span> <span class="font-medium">{{ serverStatus.uptime }}</span></div>
          <div><span class="text-muted">Started:</span> {{ serverStatus.start_time ? new Date(serverStatus.start_time).toLocaleString() : '—' }}</div>
          <div><span class="text-muted">Version:</span> <span class="font-mono">{{ serverStatus.version }}</span></div>
          <div><span class="text-muted">Go:</span> {{ serverStatus.go_version }}</div>
          <div><span class="text-muted">Modules:</span> {{ serverStatus.module_count }}</div>
        </div>
      </UCard>

      <!-- Module Health -->
      <UCard>
        <template #header>
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-puzzle" class="size-4" />
            Module Health
            <span class="text-muted text-xs font-normal">(click for details)</span>
          </div>
        </template>
        <div v-if="moduleStatuses.length === 0" class="text-muted text-sm text-center py-4">No module data.</div>
        <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-2">
          <button
            v-for="m in moduleStatuses"
            :key="m.name"
            class="flex items-center justify-between text-xs rounded px-2 py-1.5 hover:bg-elevated transition-colors cursor-pointer"
            :class="m.status === 'healthy' ? 'bg-success/10' : m.status === 'degraded' ? 'bg-warning/10' : 'bg-error/10'"
            @click="showModuleDetail(m.name)"
          >
            <span class="truncate mr-1">{{ m.name }}</span>
            <UBadge :label="m.status" :color="moduleStatusColor(m.status)" size="xs" variant="subtle" />
          </button>
        </div>
      </UCard>
    </template>

    <!-- Module Detail Modal -->
    <UModal
      v-if="moduleDetail || moduleDetailLoading"
      :open="!!(moduleDetail || moduleDetailLoading)"
      :title="moduleDetail ? moduleDetail.name : 'Loading…'"
      @update:open="val => { if (!val) moduleDetail = null }"
    >
      <template #body>
        <div v-if="moduleDetailLoading" class="flex justify-center py-4">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <div v-else-if="moduleDetail" class="space-y-2 text-sm">
          <div class="flex items-center gap-2">
            <span class="text-muted">Status:</span>
            <UBadge :label="moduleDetail.status" :color="moduleStatusColor(moduleDetail.status)" variant="subtle" />
          </div>
          <div v-if="moduleDetail.message">
            <span class="text-muted">Message:</span>
            <p class="mt-1 font-mono text-xs bg-muted rounded p-2">{{ moduleDetail.message }}</p>
          </div>
          <div v-if="moduleDetail.last_check">
            <span class="text-muted">Last Check:</span> {{ new Date(moduleDetail.last_check).toLocaleString() }}
          </div>
        </div>
      </template>
      <template #footer>
        <UButton variant="ghost" color="neutral" label="Close" @click="moduleDetail = null" />
      </template>
    </UModal>
  </div>
</template>

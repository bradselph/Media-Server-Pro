<script setup lang="ts">
import type { UpdateInfo, UpdateStatus } from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

// ── Update config ─────────────────────────────────────────────────────────────
const updateMethod = ref<'source' | 'binary'>('binary')
const updateBranch = ref('main')
const configLoading = ref(false)
const configSaving = ref(false)

async function loadUpdateConfig() {
  configLoading.value = true
  try {
    const cfg = await adminApi.getUpdateConfig()
    updateMethod.value = cfg.update_method
    updateBranch.value = cfg.branch
  } catch { /* silently ignore — default to binary */ }
  finally { configLoading.value = false }
}

async function saveUpdateConfig() {
  configSaving.value = true
  try {
    const cfg = await adminApi.setUpdateConfig({ update_method: updateMethod.value, branch: updateBranch.value })
    updateMethod.value = cfg.update_method
    updateBranch.value = cfg.branch
    toast.add({ title: 'Update config saved', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to save config', color: 'error', icon: 'i-lucide-x' })
  } finally { configSaving.value = false }
}

// ── Binary update ─────────────────────────────────────────────────────────────
const info = ref<UpdateInfo | null>(null)
const status = ref<UpdateStatus | null>(null)
const checking = ref(false)
const applying = ref(false)
const confirmOpen = ref(false)

const pollInterval = ref<ReturnType<typeof setInterval> | null>(null)

function stopPolling() {
  if (pollInterval.value !== null) {
    clearInterval(pollInterval.value)
    pollInterval.value = null
  }
}

onUnmounted(stopPolling)

async function checkForUpdates() {
  checking.value = true
  try {
    info.value = await adminApi.checkForUpdates()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Check failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    checking.value = false
  }
}

async function applyUpdate() {
  confirmOpen.value = false
  applying.value = true
  try {
    const res = await adminApi.applyUpdate()
    toast.add({ title: 'Update applying — server will restart shortly', color: 'info', icon: 'i-lucide-info' })
    status.value = res ?? { in_progress: true, stage: 'applying', progress: 0 }
    if (status.value?.in_progress) {
      stopPolling()
      pollInterval.value = setInterval(async () => {
        try {
          const s = await adminApi.getUpdateStatus()
          status.value = s
          if (!s.in_progress) { stopPolling(); applying.value = false }
        } catch { stopPolling(); applying.value = false }
      }, 3000)
    } else {
      applying.value = false
    }
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Update failed', color: 'error', icon: 'i-lucide-x' })
    applying.value = false
  }
}

// ── Source update ─────────────────────────────────────────────────────────────
const sourceInfo = ref<{ updates_available: boolean; remote_commit: string } | null>(null)
const sourceChecking = ref(false)
const sourceApplying = ref(false)
const sourceConfirmOpen = ref(false)

async function checkSourceUpdates() {
  sourceChecking.value = true
  try {
    sourceInfo.value = await adminApi.checkSourceUpdates()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Source check failed', color: 'error', icon: 'i-lucide-x' })
  } finally { sourceChecking.value = false }
}

async function applySourceUpdate() {
  sourceConfirmOpen.value = false
  sourceApplying.value = true
  try {
    const res = await adminApi.applySourceUpdate()
    status.value = res ?? { in_progress: true, stage: 'pulling', progress: 0 }
    toast.add({ title: 'Source update started — server will restart', color: 'info', icon: 'i-lucide-info' })
    if (status.value?.in_progress) {
      stopPolling()
      pollInterval.value = setInterval(async () => {
        try {
          const s = await adminApi.getSourceUpdateProgress()
          status.value = s
          if (!s.in_progress) { stopPolling(); sourceApplying.value = false }
        } catch { stopPolling(); sourceApplying.value = false }
      }, 3000)
    } else {
      sourceApplying.value = false
    }
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Source update failed', color: 'error', icon: 'i-lucide-x' })
    sourceApplying.value = false
  }
}

onMounted(async () => {
  await loadUpdateConfig()
  if (updateMethod.value === 'source') checkSourceUpdates()
  else checkForUpdates()
})
</script>

<template>
  <div class="space-y-4 max-w-2xl">
    <!-- Update method config -->
    <UCard>
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-settings-2" class="size-4" />
          Update Configuration
        </div>
      </template>
      <div v-if="configLoading" class="flex justify-center py-3">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
      </div>
      <div v-else class="space-y-3">
        <div class="flex flex-wrap gap-3 items-end">
          <UFormField label="Update Method">
            <USelect
              v-model="updateMethod"
              :items="[{ label: 'Binary (GitHub release)', value: 'binary' }, { label: 'Source (git pull)', value: 'source' }]"
              class="w-52"
            />
          </UFormField>
          <UFormField label="Branch">
            <UInput v-model="updateBranch" placeholder="main" class="w-36" />
          </UFormField>
          <UButton :loading="configSaving" icon="i-lucide-save" label="Save" size="sm" variant="outline" color="neutral" @click="saveUpdateConfig" />
        </div>
        <p class="text-xs text-muted">
          <span v-if="updateMethod === 'source'">Source mode: pulls latest code from git and rebuilds.</span>
          <span v-else>Binary mode: downloads a pre-built release from GitHub.</span>
        </p>
      </div>
    </UCard>

    <!-- ── Binary update ─────────────────────────────────────────── -->
    <template v-if="updateMethod === 'binary'">
      <UCard>
        <template #header>
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-package" class="size-4" />
            Version Info
          </div>
        </template>
        <div class="space-y-2 text-sm">
          <div class="flex items-center gap-2">
            <span class="text-muted">Current:</span>
            <span class="font-mono font-medium">{{ info?.current_version || '—' }}</span>
          </div>
          <div class="flex items-center gap-2">
            <span class="text-muted">Latest:</span>
            <span class="font-mono">{{ info?.latest_version || '—' }}</span>
            <UBadge v-if="info?.update_available" label="Update Available" color="warning" variant="subtle" size="xs" />
            <UBadge v-else-if="info && !info.update_available" label="Up to date" color="success" variant="subtle" size="xs" />
          </div>
          <div v-if="info?.published_at" class="flex items-center gap-2">
            <span class="text-muted">Released:</span>
            <span>{{ new Date(info.published_at).toLocaleDateString() }}</span>
          </div>
        </div>
      </UCard>

      <UCard v-if="info?.release_notes">
        <template #header><div class="font-semibold">Release Notes</div></template>
        <pre class="text-sm whitespace-pre-wrap text-muted">{{ info.release_notes }}</pre>
      </UCard>

      <div class="flex gap-2">
        <UButton icon="i-lucide-refresh-cw" label="Check for Updates" :loading="checking" variant="outline" color="neutral" @click="checkForUpdates" />
        <UButton v-if="info?.update_available" icon="i-lucide-download" label="Apply Update" :loading="applying" color="primary" @click="confirmOpen = true" />
      </div>

      <UModal v-model:open="confirmOpen" title="Apply Update" description="This will restart the server. Active streams will be interrupted.">
        <template #footer>
          <UButton variant="ghost" color="neutral" label="Cancel" @click="confirmOpen = false" />
          <UButton color="warning" label="Apply Update" @click="applyUpdate" />
        </template>
      </UModal>
    </template>

    <!-- ── Source update ─────────────────────────────────────────── -->
    <template v-else>
      <UCard>
        <template #header>
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-git-pull-request" class="size-4" />
            Source Update Status
          </div>
        </template>
        <div v-if="sourceChecking" class="flex justify-center py-3">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <div v-else-if="sourceInfo" class="space-y-2 text-sm">
          <div class="flex items-center gap-2">
            <UBadge v-if="sourceInfo.updates_available" label="Updates Available" color="warning" variant="subtle" size="xs" />
            <UBadge v-else label="Up to date" color="success" variant="subtle" size="xs" />
          </div>
          <div v-if="sourceInfo.remote_commit" class="flex items-center gap-2">
            <span class="text-muted">Remote commit:</span>
            <span class="font-mono text-xs">{{ sourceInfo.remote_commit?.slice(0, 12) }}</span>
          </div>
        </div>
        <p v-else class="text-muted text-sm">Click "Check" to compare local vs remote.</p>
      </UCard>

      <div class="flex gap-2">
        <UButton icon="i-lucide-refresh-cw" label="Check for Updates" :loading="sourceChecking" variant="outline" color="neutral" @click="checkSourceUpdates" />
        <UButton v-if="sourceInfo?.updates_available" icon="i-lucide-git-pull-request" label="Apply Source Update" :loading="sourceApplying" color="primary" @click="sourceConfirmOpen = true" />
      </div>

      <UModal v-model:open="sourceConfirmOpen" title="Apply Source Update" description="This will pull from git and rebuild. The server will restart. Active streams will be interrupted.">
        <template #footer>
          <UButton variant="ghost" color="neutral" label="Cancel" @click="sourceConfirmOpen = false" />
          <UButton color="warning" label="Pull & Rebuild" @click="applySourceUpdate" />
        </template>
      </UModal>
    </template>

    <!-- Shared update status (shown during both binary and source updates) -->
    <UCard v-if="status && (status.in_progress || status.error || status.stage)">
      <template #header><div class="font-semibold">Update Progress</div></template>
      <div class="space-y-2">
        <div class="flex items-center gap-2">
          <UIcon
            :name="status.error ? 'i-lucide-x-circle' : !status.in_progress ? 'i-lucide-check-circle' : 'i-lucide-loader-2'"
            :class="[status.error ? 'text-error' : !status.in_progress ? 'text-success' : 'text-info animate-spin', 'size-4']"
          />
          <span class="text-sm capitalize">{{ status.stage || (status.in_progress ? 'In Progress' : 'Done') }}</span>
        </div>
        <UProgress v-if="status.progress != null" :value="status.progress" size="sm" />
        <p v-if="status.error" class="text-sm text-error">{{ status.error }}</p>
      </div>
    </UCard>
  </div>
</template>

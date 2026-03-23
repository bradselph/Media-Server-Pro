<script setup lang="ts">
import type { UpdateInfo, UpdateStatus } from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

const info = ref<UpdateInfo | null>(null)
const status = ref<UpdateStatus | null>(null)
const checking = ref(false)
const applying = ref(false)
const confirmOpen = ref(false)

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
    await adminApi.applyUpdate()
    toast.add({ title: 'Update applying — server will restart shortly', color: 'info', icon: 'i-lucide-info' })
    status.value = { state: 'applying' }
    // Poll status
    const poll = setInterval(async () => {
      try {
        const s = await adminApi.getUpdateStatus()
        status.value = s
        if (s.state === 'success' || s.state === 'error' || s.state === 'idle') {
          clearInterval(poll)
          applying.value = false
        }
      } catch {
        clearInterval(poll)
        applying.value = false
      }
    }, 3000)
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Update failed', color: 'error', icon: 'i-lucide-x' })
    applying.value = false
  }
}

onMounted(checkForUpdates)
</script>

<template>
  <div class="space-y-4 max-w-2xl">
    <!-- Current version -->
    <UCard>
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-package" class="size-4" />
          Version Info
        </div>
      </template>
      <div class="space-y-2 text-sm">
        <div class="flex items-center gap-2">
          <span class="text-(--ui-text-muted)">Current:</span>
          <span class="font-mono font-medium">{{ info?.current_version || '—' }}</span>
        </div>
        <div class="flex items-center gap-2">
          <span class="text-(--ui-text-muted)">Latest:</span>
          <span class="font-mono">{{ info?.latest_version || '—' }}</span>
          <UBadge
            v-if="info?.update_available"
            label="Update Available"
            color="warning"
            variant="subtle"
            size="xs"
          />
          <UBadge
            v-else-if="info && !info.update_available"
            label="Up to date"
            color="success"
            variant="subtle"
            size="xs"
          />
        </div>
        <div v-if="info?.published_at" class="flex items-center gap-2">
          <span class="text-(--ui-text-muted)">Released:</span>
          <span>{{ new Date(info.published_at).toLocaleDateString() }}</span>
        </div>
      </div>
    </UCard>

    <!-- Release notes -->
    <UCard v-if="info?.release_notes">
      <template #header>
        <div class="font-semibold">Release Notes</div>
      </template>
      <pre class="text-sm whitespace-pre-wrap text-(--ui-text-muted)">{{ info.release_notes }}</pre>
    </UCard>

    <!-- Status while updating -->
    <UCard v-if="status && status.state !== 'idle'">
      <template #header>
        <div class="font-semibold">Update Status</div>
      </template>
      <div class="space-y-2">
        <div class="flex items-center gap-2">
          <UIcon
            :name="status.state === 'success' ? 'i-lucide-check-circle' : status.state === 'error' ? 'i-lucide-x-circle' : 'i-lucide-loader-2'"
            :class="[
              status.state === 'success' ? 'text-success' : status.state === 'error' ? 'text-error' : 'text-info animate-spin',
              'size-4',
            ]"
          />
          <span class="text-sm capitalize">{{ status.state }}</span>
        </div>
        <UProgress v-if="status.progress != null" :value="status.progress" size="sm" />
        <p v-if="status.message" class="text-sm text-(--ui-text-muted)">{{ status.message }}</p>
        <p v-if="status.error" class="text-sm text-error">{{ status.error }}</p>
      </div>
    </UCard>

    <!-- Actions -->
    <div class="flex gap-2">
      <UButton
        icon="i-lucide-refresh-cw"
        label="Check for Updates"
        :loading="checking"
        variant="outline"
        color="neutral"
        @click="checkForUpdates"
      />
      <UButton
        v-if="info?.update_available"
        icon="i-lucide-download"
        label="Apply Update"
        :loading="applying"
        color="primary"
        @click="confirmOpen = true"
      />
    </div>

    <!-- Confirm modal -->
    <UModal v-model:open="confirmOpen" title="Apply Update" description="This will restart the server. Active streams will be interrupted.">
      <template #footer>
        <UButton variant="ghost" color="neutral" label="Cancel" @click="confirmOpen = false" />
        <UButton color="warning" label="Apply Update" @click="applyUpdate" />
      </template>
    </UModal>
  </div>
</template>

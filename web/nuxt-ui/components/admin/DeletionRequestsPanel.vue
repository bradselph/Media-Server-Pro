<script setup lang="ts">
import type { DataDeletionRequest } from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

const requests = ref<DataDeletionRequest[]>([])
const loading = ref(true)
const statusFilter = ref<'' | 'pending' | 'approved' | 'denied'>('')
const processOpen = ref(false)
const selected = ref<DataDeletionRequest | null>(null)
const processAction = ref<'approve' | 'deny'>('deny')
const adminNotes = ref('')
const processing = ref(false)

async function load() {
  loading.value = true
  try {
    requests.value = (await adminApi.listDeletionRequests(statusFilter.value || undefined)) ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load requests', color: 'error', icon: 'i-lucide-x' })
  } finally {
    loading.value = false
  }
}

function openProcess(req: DataDeletionRequest, action: 'approve' | 'deny') {
  selected.value = req
  processAction.value = action
  adminNotes.value = ''
  processOpen.value = true
}

async function confirmProcess() {
  if (!selected.value) return
  processing.value = true
  try {
    await adminApi.processDeletionRequest(selected.value.id, processAction.value, adminNotes.value)
    const label = processAction.value === 'approve' ? 'approved (user deleted)' : 'denied'
    toast.add({ title: `Request ${label}`, color: 'success', icon: 'i-lucide-check' })
    processOpen.value = false
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    processing.value = false
  }
}

const pendingCount = computed(() => requests.value.filter(r => r.status === 'pending').length)

onMounted(load)
watch(statusFilter, load)
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between gap-3 flex-wrap">
      <div class="flex items-center gap-2">
        <span class="text-sm font-medium text-highlighted">Data Deletion Requests</span>
        <UBadge v-if="pendingCount > 0" :label="String(pendingCount) + ' pending'" color="warning" variant="subtle" size="xs" />
      </div>
      <div class="flex items-center gap-2">
        <USelect
          v-model="statusFilter"
          :items="[
            { label: 'All', value: '' },
            { label: 'Pending', value: 'pending' },
            { label: 'Approved', value: 'approved' },
            { label: 'Denied', value: 'denied' },
          ]"
          size="xs"
          class="w-32"
        />
        <UButton icon="i-lucide-refresh-cw" label="Refresh" variant="outline" color="neutral" size="xs" @click="load" />
      </div>
    </div>

    <div v-if="loading" class="flex justify-center py-8">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-primary" />
    </div>

    <div v-else-if="requests.length === 0" class="text-center py-8 text-muted text-sm">
      No data deletion requests.
    </div>

    <div v-else class="space-y-3">
      <UCard
        v-for="req in requests"
        :key="req.id"
        :ui="{ root: req.status === 'pending' ? 'ring-1 ring-warning/40' : '' }"
      >
        <div class="flex items-start justify-between gap-3 flex-wrap">
          <div class="min-w-0 space-y-0.5">
            <div class="flex items-center gap-2 flex-wrap">
              <span class="font-medium text-sm">{{ req.username }}</span>
              <UBadge
                :label="req.status"
                :color="req.status === 'pending' ? 'warning' : req.status === 'approved' ? 'success' : 'neutral'"
                variant="subtle"
                size="xs"
              />
              <span class="text-xs text-muted">{{ new Date(req.created_at).toLocaleString() }}</span>
            </div>
            <p v-if="req.email" class="text-xs text-muted">{{ req.email }}</p>
            <p v-if="req.reason" class="text-sm text-default mt-1 italic">"{{ req.reason }}"</p>
            <p v-if="req.reviewed_by" class="text-xs text-muted mt-1">
              Reviewed by {{ req.reviewed_by }}
              <template v-if="req.reviewed_at"> on {{ new Date(req.reviewed_at).toLocaleDateString() }}</template>
            </p>
            <p v-if="req.admin_notes" class="text-xs text-muted">Admin note: {{ req.admin_notes }}</p>
          </div>
          <div v-if="req.status === 'pending'" class="flex gap-2 shrink-0">
            <UButton
              icon="i-lucide-check"
              label="Approve"
              size="xs"
              color="error"
              variant="outline"
              @click="openProcess(req, 'approve')"
            />
            <UButton
              icon="i-lucide-x"
              label="Deny"
              size="xs"
              color="neutral"
              variant="outline"
              @click="openProcess(req, 'deny')"
            />
          </div>
        </div>
      </UCard>
    </div>

    <!-- Process modal -->
    <UModal
      v-model:open="processOpen"
      :title="processAction === 'approve' ? 'Approve Deletion Request' : 'Deny Deletion Request'"
      :description="processAction === 'approve'
        ? `This will permanently delete the account and all data for '${selected?.username}'. This cannot be undone.`
        : `The request from '${selected?.username}' will be denied and closed.`"
    >
      <template #body>
        <UFormField label="Admin notes (optional)">
          <UTextarea v-model="adminNotes" placeholder="Reason for your decision…" :rows="2" />
        </UFormField>
      </template>
      <template #footer>
        <UButton variant="ghost" color="neutral" label="Cancel" @click="processOpen = false" />
        <UButton
          :loading="processing"
          :color="processAction === 'approve' ? 'error' : 'neutral'"
          :label="processAction === 'approve' ? 'Approve & Delete User' : 'Deny Request'"
          @click="confirmProcess"
        />
      </template>
    </UModal>
  </div>
</template>

<script setup lang="ts">
import type {MediaReport, MediaReportStatus} from '~/types/api'
import {useAdminFeedback} from '~/composables/useAdminFeedback'

const adminApi = useAdminApi()
const {notifyError, notifySuccess} = useAdminFeedback()

let destroyed = false
onUnmounted(() => {
  destroyed = true
})

const reports = ref<MediaReport[]>([])
const openCount = ref(0)
const loading = ref(true)
const statusFilter = ref<'' | MediaReportStatus>('open')
const updatingId = ref<string | null>(null)
const PAGE_SIZE = 50
const offset = ref(0)
const reachedEnd = ref(false)

async function load(reset = true) {
  if (reset) {
    offset.value = 0
    reachedEnd.value = false
  }
  loading.value = true
  try {
    const res = await adminApi.listMediaReports(statusFilter.value || undefined, PAGE_SIZE, offset.value)
    if (destroyed) return
    const list = res?.reports ?? []
    reports.value = reset ? list : [...reports.value, ...list]
    openCount.value = res?.open_count ?? 0
    if (list.length < PAGE_SIZE) reachedEnd.value = true
  } catch (e: unknown) {
    notifyError(e, 'Failed to load reports')
  } finally {
    if (!destroyed) loading.value = false
  }
}

async function loadMore() {
  if (reachedEnd.value || loading.value) return
  offset.value += PAGE_SIZE
  await load(false)
}

async function setStatus(report: MediaReport, status: MediaReportStatus) {
  if (updatingId.value) return
  updatingId.value = report.id
  try {
    await adminApi.updateMediaReportStatus(report.id, status)
    notifySuccess(`Report ${status}`)
    // Remove from current view if it no longer matches the active filter.
    if (statusFilter.value && statusFilter.value !== status) {
      reports.value = reports.value.filter(r => r.id !== report.id)
    } else {
      report.status = status
    }
    // Refresh the open-count badge.
    const res = await adminApi.listMediaReports('open', 1, 0)
    if (!destroyed) openCount.value = res?.open_count ?? 0
  } catch (e: unknown) {
    notifyError(e, 'Failed to update report')
  } finally {
    if (!destroyed) updatingId.value = null
  }
}

function reasonLabel(reason: string): string {
  switch (reason) {
    case 'inappropriate':
      return 'Inappropriate'
    case 'broken':
      return 'Broken / unplayable'
    case 'spam':
      return 'Spam'
    case 'copyright':
      return 'Copyright'
    case 'other':
      return 'Other'
    default:
      return reason
  }
}

function reasonColor(reason: string): 'error' | 'warning' | 'info' | 'neutral' {
  switch (reason) {
    case 'inappropriate':
    case 'copyright':
      return 'error'
    case 'broken':
      return 'warning'
    case 'spam':
      return 'info'
    default:
      return 'neutral'
  }
}

onMounted(() => load())
watch(statusFilter, () => load())
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between gap-3 flex-wrap">
      <div class="flex items-center gap-2">
        <UIcon name="i-lucide-flag" class="size-4 text-primary"/>
        <span class="text-sm font-medium text-highlighted">Media Reports</span>
        <UBadge v-if="openCount > 0" :label="`${openCount} open`" color="warning" variant="subtle" size="xs"/>
      </div>
      <div class="flex items-center gap-2">
        <USelect
            v-model="statusFilter"
            :items="[
            { label: 'Open', value: 'open' },
            { label: 'Resolved', value: 'resolved' },
            { label: 'Dismissed', value: 'dismissed' },
            { label: 'All', value: '' },
          ]"
            size="xs"
            class="w-32"
        />
        <UButton icon="i-lucide-refresh-cw" label="Refresh" variant="outline" color="neutral" size="xs"
                 @click="load(true)"/>
      </div>
    </div>

    <div v-if="loading && reports.length === 0" class="flex justify-center py-8">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-primary"/>
    </div>

    <div v-else-if="reports.length === 0" class="text-center py-8 text-muted text-sm">
      No reports matching this filter.
    </div>

    <div v-else class="space-y-3">
      <UCard
          v-for="report in reports"
          :key="report.id"
          :ui="{ root: report.status === 'open' ? 'ring-1 ring-warning/40' : '' }"
      >
        <div class="flex items-start justify-between gap-3 flex-wrap">
          <div class="min-w-0 space-y-1 flex-1">
            <div class="flex items-center gap-2 flex-wrap">
              <UBadge
                  :label="reasonLabel(report.reason)"
                  :color="reasonColor(report.reason)"
                  variant="subtle"
                  size="xs"
              />
              <UBadge
                  :label="report.status"
                  :color="report.status === 'open' ? 'warning' : report.status === 'resolved' ? 'success' : 'neutral'"
                  variant="subtle"
                  size="xs"
              />
              <span class="text-xs text-muted font-mono">{{ new Date(report.created_at).toLocaleString() }}</span>
            </div>
            <div class="flex items-center gap-2 flex-wrap text-xs text-muted">
              <span>Media:</span>
              <NuxtLink
                  :to="`/player?id=${encodeURIComponent(report.media_id)}`"
                  target="_blank"
                  class="font-mono text-primary hover:underline truncate max-w-[260px]"
              >{{ report.media_id }}
              </NuxtLink>
              <span v-if="report.reporter_id" class="font-mono">by {{ report.reporter_id }}</span>
              <span v-else class="italic">guest</span>
              <span v-if="report.ip_address" class="font-mono">· {{ report.ip_address }}</span>
            </div>
            <p v-if="report.notes" class="text-sm text-default mt-1 italic break-words">"{{ report.notes }}"</p>
            <p v-if="report.resolved_by" class="text-xs text-muted">
              {{ report.status === 'resolved' ? 'Resolved' : 'Dismissed' }} by {{ report.resolved_by }}
              <template v-if="report.resolved_at"> · {{ new Date(report.resolved_at).toLocaleString() }}</template>
            </p>
          </div>
          <div v-if="report.status === 'open'" class="flex gap-2 shrink-0">
            <UButton
                icon="i-lucide-check"
                label="Resolve"
                size="xs"
                color="success"
                variant="outline"
                :loading="updatingId === report.id"
                @click="setStatus(report, 'resolved')"
            />
            <UButton
                icon="i-lucide-x"
                label="Dismiss"
                size="xs"
                color="neutral"
                variant="outline"
                :loading="updatingId === report.id"
                @click="setStatus(report, 'dismissed')"
            />
          </div>
          <div v-else class="flex gap-2 shrink-0">
            <UButton
                icon="i-lucide-rotate-ccw"
                label="Reopen"
                size="xs"
                color="warning"
                variant="ghost"
                :loading="updatingId === report.id"
                @click="setStatus(report, 'open')"
            />
          </div>
        </div>
      </UCard>

      <div v-if="!reachedEnd" class="flex justify-center pt-2">
        <UButton
            icon="i-lucide-chevron-down"
            label="Load more"
            variant="ghost"
            color="neutral"
            size="sm"
            :loading="loading"
            @click="loadMore"
        />
      </div>
    </div>
  </div>
</template>

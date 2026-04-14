<script setup lang="ts">
import type { ReceiverDuplicate } from '~/types/api'
import { formatRelativeDate } from '~/utils/format'

const adminApi = useAdminApi()
const mediaApi = useMediaApi()
const toast = useToast()

const duplicates = ref<ReceiverDuplicate[]>([])
const loading = ref(false)
const scanning = ref(false)
const statusFilter = ref<'pending' | 'all'>('pending')
const resolvingId = ref<string | null>(null)

async function load() {
  loading.value = true
  try {
    duplicates.value = (await adminApi.listDuplicates(statusFilter.value)) ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load duplicates', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally {
    loading.value = false
  }
}

async function scan() {
  scanning.value = true
  try {
    await adminApi.scanDuplicates()
    toast.add({ title: 'Scan complete', color: 'success', icon: 'i-lucide-check' })
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Scan failed', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally {
    scanning.value = false
  }
}

async function resolve(id: string, action: 'remove_a' | 'remove_b' | 'keep_both' | 'ignore') {
  resolvingId.value = id
  try {
    await adminApi.resolveDuplicate(id, action)
    toast.add({ title: 'Resolved', color: 'success', icon: 'i-lucide-check' })
    // Remove from list if status was pending and we're filtering pending
    if (statusFilter.value === 'pending') {
      duplicates.value = duplicates.value.filter(d => d.id !== id)
    } else {
      await load()
    }
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to resolve', color: 'error', icon: 'i-lucide-alert-circle' })
  } finally {
    resolvingId.value = null
  }
}

watch(statusFilter, load)
onMounted(load)

const pendingCount = computed(() => duplicates.value.filter(d => d.status === 'pending').length)

function thumbnailUrl(itemId: string, source: string) {
  if (source === 'local') return mediaApi.getThumbnailUrl(itemId)
  return undefined
}

type BadgeColor = 'primary' | 'secondary' | 'success' | 'info' | 'warning' | 'error' | 'neutral'
const STATUS_COLORS: Record<string, BadgeColor> = {
  pending: 'warning',
  keep_both: 'success',
  ignore: 'neutral',
  remove_a: 'error',
  remove_b: 'error',
}
</script>

<template>
  <div class="space-y-4">
    <!-- Header -->
    <div class="flex items-center justify-between flex-wrap gap-3">
      <div>
        <h2 class="text-lg font-semibold text-highlighted">Duplicate Detection</h2>
        <p class="text-sm text-muted">
          Content-fingerprint duplicates detected across local and receiver media.
          <span v-if="pendingCount > 0" class="text-warning font-medium">{{ pendingCount }} pending review.</span>
        </p>
      </div>
      <div class="flex gap-2">
        <USelect
          v-model="statusFilter"
          :items="[{ label: 'Pending', value: 'pending' }, { label: 'All', value: 'all' }]"
          size="sm"
          class="w-28"
        />
        <UButton
          icon="i-lucide-scan"
          label="Scan Local"
          size="sm"
          color="neutral"
          variant="outline"
          :loading="scanning"
          @click="scan"
        />
        <UButton
          icon="i-lucide-refresh-cw"
          label="Refresh"
          size="sm"
          color="neutral"
          variant="ghost"
          :loading="loading"
          @click="load"
        />
      </div>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-12">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-primary" />
    </div>

    <!-- Empty -->
    <div v-else-if="duplicates.length === 0" class="text-center py-12 text-muted">
      <UIcon name="i-lucide-check-circle" class="size-8 mb-2 text-success" />
      <p>No {{ statusFilter === 'pending' ? 'pending ' : '' }}duplicates found.</p>
      <p class="text-xs mt-1">Run "Scan Local" to check for new content-fingerprint duplicates.</p>
    </div>

    <!-- Duplicate cards -->
    <div v-else class="space-y-4">
      <div
        v-for="dup in duplicates"
        :key="dup.id"
        class="border border-default rounded-xl overflow-hidden bg-elevated"
      >
        <!-- Status bar -->
        <div class="flex items-center justify-between px-4 py-2 bg-muted/30 border-b border-default">
          <div class="flex items-center gap-2">
            <UBadge
              :label="dup.status"
              :color="STATUS_COLORS[dup.status] ?? 'neutral'"
              variant="subtle"
              size="xs"
            />
            <span class="text-xs text-muted font-mono truncate max-w-48" :title="`fp: ${dup.fingerprint}`">
              fp: {{ dup.fingerprint.slice(0, 12) }}…
            </span>
          </div>
          <span class="text-xs text-muted">{{ formatRelativeDate(dup.detected_at) }}</span>
        </div>

        <!-- Side-by-side comparison -->
        <div class="grid grid-cols-2 divide-x divide-default">
          <!-- Item A -->
          <div class="p-4 space-y-2">
            <div class="flex items-center gap-2">
              <UBadge
                :label="dup.item_a?.source ?? 'local'"
                :color="dup.item_a?.source === 'local' ? 'primary' : 'neutral'"
                variant="subtle"
                size="xs"
              />
              <p class="text-sm font-medium truncate" :title="dup.item_a_name">{{ dup.item_a_name }}</p>
            </div>
            <div class="aspect-video rounded-lg overflow-hidden bg-muted">
              <img
                v-if="dup.item_a?.source === 'local' && dup.item_a?.id"
                :src="thumbnailUrl(dup.item_a.id, 'local')"
                :alt="dup.item_a_name"
                class="w-full h-full object-cover"
                loading="lazy"
              />
              <div v-else class="w-full h-full flex items-center justify-center">
                <UIcon name="i-lucide-server" class="size-8 text-muted" />
              </div>
            </div>
            <p v-if="dup.item_a?.id" class="text-[10px] text-muted font-mono truncate">{{ dup.item_a.id }}</p>
          </div>

          <!-- Item B -->
          <div class="p-4 space-y-2">
            <div class="flex items-center gap-2">
              <UBadge
                :label="dup.item_b?.source ?? 'local'"
                :color="dup.item_b?.source === 'local' ? 'primary' : 'neutral'"
                variant="subtle"
                size="xs"
              />
              <p class="text-sm font-medium truncate" :title="dup.item_b_name">{{ dup.item_b_name }}</p>
            </div>
            <div class="aspect-video rounded-lg overflow-hidden bg-muted">
              <img
                v-if="dup.item_b?.source === 'local' && dup.item_b?.id"
                :src="thumbnailUrl(dup.item_b.id, 'local')"
                :alt="dup.item_b_name"
                class="w-full h-full object-cover"
                loading="lazy"
              />
              <div v-else class="w-full h-full flex items-center justify-center">
                <UIcon name="i-lucide-server" class="size-8 text-muted" />
              </div>
            </div>
            <p v-if="dup.item_b?.id" class="text-[10px] text-muted font-mono truncate">{{ dup.item_b.id }}</p>
          </div>
        </div>

        <!-- Resolution actions (only for pending) -->
        <div v-if="dup.status === 'pending'" class="flex flex-wrap gap-2 px-4 py-3 border-t border-default bg-muted/10">
          <UButton
            icon="i-lucide-trash-2"
            label="Remove A"
            size="xs"
            color="error"
            variant="outline"
            :loading="resolvingId === dup.id"
            @click="resolve(dup.id, 'remove_a')"
          />
          <UButton
            icon="i-lucide-trash-2"
            label="Remove B"
            size="xs"
            color="error"
            variant="outline"
            :loading="resolvingId === dup.id"
            @click="resolve(dup.id, 'remove_b')"
          />
          <UButton
            icon="i-lucide-copy"
            label="Keep Both"
            size="xs"
            color="success"
            variant="outline"
            :loading="resolvingId === dup.id"
            @click="resolve(dup.id, 'keep_both')"
          />
          <UButton
            icon="i-lucide-eye-off"
            label="Ignore"
            size="xs"
            color="neutral"
            variant="ghost"
            :loading="resolvingId === dup.id"
            @click="resolve(dup.id, 'ignore')"
          />
        </div>
        <!-- Resolved info -->
        <div v-else class="px-4 py-2 border-t border-default text-xs text-muted">
          Resolved by {{ dup.resolved_by || 'admin' }}
          <span v-if="dup.resolved_at"> · {{ formatRelativeDate(dup.resolved_at) }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

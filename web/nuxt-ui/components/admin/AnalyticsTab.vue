<script setup lang="ts">
import type { AnalyticsSummary, DailyStats, TopMediaItem, EventStats, EventTypeCounts, AnalyticsEvent } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'
import { formatWatchTime } from '~/utils/format'

const analyticsApi = useAnalyticsApi()
const adminApi = useAdminApi()
const toast = useToast()

const summary = ref<AnalyticsSummary | null>(null)
const daily = ref<DailyStats[]>([])
const dailyMaxViews = computed(() => Math.max(1, ...daily.value.map(d => d.total_views ?? 0)))
const topMedia = ref<TopMediaItem[]>([])
const eventStats = ref<EventStats | null>(null)
const eventTypeCounts = ref<EventTypeCounts | null>(null)
const loading = ref(true)
const period = ref('7d')

// Event drill-down
const drillMode = ref<'type' | 'media' | 'user'>('type')
const drillType = ref('')
const drillMediaId = ref('')
const drillUserId = ref('')
const drillEvents = ref<AnalyticsEvent[]>([])
const drillLoading = ref(false)

async function drillByType() {
  if (!drillType.value.trim()) return
  drillLoading.value = true
  try {
    drillEvents.value = (await analyticsApi.getEventsByType(drillType.value.trim(), 50)) ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { drillLoading.value = false }
}

async function drillByMedia() {
  if (!drillMediaId.value.trim()) return
  drillLoading.value = true
  try {
    drillEvents.value = (await analyticsApi.getEventsByMedia(drillMediaId.value.trim(), 50)) ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { drillLoading.value = false }
}

async function drillByUser() {
  if (!drillUserId.value.trim()) return
  drillLoading.value = true
  try {
    drillEvents.value = (await analyticsApi.getEventsByUser(drillUserId.value.trim(), 50)) ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { drillLoading.value = false }
}

// formatWatchTime imported from ~/utils/format

async function load() {
  loading.value = true
  try {
    const [s, d, t, es, etc] = await Promise.allSettled([
      analyticsApi.getSummary(period.value),
      analyticsApi.getDaily(period.value === '7d' ? 7 : period.value === '30d' ? 30 : undefined),
      analyticsApi.getTopMedia(20),
      analyticsApi.getEventStats(),
      analyticsApi.getEventTypeCounts(),
    ])
    if (s.status === 'fulfilled') summary.value = s.value
    if (d.status === 'fulfilled') daily.value = d.value ?? []
    if (t.status === 'fulfilled') topMedia.value = t.value ?? []
    if (es.status === 'fulfilled') eventStats.value = es.value
    if (etc.status === 'fulfilled') eventTypeCounts.value = etc.value
  } finally {
    loading.value = false
  }
}

watch(period, load)
onMounted(load)
</script>

<template>
  <div class="space-y-6">
    <!-- Period selector -->
    <div class="flex items-center justify-between">
      <h3 class="font-semibold text-highlighted">Analytics Overview</h3>
      <div class="flex items-center gap-2">
        <UButtonGroup>
          <UButton
            v-for="p in [{ label: 'Today', value: 'today' }, { label: '7 Days', value: '7d' }, { label: '30 Days', value: '30d' }, { label: 'All Time', value: 'all' }]"
            :key="p.value"
            :label="p.label"
            :variant="period === p.value ? 'solid' : 'outline'"
            :color="period === p.value ? 'primary' : 'neutral'"
            size="xs"
            @click="period = p.value"
          />
        </UButtonGroup>
        <UButton
          icon="i-lucide-download"
          label="Export CSV"
          variant="outline"
          color="neutral"
          size="xs"
          tag="a"
          :href="analyticsApi.exportCsv()"
          download
        />
      </div>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="flex justify-center py-8">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-primary" />
    </div>

    <!-- Summary cards -->
    <div v-if="summary" class="grid grid-cols-2 sm:grid-cols-4 gap-3">
      <UCard
        v-for="item in [
          { label: 'Total Views', value: (summary.total_views ?? 0).toLocaleString(), icon: 'i-lucide-eye' },
          { label: 'Today Views', value: (summary.today_views ?? 0).toLocaleString(), icon: 'i-lucide-calendar' },
          { label: 'Unique Clients', value: (summary.unique_clients ?? 0).toLocaleString(), icon: 'i-lucide-users' },
          { label: 'Active Sessions', value: (summary.active_sessions ?? 0).toLocaleString(), icon: 'i-lucide-activity' },
        ]"
        :key="item.label"
        :ui="{ body: 'p-4' }"
      >
        <div class="flex items-center gap-2">
          <UIcon :name="item.icon" class="size-4 text-muted" />
          <div>
            <p class="text-lg font-bold text-highlighted">{{ item.value }}</p>
            <p class="text-xs text-muted">{{ item.label }}</p>
          </div>
        </div>
      </UCard>
    </div>

    <!-- Event stats -->
    <div v-if="eventStats || eventTypeCounts" class="grid grid-cols-1 sm:grid-cols-2 gap-3">
      <UCard v-if="eventStats">
        <template #header>
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-activity" class="size-4" />
            Event Stats
          </div>
        </template>
        <div class="space-y-1 text-sm">
          <div class="flex justify-between">
            <span class="text-muted">Total Events</span>
            <span class="font-medium">{{ (eventStats.total_events ?? 0).toLocaleString() }}</span>
          </div>
          <div v-for="(count, type) in eventStats.event_counts" :key="String(type)" class="flex justify-between">
            <span class="text-muted capitalize">{{ String(type).replace(/_/g, ' ') }}</span>
            <span>{{ (count as number).toLocaleString() }}</span>
          </div>
        </div>
      </UCard>

      <UCard v-if="eventTypeCounts && Object.keys(eventTypeCounts).length > 0">
        <template #header>
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-bar-chart" class="size-4" />
            Event Types
          </div>
        </template>
        <div class="space-y-1.5">
          <div v-for="(count, type) in eventTypeCounts" :key="String(type)" class="flex items-center gap-2">
            <span class="text-sm text-muted capitalize flex-1">{{ String(type).replace(/_/g, ' ') }}</span>
            <span class="text-sm font-medium">{{ (count as number).toLocaleString() }}</span>
          </div>
        </div>
      </UCard>
    </div>

    <!-- Event drill-down -->
    <UCard>
      <template #header>
        <div class="flex items-center justify-between">
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-search" class="size-4" />
            Event Drill-Down
          </div>
          <UButtonGroup>
            <UButton v-for="m in [{ label: 'By Type', value: 'type' }, { label: 'By Media', value: 'media' }, { label: 'By User', value: 'user' }]"
              :key="m.value" :label="m.label" size="xs"
              :variant="drillMode === m.value ? 'solid' : 'outline'"
              :color="drillMode === m.value ? 'primary' : 'neutral'"
              @click="drillMode = m.value as 'type' | 'media' | 'user'; drillEvents = []"
            />
          </UButtonGroup>
        </div>
      </template>
      <div v-if="drillMode === 'type'" class="flex gap-2">
        <UInput v-model="drillType" placeholder="Event type (e.g. view, play, download)" class="flex-1" @keyup.enter="drillByType" />
        <UButton :loading="drillLoading" icon="i-lucide-search" label="Search" :disabled="!drillType.trim()" @click="drillByType" />
      </div>
      <div v-else-if="drillMode === 'media'" class="flex gap-2">
        <UInput v-model="drillMediaId" placeholder="Media ID" class="flex-1" @keyup.enter="drillByMedia" />
        <UButton :loading="drillLoading" icon="i-lucide-search" label="Search" :disabled="!drillMediaId.trim()" @click="drillByMedia" />
      </div>
      <div v-else class="flex gap-2">
        <UInput v-model="drillUserId" placeholder="User ID" class="flex-1" @keyup.enter="drillByUser" />
        <UButton :loading="drillLoading" icon="i-lucide-search" label="Search" :disabled="!drillUserId.trim()" @click="drillByUser" />
      </div>
      <div v-if="drillLoading" class="flex justify-center py-4 mt-2">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
      </div>
      <div v-else-if="drillEvents.length > 0" class="mt-3 divide-y divide-default max-h-64 overflow-y-auto">
        <div v-for="ev in drillEvents" :key="ev.id" class="py-2 text-sm flex items-start gap-3">
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2 flex-wrap">
              <UBadge :label="ev.type" color="neutral" variant="subtle" size="xs" />
              <span v-if="ev.media_id" class="font-mono text-xs text-muted">{{ ev.media_id.slice(0, 8) }}…</span>
              <span v-if="ev.user_id" class="text-xs text-muted">{{ ev.user_id }}</span>
              <span v-if="ev.ip_address" class="text-xs text-muted">{{ ev.ip_address }}</span>
            </div>
            <p class="text-xs text-muted mt-0.5">{{ ev.timestamp ? new Date(ev.timestamp).toLocaleString() : '' }}</p>
            <pre v-if="ev.data && Object.keys(ev.data).length > 0" class="text-xs text-muted mt-1 bg-elevated rounded px-2 py-1 whitespace-pre-wrap break-all max-h-20 overflow-y-auto">{{ JSON.stringify(ev.data, null, 2) }}</pre>
          </div>
        </div>
      </div>
      <p v-else-if="!drillLoading && (drillType || drillMediaId || drillUserId) && drillEvents.length === 0" class="text-center py-4 text-muted text-sm mt-2">No events found.</p>
    </UCard>

    <!-- Top media -->
    <UCard v-if="topMedia.length > 0">
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-trending-up" class="size-4" />
          Top Media
        </div>
      </template>
      <UTable
        :data="topMedia"
        :columns="[
          { accessorKey: 'filename', header: 'Title' },
          { accessorKey: 'views', header: 'Views' },
        ]"
      >
        <template #filename-cell="{ row }">
          <span class="text-sm font-medium truncate max-w-xs block" :title="getDisplayTitle(row.original)">
            {{ getDisplayTitle(row.original) }}
          </span>
        </template>
        <template #views-cell="{ row }">
          <span class="text-sm">{{ (row.original.views ?? 0).toLocaleString() }}</span>
        </template>
      </UTable>
    </UCard>

    <!-- Daily breakdown -->
    <UCard v-if="daily.length > 0">
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-bar-chart-2" class="size-4" />
          Daily Breakdown
        </div>
      </template>

      <!-- CSS bar chart — views per day -->
      <div class="mb-4 space-y-1">
        <div
          v-for="row in daily.slice().reverse()"
          :key="row.date"
          class="flex items-center gap-2 text-xs"
        >
          <span class="w-24 shrink-0 font-mono text-muted text-right">{{ row.date }}</span>
          <div class="flex-1 bg-muted/20 rounded-full h-4 overflow-hidden">
            <div
              class="h-full rounded-full bg-primary transition-all duration-300"
              :style="{ width: dailyMaxViews > 0 ? `${Math.round(((row.total_views ?? 0) / dailyMaxViews) * 100)}%` : '0%' }"
            />
          </div>
          <span class="w-12 shrink-0 text-right text-muted">{{ (row.total_views ?? 0).toLocaleString() }}</span>
        </div>
      </div>

      <UTable
        :data="daily"
        :columns="[
          { accessorKey: 'date', header: 'Date' },
          { accessorKey: 'total_views', header: 'Views' },
          { accessorKey: 'unique_users', header: 'Unique Users' },
          { accessorKey: 'total_watch_time', header: 'Watch Time' },
        ]"
      >
        <template #date-cell="{ row }">
          <span class="text-sm font-mono">{{ row.original.date }}</span>
        </template>
        <template #total_views-cell="{ row }">{{ (row.original.total_views ?? 0).toLocaleString() }}</template>
        <template #unique_users-cell="{ row }">{{ (row.original.unique_users ?? 0).toLocaleString() }}</template>
        <template #total_watch_time-cell="{ row }">{{ formatWatchTime(row.original.total_watch_time) }}</template>
      </UTable>
    </UCard>
  </div>
</template>

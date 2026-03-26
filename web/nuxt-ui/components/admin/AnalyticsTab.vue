<script setup lang="ts">
import type { AnalyticsSummary, DailyStats, TopMediaItem, EventStats, EventTypeCounts } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'

const analyticsApi = useAnalyticsApi()
const adminApi = useAdminApi()
const toast = useToast()

const summary = ref<AnalyticsSummary | null>(null)
const daily = ref<DailyStats[]>([])
const topMedia = ref<TopMediaItem[]>([])
const eventStats = ref<EventStats | null>(null)
const eventTypeCounts = ref<EventTypeCounts | null>(null)
const loading = ref(true)
const period = ref('7d')

function formatTime(secs?: number): string {
  if (!secs) return '—'
  const h = Math.floor(secs / 3600)
  const m = Math.floor((secs % 3600) / 60)
  return h > 0 ? `${h}h ${m}m` : `${m}m`
}

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
        <template #total_watch_time-cell="{ row }">{{ formatTime(row.original.total_watch_time) }}</template>
      </UTable>
    </UCard>
  </div>
</template>

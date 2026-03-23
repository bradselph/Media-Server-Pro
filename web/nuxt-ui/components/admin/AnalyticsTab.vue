<script setup lang="ts">
import type { AnalyticsSummary, DailyStats, TopMediaItem } from '~/types/api'

const analyticsApi = useAnalyticsApi()
const adminApi = useAdminApi()
const toast = useToast()

const summary = ref<AnalyticsSummary | null>(null)
const daily = ref<DailyStats[]>([])
const topMedia = ref<TopMediaItem[]>([])
const loading = ref(true)
const period = ref('7d')

function formatBytes(bytes: number): string {
  if (!bytes) return '—'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / k ** i).toFixed(1)} ${sizes[i]}`
}

function formatTime(secs?: number): string {
  if (!secs) return '—'
  const h = Math.floor(secs / 3600)
  const m = Math.floor((secs % 3600) / 60)
  return h > 0 ? `${h}h ${m}m` : `${m}m`
}

async function load() {
  loading.value = true
  try {
    const [s, d, t] = await Promise.allSettled([
      analyticsApi.getSummary(period.value),
      analyticsApi.getDaily(period.value === '7d' ? 7 : period.value === '30d' ? 30 : undefined),
      analyticsApi.getTopMedia(20),
    ])
    if (s.status === 'fulfilled') summary.value = s.value
    if (d.status === 'fulfilled') daily.value = d.value ?? []
    if (t.status === 'fulfilled') topMedia.value = t.value ?? []
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
          :to="analyticsApi.exportCsv()"
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
          { label: 'Unique Viewers', value: (summary.unique_viewers ?? 0).toLocaleString(), icon: 'i-lucide-users' },
          { label: 'Total Watch Time', value: formatTime(summary.total_watch_time), icon: 'i-lucide-clock' },
          { label: 'Avg Watch Time', value: formatTime(summary.avg_watch_time), icon: 'i-lucide-timer' },
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
          { key: 'title', label: 'Title' },
          { key: 'views', label: 'Views' },
          { key: 'unique_viewers', label: 'Unique Viewers' },
          { key: 'avg_completion', label: 'Avg Completion' },
        ]"
      >
        <template #title-cell="{ row }">
          <span class="text-sm font-medium truncate max-w-xs block" :title="row.original.title">
            {{ row.original.title || row.original.media_id }}
          </span>
        </template>
        <template #views-cell="{ row }">
          <span class="text-sm">{{ (row.original.views ?? 0).toLocaleString() }}</span>
        </template>
        <template #unique_viewers-cell="{ row }">
          <span class="text-sm">{{ (row.original.unique_viewers ?? 0).toLocaleString() }}</span>
        </template>
        <template #avg_completion-cell="{ row }">
          <div class="flex items-center gap-2 min-w-24">
            <UProgress :value="Math.round((row.original.avg_completion ?? 0) * 100)" size="xs" class="flex-1" />
            <span class="text-xs">{{ Math.round((row.original.avg_completion ?? 0) * 100) }}%</span>
          </div>
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
          { key: 'date', label: 'Date' },
          { key: 'views', label: 'Views' },
          { key: 'unique_viewers', label: 'Unique Viewers' },
          { key: 'bandwidth', label: 'Bandwidth' },
        ]"
      >
        <template #date-cell="{ row }">
          <span class="text-sm font-mono">{{ row.original.date }}</span>
        </template>
        <template #views-cell="{ row }">{{ (row.original.views ?? 0).toLocaleString() }}</template>
        <template #unique_viewers-cell="{ row }">{{ (row.original.unique_viewers ?? 0).toLocaleString() }}</template>
        <template #bandwidth-cell="{ row }">{{ formatBytes(row.original.bandwidth) }}</template>
      </UTable>
    </UCard>
  </div>
</template>

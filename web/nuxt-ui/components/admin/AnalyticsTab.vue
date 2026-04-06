<script setup lang="ts">
import type { AnalyticsSummary, DailyStats, TopMediaItem, EventStats, EventTypeCounts, AnalyticsEvent, ContentPerformanceItem } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'
import { formatWatchTime } from '~/utils/format'

const analyticsApi = useAnalyticsApi()
const toast = useToast()

const summary = ref<AnalyticsSummary | null>(null)
const daily = ref<DailyStats[]>([])
const dailyMaxViews = computed(() => Math.max(1, ...daily.value.map(d => d.total_views ?? 0)))
const dailyReversed = computed(() => [...daily.value].reverse())
const topMedia = ref<TopMediaItem[]>([])
const contentPerf = ref<ContentPerformanceItem[]>([])
const eventStats = ref<EventStats | null>(null)
const eventTypeCounts = ref<EventTypeCounts | null>(null)
const loading = ref(true)
const period = ref('7d')

// Auto-refresh
const autoRefresh = ref(false)
let autoRefreshTimer: ReturnType<typeof setInterval> | null = null

function toggleAutoRefresh() {
  autoRefresh.value = !autoRefresh.value
  if (autoRefresh.value) {
    autoRefreshTimer = setInterval(load, 30_000)
  } else if (autoRefreshTimer) {
    clearInterval(autoRefreshTimer)
    autoRefreshTimer = null
  }
}

onUnmounted(() => {
  if (autoRefreshTimer) clearInterval(autoRefreshTimer)
})

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

// Computed: event type distribution for visual bar chart
const eventTypeEntries = computed(() => {
  const counts = eventTypeCounts.value
  if (!counts) return []
  const total = Object.values(counts).reduce((a, b) => a + b, 0)
  return Object.entries(counts)
    .sort((a, b) => b[1] - a[1])
    .map(([type, count]) => ({ type, count, pct: total > 0 ? (count / total * 100) : 0 }))
})

// Computed: hourly activity bars (from eventStats)
const hourlyMax = computed(() => Math.max(1, ...(eventStats.value?.hourly_events ?? [])))

// Recent activity from summary
const recentActivity = computed(() => summary.value?.recent_activity ?? [])

async function load() {
  loading.value = true
  try {
    const [s, d, t, cp, es, etc] = await Promise.allSettled([
      analyticsApi.getSummary(period.value),
      analyticsApi.getDaily(period.value === 'today' ? 1 : period.value === '7d' ? 7 : period.value === '30d' ? 30 : 90),
      analyticsApi.getTopMedia(20),
      analyticsApi.getContentPerformance(20),
      analyticsApi.getEventStats(),
      analyticsApi.getEventTypeCounts(),
    ])
    if (s.status === 'fulfilled') summary.value = s.value
    if (d.status === 'fulfilled') daily.value = d.value ?? []
    if (t.status === 'fulfilled') topMedia.value = t.value ?? []
    if (cp.status === 'fulfilled') contentPerf.value = cp.value ?? []
    if (es.status === 'fulfilled') eventStats.value = es.value
    if (etc.status === 'fulfilled') eventTypeCounts.value = etc.value
  } finally {
    loading.value = false
  }
}

watch(period, load)
onMounted(load)

function formatPct(v: number): string {
  return v >= 1 ? `${Math.round(v)}%` : v > 0 ? `${v.toFixed(1)}%` : '0%'
}

const EVENT_COLORS: Record<string, string> = {
  play: 'bg-green-500', pause: 'bg-yellow-500', resume: 'bg-blue-500',
  seek: 'bg-purple-500', complete: 'bg-emerald-500', error: 'bg-red-500',
  quality_change: 'bg-cyan-500', view: 'bg-indigo-500', playback: 'bg-teal-500',
}
</script>

<template>
  <div class="space-y-6">
    <!-- Header with period selector and controls -->
    <div class="flex items-center justify-between flex-wrap gap-2">
      <h3 class="font-semibold text-highlighted">Analytics Overview</h3>
      <div class="flex items-center gap-2 flex-wrap">
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
          :icon="autoRefresh ? 'i-lucide-pause' : 'i-lucide-refresh-cw'"
          :label="autoRefresh ? 'Stop' : 'Auto'"
          :variant="autoRefresh ? 'solid' : 'outline'"
          :color="autoRefresh ? 'primary' : 'neutral'"
          size="xs"
          @click="toggleAutoRefresh"
        />
        <UButton
          icon="i-lucide-download"
          label="Export CSV"
          variant="outline"
          color="neutral"
          size="xs"
          tag="a"
          :href="analyticsApi.exportCsv(period)"
          download
        />
      </div>
    </div>

    <!-- Loading -->
    <div v-if="loading && !summary" class="flex justify-center py-8">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-primary" />
    </div>

    <!-- Summary cards (6 metrics) -->
    <div v-if="summary" class="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
      <UCard
        v-for="item in [
          { label: 'Total Views', value: (summary.total_views ?? 0).toLocaleString(), icon: 'i-lucide-eye' },
          { label: 'Today Views', value: (summary.today_views ?? 0).toLocaleString(), icon: 'i-lucide-calendar' },
          { label: 'Watch Time', value: formatWatchTime(summary.total_watch_time), icon: 'i-lucide-clock' },
          { label: 'Total Events', value: (summary.total_events ?? 0).toLocaleString(), icon: 'i-lucide-zap' },
          { label: 'Unique Clients', value: (summary.unique_clients ?? 0).toLocaleString(), icon: 'i-lucide-users' },
          { label: 'Active Sessions', value: (summary.active_sessions ?? 0).toLocaleString(), icon: 'i-lucide-activity' },
        ]"
        :key="item.label"
        :ui="{ body: 'p-3' }"
      >
        <div class="flex items-center gap-2">
          <UIcon :name="item.icon" class="size-4 text-muted shrink-0" />
          <div class="min-w-0">
            <p class="text-lg font-bold text-highlighted truncate">{{ item.value }}</p>
            <p class="text-xs text-muted">{{ item.label }}</p>
          </div>
        </div>
      </UCard>
    </div>

    <!-- Today's Traffic Breakdown -->
    <div v-if="summary && (summary.today_logins || summary.today_logins_failed || summary.today_registrations || summary.today_age_gate_passes || summary.today_downloads || summary.today_searches)" class="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
      <UCard
        v-for="item in [
          { label: 'Logins', value: summary.today_logins ?? 0, icon: 'i-lucide-log-in', color: 'text-success' },
          { label: 'Failed Logins', value: summary.today_logins_failed ?? 0, icon: 'i-lucide-shield-alert', color: 'text-error' },
          { label: 'Registrations', value: summary.today_registrations ?? 0, icon: 'i-lucide-user-plus', color: 'text-primary' },
          { label: 'Age Gate', value: summary.today_age_gate_passes ?? 0, icon: 'i-lucide-shield-check', color: 'text-warning' },
          { label: 'Downloads', value: summary.today_downloads ?? 0, icon: 'i-lucide-download', color: 'text-info' },
          { label: 'Searches', value: summary.today_searches ?? 0, icon: 'i-lucide-search', color: 'text-muted' },
        ].filter(i => i.value > 0)"
        :key="item.label"
        :ui="{ body: 'p-3' }"
      >
        <div class="flex items-center gap-2">
          <UIcon :name="item.icon" :class="[item.color, 'size-4 shrink-0']" />
          <div class="min-w-0">
            <p class="text-lg font-bold text-highlighted truncate">{{ item.value.toLocaleString() }}</p>
            <p class="text-xs text-muted">{{ item.label }}</p>
          </div>
        </div>
      </UCard>
    </div>

    <!-- Charts row: Event Distribution + Hourly Activity -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <!-- Event Type Distribution -->
      <UCard v-if="eventTypeEntries.length > 0">
        <template #header>
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-pie-chart" class="size-4" />
            Event Distribution
          </div>
        </template>
        <div class="space-y-2">
          <div v-for="entry in eventTypeEntries" :key="entry.type" class="flex items-center gap-2">
            <span class="w-24 shrink-0 text-xs text-muted capitalize truncate" :title="entry.type.replace(/_/g, ' ')">
              {{ entry.type.replace(/_/g, ' ') }}
            </span>
            <div class="flex-1 bg-muted/20 rounded-full h-5 overflow-hidden relative">
              <div
                :class="EVENT_COLORS[entry.type] ?? 'bg-gray-500'"
                class="h-full rounded-full transition-all duration-500"
                :style="{ width: `${entry.pct}%` }"
              />
            </div>
            <span class="w-16 shrink-0 text-right text-xs text-muted">
              {{ entry.count.toLocaleString() }}
            </span>
            <span class="w-10 shrink-0 text-right text-xs font-medium">
              {{ formatPct(entry.pct) }}
            </span>
          </div>
        </div>
      </UCard>

      <!-- Hourly Activity (today) -->
      <UCard v-if="eventStats?.hourly_events">
        <template #header>
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-clock" class="size-4" />
            Today's Hourly Activity
          </div>
        </template>
        <div class="flex items-end gap-px h-32">
          <div
            v-for="(count, hour) in eventStats.hourly_events"
            :key="hour"
            class="flex-1 flex flex-col items-center justify-end gap-0.5 group relative"
          >
            <div
              class="w-full rounded-t bg-primary/70 hover:bg-primary transition-colors min-h-[2px]"
              :style="{ height: `${Math.max(2, (count / hourlyMax) * 100)}%` }"
              :title="`${String(hour).padStart(2, '0')}:00 — ${count} events`"
            />
            <span v-if="hour % 3 === 0" class="text-[10px] text-muted leading-none">{{ String(hour).padStart(2, '0') }}</span>
            <span v-else class="text-[10px] text-transparent leading-none">00</span>
          </div>
        </div>
      </UCard>
    </div>

    <!-- Content Performance -->
    <UCard v-if="contentPerf.length > 0">
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-bar-chart-3" class="size-4" />
          Content Performance
        </div>
      </template>
      <div class="overflow-x-auto">
        <UTable
          :data="contentPerf"
          :columns="[
            { accessorKey: 'filename', header: 'Title' },
            { accessorKey: 'total_views', header: 'Views' },
            { accessorKey: 'total_completions', header: 'Completions' },
            { accessorKey: 'completion_rate', header: 'Completion %' },
            { accessorKey: 'avg_watch_duration', header: 'Avg Watch' },
            { accessorKey: 'unique_viewers', header: 'Unique' },
          ]"
        >
          <template #filename-cell="{ row }">
            <span class="text-sm font-medium truncate max-w-xs block" :title="row.original.filename">
              {{ row.original.filename }}
            </span>
          </template>
          <template #total_views-cell="{ row }">{{ (row.original.total_views ?? 0).toLocaleString() }}</template>
          <template #total_completions-cell="{ row }">{{ (row.original.total_completions ?? 0).toLocaleString() }}</template>
          <template #completion_rate-cell="{ row }">
            <div class="flex items-center gap-2">
              <div class="w-16 bg-muted/20 rounded-full h-2 overflow-hidden">
                <div class="h-full rounded-full bg-emerald-500" :style="{ width: `${Math.min(100, (row.original.completion_rate ?? 0) * 100)}%` }" />
              </div>
              <span class="text-xs">{{ formatPct((row.original.completion_rate ?? 0) * 100) }}</span>
            </div>
          </template>
          <template #avg_watch_duration-cell="{ row }">{{ formatWatchTime(row.original.avg_watch_duration) }}</template>
          <template #unique_viewers-cell="{ row }">{{ (row.original.unique_viewers ?? 0).toLocaleString() }}</template>
        </UTable>
      </div>
    </UCard>

    <!-- Recent Activity Feed -->
    <UCard v-if="recentActivity.length > 0">
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-radio" class="size-4" />
          Recent Activity
          <span v-if="autoRefresh" class="relative flex h-2 w-2">
            <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75" />
            <span class="relative inline-flex rounded-full h-2 w-2 bg-green-500" />
          </span>
        </div>
      </template>
      <div class="divide-y divide-default max-h-48 overflow-y-auto">
        <div v-for="(a, i) in recentActivity" :key="i" class="py-1.5 flex items-center gap-2 text-sm">
          <UBadge :label="a.type" :color="a.type === 'error' ? 'error' : 'neutral'" variant="subtle" size="xs" />
          <span class="flex-1 truncate text-muted" :title="a.filename">{{ a.filename }}</span>
          <span class="text-xs text-muted shrink-0">{{ a.timestamp ? new Date(a.timestamp * 1000).toLocaleTimeString() : '' }}</span>
        </div>
      </div>
    </UCard>

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
        <UInput v-model="drillType" placeholder="Event type (e.g. view, play, complete)" class="flex-1" @keyup.enter="drillByType" />
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
          Top Media by Views
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
          v-for="row in dailyReversed"
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

    <!-- Empty state -->
    <div v-if="!loading && !summary" class="text-center py-12 text-muted">
      <UIcon name="i-lucide-bar-chart" class="size-10 mb-3 mx-auto opacity-40" />
      <p class="text-lg font-medium">No analytics data</p>
      <p class="text-sm mt-1">Analytics events will appear here as users interact with media.</p>
    </div>
  </div>
</template>

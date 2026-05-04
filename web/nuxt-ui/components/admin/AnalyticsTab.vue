<script setup lang="ts">
import type { AnalyticsSummary, DailyStats, TopMediaItem, EventStats, EventTypeCounts, AnalyticsEvent, ContentPerformanceItem, UserAnalytics } from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'
import { formatWatchTime, formatBytes } from '~/utils/format'

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

// Per-user aggregate, populated on demand from the drill-down's "By User" tab
// so admins can see totals/watch-time/etc. for a specific user without
// scrolling raw events.
const userAggregate = ref<UserAnalytics | null>(null)
const userAggregateLoading = ref(false)

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
  userAggregate.value = null
  try {
    // Fire both the raw-events query (event drill stream) and the aggregate
    // (per-user totals card) in parallel so admins see both views. The
    // aggregate endpoint takes a username, the events endpoint takes a user
    // ID — same URL string handles both because most operators use either.
    drillEvents.value = (await analyticsApi.getEventsByUser(drillUserId.value.trim(), 50)) ?? []
    void loadUserAggregate(drillUserId.value.trim())
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally { drillLoading.value = false }
}

async function loadUserAggregate(usernameOrID: string) {
  if (!usernameOrID) return
  userAggregateLoading.value = true
  try {
    userAggregate.value = await analyticsApi.getUserAnalytics(usernameOrID)
  } catch {
    // Aggregate is best-effort; non-existent users or non-username queries
    // (raw user IDs) will 404. Don't toast — the events list itself surfaces
    // useful info, and an empty aggregate is a valid signal.
    userAggregate.value = null
  } finally {
    userAggregateLoading.value = false
  }
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

function periodToDays(p: string): number {
  if (p === 'today') return 1
  if (p === '7d') return 7
  if (p === '30d') return 30
  return 90
}

async function load() {
  loading.value = true
  const capturedPeriod = period.value
  try {
    const [s, d, t, cp, es, etc] = await Promise.allSettled([
      analyticsApi.getSummary(period.value),
      analyticsApi.getDaily(periodToDays(period.value)),
      analyticsApi.getTopMedia(20),
      analyticsApi.getContentPerformance(20),
      analyticsApi.getEventStats(),
      analyticsApi.getEventTypeCounts(),
    ])
    if (period.value !== capturedPeriod) return
    if (s.status === 'fulfilled') summary.value = s.value
    if (d.status === 'fulfilled') daily.value = d.value ?? []
    if (t.status === 'fulfilled') topMedia.value = t.value ?? []
    if (cp.status === 'fulfilled') contentPerf.value = cp.value ?? []
    if (es.status === 'fulfilled') eventStats.value = es.value
    if (etc.status === 'fulfilled') eventTypeCounts.value = etc.value
    const failed = [s, d, t, cp, es, etc].filter(r => r.status === 'rejected')
    if (failed.length) toast.add({ title: 'Some analytics data failed to load', color: 'warning', icon: 'i-lucide-alert-triangle' })
  } finally {
    loading.value = false
  }
}

watch(period, load)
onMounted(load)

function formatPct(v: number): string {
  if (v >= 1) return `${Math.round(v)}%`
  if (v > 0) return `${v.toFixed(1)}%`
  return '0%'
}

const EVENT_COLORS: Record<string, string> = {
  play: 'bg-green-500', pause: 'bg-yellow-500', resume: 'bg-blue-500',
  seek: 'bg-purple-500', complete: 'bg-emerald-500', error: 'bg-red-500',
  quality_change: 'bg-cyan-500', view: 'bg-indigo-500', playback: 'bg-teal-500',
  favorite_add: 'bg-pink-500', favorite_remove: 'bg-pink-300', rating_set: 'bg-amber-500',
  playlist_create: 'bg-sky-500', playlist_delete: 'bg-sky-300', playlist_item_add: 'bg-sky-400',
  upload_success: 'bg-lime-500', upload_failed: 'bg-orange-500',
  password_change: 'bg-violet-500', account_delete: 'bg-rose-500',
  hls_start: 'bg-teal-400', hls_error: 'bg-red-400',
  media_deleted: 'bg-red-300', api_token_create: 'bg-fuchsia-500',
  api_token_revoke: 'bg-fuchsia-300', admin_action: 'bg-slate-500',
  server_error: 'bg-red-600',
}

// Today's traffic metrics — split into semantic groups so a busy day doesn't
// just produce a wall of identical cards. Each entry is hidden when the count
// is zero (filtered by template) so admins only see what's actually happening.
type TrafficGroup = { title: string; items: { label: string; value: number; icon: string; color: string }[] }
const trafficGroups = computed<TrafficGroup[]>(() => {
  const s = summary.value
  if (!s) return []
  return [
    {
      title: 'Auth',
      items: [
        { label: 'Logins', value: s.today_logins ?? 0, icon: 'i-lucide-log-in', color: 'text-success' },
        { label: 'Failed Logins', value: s.today_logins_failed ?? 0, icon: 'i-lucide-shield-alert', color: 'text-error' },
        { label: 'Logouts', value: s.today_logouts ?? 0, icon: 'i-lucide-log-out', color: 'text-muted' },
        { label: 'Registrations', value: s.today_registrations ?? 0, icon: 'i-lucide-user-plus', color: 'text-primary' },
        { label: 'Age Gate', value: s.today_age_gate_passes ?? 0, icon: 'i-lucide-shield-check', color: 'text-warning' },
        { label: 'Password Changes', value: s.today_password_changes ?? 0, icon: 'i-lucide-key', color: 'text-info' },
        { label: 'Account Deletions', value: s.today_account_deletions ?? 0, icon: 'i-lucide-user-x', color: 'text-error' },
      ],
    },
    {
      title: 'Content',
      items: [
        { label: 'Downloads', value: s.today_downloads ?? 0, icon: 'i-lucide-download', color: 'text-info' },
        { label: 'Searches', value: s.today_searches ?? 0, icon: 'i-lucide-search', color: 'text-muted' },
        { label: 'Favorites Added', value: s.today_favorites_added ?? 0, icon: 'i-lucide-heart', color: 'text-pink-500' },
        { label: 'Favorites Removed', value: s.today_favorites_removed ?? 0, icon: 'i-lucide-heart-off', color: 'text-muted' },
        { label: 'Ratings Set', value: s.today_ratings_set ?? 0, icon: 'i-lucide-star', color: 'text-amber-500' },
        { label: 'Uploads OK', value: s.today_uploads_succeeded ?? 0, icon: 'i-lucide-upload', color: 'text-success' },
        { label: 'Uploads Failed', value: s.today_uploads_failed ?? 0, icon: 'i-lucide-alert-triangle', color: 'text-warning' },
      ],
    },
    {
      title: 'Playlists & Streaming',
      items: [
        { label: 'Playlists Created', value: s.today_playlists_created ?? 0, icon: 'i-lucide-list-plus', color: 'text-sky-500' },
        { label: 'Playlists Deleted', value: s.today_playlists_deleted ?? 0, icon: 'i-lucide-list-x', color: 'text-muted' },
        { label: 'Items Added', value: s.today_playlist_items_added ?? 0, icon: 'i-lucide-list', color: 'text-sky-400' },
        { label: 'HLS Starts', value: s.today_hls_starts ?? 0, icon: 'i-lucide-play-circle', color: 'text-teal-500' },
        { label: 'HLS Errors', value: s.today_hls_errors ?? 0, icon: 'i-lucide-x-circle', color: 'text-error' },
      ],
    },
    {
      title: 'Admin & System',
      items: [
        { label: 'Admin Actions', value: s.today_admin_actions ?? 0, icon: 'i-lucide-shield', color: 'text-slate-500' },
        { label: 'Bulk Deletes', value: s.today_bulk_deletes ?? 0, icon: 'i-lucide-trash', color: 'text-error' },
        { label: 'Bulk Updates', value: s.today_bulk_updates ?? 0, icon: 'i-lucide-edit', color: 'text-warning' },
        { label: 'Role Changes', value: s.today_user_role_changes ?? 0, icon: 'i-lucide-user-cog', color: 'text-primary' },
        { label: 'Prefs Changed', value: s.today_preferences_changes ?? 0, icon: 'i-lucide-sliders', color: 'text-muted' },
        { label: 'Tokens Created', value: s.today_api_tokens_created ?? 0, icon: 'i-lucide-key-round', color: 'text-fuchsia-500' },
        { label: 'Tokens Revoked', value: s.today_api_tokens_revoked ?? 0, icon: 'i-lucide-key-square', color: 'text-muted' },
        { label: 'Media Deletions', value: s.today_media_deletions ?? 0, icon: 'i-lucide-trash-2', color: 'text-error' },
        { label: 'Server Errors', value: s.today_server_errors ?? 0, icon: 'i-lucide-alert-octagon', color: 'text-red-600' },
        { label: 'Mature Blocked', value: s.today_mature_blocked ?? 0, icon: 'i-lucide-shield-x', color: 'text-warning' },
        { label: 'Permission Denied', value: s.today_permission_denied ?? 0, icon: 'i-lucide-lock', color: 'text-error' },
      ],
    },
  ]
})

// Server-health summary — surfaces failure-side counters in one place so
// operators can see at a glance whether the server is misbehaving today
// without scanning the whole 24-card breakdown.
const serverHealth = computed(() => {
  const s = summary.value
  if (!s) return null
  const errors = (s.today_server_errors ?? 0) + (s.today_hls_errors ?? 0) + (s.today_uploads_failed ?? 0)
  const blocks = (s.today_mature_blocked ?? 0) + (s.today_permission_denied ?? 0) + (s.today_logins_failed ?? 0)
  return {
    errors,
    blocks,
    serverErrors: s.today_server_errors ?? 0,
    hlsErrors: s.today_hls_errors ?? 0,
    uploadFails: s.today_uploads_failed ?? 0,
    matureBlocked: s.today_mature_blocked ?? 0,
    permDenied: s.today_permission_denied ?? 0,
    failedLogins: s.today_logins_failed ?? 0,
    healthy: errors === 0,
  }
})

// Whether ANY traffic metric has activity today — used to gate the whole
// breakdown section so a fresh DB doesn't render a long empty header.
const hasTrafficActivity = computed(() =>
  trafficGroups.value.some(g => g.items.some(it => it.value > 0))
)
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

    <!-- Summary cards (8 metrics — added today bandwidth + active streams). -->
    <div v-if="summary" class="grid grid-cols-2 sm:grid-cols-4 lg:grid-cols-8 gap-3">
      <UCard
        v-for="item in [
          { label: 'Total Views', value: (summary.total_views ?? 0).toLocaleString(), icon: 'i-lucide-eye' },
          { label: 'Today Views', value: (summary.today_views ?? 0).toLocaleString(), icon: 'i-lucide-calendar' },
          { label: 'Watch Time', value: formatWatchTime(summary.total_watch_time), icon: 'i-lucide-clock' },
          { label: 'Today Bandwidth', value: formatBytes(summary.today_bytes_served ?? 0), icon: 'i-lucide-network' },
          { label: 'Streams Today', value: (summary.today_stream_starts ?? 0).toLocaleString(), icon: 'i-lucide-play-circle' },
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

    <!-- Server Health banner — only shows when something abnormal is happening
         today. Splits into "errors" (bad) and "blocks" (security signal). -->
    <UAlert
      v-if="serverHealth && (serverHealth.errors > 0 || serverHealth.blocks > 0)"
      :color="serverHealth.errors > 0 ? 'error' : 'warning'"
      variant="subtle"
      :icon="serverHealth.errors > 0 ? 'i-lucide-alert-octagon' : 'i-lucide-shield-alert'"
      :title="serverHealth.errors > 0 ? `Server health: ${serverHealth.errors} error(s) today` : `Security activity: ${serverHealth.blocks} block(s) today`"
    >
      <template #description>
        <div class="text-xs flex flex-wrap gap-x-4 gap-y-1 mt-1">
          <span v-if="serverHealth.serverErrors > 0">5xx responses: <strong>{{ serverHealth.serverErrors }}</strong></span>
          <span v-if="serverHealth.hlsErrors > 0">HLS errors: <strong>{{ serverHealth.hlsErrors }}</strong></span>
          <span v-if="serverHealth.uploadFails > 0">Upload failures: <strong>{{ serverHealth.uploadFails }}</strong></span>
          <span v-if="serverHealth.failedLogins > 0">Failed logins: <strong>{{ serverHealth.failedLogins }}</strong></span>
          <span v-if="serverHealth.matureBlocked > 0">Mature blocked: <strong>{{ serverHealth.matureBlocked }}</strong></span>
          <span v-if="serverHealth.permDenied > 0">Permission denied: <strong>{{ serverHealth.permDenied }}</strong></span>
        </div>
      </template>
    </UAlert>

    <!-- Today's Traffic Breakdown — grouped, only shows non-zero counters so
         a quiet day stays visually quiet instead of rendering 24 zero cards. -->
    <div v-if="summary && hasTrafficActivity" class="space-y-3">
      <template v-for="group in trafficGroups" :key="group.title">
        <div v-if="group.items.some(i => i.value > 0)">
          <h4 class="text-xs font-semibold uppercase tracking-wide text-muted mb-2">{{ group.title }}</h4>
          <div class="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
            <UCard
              v-for="item in group.items.filter(i => i.value > 0)"
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
        </div>
      </template>
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
          <UBadge :label="a.type" :color="a.type === 'error' || a.type === 'server_error' || a.type === 'login_failed' ? 'error' : 'neutral'" variant="subtle" size="xs" />
          <span class="flex-1 truncate text-muted">
            <!-- Media events: filename. Auth/admin events: username + IP.
                 Both display to keep the feed scannable. -->
            <span v-if="a.filename" :title="a.filename">{{ a.filename }}</span>
            <span v-else-if="a.username" class="font-medium">{{ a.username }}</span>
            <span v-else-if="a.ip_address" class="font-mono text-xs">{{ a.ip_address }}</span>
            <span v-else class="italic">(system)</span>
            <span v-if="a.username && a.ip_address" class="ml-2 font-mono text-xs text-muted/70">{{ a.ip_address }}</span>
          </span>
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
        <UInput v-model="drillUserId" placeholder="Username (preferred) or User ID" class="flex-1" @keyup.enter="drillByUser" />
        <UButton :loading="drillLoading" icon="i-lucide-search" label="Search" :disabled="!drillUserId.trim()" @click="drillByUser" />
      </div>

      <!-- Per-user aggregate (only renders when the lookup-by-username
           endpoint resolved). Shows totals plus first/last seen so admins can
           gauge engagement without scrolling raw events. -->
      <div v-if="drillMode === 'user' && (userAggregate || userAggregateLoading)" class="mt-3">
        <div v-if="userAggregateLoading" class="flex justify-center py-3">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-4 text-muted" />
        </div>
        <div v-else-if="userAggregate" class="space-y-2">
          <div class="grid grid-cols-2 sm:grid-cols-4 lg:grid-cols-6 gap-2">
            <UCard
              v-for="m in [
                { label: 'Views', value: userAggregate.total_views, icon: 'i-lucide-eye' },
                { label: 'Playbacks', value: userAggregate.total_playbacks, icon: 'i-lucide-play' },
                { label: 'Completions', value: userAggregate.total_completions, icon: 'i-lucide-check-circle' },
                { label: 'Watch Time', value: userAggregate.total_watch_time, icon: 'i-lucide-clock', isTime: true },
                { label: 'Downloads', value: userAggregate.total_downloads, icon: 'i-lucide-download' },
                { label: 'Searches', value: userAggregate.total_searches, icon: 'i-lucide-search' },
                { label: 'Favorites +', value: userAggregate.favorites_added, icon: 'i-lucide-heart' },
                { label: 'Favorites −', value: userAggregate.favorites_removed, icon: 'i-lucide-heart-off' },
                { label: 'Ratings', value: userAggregate.ratings_set, icon: 'i-lucide-star' },
                { label: 'Playlists +', value: userAggregate.playlists_created, icon: 'i-lucide-list-plus' },
                { label: 'Uploads OK', value: userAggregate.uploads_succeeded, icon: 'i-lucide-upload' },
                { label: 'Uploads Fail', value: userAggregate.uploads_failed, icon: 'i-lucide-alert-triangle' },
                { label: 'Logins', value: userAggregate.logins, icon: 'i-lucide-log-in' },
                { label: 'Failed Logins', value: userAggregate.logins_failed, icon: 'i-lucide-shield-alert' },
                { label: 'Logouts', value: userAggregate.logouts, icon: 'i-lucide-log-out' },
                { label: 'Unique Media', value: userAggregate.unique_media, icon: 'i-lucide-clapperboard' },
              ]"
              :key="m.label"
              :ui="{ body: 'p-2' }"
            >
              <div class="flex items-center gap-2">
                <UIcon :name="m.icon" class="size-3.5 text-muted shrink-0" />
                <div class="min-w-0">
                  <p class="text-sm font-bold text-highlighted truncate">
                    {{ m.isTime ? formatWatchTime(m.value) : (m.value ?? 0).toLocaleString() }}
                  </p>
                  <p class="text-[10px] text-muted leading-tight">{{ m.label }}</p>
                </div>
              </div>
            </UCard>
          </div>
          <div class="flex items-center gap-4 text-xs text-muted flex-wrap">
            <span v-if="userAggregate.first_seen">First seen: {{ new Date(userAggregate.first_seen).toLocaleString() }}</span>
            <span v-if="userAggregate.last_seen">Last seen: {{ new Date(userAggregate.last_seen).toLocaleString() }}</span>
            <span v-if="userAggregate.most_viewed_media_id">
              Most-viewed:
              <span class="font-mono">{{ userAggregate.most_viewed_media_id.slice(0, 8) }}…</span>
              <span class="ml-1">({{ userAggregate.most_viewed_count }})</span>
            </span>
            <span>Total events scanned: {{ userAggregate.total_events.toLocaleString() }}</span>
          </div>
        </div>
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
          { accessorKey: 'logins', header: 'Logins' },
          { accessorKey: 'registrations', header: 'Signups' },
          { accessorKey: 'downloads', header: 'Downloads' },
          { accessorKey: 'searches', header: 'Searches' },
          { accessorKey: 'uploads_succeeded', header: 'Uploads' },
          { accessorKey: 'favorites_added', header: 'Favorites' },
          { accessorKey: 'ratings_set', header: 'Ratings' },
          { accessorKey: 'hls_starts', header: 'HLS' },
          { accessorKey: 'admin_actions', header: 'Admin' },
        ]"
      >
        <template #date-cell="{ row }">
          <span class="text-sm font-mono">{{ row.original.date }}</span>
        </template>
        <template #total_views-cell="{ row }">{{ (row.original.total_views ?? 0).toLocaleString() }}</template>
        <template #unique_users-cell="{ row }">{{ (row.original.unique_users ?? 0).toLocaleString() }}</template>
        <template #total_watch_time-cell="{ row }">{{ formatWatchTime(row.original.total_watch_time) }}</template>
        <template #logins-cell="{ row }">{{ (row.original.logins ?? 0).toLocaleString() }}</template>
        <template #registrations-cell="{ row }">{{ (row.original.registrations ?? 0).toLocaleString() }}</template>
        <template #downloads-cell="{ row }">{{ (row.original.downloads ?? 0).toLocaleString() }}</template>
        <template #searches-cell="{ row }">{{ (row.original.searches ?? 0).toLocaleString() }}</template>
        <template #uploads_succeeded-cell="{ row }">{{ (row.original.uploads_succeeded ?? 0).toLocaleString() }}</template>
        <template #favorites_added-cell="{ row }">{{ (row.original.favorites_added ?? 0).toLocaleString() }}</template>
        <template #ratings_set-cell="{ row }">{{ (row.original.ratings_set ?? 0).toLocaleString() }}</template>
        <template #hls_starts-cell="{ row }">{{ (row.original.hls_starts ?? 0).toLocaleString() }}</template>
        <template #admin_actions-cell="{ row }">{{ (row.original.admin_actions ?? 0).toLocaleString() }}</template>
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

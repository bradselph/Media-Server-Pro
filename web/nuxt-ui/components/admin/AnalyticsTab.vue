<script setup lang="ts">
import type {
  AnalyticsSummary, DailyStats, TopMediaItem, EventStats, EventTypeCounts,
  AnalyticsEvent, ContentPerformanceItem, UserAnalytics,
  TopUserEntry, SearchQueryEntry, FailedLoginEntry, ErrorPathEntry,
  MetricTimelineEntry, CohortMetrics, HourlyHeatmapCell, QualityBucket,
  PeriodComparison, Funnel, DeviceBucket, MediaDetail, RetentionGrid,
  AnomalyReport, IPSummary, ModuleDiagnostics,
} from '~/types/api'
import { getDisplayTitle } from '~/utils/mediaTitle'
import { formatWatchTime, formatBytes } from '~/utils/format'

const analyticsApi = useAnalyticsApi()
const toast = useToast()

// ── Filtering & view-state ──────────────────────────────────────────────────
// Most filters live in URL-style refs so the user can tweak the dashboard
// without remounting it. period drives everything time-bound; eventTypeFilter
// narrows the Event Distribution chart and drill-down inputs.
const eventTypeFilter = ref<string>('') // empty = all
const drillDateFilter = ref<string>('') // YYYY-MM-DD; restricts daily breakdown
const showTrafficGroups = ref<Record<string, boolean>>({
  Auth: true, Content: true, 'Playlists & Streaming': true, 'Admin & System': true,
})

// Top-users panel state.
const topUserMetric = ref<'views' | 'watch_time' | 'uploads' | 'downloads' | 'events'>('views')
const topUsers = ref<TopUserEntry[]>([])
const topUsersLoading = ref(false)

// Top-searches state.
const topSearches = ref<SearchQueryEntry[]>([])

// Active streams (live snapshot, refreshed with the page).
const activeStreams = ref<Array<{ id: string; media_id: string; filename: string; user_id: string; ip_address: string; quality: string; position: number; started_at: number; last_update: number; bytes_sent: number }>>([])

// Security review.
const failedLogins = ref<FailedLoginEntry[]>([])
const errorPaths = ref<ErrorPathEntry[]>([])

// Time-series state. Two charts: bandwidth (single series), and a multi-line
// "engagement overview" overlaying views/streams/uploads.
const timelineDays = ref<number>(30)
const tlViews = ref<MetricTimelineEntry[]>([])
const tlStreams = ref<MetricTimelineEntry[]>([])
const tlUploads = ref<MetricTimelineEntry[]>([])
const tlBandwidth = ref<MetricTimelineEntry[]>([])
const tlLogins = ref<MetricTimelineEntry[]>([])
const tlServerErrors = ref<MetricTimelineEntry[]>([])

// Cohort metrics + heatmap + quality + content gaps + period comparison.
const cohort = ref<CohortMetrics | null>(null)
const heatmap = ref<HourlyHeatmapCell[]>([])
const quality = ref<QualityBucket[]>([])
const contentGaps = ref<SearchQueryEntry[]>([])
const cmpViews = ref<PeriodComparison | null>(null)
const cmpStreams = ref<PeriodComparison | null>(null)
const cmpBandwidth = ref<PeriodComparison | null>(null)
const cmpLogins = ref<PeriodComparison | null>(null)

// Anomaly report — banner at the top of the page when something unusual
// is happening today.
const anomalies = ref<AnomalyReport | null>(null)

// Per-IP traffic summary + analytics module's own diagnostics.
const ipSummary = ref<IPSummary | null>(null)
const diagnostics = ref<ModuleDiagnostics | null>(null)

// Funnel + device/browser breakdown + per-media drill + retention grid.
const funnel = ref<Funnel | null>(null)
const devices = ref<DeviceBucket[]>([])
const browsers = ref<DeviceBucket[]>([])
const retention = ref<RetentionGrid | null>(null)
const mediaDetail = ref<MediaDetail | null>(null)
const mediaDetailLoading = ref(false)
const mediaDetailOpen = ref(false)
const mediaDetailTitle = ref('')

// Panel visibility — admin-controlled. Persisted to localStorage so the
// dashboard remembers the layout across sessions. Each entry is true by
// default (everything visible until the admin hides it).
const PANEL_KEYS = [
  'cohort', 'comparison', 'timeline', 'bandwidth', 'errorsChart',
  'traffic', 'distribution', 'hourly', 'heatmap', 'funnel', 'quality',
  'gaps', 'devices', 'retention', 'topUsers', 'topSearches', 'activeStreams',
  'errorPaths', 'failedLogins', 'recent', 'drill', 'topMedia',
  'contentPerf', 'daily', 'ips', 'diagnostics',
] as const
type PanelKey = typeof PANEL_KEYS[number]
const panelVisibility = ref<Record<PanelKey, boolean>>(
  Object.fromEntries(PANEL_KEYS.map(k => [k, true])) as Record<PanelKey, boolean>
)
// Hydrate from localStorage on mount and persist on change. SSR-safe via
// process.client guard so the initial server render stays deterministic.
onMounted(() => {
  if (!import.meta.client) return
  try {
    const raw = localStorage.getItem('analytics:panelVisibility')
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<Record<PanelKey, boolean>>
      for (const k of PANEL_KEYS) {
        if (typeof parsed[k] === 'boolean') panelVisibility.value[k] = parsed[k]!
      }
    }
  } catch { /* corrupted JSON — fall through to defaults */ }
})
watch(panelVisibility, (v) => {
  if (!import.meta.client) return
  try { localStorage.setItem('analytics:panelVisibility', JSON.stringify(v)) } catch { /* quota / privacy mode — ignore */ }
}, { deep: true })

// Built-in presets — quick-switch dashboard layouts admins can apply with
// one click. Each preset is a list of panels to enable; everything else
// gets hidden. "All" restores the default (everything on).
const PRESETS: Record<string, PanelKey[] | 'all'> = {
  All: 'all',
  Engagement: ['cohort', 'comparison', 'timeline', 'bandwidth', 'traffic',
    'distribution', 'retention', 'topUsers', 'topMedia', 'topSearches',
    'contentPerf', 'funnel', 'recent'],
  Operations: ['comparison', 'timeline', 'bandwidth', 'errorsChart',
    'errorPaths', 'failedLogins', 'activeStreams', 'quality', 'devices',
    'ips', 'diagnostics', 'recent', 'drill'],
  Security: ['comparison', 'errorsChart', 'errorPaths', 'failedLogins',
    'ips', 'recent', 'drill'],
  Content: ['cohort', 'timeline', 'topMedia', 'contentPerf', 'topSearches',
    'gaps', 'funnel', 'quality'],
}
function applyPreset(name: string) {
  const preset = PRESETS[name]
  if (preset === 'all') {
    for (const k of PANEL_KEYS) panelVisibility.value[k] = true
    return
  }
  const enable = new Set(preset)
  for (const k of PANEL_KEYS) panelVisibility.value[k] = enable.has(k)
}

// ── Live event tail (SSE) ───────────────────────────────────────────────────
// Opens an EventSource against /api/admin/analytics/stream and pushes new
// events into a ring buffer. Capped at 100 entries so the panel doesn't
// grow unbounded for an admin who leaves it open all day.
const liveTailOpen = ref(false)
const liveTail = ref<AnalyticsEvent[]>([])
const liveTailMax = 100
let liveTailSource: EventSource | null = null

function startLiveTail() {
  if (!import.meta.client) return
  if (liveTailSource) return
  try {
    liveTailSource = new EventSource('/api/admin/analytics/stream', { withCredentials: true })
    liveTailSource.addEventListener('analytics', (e) => {
      try {
        const ev = JSON.parse(e.data) as AnalyticsEvent
        liveTail.value.unshift(ev)
        if (liveTail.value.length > liveTailMax) liveTail.value.length = liveTailMax
      } catch { /* ignore malformed */ }
    })
    liveTailSource.addEventListener('error', () => {
      // EventSource auto-reconnects on network errors; only act if the
      // connection is in a permanently-failed state.
      if (liveTailSource && liveTailSource.readyState === EventSource.CLOSED) {
        toast.add({ title: 'Live tail disconnected', color: 'warning', icon: 'i-lucide-wifi-off' })
        stopLiveTail()
      }
    })
  } catch (e: unknown) {
    toast.add({ title: 'Failed to open live tail', color: 'error', icon: 'i-lucide-x' })
  }
}

function stopLiveTail() {
  if (liveTailSource) {
    liveTailSource.close()
    liveTailSource = null
  }
}

function toggleLiveTail() {
  liveTailOpen.value = !liveTailOpen.value
  if (liveTailOpen.value) startLiveTail()
  else stopLiveTail()
}

onUnmounted(() => stopLiveTail())

// Open the per-media analytics modal for a specific media item.
async function openMediaDetail(mediaId: string, title: string) {
  mediaDetailTitle.value = title
  mediaDetailOpen.value = true
  mediaDetail.value = null
  mediaDetailLoading.value = true
  try {
    mediaDetail.value = await analyticsApi.getMediaAnalytics(mediaId, 30)
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load media analytics', color: 'error', icon: 'i-lucide-x' })
  } finally {
    mediaDetailLoading.value = false
  }
}

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
  const days = periodToDays(period.value)
  // Pull ALL panels in parallel — Promise.allSettled tolerates partial
  // failures so one broken endpoint doesn't blank the whole dashboard.
  try {
    const days4chart = Math.max(days, timelineDays.value)
    const results = await Promise.allSettled([
      analyticsApi.getSummary(period.value),                        // 0
      analyticsApi.getDaily(days),                                  // 1
      analyticsApi.getTopMedia(20),                                 // 2
      analyticsApi.getContentPerformance(20),                       // 3
      analyticsApi.getEventStats(),                                 // 4
      analyticsApi.getEventTypeCounts(),                            // 5
      analyticsApi.getTopUsers(topUserMetric.value, 10),            // 6
      analyticsApi.getTopSearches(15),                              // 7
      analyticsApi.getActiveStreams(),                              // 8
      analyticsApi.getFailedLogins(20),                             // 9
      analyticsApi.getErrorPaths(15),                               // 10
      analyticsApi.getMetricTimeline('total_views', days4chart),    // 11
      analyticsApi.getMetricTimeline('stream_starts', days4chart),  // 12
      analyticsApi.getMetricTimeline('uploads_succeeded', days4chart), // 13
      analyticsApi.getMetricTimeline('bytes_served', days4chart),   // 14
      analyticsApi.getMetricTimeline('logins', days4chart),         // 15
      analyticsApi.getMetricTimeline('server_errors', days4chart),  // 16
      analyticsApi.getCohortMetrics(),                              // 17
      analyticsApi.getHourlyHeatmap(30),                            // 18
      analyticsApi.getQualityBreakdown(30),                         // 19
      analyticsApi.getContentGaps(30, 10),                          // 20
      analyticsApi.getPeriodComparison('total_views', days),        // 21
      analyticsApi.getPeriodComparison('stream_starts', days),      // 22
      analyticsApi.getPeriodComparison('bytes_served', days),       // 23
      analyticsApi.getPeriodComparison('logins', days),             // 24
      analyticsApi.getFunnel(30),                                   // 25
      analyticsApi.getDeviceBreakdown(30),                          // 26
      analyticsApi.getRetention(12),                                // 27
      analyticsApi.getAnomalies(2.5, 14),                           // 28
      analyticsApi.getIPSummary(30, 15),                            // 29
      analyticsApi.getDiagnostics(),                                // 30
    ])
    if (period.value !== capturedPeriod) return
    const r = (i: number) => results[i].status === 'fulfilled' ? (results[i] as PromiseFulfilledResult<unknown>).value : null
    if (r(0) !== null) summary.value = r(0) as AnalyticsSummary
    if (r(1) !== null) daily.value = (r(1) as DailyStats[]) ?? []
    if (r(2) !== null) topMedia.value = (r(2) as TopMediaItem[]) ?? []
    if (r(3) !== null) contentPerf.value = (r(3) as ContentPerformanceItem[]) ?? []
    if (r(4) !== null) eventStats.value = r(4) as EventStats
    if (r(5) !== null) eventTypeCounts.value = r(5) as EventTypeCounts
    if (r(6) !== null) topUsers.value = (r(6) as TopUserEntry[]) ?? []
    if (r(7) !== null) topSearches.value = (r(7) as SearchQueryEntry[]) ?? []
    if (r(8) !== null) activeStreams.value = (r(8) as typeof activeStreams.value) ?? []
    if (r(9) !== null) failedLogins.value = (r(9) as FailedLoginEntry[]) ?? []
    if (r(10) !== null) errorPaths.value = (r(10) as ErrorPathEntry[]) ?? []
    if (r(11) !== null) tlViews.value = (r(11) as MetricTimelineEntry[]) ?? []
    if (r(12) !== null) tlStreams.value = (r(12) as MetricTimelineEntry[]) ?? []
    if (r(13) !== null) tlUploads.value = (r(13) as MetricTimelineEntry[]) ?? []
    if (r(14) !== null) tlBandwidth.value = (r(14) as MetricTimelineEntry[]) ?? []
    if (r(15) !== null) tlLogins.value = (r(15) as MetricTimelineEntry[]) ?? []
    if (r(16) !== null) tlServerErrors.value = (r(16) as MetricTimelineEntry[]) ?? []
    if (r(17) !== null) cohort.value = r(17) as CohortMetrics
    if (r(18) !== null) heatmap.value = (r(18) as HourlyHeatmapCell[]) ?? []
    if (r(19) !== null) quality.value = (r(19) as QualityBucket[]) ?? []
    if (r(20) !== null) contentGaps.value = (r(20) as SearchQueryEntry[]) ?? []
    if (r(21) !== null) cmpViews.value = r(21) as PeriodComparison
    if (r(22) !== null) cmpStreams.value = r(22) as PeriodComparison
    if (r(23) !== null) cmpBandwidth.value = r(23) as PeriodComparison
    if (r(24) !== null) cmpLogins.value = r(24) as PeriodComparison
    if (r(25) !== null) funnel.value = r(25) as Funnel
    if (r(26) !== null) {
      const d = r(26) as { devices: DeviceBucket[]; browsers: DeviceBucket[] }
      devices.value = d.devices ?? []
      browsers.value = d.browsers ?? []
    }
    if (r(27) !== null) retention.value = r(27) as RetentionGrid
    if (r(28) !== null) anomalies.value = r(28) as AnomalyReport
    if (r(29) !== null) ipSummary.value = r(29) as IPSummary
    if (r(30) !== null) diagnostics.value = r(30) as ModuleDiagnostics

    const failed = results.filter(x => x.status === 'rejected')
    if (failed.length) toast.add({ title: `${failed.length} analytics endpoint(s) failed`, color: 'warning', icon: 'i-lucide-alert-triangle' })
  } finally {
    loading.value = false
  }
}

// Reload top-users when the metric selector changes — cheap, no dashboard
// re-fetch needed.
async function reloadTopUsers() {
  topUsersLoading.value = true
  try {
    topUsers.value = (await analyticsApi.getTopUsers(topUserMetric.value, 10)) ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load top users', color: 'error', icon: 'i-lucide-x' })
  } finally {
    topUsersLoading.value = false
  }
}
watch(topUserMetric, reloadTopUsers)
// Reload timelines when the user widens the chart range.
watch(timelineDays, async (n) => {
  const [v, s, u, b, l, e] = await Promise.allSettled([
    analyticsApi.getMetricTimeline('total_views', n),
    analyticsApi.getMetricTimeline('stream_starts', n),
    analyticsApi.getMetricTimeline('uploads_succeeded', n),
    analyticsApi.getMetricTimeline('bytes_served', n),
    analyticsApi.getMetricTimeline('logins', n),
    analyticsApi.getMetricTimeline('server_errors', n),
  ])
  if (v.status === 'fulfilled') tlViews.value = v.value ?? []
  if (s.status === 'fulfilled') tlStreams.value = s.value ?? []
  if (u.status === 'fulfilled') tlUploads.value = u.value ?? []
  if (b.status === 'fulfilled') tlBandwidth.value = b.value ?? []
  if (l.status === 'fulfilled') tlLogins.value = l.value ?? []
  if (e.status === 'fulfilled') tlServerErrors.value = e.value ?? []
})

// Filtered event-type entries — when the filter is set, only that one row
// shows in the bar chart and drill-down inputs auto-fill.
const filteredEventTypeEntries = computed(() => {
  if (!eventTypeFilter.value) return eventTypeEntries.value
  const needle = eventTypeFilter.value.toLowerCase()
  return eventTypeEntries.value.filter(e => e.type.toLowerCase().includes(needle))
})

// Filtered daily breakdown — restricted to the row whose date matches
// drillDateFilter when set, otherwise all rows.
const filteredDaily = computed(() => {
  if (!drillDateFilter.value) return daily.value
  return daily.value.filter(d => d.date === drillDateFilter.value)
})

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

// Heatmap helpers — flattened into a 7×24 matrix indexed by [day][hour] so
// the template can iterate without doing the math. Cells fall back to 0
// when the backend hasn't seen any events for that bucket.
const heatmapMatrix = computed(() => {
  const m: number[][] = Array.from({ length: 7 }, () => Array(24).fill(0))
  for (const cell of heatmap.value) {
    if (cell.day_of_week >= 0 && cell.day_of_week < 7 && cell.hour >= 0 && cell.hour < 24) {
      m[cell.day_of_week][cell.hour] = cell.count
    }
  }
  return m
})
const heatmapMax = computed(() => {
  let max = 0
  for (const row of heatmapMatrix.value) for (const v of row) if (v > max) max = v
  return max
})
const heatmapPalette = ['bg-muted/10', 'bg-emerald-500/15', 'bg-emerald-500/30', 'bg-emerald-500/50', 'bg-emerald-500/70', 'bg-emerald-500/90']
function heatmapClass(count: number): string {
  if (count === 0 || heatmapMax.value === 0) return heatmapPalette[0]
  // 5-bucket quantization keeps the palette discrete and readable.
  const ratio = count / heatmapMax.value
  const idx = Math.min(heatmapPalette.length - 1, Math.max(1, Math.ceil(ratio * 5)))
  return heatmapPalette[idx]
}
const DAY_LABELS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

// Format a period-comparison delta. Returns "+12%" / "−4%" / "—" depending on
// sign, with the "vs prev" tooltip context handled separately in the template.
function formatDelta(c: PeriodComparison | null): { label: string; tone: 'success' | 'error' | 'neutral' } {
  if (!c || (c.current === 0 && c.previous === 0)) return { label: '—', tone: 'neutral' }
  const pct = c.delta_pct
  const sign = pct > 0 ? '+' : ''
  const tone: 'success' | 'error' | 'neutral' = pct > 0 ? 'success' : pct < 0 ? 'error' : 'neutral'
  // Cap display at ±999% so a "previous=0, current=N" doesn't dominate the row.
  const capped = Math.max(-999, Math.min(999, pct))
  return { label: `${sign}${capped.toFixed(0)}%`, tone }
}

// Total bytes across all quality buckets, used to render percentages.
const qualityTotalBytes = computed(() => quality.value.reduce((a, b) => a + (b.bytes_sent || 0), 0))
const qualityTotalStreams = computed(() => quality.value.reduce((a, b) => a + (b.streams || 0), 0))

// Color the from-previous percentage by drop-off severity. > 50% retained =
// healthy (success), 25-50% = neutral, < 25% = warning, 0 = error.
function funnelTone(pct: number): 'success' | 'warning' | 'error' | 'neutral' {
  if (pct === 0) return 'error'
  if (pct >= 50) return 'success'
  if (pct >= 25) return 'neutral'
  return 'warning'
}

// Total events across the device breakdown, used for percentage labels.
const deviceTotalEvents = computed(() => devices.value.reduce((a, b) => a + b.events, 0))
const browserTotalEvents = computed(() => browsers.value.reduce((a, b) => a + b.events, 0))

// Retention cell shading. 5 buckets keep the visual discrete; gaps in the
// upper triangle (cells beyond the cohort's age) render as muted/transparent
// instead of a misleading 0% block.
const retentionPalette = ['bg-muted/10', 'bg-emerald-500/15', 'bg-emerald-500/30', 'bg-emerald-500/50', 'bg-emerald-500/70', 'bg-emerald-500/90']
function retentionCellClass(pct: number, weekIndex: number, cohortAge: number): string {
  if (weekIndex > cohortAge) return 'bg-transparent'
  if (pct === 0) return retentionPalette[0]
  // Buckets: 0-5, 5-15, 15-30, 30-50, 50-75, 75+.
  if (pct >= 75) return retentionPalette[5]
  if (pct >= 50) return retentionPalette[4]
  if (pct >= 30) return retentionPalette[3]
  if (pct >= 15) return retentionPalette[2]
  return retentionPalette[1]
}

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
        <!-- Live tail toggle — opens an SSE connection. While open, every
             tracked event flows into a ring buffer the panel below renders. -->
        <UButton
          :icon="liveTailOpen ? 'i-lucide-radio' : 'i-lucide-radio-tower'"
          :label="liveTailOpen ? 'Live' : 'Tail'"
          :variant="liveTailOpen ? 'solid' : 'outline'"
          :color="liveTailOpen ? 'success' : 'neutral'"
          size="xs"
          @click="toggleLiveTail"
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
        <!-- Panels visibility menu — admins can hide widgets they don't
             care about. Choices persist to localStorage so the layout
             survives reloads. -->
        <UPopover :ui="{ content: 'w-64' }">
          <UButton icon="i-lucide-layout-dashboard" label="Panels" variant="outline" color="neutral" size="xs" />
          <template #content>
            <div class="p-3 max-h-96 overflow-y-auto space-y-1.5 text-sm">
              <div class="mb-3">
                <p class="text-xs uppercase tracking-wide font-semibold text-muted mb-1.5">Presets</p>
                <div class="flex flex-wrap gap-1">
                  <UButton
                    v-for="name in Object.keys(PRESETS)"
                    :key="name"
                    :label="name"
                    size="xs"
                    variant="outline"
                    color="neutral"
                    @click="applyPreset(name)"
                  />
                </div>
                <p class="text-[10px] text-muted mt-1.5 italic">
                  One-click layout switches; individual toggles below override.
                </p>
              </div>
              <div class="flex items-center justify-between mb-2">
                <span class="text-xs uppercase tracking-wide font-semibold text-muted">Show panels</span>
              </div>
              <label v-for="k in PANEL_KEYS" :key="k" class="flex items-center gap-2 cursor-pointer hover:bg-muted/10 rounded px-1 py-0.5">
                <UCheckbox v-model="panelVisibility[k]" />
                <span class="text-xs capitalize">{{ k.replace(/([A-Z])/g, ' $1').trim() }}</span>
              </label>
            </div>
          </template>
        </UPopover>
      </div>
    </div>

    <!-- Filter strip — type filter chip + chart range slider. The same
         eventTypeFilter feeds the distribution chart and pre-fills the
         drill-down panel; the chart range is independent of `period` so
         operators can zoom out the trend line without losing the daily
         table they're looking at. -->
    <div class="flex flex-wrap gap-3 items-center">
      <UInput
        v-model="eventTypeFilter"
        size="xs"
        icon="i-lucide-filter"
        placeholder="Filter event types (e.g. login, view, upload)"
        class="w-72"
      />
      <UButton
        v-if="eventTypeFilter"
        size="xs"
        variant="ghost"
        color="neutral"
        icon="i-lucide-x"
        label="Clear"
        @click="eventTypeFilter = ''"
      />
      <span class="text-xs text-muted">Chart range:</span>
      <UButtonGroup>
        <UButton
          v-for="d in [7, 14, 30, 90, 180, 365]"
          :key="d"
          :label="`${d}d`"
          size="xs"
          :variant="timelineDays === d ? 'solid' : 'outline'"
          :color="timelineDays === d ? 'primary' : 'neutral'"
          @click="timelineDays = d"
        />
      </UButtonGroup>
    </div>

    <!-- Loading -->
    <div v-if="loading && !summary" class="flex justify-center py-8">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-primary" />
    </div>

    <!-- Anomalies banner — appears only when at least one watched metric
         is statistically far from its rolling baseline. Each anomaly gets
         its own pill so admins can see at a glance which metrics need
         attention. Spikes in error metrics colour error; growth spikes
         in engagement colour primary. -->
    <UAlert
      v-if="anomalies && anomalies.anomalies.length > 0"
      :color="anomalies.anomalies.some(a => ['server_errors','hls_errors','logins_failed','mature_blocked','permission_denied'].includes(a.metric)) ? 'error' : 'warning'"
      variant="subtle"
      icon="i-lucide-zap"
      :title="`${anomalies.anomalies.length} anomaly${anomalies.anomalies.length === 1 ? '' : 'ies'} vs ${anomalies.window_days}-day baseline`"
    >
      <template #description>
        <div class="flex flex-wrap gap-2 mt-1.5">
          <UBadge
            v-for="a in anomalies.anomalies"
            :key="a.date + a.metric"
            :color="['server_errors','hls_errors','logins_failed','mature_blocked','permission_denied'].includes(a.metric) ? 'error' :
                    a.direction === 'dip' ? 'warning' : 'primary'"
            variant="subtle"
            size="xs"
            class="cursor-pointer"
            :title="`${a.metric}: ${a.value.toLocaleString()} (baseline ${a.baseline.toFixed(1)}, z=${a.z_score.toFixed(1)})`"
            @click="drillDateFilter = a.date"
          >
            <UIcon :name="a.direction === 'spike' ? 'i-lucide-trending-up' : 'i-lucide-trending-down'" class="size-3 mr-1" />
            {{ a.metric }} {{ a.direction }} ({{ a.z_score !== 0 ? `${a.z_score.toFixed(1)}σ` : 'absolute' }})
          </UBadge>
        </div>
      </template>
    </UAlert>

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

    <!-- Cohort + period-delta strip — DAU/WAU/MAU + stickiness on the left,
         period-over-period deltas on the right. Quick-glance health pulse. -->
    <div v-if="(panelVisibility.cohort || panelVisibility.comparison) && (cohort || cmpViews)" class="grid grid-cols-1 md:grid-cols-2 gap-3">
      <UCard v-if="cohort && panelVisibility.cohort" :ui="{ body: 'p-3' }">
        <div class="flex items-center justify-between mb-2">
          <span class="text-xs uppercase tracking-wide font-semibold text-muted">User Cohorts (last 30 days)</span>
          <UIcon name="i-lucide-users-round" class="size-4 text-primary" />
        </div>
        <div class="grid grid-cols-3 gap-3">
          <div>
            <p class="text-2xl font-bold text-highlighted">{{ cohort.dau.toLocaleString() }}</p>
            <p class="text-xs text-muted">DAU</p>
          </div>
          <div>
            <p class="text-2xl font-bold text-highlighted">{{ cohort.wau.toLocaleString() }}</p>
            <p class="text-xs text-muted">WAU</p>
          </div>
          <div>
            <p class="text-2xl font-bold text-highlighted">{{ cohort.mau.toLocaleString() }}</p>
            <p class="text-xs text-muted">MAU</p>
          </div>
        </div>
        <div class="flex items-center gap-4 mt-2 text-xs text-muted">
          <span title="DAU divided by WAU — share of weekly users active today">
            DAU/WAU: <strong class="text-highlighted">{{ (cohort.stickiness_dau_wau * 100).toFixed(0) }}%</strong>
          </span>
          <span title="DAU divided by MAU — share of monthly users active today">
            DAU/MAU: <strong class="text-highlighted">{{ (cohort.stickiness_dau_mau * 100).toFixed(0) }}%</strong>
          </span>
        </div>
      </UCard>

      <UCard v-if="panelVisibility.comparison" :ui="{ body: 'p-3' }">
        <div class="flex items-center justify-between mb-2">
          <span class="text-xs uppercase tracking-wide font-semibold text-muted">Period Comparison ({{ cmpViews?.window_days ?? '—' }}d vs prev)</span>
          <UIcon name="i-lucide-trending-up" class="size-4 text-primary" />
        </div>
        <div class="grid grid-cols-2 lg:grid-cols-4 gap-3">
          <template v-for="cmp in [
            { label: 'Views', cmp: cmpViews },
            { label: 'Streams', cmp: cmpStreams },
            { label: 'Bandwidth', cmp: cmpBandwidth, isBytes: true },
            { label: 'Logins', cmp: cmpLogins },
          ]" :key="cmp.label">
            <div v-if="cmp.cmp" class="flex flex-col">
              <p class="text-lg font-bold text-highlighted truncate">
                {{ cmp.isBytes ? formatBytes(cmp.cmp.current) : cmp.cmp.current.toLocaleString() }}
              </p>
              <div class="flex items-center gap-1 text-xs">
                <span class="text-muted">{{ cmp.label }}</span>
                <UBadge
                  :color="formatDelta(cmp.cmp).tone === 'success' ? 'success' : formatDelta(cmp.cmp).tone === 'error' ? 'error' : 'neutral'"
                  variant="subtle"
                  size="xs"
                >
                  {{ formatDelta(cmp.cmp).label }}
                </UBadge>
              </div>
            </div>
          </template>
        </div>
      </UCard>
    </div>

    <!-- Engagement timeline — overlays views, streams, uploads, and logins
         on a single chart so operators can see how the four highest-signal
         metrics correlate over the chart range. Bandwidth gets its own
         chart below since the y-axis is incompatible (bytes vs counts). -->
    <UCard v-if="panelVisibility.timeline && tlViews.length > 0">
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-line-chart" class="size-4" />
          Engagement Timeline ({{ timelineDays }} days)
        </div>
      </template>
      <MetricLineChart
        :series="[
          { label: 'Views', color: 'stroke-primary text-primary', values: tlViews },
          { label: 'Streams', color: 'stroke-emerald-500 text-emerald-500', values: tlStreams },
          { label: 'Uploads', color: 'stroke-amber-500 text-amber-500', values: tlUploads },
          { label: 'Logins', color: 'stroke-sky-500 text-sky-500', values: tlLogins },
        ]"
        :height="220"
        @point-click="(date: string) => { drillDateFilter = date }"
      />
    </UCard>

    <!-- Bandwidth + Server-Errors charts — separate axes (bytes / count). -->
    <div v-if="tlBandwidth.length > 0 || tlServerErrors.length > 0" class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <UCard v-if="panelVisibility.bandwidth && tlBandwidth.length > 0">
        <template #header>
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-network" class="size-4" />
            Bandwidth Served ({{ timelineDays }} days)
          </div>
        </template>
        <MetricLineChart
          :series="[
            { label: 'Bytes', color: 'stroke-cyan-500 text-cyan-500', values: tlBandwidth, format: (v: number) => formatBytes(v) },
          ]"
          :height="180"
          @point-click="(date: string) => { drillDateFilter = date }"
        />
      </UCard>
      <UCard v-if="panelVisibility.errorsChart && tlServerErrors.length > 0 && tlServerErrors.some(e => e.value > 0)">
        <template #header>
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-alert-octagon" class="size-4 text-error" />
            5xx Server Errors ({{ timelineDays }} days)
          </div>
        </template>
        <MetricLineChart
          :series="[
            { label: 'Errors', color: 'stroke-red-500 text-red-500', values: tlServerErrors },
          ]"
          :height="180"
          @point-click="(date: string) => { drillDateFilter = date }"
        />
      </UCard>
    </div>

    <!-- Today's Traffic Breakdown — grouped, only shows non-zero counters so
         a quiet day stays visually quiet instead of rendering 24 zero cards. -->
    <div v-if="panelVisibility.traffic && summary && hasTrafficActivity" class="space-y-3">
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
      <UCard v-if="panelVisibility.distribution && filteredEventTypeEntries.length > 0">
        <template #header>
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-pie-chart" class="size-4" />
            Event Distribution
            <span v-if="eventTypeFilter" class="text-xs font-normal text-muted">(filtered: {{ eventTypeFilter }})</span>
          </div>
        </template>
        <div class="space-y-2 max-h-72 overflow-y-auto">
          <div v-for="entry in filteredEventTypeEntries" :key="entry.type"
               class="flex items-center gap-2 cursor-pointer hover:bg-muted/10 rounded px-1 py-0.5"
               :title="`Click to drill events of type: ${entry.type}`"
               @click="drillMode = 'type'; drillType = entry.type; drillByType()">
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
      <UCard v-if="panelVisibility.hourly && eventStats?.hourly_events">
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
    <UCard v-if="panelVisibility.contentPerf && contentPerf.length > 0">
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

    <!-- Conversion funnel — view → playback → completion, with overall
         and authenticated/anonymous splits. The "from prev" percentages
         show step-over-step retention; the bars shade by tone so admins
         can spot where users are falling out of the experience. -->
    <UCard v-if="panelVisibility.funnel && funnel && funnel.stages?.[0]?.count > 0">
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-filter" class="size-4" />
          Conversion Funnel ({{ funnel.window_days }} days)
        </div>
      </template>
      <div class="space-y-4">
        <template v-for="row in [
          { label: 'Overall', stages: funnel.stages },
          { label: 'Authenticated', stages: funnel.authenticated },
          { label: 'Anonymous', stages: funnel.anonymous },
        ]" :key="row.label">
          <div v-if="row.stages.length > 0">
            <div class="flex items-center justify-between mb-1.5">
              <span class="text-xs uppercase tracking-wide font-semibold text-muted">{{ row.label }}</span>
              <span class="text-xs text-muted">
                {{ row.stages[0].count.toLocaleString() }} → {{ row.stages[row.stages.length - 1].count.toLocaleString() }}
                ({{ row.stages[row.stages.length - 1].from_top_pct.toFixed(1) }}% conversion)
              </span>
            </div>
            <div class="grid grid-cols-3 gap-1.5">
              <div v-for="(s, i) in row.stages" :key="i" class="relative">
                <div
                  :class="[
                    'rounded p-2.5 border-l-4',
                    funnelTone(s.from_previous_pct) === 'success' ? 'border-l-emerald-500 bg-emerald-500/5' :
                    funnelTone(s.from_previous_pct) === 'warning' ? 'border-l-amber-500 bg-amber-500/5' :
                    funnelTone(s.from_previous_pct) === 'error' ? 'border-l-red-500 bg-red-500/5' :
                    'border-l-default bg-muted/5'
                  ]"
                >
                  <p class="text-xs text-muted">{{ s.stage }}</p>
                  <p class="text-base font-bold text-highlighted">{{ s.count.toLocaleString() }}</p>
                  <p v-if="i > 0" class="text-[10px] text-muted">
                    {{ s.from_previous_pct.toFixed(1) }}% of prev
                  </p>
                </div>
              </div>
            </div>
          </div>
        </template>
      </div>
    </UCard>

    <!-- Hourly heatmap (7 days × 24 hours) — surfaces traffic peaks per
         weekday so operators can correlate scheduled tasks / cron / load
         with user activity windows. Rendered as a discrete-quantized grid;
         each cell tooltips its raw count. -->
    <UCard v-if="panelVisibility.heatmap && heatmap.length > 0 && heatmapMax > 0">
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-grid" class="size-4" />
          Hourly Activity Heatmap (last 30 days)
        </div>
      </template>
      <div class="overflow-x-auto">
        <div class="inline-grid gap-0.5" :style="{ gridTemplateColumns: 'auto repeat(24, minmax(14px, 1fr))' }">
          <!-- header row: hour labels every 3 hours -->
          <div></div>
          <div v-for="h in 24" :key="`h-${h - 1}`" class="text-[9px] text-muted text-center leading-tight">
            <span v-if="(h - 1) % 3 === 0">{{ String(h - 1).padStart(2, '0') }}</span>
          </div>
          <!-- one row per day -->
          <template v-for="(dow, i) in DAY_LABELS" :key="dow">
            <div class="text-[10px] text-muted pr-1 self-center text-right">{{ dow }}</div>
            <div
              v-for="(count, h) in heatmapMatrix[i]"
              :key="`c-${i}-${h}`"
              :class="[heatmapClass(count), 'h-4 rounded-sm cursor-default']"
              :title="`${dow} ${String(h).padStart(2, '0')}:00 — ${count.toLocaleString()} events`"
            />
          </template>
        </div>
        <div class="flex items-center gap-1.5 mt-2 text-[10px] text-muted">
          <span>Less</span>
          <span v-for="(c, i) in heatmapPalette" :key="i" :class="[c, 'w-3 h-3 rounded-sm']" />
          <span>More</span>
          <span class="ml-2">peak: {{ heatmapMax.toLocaleString() }} events/hour</span>
        </div>
      </div>
    </UCard>

    <!-- Retention grid (week-N retention by signup cohort). Upper-triangular
         by construction — younger cohorts have fewer cells. Empty cells in
         the upper triangle are rendered transparent (not 0%) so admins
         don't read a "drop-off" where there's just no data yet. -->
    <UCard v-if="panelVisibility.retention && retention && retention.weeks?.length > 0">
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-table-2" class="size-4" />
          Cohort Retention ({{ retention.cohort_weeks }} weeks)
        </div>
      </template>
      <div class="overflow-x-auto">
        <table class="text-xs">
          <thead>
            <tr>
              <th class="px-2 py-1 text-left font-semibold text-muted">Cohort</th>
              <th class="px-2 py-1 text-right font-semibold text-muted">Size</th>
              <th
                v-for="w in retention.cohort_weeks"
                :key="`wh-${w - 1}`"
                class="px-1.5 py-1 text-center font-semibold text-muted"
              >W{{ w - 1 }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, i) in retention.weeks" :key="row.cohort_start" class="hover:bg-muted/5">
              <td class="px-2 py-1 font-mono text-muted whitespace-nowrap">{{ row.cohort_start }}</td>
              <td class="px-2 py-1 text-right font-mono text-muted">{{ row.cohort_size.toLocaleString() }}</td>
              <td
                v-for="w in retention.cohort_weeks"
                :key="`c-${i}-${w - 1}`"
                :class="[
                  retentionCellClass(row.retention[w - 1] ?? 0, w - 1, retention.weeks.length - 1 - i),
                  'px-1.5 py-1 text-center font-mono',
                  (w - 1) > (retention.weeks.length - 1 - i) ? 'text-transparent' : 'text-highlighted'
                ]"
                :title="(w - 1) > (retention.weeks.length - 1 - i) ?
                  '(cohort younger than this week)' :
                  `${row.cohort_start} → week ${w - 1}: ${(row.retention[w - 1] ?? 0).toFixed(1)}%`"
              >
                {{ ((w - 1) > (retention.weeks.length - 1 - i)) ? '·' : (row.retention[w - 1] ?? 0).toFixed(0) + '%' }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p class="text-xs text-muted mt-2 italic">
        Each cell is the % of the signup-week cohort that returned in week N.
        Active = any user-attributed event that week (login, view, search…).
      </p>
    </UCard>

    <!-- Device + Browser breakdown — two side-by-side panels showing how
         events split across device families and browser families. Sorted
         by event count descending so the dominant client is at the top. -->
    <div v-if="panelVisibility.devices && (devices.length > 0 || browsers.length > 0)" class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <UCard v-if="devices.length > 0">
        <template #header>
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-smartphone" class="size-4" />
            Devices (last 30 days)
          </div>
        </template>
        <div class="space-y-1.5">
          <div v-for="d in devices" :key="d.family" class="flex items-center gap-2 text-xs">
            <span class="w-24 shrink-0 truncate" :title="d.family">{{ d.family }}</span>
            <div class="flex-1 bg-muted/20 rounded-full h-3 overflow-hidden">
              <div
                class="h-full rounded-full bg-primary transition-all"
                :style="{ width: deviceTotalEvents > 0 ? `${Math.round((d.events / deviceTotalEvents) * 100)}%` : '0%' }"
              />
            </div>
            <span class="w-16 shrink-0 text-right text-muted">{{ d.events.toLocaleString() }}</span>
            <span class="w-12 shrink-0 text-right text-[11px] text-muted" :title="`${d.unique_users} unique users`">
              {{ d.unique_users }}u
            </span>
          </div>
        </div>
      </UCard>

      <UCard v-if="browsers.length > 0">
        <template #header>
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-globe" class="size-4" />
            Browsers (last 30 days)
          </div>
        </template>
        <div class="space-y-1.5">
          <div v-for="b in browsers" :key="b.family" class="flex items-center gap-2 text-xs">
            <span class="w-24 shrink-0 truncate" :title="b.family">{{ b.family }}</span>
            <div class="flex-1 bg-muted/20 rounded-full h-3 overflow-hidden">
              <div
                class="h-full rounded-full bg-sky-500 transition-all"
                :style="{ width: browserTotalEvents > 0 ? `${Math.round((b.events / browserTotalEvents) * 100)}%` : '0%' }"
              />
            </div>
            <span class="w-16 shrink-0 text-right text-muted">{{ b.events.toLocaleString() }}</span>
            <span class="w-12 shrink-0 text-right text-[11px] text-muted">{{ b.unique_users }}u</span>
          </div>
        </div>
      </UCard>
    </div>

    <!-- Per-IP traffic — two side-by-side leaderboards (by event volume,
         by bandwidth) so admins can spot scrapers (high events, low bytes)
         vs heavy streamers (low events, high bytes). -->
    <UCard v-if="panelVisibility.ips && ipSummary && ipSummary.unique_ips > 0">
      <template #header>
        <div class="flex items-center justify-between gap-2">
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-network" class="size-4" />
            IP Traffic — {{ ipSummary.unique_ips.toLocaleString() }} unique IPs (last 30 days)
          </div>
        </div>
      </template>
      <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <div>
          <p class="text-xs uppercase tracking-wide font-semibold text-muted mb-2">By event volume</p>
          <div class="divide-y divide-default max-h-72 overflow-y-auto">
            <div v-for="ip in ipSummary.top_by_events" :key="`e-${ip.ip_address}`" class="py-1.5 flex items-center gap-2 text-xs">
              <span class="flex-1 font-mono truncate">{{ ip.ip_address }}</span>
              <UBadge color="neutral" variant="subtle" size="xs">{{ ip.events.toLocaleString() }}</UBadge>
              <span class="text-muted shrink-0">{{ ip.unique_user_ids }}u</span>
            </div>
          </div>
        </div>
        <div>
          <p class="text-xs uppercase tracking-wide font-semibold text-muted mb-2">By bandwidth</p>
          <div class="divide-y divide-default max-h-72 overflow-y-auto">
            <div v-for="ip in ipSummary.top_by_bytes" :key="`b-${ip.ip_address}`" class="py-1.5 flex items-center gap-2 text-xs">
              <span class="flex-1 font-mono truncate">{{ ip.ip_address }}</span>
              <UBadge color="primary" variant="subtle" size="xs">{{ formatBytes(ip.bytes_sent) }}</UBadge>
              <span class="text-muted shrink-0">{{ ip.events.toLocaleString() }}e</span>
            </div>
          </div>
        </div>
      </div>
    </UCard>

    <!-- Quality breakdown + content gaps — two side-by-side panels showing
         (a) which resolutions are eating bandwidth, (b) what users are
         searching for that the catalog doesn't cover. -->
    <div v-if="(panelVisibility.quality || panelVisibility.gaps) && (quality.length > 0 || contentGaps.length > 0)" class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <UCard v-if="panelVisibility.quality && quality.length > 0">
        <template #header>
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-monitor" class="size-4" />
            Stream Quality Breakdown
          </div>
        </template>
        <div class="space-y-2">
          <div v-for="q in quality" :key="q.quality" class="flex items-center gap-2 text-xs">
            <span class="w-16 shrink-0 font-medium truncate" :title="q.quality">{{ q.quality }}</span>
            <div class="flex-1 bg-muted/20 rounded-full h-3 overflow-hidden">
              <div
                class="h-full rounded-full bg-cyan-500 transition-all"
                :style="{ width: qualityTotalBytes > 0 ? `${Math.round((q.bytes_sent / qualityTotalBytes) * 100)}%` : '0%' }"
              />
            </div>
            <span class="w-20 shrink-0 text-right font-mono text-[11px] text-muted">{{ formatBytes(q.bytes_sent) }}</span>
            <span class="w-12 shrink-0 text-right text-muted">{{ q.streams.toLocaleString() }}</span>
          </div>
          <div class="flex items-center gap-3 pt-2 mt-2 border-t border-default text-xs text-muted">
            <span>Total: <strong class="text-highlighted">{{ qualityTotalStreams.toLocaleString() }}</strong> streams</span>
            <span><strong class="text-highlighted">{{ formatBytes(qualityTotalBytes) }}</strong> served</span>
          </div>
        </div>
      </UCard>

      <UCard v-if="panelVisibility.gaps && contentGaps.length > 0">
        <template #header>
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-search-x" class="size-4 text-warning" />
            Content Gaps — searches with no results
          </div>
        </template>
        <div class="space-y-1.5 max-h-72 overflow-y-auto">
          <div v-for="(g, i) in contentGaps" :key="i" class="flex items-center gap-2 text-sm py-1">
            <span class="flex-1 truncate font-mono text-xs" :title="g.query">{{ g.query }}</span>
            <UBadge color="warning" variant="subtle" size="xs">
              {{ g.empty_count }}/{{ g.count }} empty
            </UBadge>
          </div>
        </div>
        <p class="text-xs text-muted mt-2 italic">
          Queries that mostly returned zero results — strong signal for what to
          add to the library.
        </p>
      </UCard>
    </div>

    <!-- Top Users + Top Searches — side by side. Top Users has a metric
         selector so admins can sort by views, watch_time, uploads, etc.
         without leaving the page. -->
    <div v-if="panelVisibility.topUsers || panelVisibility.topSearches" class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <UCard v-if="panelVisibility.topUsers">
        <template #header>
          <div class="flex items-center justify-between gap-2 flex-wrap">
            <div class="font-semibold flex items-center gap-2">
              <UIcon name="i-lucide-trophy" class="size-4 text-amber-500" />
              Top Users
            </div>
            <div class="flex items-center gap-2">
              <UButton
                size="xs"
                variant="ghost"
                color="neutral"
                icon="i-lucide-download"
                tag="a"
                :href="analyticsApi.exportPanelUrl('top-users', 'csv', { metric: topUserMetric, limit: 50 })"
                download
                title="Export this panel as CSV"
              />
              <UButtonGroup>
                <UButton
                  v-for="m in [
                    { label: 'Views', value: 'views' },
                    { label: 'Watch', value: 'watch_time' },
                    { label: 'Uploads', value: 'uploads' },
                    { label: 'Downloads', value: 'downloads' },
                    { label: 'All', value: 'events' },
                  ]"
                  :key="m.value"
                  :label="m.label"
                  size="xs"
                  :variant="topUserMetric === m.value ? 'solid' : 'outline'"
                  :color="topUserMetric === m.value ? 'primary' : 'neutral'"
                  @click="topUserMetric = m.value as typeof topUserMetric"
                />
              </UButtonGroup>
            </div>
          </div>
        </template>
        <div v-if="topUsersLoading" class="flex justify-center py-4">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-4 text-muted" />
        </div>
        <div v-else-if="topUsers.length === 0" class="text-center text-sm text-muted py-4">
          No user activity in the current window.
        </div>
        <div v-else class="divide-y divide-default max-h-72 overflow-y-auto">
          <div v-for="(u, i) in topUsers" :key="u.user_id" class="py-2 flex items-center gap-3 text-sm">
            <span class="w-6 shrink-0 font-mono text-muted text-right">#{{ i + 1 }}</span>
            <span class="flex-1 truncate">
              <span v-if="u.username" class="font-medium">{{ u.username }}</span>
              <span v-else class="font-mono text-xs text-muted">{{ u.user_id }}</span>
            </span>
            <div class="flex items-center gap-2 shrink-0">
              <UBadge v-if="topUserMetric === 'watch_time'" color="primary" variant="subtle" size="xs">
                {{ formatWatchTime(u.metric) }}
              </UBadge>
              <UBadge v-else color="primary" variant="subtle" size="xs">
                {{ Math.round(u.metric).toLocaleString() }}
              </UBadge>
              <UButton
                size="xs"
                variant="ghost"
                color="neutral"
                icon="i-lucide-search"
                @click="drillMode = 'user'; drillUserId = u.username || u.user_id; drillByUser()"
              />
            </div>
          </div>
        </div>
      </UCard>

      <UCard v-if="panelVisibility.topSearches">
        <template #header>
          <div class="flex items-center justify-between gap-2">
            <div class="font-semibold flex items-center gap-2">
              <UIcon name="i-lucide-search" class="size-4 text-info" />
              Top Searches
            </div>
            <UButton
              size="xs"
              variant="ghost"
              color="neutral"
              icon="i-lucide-download"
              tag="a"
              :href="analyticsApi.exportPanelUrl('top-searches', 'csv', { limit: 100 })"
              download
              title="Export this panel as CSV"
            />
          </div>
        </template>
        <div v-if="topSearches.length === 0" class="text-center text-sm text-muted py-4">
          No searches recorded.
        </div>
        <div v-else class="space-y-1.5 max-h-72 overflow-y-auto">
          <div v-for="(q, i) in topSearches" :key="i" class="flex items-center gap-2 text-sm py-1">
            <span class="flex-1 truncate font-mono text-xs" :title="q.query">{{ q.query }}</span>
            <UBadge v-if="q.empty_count > 0" color="warning" variant="subtle" size="xs"
                    :title="`${q.empty_count} of ${q.count} returned no results`">
              {{ q.empty_count }} empty
            </UBadge>
            <UBadge color="neutral" variant="subtle" size="xs">{{ q.count }}</UBadge>
          </div>
        </div>
      </UCard>
    </div>

    <!-- Active Streams (live snapshot) — capacity / debugging signal that the
         existing Streaming tab also shows, but here in context with the rest
         of the analytics. Refreshes when the page reloads / auto-refresh. -->
    <UCard v-if="panelVisibility.activeStreams && activeStreams.length > 0">
      <template #header>
        <div class="font-semibold flex items-center gap-2">
          <UIcon name="i-lucide-radio-tower" class="size-4 text-emerald-500" />
          Active Streams ({{ activeStreams.length }})
          <span class="relative flex h-2 w-2 ml-1">
            <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
            <span class="relative inline-flex rounded-full h-2 w-2 bg-emerald-500" />
          </span>
        </div>
      </template>
      <UTable
        :data="activeStreams"
        :columns="[
          { accessorKey: 'filename', header: 'Media' },
          { accessorKey: 'user_id', header: 'User' },
          { accessorKey: 'ip_address', header: 'IP' },
          { accessorKey: 'quality', header: 'Quality' },
          { accessorKey: 'position', header: 'Position' },
          { accessorKey: 'bytes_sent', header: 'Bytes' },
          { accessorKey: 'started_at', header: 'Started' },
        ]"
      >
        <template #filename-cell="{ row }">
          <span class="text-sm font-medium truncate max-w-xs block" :title="row.original.filename">
            {{ row.original.filename }}
          </span>
        </template>
        <template #position-cell="{ row }">{{ formatWatchTime(row.original.position) }}</template>
        <template #bytes_sent-cell="{ row }">{{ formatBytes(row.original.bytes_sent) }}</template>
        <template #started_at-cell="{ row }">{{ new Date(row.original.started_at * 1000).toLocaleTimeString() }}</template>
        <template #user_id-cell="{ row }">
          <span v-if="row.original.user_id" class="text-sm">{{ row.original.user_id }}</span>
          <span v-else class="italic text-xs text-muted">anonymous</span>
        </template>
      </UTable>
    </UCard>

    <!-- Server Errors-by-Path — only renders when there's something wrong.
         Shown alongside (not inside) the health banner so the table can
         breathe and is sortable. -->
    <UCard v-if="panelVisibility.errorPaths && errorPaths.length > 0">
      <template #header>
        <div class="flex items-center justify-between gap-2">
          <div class="font-semibold flex items-center gap-2 text-error">
            <UIcon name="i-lucide-bug" class="size-4" />
            Server Errors by Path
          </div>
          <UButton
            size="xs"
            variant="ghost"
            color="neutral"
            icon="i-lucide-download"
            tag="a"
            :href="analyticsApi.exportPanelUrl('error-paths', 'csv', { limit: 200 })"
            download
            title="Export this panel as CSV"
          />
        </div>
      </template>
      <UTable
        :data="errorPaths"
        :columns="[
          { accessorKey: 'method', header: 'Method' },
          { accessorKey: 'path', header: 'Path' },
          { accessorKey: 'status', header: 'Status' },
          { accessorKey: 'count', header: 'Count' },
          { accessorKey: 'last_seen', header: 'Last Seen' },
        ]"
      >
        <template #method-cell="{ row }">
          <UBadge color="neutral" variant="subtle" size="xs">{{ row.original.method }}</UBadge>
        </template>
        <template #path-cell="{ row }">
          <span class="font-mono text-xs truncate max-w-md block" :title="row.original.path">{{ row.original.path }}</span>
        </template>
        <template #status-cell="{ row }">
          <UBadge color="error" variant="subtle" size="xs">{{ row.original.status }}</UBadge>
        </template>
        <template #count-cell="{ row }">{{ row.original.count.toLocaleString() }}</template>
        <template #last_seen-cell="{ row }">{{ new Date(row.original.last_seen).toLocaleString() }}</template>
      </UTable>
    </UCard>

    <!-- Failed Logins — recent N login_failed events with attempted username
         and IP so security review is one click away. -->
    <UCard v-if="panelVisibility.failedLogins && failedLogins.length > 0">
      <template #header>
        <div class="flex items-center justify-between gap-2">
          <div class="font-semibold flex items-center gap-2 text-error">
            <UIcon name="i-lucide-shield-alert" class="size-4" />
            Recent Failed Logins ({{ failedLogins.length }})
          </div>
          <UButton
            size="xs"
            variant="ghost"
            color="neutral"
            icon="i-lucide-download"
            tag="a"
            :href="analyticsApi.exportPanelUrl('failed-logins', 'csv', { limit: 200 })"
            download
            title="Export this panel as CSV"
          />
        </div>
      </template>
      <div class="divide-y divide-default max-h-64 overflow-y-auto">
        <div v-for="(f, i) in failedLogins" :key="i" class="py-1.5 flex items-center gap-3 text-sm">
          <UBadge color="error" variant="subtle" size="xs">{{ f.username || 'unknown' }}</UBadge>
          <span class="flex-1 font-mono text-xs text-muted truncate" :title="f.user_agent">{{ f.ip_address }}</span>
          <span v-if="f.reason" class="text-xs text-muted italic">{{ f.reason }}</span>
          <span class="text-xs text-muted shrink-0">{{ new Date(f.timestamp).toLocaleString() }}</span>
        </div>
      </div>
    </UCard>

    <!-- Recent Activity Feed -->
    <UCard v-if="panelVisibility.recent && recentActivity.length > 0">
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

    <!-- Live event tail — only visible while the SSE connection is active.
         Streams every tracked event in real time so admins can watch the
         server breathe. Capped at 100 rows in-memory; the live tail
         intentionally has no auto-scroll-to-bottom because admins
         frequently want to read recent rows without being yanked. -->
    <UCard v-if="liveTailOpen">
      <template #header>
        <div class="flex items-center justify-between">
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-radio" class="size-4 text-emerald-500" />
            Live Event Tail
            <span class="relative flex h-2 w-2 ml-1">
              <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
              <span class="relative inline-flex rounded-full h-2 w-2 bg-emerald-500" />
            </span>
            <span class="text-xs font-normal text-muted">{{ liveTail.length }} / {{ liveTailMax }}</span>
          </div>
          <UButton size="xs" variant="ghost" color="neutral" icon="i-lucide-trash" label="Clear" @click="liveTail = []" />
        </div>
      </template>
      <div v-if="liveTail.length === 0" class="text-center text-sm text-muted py-3 italic">
        Waiting for events…
      </div>
      <div v-else class="divide-y divide-default max-h-72 overflow-y-auto">
        <div v-for="(ev, i) in liveTail" :key="`${ev.id}-${i}`" class="py-1.5 flex items-start gap-3 text-sm">
          <UBadge
            :color="ev.type === 'server_error' || ev.type === 'login_failed' || ev.type === 'error' ? 'error' :
                    ev.type === 'login' || ev.type === 'register' ? 'success' :
                    ev.type === 'mature_blocked' || ev.type === 'permission_denied' ? 'warning' : 'neutral'"
            variant="subtle"
            size="xs"
          >
            {{ ev.type }}
          </UBadge>
          <div class="flex-1 min-w-0 text-xs text-muted flex items-center gap-2 flex-wrap">
            <span v-if="ev.user_id" class="font-medium">{{ ev.user_id }}</span>
            <span v-if="ev.ip_address" class="font-mono">{{ ev.ip_address }}</span>
            <span v-if="ev.media_id" class="font-mono">{{ ev.media_id.slice(0, 8) }}…</span>
          </div>
          <span class="text-xs text-muted shrink-0">{{ new Date(ev.timestamp).toLocaleTimeString() }}</span>
        </div>
      </div>
    </UCard>

    <!-- Event drill-down -->
    <UCard v-if="panelVisibility.drill">
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

    <!-- Top media — clickable rows open the per-media analytics modal. -->
    <UCard v-if="panelVisibility.topMedia && topMedia.length > 0">
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
          { accessorKey: 'media_id', header: '' },
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
        <template #media_id-cell="{ row }">
          <UButton
            size="xs"
            variant="ghost"
            color="neutral"
            icon="i-lucide-bar-chart-3"
            label="Drill"
            @click="openMediaDetail(row.original.media_id, getDisplayTitle(row.original))"
          />
        </template>
      </UTable>
    </UCard>

    <!-- Per-media analytics modal — opens when a Top Media row is clicked.
         Mirrors the per-user pattern: cached stats on top, view + playback
         sparklines below. -->
    <UModal v-model:open="mediaDetailOpen" :ui="{ content: 'max-w-3xl' }">
      <template #content>
        <UCard>
          <template #header>
            <div class="flex items-center justify-between">
              <div class="font-semibold flex items-center gap-2">
                <UIcon name="i-lucide-bar-chart-3" class="size-4" />
                Media Analytics
              </div>
              <UButton size="xs" variant="ghost" color="neutral" icon="i-lucide-x" @click="mediaDetailOpen = false" />
            </div>
          </template>
          <div class="space-y-4">
            <p class="text-sm font-medium truncate" :title="mediaDetailTitle">{{ mediaDetailTitle }}</p>
            <div v-if="mediaDetailLoading" class="flex justify-center py-6">
              <UIcon name="i-lucide-loader-2" class="animate-spin size-5 text-muted" />
            </div>
            <div v-else-if="mediaDetail">
              <div class="grid grid-cols-2 sm:grid-cols-4 gap-3 mb-3">
                <UCard :ui="{ body: 'p-2' }">
                  <p class="text-base font-bold text-highlighted">{{ (mediaDetail.stats.total_views ?? 0).toLocaleString() }}</p>
                  <p class="text-[11px] text-muted">Views</p>
                </UCard>
                <UCard :ui="{ body: 'p-2' }">
                  <p class="text-base font-bold text-highlighted">{{ (mediaDetail.stats.unique_viewers ?? 0).toLocaleString() }}</p>
                  <p class="text-[11px] text-muted">Unique viewers</p>
                </UCard>
                <UCard :ui="{ body: 'p-2' }">
                  <p class="text-base font-bold text-highlighted">{{ formatPct((mediaDetail.stats.completion_rate ?? 0) * 100) }}</p>
                  <p class="text-[11px] text-muted">Completion rate</p>
                </UCard>
                <UCard :ui="{ body: 'p-2' }">
                  <p class="text-base font-bold text-highlighted">{{ formatWatchTime(mediaDetail.stats.avg_watch_duration ?? 0) }}</p>
                  <p class="text-[11px] text-muted">Avg watch</p>
                </UCard>
              </div>
              <p class="text-xs uppercase tracking-wide font-semibold text-muted mb-1">Activity (last 30 days)</p>
              <MetricLineChart
                :series="[
                  { label: 'Views', color: 'stroke-primary text-primary', values: mediaDetail.view_timeline },
                  { label: 'Playbacks', color: 'stroke-emerald-500 text-emerald-500', values: mediaDetail.playback_timeline },
                ]"
                :height="160"
              />
              <!-- Playback abandonment histogram. Each bucket = a 10%
                   progress range (0-10, 10-20, …); the bar height is the
                   count of playback events that stopped in that range.
                   Strong drop-offs at the start/end signal "intro too
                   long" or "credits skipped" patterns. -->
              <div v-if="mediaDetail.abandonment && mediaDetail.abandonment.some(b => b.count > 0)" class="mt-4">
                <p class="text-xs uppercase tracking-wide font-semibold text-muted mb-1">Playback abandonment</p>
                <div class="flex items-end gap-1 h-24">
                  <div
                    v-for="(b, i) in mediaDetail.abandonment"
                    :key="i"
                    class="flex-1 flex flex-col items-center justify-end gap-0.5 group relative"
                  >
                    <div
                      :class="['w-full rounded-t transition-colors min-h-[2px]',
                        i < 2 ? 'bg-warning/70' :
                        i >= 8 ? 'bg-emerald-500/70' :
                        'bg-primary/70']"
                      :style="{ height: `${Math.max(2, (b.count / Math.max(...mediaDetail.abandonment.map(x => x.count), 1)) * 100)}%` }"
                      :title="`${b.range}: ${b.count} playbacks ended in this range`"
                    />
                    <span class="text-[9px] text-muted leading-none">{{ b.range }}</span>
                  </div>
                </div>
                <p class="text-[10px] text-muted mt-1 italic">
                  Bars in the early range (warning) often mean the intro is losing viewers; bars near 100% (success) mean the media is being completed.
                </p>
              </div>
            </div>
            <div v-else class="text-center text-sm text-muted py-4">
              No data available for this item.
            </div>
          </div>
        </UCard>
      </template>
    </UModal>

    <!-- Daily breakdown -->
    <UCard v-if="panelVisibility.daily && daily.length > 0">
      <template #header>
        <div class="flex items-center justify-between gap-2 flex-wrap">
          <div class="font-semibold flex items-center gap-2">
            <UIcon name="i-lucide-bar-chart-2" class="size-4" />
            Daily Breakdown
            <span v-if="drillDateFilter" class="text-xs font-normal text-muted">(filtered: {{ drillDateFilter }})</span>
          </div>
          <div class="flex items-center gap-2">
            <UInput
              v-model="drillDateFilter"
              size="xs"
              type="date"
              placeholder="Filter date"
              class="w-40"
            />
            <UButton
              v-if="drillDateFilter"
              size="xs"
              variant="ghost"
              color="neutral"
              icon="i-lucide-x"
              @click="drillDateFilter = ''"
            />
          </div>
        </div>
      </template>

      <!-- CSS bar chart — views per day. Clickable rows so admins can
           drill the daily breakdown by clicking the bar instead of typing. -->
      <div class="mb-4 space-y-1">
        <div
          v-for="row in dailyReversed"
          :key="row.date"
          class="flex items-center gap-2 text-xs cursor-pointer hover:bg-muted/10 rounded px-1 py-0.5"
          @click="drillDateFilter = row.date === drillDateFilter ? '' : row.date"
        >
          <span class="w-24 shrink-0 font-mono text-muted text-right">{{ row.date }}</span>
          <div class="flex-1 bg-muted/20 rounded-full h-4 overflow-hidden">
            <div
              class="h-full rounded-full bg-primary transition-all duration-300"
              :class="drillDateFilter && row.date !== drillDateFilter ? 'opacity-30' : ''"
              :style="{ width: dailyMaxViews > 0 ? `${Math.round(((row.total_views ?? 0) / dailyMaxViews) * 100)}%` : '0%' }"
            />
          </div>
          <span class="w-12 shrink-0 text-right text-muted">{{ (row.total_views ?? 0).toLocaleString() }}</span>
        </div>
      </div>

      <UTable
        :data="filteredDaily"
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

    <!-- Module diagnostics — analytics module's own internal counters.
         Helps debug "why is the dashboard slow / stale" without server
         log access. Hidden by default to keep the page tidy; admins
         re-enable it from the panels menu when investigating. -->
    <UCard v-if="panelVisibility.diagnostics && diagnostics" :ui="{ body: 'p-3' }">
      <template #header>
        <div class="font-semibold flex items-center gap-2 text-muted">
          <UIcon name="i-lucide-activity" class="size-4" />
          Analytics Module Diagnostics
          <UBadge :color="diagnostics.healthy ? 'success' : 'error'" variant="subtle" size="xs">
            {{ diagnostics.healthy ? 'Healthy' : 'Unhealthy' }}
          </UBadge>
        </div>
      </template>
      <div class="grid grid-cols-2 sm:grid-cols-4 lg:grid-cols-6 gap-2 text-xs">
        <div>
          <p class="font-bold text-highlighted">{{ diagnostics.cache_entries.toLocaleString() }}</p>
          <p class="text-muted">Cached aggregations</p>
        </div>
        <div>
          <p class="font-bold text-highlighted">{{ diagnostics.dirty_days.toLocaleString() }}</p>
          <p class="text-muted">Dirty days (pending flush)</p>
        </div>
        <div>
          <p class="font-bold text-highlighted">{{ diagnostics.active_subscribers.toLocaleString() }}</p>
          <p class="text-muted">Live SSE subscribers</p>
        </div>
        <div>
          <p class="font-bold text-highlighted">{{ diagnostics.sessions_tracked.toLocaleString() }}</p>
          <p class="text-muted">In-mem sessions</p>
        </div>
        <div>
          <p class="font-bold text-highlighted">{{ diagnostics.media_tracked.toLocaleString() }}</p>
          <p class="text-muted">Tracked media items</p>
        </div>
        <div>
          <p class="font-bold text-highlighted">{{ diagnostics.max_reconstruct_events.toLocaleString() }}</p>
          <p class="text-muted">Max reconstruct cap</p>
        </div>
      </div>
    </UCard>

    <!-- Empty state -->
    <div v-if="!loading && !summary" class="text-center py-12 text-muted">
      <UIcon name="i-lucide-bar-chart" class="size-10 mb-3 mx-auto opacity-40" />
      <p class="text-lg font-medium">No analytics data</p>
      <p class="text-sm mt-1">Analytics events will appear here as users interact with media.</p>
    </div>
  </div>
</template>

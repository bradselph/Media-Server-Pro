import {useState} from 'react'
import {Link} from 'react-router-dom'
import {useQuery} from '@tanstack/react-query'
import {adminApi, analyticsApi} from '@/api/endpoints'
import type {AnalyticsEvent, EventStats, SuggestionStats} from '@/api/types'
import {errMsg} from './adminUtils'

const ADMIN_STAT_CARD = 'admin-stat-card'
const TEXT_MUTED = 'var(--text-muted)'

// ── Tab: Analytics ────────────────────────────────────────────────────────────

export function AnalyticsTab() {
    const [exportingAnalytics, setExportingAnalytics] = useState(false)
    const [exportingAuditLog, setExportingAuditLog] = useState(false)
    const [exportError, setExportError] = useState<string | null>(null)
    const [eventsByTypeFilter, setEventsByTypeFilter] = useState<string>('')
    const [eventsByMediaId, setEventsByMediaId] = useState('')

    const {data: summary} = useQuery({
        queryKey: ['analytics-summary'],
        queryFn: () => analyticsApi.getSummary(),
    })

    const {data: topMedia = []} = useQuery({
        queryKey: ['analytics-top'],
        queryFn: () => adminApi.getTopMedia(10),
    })

    const {data: eventCounts} = useQuery({
        queryKey: ['analytics-event-counts'],
        queryFn: () => adminApi.getEventTypeCounts(),
    })

    const {data: eventsByType = []} = useQuery<AnalyticsEvent[]>({
        queryKey: ['analytics-events-by-type', eventsByTypeFilter],
        queryFn: () => adminApi.getEventsByType(eventsByTypeFilter, 100),
        enabled: !!eventsByTypeFilter,
    })

    const {data: eventsByMedia = []} = useQuery<AnalyticsEvent[]>({
        queryKey: ['analytics-events-by-media', eventsByMediaId],
        queryFn: () => adminApi.getEventsByMedia(eventsByMediaId, 100),
        enabled: !!eventsByMediaId.trim(),
    })

    // Feature 5: Event stats
    const {data: eventStats} = useQuery<EventStats>({
        queryKey: ['analytics-event-stats'],
        queryFn: () => adminApi.getEventStats(),
    })

    // Feature 9: Suggestion stats
    const {data: suggestionStats} = useQuery<SuggestionStats>({
        queryKey: ['admin-suggestion-stats'],
        queryFn: () => adminApi.getSuggestionStats(),
    })

    async function handleExportAnalytics() {
        setExportingAnalytics(true)
        setExportError(null)
        try {
            const blob = await adminApi.exportAnalytics()
            const url = window.URL.createObjectURL(blob)
            const a = document.createElement('a')
            a.href = url
            a.download = `analytics-export-${new Date().toISOString().slice(0, 10)}.csv`
            document.body.appendChild(a)
            a.click()
            document.body.removeChild(a)
            window.URL.revokeObjectURL(url)
        } catch (err) {
            setExportError(errMsg(err))
        } finally {
            setExportingAnalytics(false)
        }
    }

    async function handleExportAuditLog() {
        setExportingAuditLog(true)
        setExportError(null)
        try {
            const res = await fetch(adminApi.exportAuditLogUrl(), { credentials: 'include' })
            if (!res.ok) {
                const text = await res.text().catch(() => '')
                throw new Error(text || `Export failed (${res.status})`)
            }
            const blob = await res.blob()
            const url = window.URL.createObjectURL(blob)
            const a = document.createElement('a')
            a.href = url
            a.download = `audit-log-${new Date().toISOString().slice(0, 10)}.csv`
            document.body.appendChild(a)
            a.click()
            document.body.removeChild(a)
            window.URL.revokeObjectURL(url)
        } catch (err) {
            setExportError(errMsg(err))
        } finally {
            setExportingAuditLog(false)
        }
    }

    return (
        <div>
            <div className="admin-card">
                <h2>Analytics Overview</h2>
                {summary?.analytics_disabled && (
                    <p style={{color: TEXT_MUTED, fontSize: 13}}>Analytics is disabled. Enable it in server
                        settings to collect data.</p>
                )}
                {summary && !summary.analytics_disabled && (
                    <div className="admin-stats-grid">
                        <div className={ADMIN_STAT_CARD}>
                            <span className="admin-stat-value">{(summary.total_events ?? 0).toLocaleString()}</span>
                            <span className="admin-stat-label">Total Events</span>
                        </div>
                        <div className={ADMIN_STAT_CARD}>
                            <span className="admin-stat-value">{(summary.unique_clients ?? 0).toLocaleString()}</span>
                            <span className="admin-stat-label">Unique Clients</span>
                        </div>
                        <div className={ADMIN_STAT_CARD}>
                            <span className="admin-stat-value">{summary.active_sessions ?? 0}</span>
                            <span className="admin-stat-label">Active Now</span>
                        </div>
                        <div className={ADMIN_STAT_CARD}>
                            <span className="admin-stat-value">{(summary.total_views ?? 0).toLocaleString()}</span>
                            <span className="admin-stat-label">Total Views</span>
                        </div>
                        <div className={ADMIN_STAT_CARD}>
                            <span className="admin-stat-value">{(summary.today_views ?? 0).toLocaleString()}</span>
                            <span className="admin-stat-label">Views Today</span>
                        </div>
                        <div className={ADMIN_STAT_CARD}>
                            <span className="admin-stat-value">{(summary.total_media ?? 0).toLocaleString()}</span>
                            <span className="admin-stat-label">Media Tracked</span>
                        </div>
                    </div>
                )}
                {/* Feature 5: Event detail stats — guard empty/partial response when analytics disabled */}
                {eventStats && (eventStats.total_events !== null || (eventStats.event_counts && Object.keys(eventStats.event_counts).length > 0) || (eventStats.hourly_events?.length ?? 0) > 0) && (
                    <>
                    <div className="admin-stats-grid" style={{marginTop: 12}}>
                        <div className={ADMIN_STAT_CARD}>
                            <span className="admin-stat-value">{(eventStats.event_counts && Object.keys(eventStats.event_counts).length) ?? 0}</span>
                            <span className="admin-stat-label">Event Types</span>
                        </div>
                        <div className={ADMIN_STAT_CARD}>
                            <span className="admin-stat-value">{(eventStats.total_events ?? 0).toLocaleString()}</span>
                            <span className="admin-stat-label">Total Tracked</span>
                        </div>
                    </div>
                    {/* Hourly activity distribution */}
                    {eventStats.hourly_events && eventStats.hourly_events.length > 0 && eventStats.hourly_events.some(v => v > 0) && (
                        <div style={{marginTop: 12}}>
                            <h3 style={{fontSize: 14, margin: '0 0 8px'}}>Today&apos;s Hourly Activity</h3>
                            <div style={{display: 'flex', alignItems: 'flex-end', gap: 2, height: 60}}>
                                {eventStats.hourly_events.map((count, hour) => {
                                    const max = Math.max(...eventStats.hourly_events)
                                    const pct = max > 0 ? (count / max) * 100 : 0
                                    return (
                                        <div key={hour} title={`${hour}:00 — ${count} events`}
                                             style={{flex: 1, minWidth: 0, background: 'var(--color-primary, #3b82f6)',
                                                     borderRadius: '2px 2px 0 0', height: `${Math.max(pct, 2)}%`,
                                                     opacity: count > 0 ? 1 : 0.15}} />
                                    )
                                })}
                            </div>
                            <div style={{display: 'flex', justifyContent: 'space-between', fontSize: 10, color: TEXT_MUTED, marginTop: 2}}>
                                <span>0:00</span><span>6:00</span><span>12:00</span><span>18:00</span><span>23:00</span>
                            </div>
                        </div>
                    )}
                    </>
                )}
                {/* Feature 9: Suggestion stats — guard partial/empty when suggestions module unavailable */}
                {suggestionStats && (
                    <div className="admin-stats-grid" style={{marginTop: 12}}>
                        <div className={ADMIN_STAT_CARD}>
                            <span className="admin-stat-value">{(suggestionStats.total_profiles ?? 0).toLocaleString()}</span>
                            <span className="admin-stat-label">User Profiles</span>
                        </div>
                        <div className={ADMIN_STAT_CARD}>
                            <span className="admin-stat-value">{(suggestionStats.total_media ?? 0).toLocaleString()}</span>
                            <span className="admin-stat-label">Media Tracked</span>
                        </div>
                        <div className={ADMIN_STAT_CARD}>
                            <span className="admin-stat-value">{(suggestionStats.total_views ?? 0).toLocaleString()}</span>
                            <span className="admin-stat-label">Views Tracked</span>
                        </div>
                        <div className={ADMIN_STAT_CARD}>
                            <span className="admin-stat-value">{(suggestionStats.total_watch_time ?? 0) > 3600 ? `${((suggestionStats.total_watch_time ?? 0) / 3600).toFixed(1)}h` : `${Math.round((suggestionStats.total_watch_time ?? 0) / 60)}m`}</span>
                            <span className="admin-stat-label">Watch Time</span>
                        </div>
                    </div>
                )}
                {exportError && (
                    <p style={{color: 'var(--color-error)', fontSize: 13, marginTop: 8}}>{exportError}</p>
                )}
                <div className="admin-action-row" style={{marginTop: 8}}>
                    <button type="button" className="admin-btn" onClick={handleExportAuditLog} disabled={exportingAuditLog}>
                        <i className="bi bi-download"/> {exportingAuditLog ? 'Exporting...' : 'Export Audit Log'}
                    </button>
                    <button className="admin-btn" onClick={handleExportAnalytics} disabled={exportingAnalytics}>
                        <i className="bi bi-file-earmark-spreadsheet"/> {exportingAnalytics ? 'Exporting...' : 'Export Analytics CSV'}
                    </button>
                </div>
            </div>

            {/* Top Media */}
            {topMedia.length > 0 && (
                <div className="admin-card">
                    <h2>Top Viewed Media</h2>
                    <div className="admin-table-wrapper">
                        <table className="admin-table">
                            <thead>
                            <tr>
                                <th>#</th>
                                <th>Title</th>
                                <th>Views</th>
                            </tr>
                            </thead>
                            <tbody>
                            {topMedia.map((item, i) => (
                                <tr key={item.media_id}>
                                    <td>{i + 1}</td>
                                    <td>
                                        <Link to={`/player?id=${encodeURIComponent(item.media_id)}`}
                                           style={{color: 'var(--text-color)'}}>
                                            {item.filename}
                                        </Link>
                                    </td>
                                    <td>{item.views.toLocaleString()}</td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                </div>
            )}

            {/* Recent Activity from summary */}
            {summary?.recent_activity && summary.recent_activity.length > 0 && (
                <div className="admin-card">
                    <h2>Recent Activity</h2>
                    <div className="admin-table-wrapper">
                        <table className="admin-table">
                            <thead>
                            <tr>
                                <th>Event</th>
                                <th>Media</th>
                                <th>Time</th>
                            </tr>
                            </thead>
                            <tbody>
                            {summary.recent_activity.map((act, i) => (
                                <tr key={i}>
                                    <td><span className="status-badge">{act.type}</span></td>
                                    <td>{act.filename}</td>
                                    <td>{new Date(act.timestamp * 1000).toLocaleString()}</td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                </div>
            )}

            {/* Event Type Breakdown */}
            {eventCounts && Object.keys(eventCounts).length > 0 && (
                <div className="admin-card">
                    <h2>Event Type Breakdown</h2>
                    <div className="admin-table-wrapper">
                        <table className="admin-table">
                            <thead>
                            <tr>
                                <th>Event Type</th>
                                <th>Count</th>
                            </tr>
                            </thead>
                            <tbody>
                            {Object.entries(eventCounts).sort((a, b) => b[1] - a[1]).map(([type, count]) => (
                                <tr key={type}>
                                    <td>{type}</td>
                                    <td>{count.toLocaleString()}</td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                    <div style={{marginTop: 12}}>
                        <label style={{marginRight: 8}}>View events by type:</label>
                        <select
                            className="admin-select"
                            value={eventsByTypeFilter}
                            onChange={e => { setEventsByTypeFilter(e.target.value); }}
                        >
                            <option value="">— Select type —</option>
                            {Object.keys(eventCounts).sort().map(t => (
                                <option key={t} value={t}>{t}</option>
                            ))}
                        </select>
                        {eventsByTypeFilter && eventsByType.length > 0 && (
                            <div className="admin-table-wrapper" style={{marginTop: 8, maxHeight: 200, overflow: 'auto'}}>
                                <table className="admin-table">
                                    <thead>
                                    <tr>
                                        <th>Time</th>
                                        <th>Media ID</th>
                                        <th>User ID</th>
                                    </tr>
                                    </thead>
                                    <tbody>
                                    {eventsByType.slice(0, 50).map((ev, i) => (
                                        <tr key={ev.id ?? i}>
                                            <td>{ev.timestamp ? new Date(ev.timestamp).toLocaleString() : '—'}</td>
                                            <td style={{fontSize: 12}}>{ev.media_id ?? '—'}</td>
                                            <td style={{fontSize: 12}}>{ev.user_id ?? '—'}</td>
                                        </tr>
                                    ))}
                                    </tbody>
                                </table>
                                {eventsByType.length > 50 && <p style={{fontSize: 12, color: TEXT_MUTED}}>Showing first 50 of {eventsByType.length}</p>}
                            </div>
                        )}
                    </div>
                </div>
            )}

            {/* Events by media */}
            <div className="admin-card">
                <h2>Events by Media</h2>
                <p style={{fontSize: 13, color: TEXT_MUTED, marginBottom: 8}}>Enter a media ID (e.g. from Top Viewed Media) to list recent events.</p>
                <div style={{display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap'}}>
                    <input
                        type="text"
                        className="admin-input"
                        placeholder="Media ID"
                        value={eventsByMediaId}
                        onChange={e => { setEventsByMediaId(e.target.value); }}
                        style={{minWidth: 200}}
                    />
                </div>
                {eventsByMediaId.trim() && eventsByMedia.length > 0 && (
                    <div className="admin-table-wrapper" style={{marginTop: 12, maxHeight: 200, overflow: 'auto'}}>
                        <table className="admin-table">
                            <thead>
                            <tr>
                                <th>Time</th>
                                <th>Type</th>
                                <th>User ID</th>
                            </tr>
                            </thead>
                            <tbody>
                            {eventsByMedia.slice(0, 50).map((ev, i) => (
                                <tr key={ev.id ?? i}>
                                    <td>{ev.timestamp ? new Date(ev.timestamp).toLocaleString() : '—'}</td>
                                    <td><span className="status-badge">{ev.type ?? '—'}</span></td>
                                    <td style={{fontSize: 12}}>{ev.user_id ?? '—'}</td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                        {eventsByMedia.length > 50 && <p style={{fontSize: 12, color: TEXT_MUTED}}>Showing first 50 of {eventsByMedia.length}</p>}
                    </div>
                )}
            </div>
        </div>
    )
}

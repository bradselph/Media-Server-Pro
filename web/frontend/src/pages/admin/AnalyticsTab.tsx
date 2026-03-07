import {useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {adminApi, analyticsApi} from '@/api/endpoints'
import type {EventStats, SuggestionStats} from '@/api/types'
import {errMsg} from './helpers'

// ── Tab: Analytics ────────────────────────────────────────────────────────────

export function AnalyticsTab() {
    const [exportingAnalytics, setExportingAnalytics] = useState(false)
    const [exportError, setExportError] = useState<string | null>(null)

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

    return (
        <div>
            <div className="admin-card">
                <h2>Analytics Overview</h2>
                {summary?.analytics_disabled && (
                    <p style={{color: 'var(--text-muted)', fontSize: 13}}>Analytics is disabled. Enable it in server
                        settings to collect data.</p>
                )}
                {summary && !summary.analytics_disabled && (
                    <div className="admin-stats-grid">
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{(summary.total_events ?? 0).toLocaleString()}</span>
                            <span className="admin-stat-label">Total Events</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{(summary.unique_clients ?? 0).toLocaleString()}</span>
                            <span className="admin-stat-label">Unique Clients</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{summary.active_sessions ?? 0}</span>
                            <span className="admin-stat-label">Active Now</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{(summary.total_views ?? 0).toLocaleString()}</span>
                            <span className="admin-stat-label">Total Views</span>
                        </div>
                    </div>
                )}
                {/* Feature 5: Event detail stats */}
                {eventStats && (
                    <div className="admin-stats-grid" style={{marginTop: 12}}>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{Object.keys(eventStats.event_counts).length}</span>
                            <span className="admin-stat-label">Event Types</span>
                        </div>
                    </div>
                )}
                {/* Feature 9: Suggestion stats */}
                {suggestionStats && (
                    <div className="admin-stats-grid" style={{marginTop: 12}}>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{suggestionStats.total_profiles.toLocaleString()}</span>
                            <span className="admin-stat-label">User Profiles</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{suggestionStats.total_media.toLocaleString()}</span>
                            <span className="admin-stat-label">Media Tracked</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{suggestionStats.total_views.toLocaleString()}</span>
                            <span className="admin-stat-label">Views Tracked</span>
                        </div>
                    </div>
                )}
                {exportError && (
                    <p style={{color: 'var(--color-error)', fontSize: 13, marginTop: 8}}>{exportError}</p>
                )}
                <div className="admin-action-row" style={{marginTop: 8}}>
                    <a href={adminApi.exportAuditLogUrl()} className="admin-btn">
                        <i className="bi bi-download"/> Export Audit Log
                    </a>
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
                                        <a href={`/player?id=${encodeURIComponent(item.media_id)}`}
                                           style={{color: 'var(--text-color)'}}>
                                            {item.filename}
                                        </a>
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
                </div>
            )}
        </div>
    )
}

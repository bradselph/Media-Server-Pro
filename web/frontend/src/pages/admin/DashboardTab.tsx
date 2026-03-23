import {useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {adminApi} from '@/api/endpoints'
import type {StreamSession, UploadProgress} from '@/api/types'
import {errMsg, formatBytes, formatUptime} from './adminUtils'

export function DashboardTab() {
    const [actionMsg, setActionMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

    const {data: stats, isLoading: statsLoading} = useQuery({
        queryKey: ['admin-stats'],
        queryFn: () => adminApi.getStats(),
        refetchInterval: 30000,
    })

    const {data: system, isLoading: sysLoading} = useQuery({
        queryKey: ['admin-system'],
        queryFn: () => adminApi.getSystemInfo(),
        refetchInterval: 60000,
    })

    const {data: activeStreams = [], isError: streamsError} = useQuery<StreamSession[]>({
        queryKey: ['admin-active-streams'],
        queryFn: () => adminApi.getActiveStreams(),
        refetchInterval: 10000,
        retry: 1,
    })

    const {data: activeUploads = [], isError: uploadsError} = useQuery<UploadProgress[]>({
        queryKey: ['admin-active-uploads'],
        queryFn: () => adminApi.getActiveUploads(),
        refetchInterval: 10000,
        retry: 1,
    })

    async function handleAction(fn: () => Promise<unknown>, successMsg: string) {
        setActionMsg(null)
        try {
            await fn()
            setActionMsg({type: 'success', text: successMsg})
        } catch (err: unknown) {
            setActionMsg({type: 'error', text: errMsg(err)})
        }
    }

    const diskPct = stats ? Math.round(((stats.disk_usage ?? 0) / ((stats.disk_total ?? 0) || 1)) * 100) : 0
    const memPct = system ? Math.round(((system.memory_used ?? 0) / ((system.memory_total ?? 0) || 1)) * 100) : 0
    let diskFillClass = ''
    if (diskPct > 90) diskFillClass = 'danger'
    else if (diskPct > 70) diskFillClass = 'warning'

    return (
        <div>
            {actionMsg && (
                <div className={`admin-alert admin-alert-${actionMsg.type === 'success' ? 'success' : 'danger'}`}>
                    {actionMsg.text}
                </div>
            )}

            {statsLoading ? (
                <p style={{color: 'var(--text-muted)'}}>Loading statistics...</p>
            ) : stats && (
                <>
                    <div className="admin-stats-grid">
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{(stats.total_videos ?? 0).toLocaleString()}</span>
                            <span className="admin-stat-label">Videos</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{(stats.total_audio ?? 0).toLocaleString()}</span>
                            <span className="admin-stat-label">Audio Files</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{(stats.total_users ?? 0).toLocaleString()}</span>
                            <span className="admin-stat-label">Total Users</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{(stats.active_sessions ?? 0).toLocaleString()}</span>
                            <span className="admin-stat-label">Active Streams</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{(stats.total_views ?? 0).toLocaleString()}</span>
                            <span className="admin-stat-label">Total Views</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{stats.hls_jobs_running ?? 0}</span>
                            <span className="admin-stat-label">HLS Running</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{stats.hls_jobs_completed ?? 0}</span>
                            <span className="admin-stat-label">HLS Completed</span>
                        </div>
                    </div>

                    <div className="admin-card">
                        <h2>Storage</h2>
                        <div>
                            <div style={{
                                display: 'flex',
                                justifyContent: 'space-between',
                                fontSize: 13,
                                marginBottom: 4
                            }}>
                                <span>Disk Usage</span>
                                <span>{formatBytes(stats.disk_usage ?? 0)} / {formatBytes(stats.disk_total ?? 0)}</span>
                            </div>
                            <div className="disk-usage-bar">
                                <div
                                    className={`disk-usage-fill ${diskFillClass}`}
                                    style={{width: `${diskPct}%`}}
                                />
                            </div>
                            <div style={{fontSize: 12, color: 'var(--text-muted)', marginTop: 4}}>
                                {diskPct}% used · {formatBytes(stats.disk_free ?? 0)} free
                            </div>
                        </div>
                    </div>

                    <div className="admin-card">
                        <h2>Live streams</h2>
                        <p style={{fontSize: 12, color: 'var(--text-muted)', margin: '0 0 10px'}}>
                            Concurrent playback sessions (refreshes every 10s). Matches the “Active Streams” total above when all are counted server-side.
                        </p>
                        {streamsError ? (
                            <p style={{color: 'var(--text-muted)', fontSize: 13}}>Could not load active streams.</p>
                        ) : activeStreams.length === 0 ? (
                            <p style={{color: 'var(--text-muted)', fontSize: 13}}>No active streams.</p>
                        ) : (
                            <div className="admin-table-wrapper">
                                <table className="admin-table">
                                    <thead>
                                    <tr>
                                        <th>User</th>
                                        <th>Media ID</th>
                                        <th>Quality</th>
                                        <th>Sent</th>
                                        <th>Client IP</th>
                                        <th>Since</th>
                                    </tr>
                                    </thead>
                                    <tbody>
                                    {activeStreams.map(s => (
                                        <tr key={s.id}>
                                            <td style={{fontSize: 12}}>{s.user_id || '—'}</td>
                                            <td style={{fontSize: 11, fontFamily: 'monospace'}} title={s.media_id}>
                                                {s.media_id && s.media_id.length > 8 ? `${s.media_id.slice(0, 8)}…` : (s.media_id || '—')}
                                            </td>
                                            <td>{s.quality || '—'}</td>
                                            <td>{formatBytes(s.bytes_sent ?? 0)}</td>
                                            <td style={{fontSize: 12}}>{s.ip_address || '—'}</td>
                                            <td style={{fontSize: 12}}>{new Date(s.started_at).toLocaleString()}</td>
                                        </tr>
                                    ))}
                                    </tbody>
                                </table>
                            </div>
                        )}
                    </div>

                    <div className="admin-card">
                        <h2>Active uploads</h2>
                        {uploadsError ? (
                            <p style={{color: 'var(--text-muted)', fontSize: 13}}>Could not load active uploads.</p>
                        ) : activeUploads.length === 0 ? (
                            <p style={{color: 'var(--text-muted)', fontSize: 13}}>No uploads in progress.</p>
                        ) : (
                            <div className="admin-table-wrapper">
                                <table className="admin-table">
                                    <thead>
                                    <tr>
                                        <th>File</th>
                                        <th>User</th>
                                        <th>Progress</th>
                                        <th>Status</th>
                                    </tr>
                                    </thead>
                                    <tbody>
                                    {activeUploads.map(u => (
                                        <tr key={u.id}>
                                            <td style={{fontSize: 12}}>{u.filename}</td>
                                            <td style={{fontSize: 12}}>{u.user_id || '—'}</td>
                                            <td>{typeof u.progress === 'number' ? `${Math.round(u.progress)}%` : '—'}</td>
                                            <td>{u.status}</td>
                                        </tr>
                                    ))}
                                    </tbody>
                                </table>
                            </div>
                        )}
                    </div>
                </>
            )}

            {!sysLoading && system && (
                <div className="admin-card">
                    <h2>System Info</h2>
                    <div style={{
                        display: 'grid',
                        gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))',
                        gap: 12,
                        marginBottom: 16
                    }}>
                        <div><strong>Version:</strong> {typeof system.version === 'string' ? system.version : '—'}</div>
                        <div><strong>OS:</strong> {system.os}/{system.arch}</div>
                        <div><strong>Go:</strong> {typeof system.go_version === 'string' ? system.go_version : '—'}</div>
                        <div><strong>Uptime:</strong> {formatUptime(system.uptime)}</div>
                        <div><strong>CPUs:</strong> {system.cpu_count}</div>
                        <div>
                            <strong>Memory:</strong> {formatBytes(system.memory_used)} / {formatBytes(system.memory_total)}
                            <div className="admin-progress-bg" style={{marginTop: 4}}>
                                <div className="admin-progress-fill" style={{width: `${memPct}%`}}/>
                            </div>
                        </div>
                    </div>
                    {system.modules?.length > 0 && (
                        <>
                            <h3 style={{margin: '0 0 10px'}}>Module Health</h3>
                            <div className="module-health-grid">
                                {system.modules.map(m => (
                                    <div key={m.name} className="module-health-item">
                                        <span className="module-health-name">{m.name}</span>
                                        <span className={`status-badge status-${m.status}`}>{m.status}</span>
                                    </div>
                                ))}
                            </div>
                        </>
                    )}
                </div>
            )}

            <div className="admin-card">
                <h2>Server Controls</h2>
                <div className="admin-action-row">
                    <button className="admin-btn"
                            onClick={() => handleAction(() => adminApi.clearCache(), 'Cache cleared')}>
                        <i className="bi bi-trash-fill"/> Clear Cache
                    </button>
                    <button className="admin-btn"
                            onClick={() => handleAction(() => adminApi.scanMedia(), 'Media scan triggered')}>
                        <i className="bi bi-search"/> Scan Media
                    </button>
                    <button className="admin-btn admin-btn-warning" onClick={() => {
                        if (window.confirm('Restart the server? Active streams will be interrupted.')) {
                            handleAction(() => adminApi.restartServer(), 'Server restarting… page will reload in 10s')
                            setTimeout(() => { window.location.reload(); }, 10000)
                        }
                    }}>
                        <i className="bi bi-arrow-clockwise"/> Restart Server
                    </button>
                    <button className="admin-btn admin-btn-danger" onClick={() => {
                        if (window.confirm('Shut down the server?'))
                            handleAction(() => adminApi.shutdownServer(), 'Server shutting down...')
                    }}>
                        <i className="bi bi-power"/> Shutdown
                    </button>
                </div>
            </div>
        </div>
    )
}

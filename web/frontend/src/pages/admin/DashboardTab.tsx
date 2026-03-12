import {useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {adminApi} from '@/api/endpoints'
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
                            <span className="admin-stat-label">Active Sessions</span>
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
                        <div><strong>Version:</strong> {system.version}</div>
                        <div><strong>OS:</strong> {system.os}/{system.arch}</div>
                        <div><strong>Go:</strong> {system.go_version}</div>
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

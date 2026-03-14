import {useState} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import {adminApi} from '@/api/endpoints'
import type {HLSValidationResult, ScheduledTask} from '@/api/types'
import {errMsg, formatBytes} from './adminUtils'

function hlsStatusBadgeClass(status: string): string {
  if (status === 'completed') return 'enabled'
  if (status === 'failed') return 'error'
  if (status === 'running') return 'running'
  return 'disabled'
}

function taskStatusOrder(t: ScheduledTask): number {
  if (t.running) return 0
  if (t.enabled) return 1
  return 2
}

// ── Tab: Streaming ────────────────────────────────────────────────────────────

type HLSSortKey = 'id' | 'status' | 'progress'
export function StreamingTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    // Feature 8: HLS validation
    const [validationResult, setValidationResult] = useState<HLSValidationResult | null>(null)
    const [validatingId, setValidatingId] = useState<string | null>(null)
    // HLS sort/filter state
    const [hlsSortBy, setHlsSortBy] = useState<HLSSortKey>('id')
    const [hlsSortOrder, setHlsSortOrder] = useState<'asc' | 'desc'>('asc')
    const [hlsFilterStatus, setHlsFilterStatus] = useState<string>('')

    const {data: tasks = []} = useQuery({
        queryKey: ['admin-tasks'],
        queryFn: () => adminApi.listTasks(),
        refetchInterval: 15000,
    })

    const {data: hlsStats} = useQuery({
        queryKey: ['admin-hls-stats'],
        queryFn: () => adminApi.getHLSStats(),
        refetchInterval: 15000,
    })

    const {data: hlsJobs = []} = useQuery({
        queryKey: ['admin-hls-jobs'],
        queryFn: () => adminApi.listHLSJobs(),
        refetchInterval: 15000,
    })

    async function handleValidateHLS(id: string) {
        setValidatingId(id)
        try {
            const result = await adminApi.validateHLS(id)
            setValidationResult(result)
        } catch (err) {
            setMsg({type: 'error', text: `Validation failed: ${errMsg(err)}`})
        } finally {
            setValidatingId(null)
        }
    }

    async function handleRunTask(id: string) {
        try {
            await adminApi.runTask(id)
            setMsg({type: 'success', text: 'Task triggered.'})
            await queryClient.invalidateQueries({queryKey: ['admin-tasks']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleToggleTask(task: ScheduledTask) {
        try {
            if (task.enabled) await adminApi.disableTask(task.id)
            else await adminApi.enableTask(task.id)
            await queryClient.invalidateQueries({queryKey: ['admin-tasks']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleStopTask(id: string) {
        try {
            await adminApi.stopTask(id)
            setMsg({type: 'success', text: 'Task stopped.'})
            await queryClient.invalidateQueries({queryKey: ['admin-tasks']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleDeleteHLSJob(id: string) {
        try {
            await adminApi.deleteHLSJob(id)
            setMsg({type: 'success', text: 'HLS job deleted.'})
            await queryClient.invalidateQueries({queryKey: ['admin-hls-jobs']});
            await queryClient.invalidateQueries({queryKey: ['admin-hls-stats']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleCleanLocks() {
        try {
            await adminApi.cleanHLSStaleLocks()
            setMsg({type: 'success', text: 'Stale locks removed.'})
            await queryClient.invalidateQueries({queryKey: ['admin-hls-jobs']});
            await queryClient.invalidateQueries({queryKey: ['admin-hls-stats']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleCleanInactive() {
        try {
            await adminApi.cleanHLSInactive(24)
            setMsg({type: 'success', text: 'Inactive HLS jobs cleaned.'})
            await queryClient.invalidateQueries({queryKey: ['admin-hls-jobs']});
            await queryClient.invalidateQueries({queryKey: ['admin-hls-stats']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    return (
        <div>
            {msg && <div
                className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}>{msg.text}</div>}

            {/* HLS Overview */}
            {hlsStats && (
                <div className="admin-card">
                    <h2>HLS Transcoding</h2>
                    <div className="admin-stats-grid">
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{hlsStats.running_jobs}</span>
                            <span className="admin-stat-label">Running</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{hlsStats.completed_jobs}</span>
                            <span className="admin-stat-label">Completed</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{hlsStats.failed_jobs}</span>
                            <span className="admin-stat-label">Failed</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{formatBytes(hlsStats.cache_size_bytes)}</span>
                            <span className="admin-stat-label">Cache Size</span>
                        </div>
                    </div>
                    <div className="admin-action-row" style={{marginTop: 8}}>
                        <button className="admin-btn" onClick={handleCleanLocks}>
                            <i className="bi bi-unlock"/> Clean Stale Locks
                        </button>
                        <button className="admin-btn admin-btn-warning" onClick={handleCleanInactive}>
                            <i className="bi bi-trash"/> Clean Inactive (24h)
                        </button>
                    </div>
                </div>
            )}

            {/* HLS Jobs */}
            <div className="admin-card">
                <h2>HLS Jobs</h2>
                <div style={{marginBottom: 10, display: 'flex', gap: 8, alignItems: 'center'}}>
                    <select value={hlsFilterStatus} onChange={e => { setHlsFilterStatus(e.target.value); }}
                            style={{padding: '6px 10px', border: '1px solid var(--border-color)', borderRadius: 6,
                                    background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13}}>
                        <option value="">All Status</option>
                        <option value="pending">Pending</option>
                        <option value="running">Running</option>
                        <option value="completed">Completed</option>
                        <option value="failed">Failed</option>
                        <option value="cancelled">Cancelled</option>
                    </select>
                    {hlsFilterStatus && (
                        <button className="admin-btn" style={{fontSize: 12, padding: '4px 10px'}}
                                onClick={() => { setHlsFilterStatus(''); }}>
                            <i className="bi bi-x-circle"/> Clear
                        </button>
                    )}
                </div>
                <div className="admin-table-wrapper">
                    <table className="admin-table">
                        <thead>
                        <tr>
                            {(['id', 'status', 'progress'] as HLSSortKey[]).map(col => (
                                <th key={col} style={{cursor: 'pointer', userSelect: 'none', whiteSpace: 'nowrap'}}
                                    onClick={() => { if (hlsSortBy === col) setHlsSortOrder(p => p === 'asc' ? 'desc' : 'asc'); else { setHlsSortBy(col); setHlsSortOrder('asc') } }}>
                                    {{id: 'File', status: 'Status', progress: 'Progress'}[col]}
                                    {hlsSortBy === col ? <span style={{marginLeft: 4}}>{hlsSortOrder === 'asc' ? '\u25B2' : '\u25BC'}</span> : <span style={{opacity: 0.3, marginLeft: 4}}>&#x21C5;</span>}
                                </th>
                            ))}
                            <th>Qualities</th>
                            <th>Actions</th>
                        </tr>
                        </thead>
                        <tbody>
                        {(() => {
                            const filteredJobs = hlsJobs
                                .filter(j => !hlsFilterStatus || j.status === hlsFilterStatus)
                                .sort((a, b) => {
                                    let cmp = 0
                                    switch (hlsSortBy) {
                                        case 'id': cmp = a.id.localeCompare(b.id); break
                                        case 'status': cmp = a.status.localeCompare(b.status); break
                                        case 'progress': cmp = a.progress - b.progress; break
                                    }
                                    return hlsSortOrder === 'desc' ? -cmp : cmp
                                })
                            return filteredJobs.length === 0 ? (
                                <tr>
                                    <td colSpan={5} style={{textAlign: 'center', color: 'var(--text-muted)'}}>No HLS jobs
                                    </td>
                                </tr>
                            ) : filteredJobs.map(job => (
                                <tr key={job.id}>
                                    <td style={{maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'}}
                                        title={job.id}>{job.id}</td>
                                    <td>
                                        <span className={`status-badge status-${hlsStatusBadgeClass(job.status)}`}>
                                          {job.status}
                                        </span>
                                    </td>
                                    <td>{job.status === 'running' ? `${Math.round(job.progress)}%` : '—'}</td>
                                    <td>{job.qualities?.join(', ') || '—'}</td>
                                    <td>
                                        <div style={{display: 'flex', gap: 6}}>
                                            {job.status === 'completed' && (
                                                <button className="admin-btn"
                                                        onClick={() => handleValidateHLS(job.id)}
                                                        disabled={validatingId === job.id}>
                                                    <i className="bi bi-check2-circle"/> {validatingId === job.id ? '...' : 'Validate'}
                                                </button>
                                            )}
                                            <button className="admin-btn admin-btn-danger"
                                                    onClick={() => handleDeleteHLSJob(job.id)}>
                                                <i className="bi bi-trash"/> Delete
                                            </button>
                                        </div>
                                    </td>
                                </tr>
                            ))
                        })()}
                        </tbody>
                    </table>
                </div>
            </div>

            {/* Background Tasks */}
            <div className="admin-card">
                <h2>Background Tasks</h2>
                <div className="admin-table-wrapper">
                    <table className="admin-table">
                        <thead>
                        <tr>
                            {(['name', 'schedule', 'status', 'last_run', 'next_run'] as const).map(col => {
                                const labels: Record<string, string> = {name: 'Task', schedule: 'Interval', status: 'Status', last_run: 'Last Run', next_run: 'Next Run'}
                                return <th key={col}>{labels[col]}</th>
                            })}
                            <th>Actions</th>
                        </tr>
                        </thead>
                        <tbody>
                        {tasks.length === 0 ? (
                            <tr>
                                <td colSpan={6} style={{textAlign: 'center', color: 'var(--text-muted)'}}>No tasks
                                    configured
                                </td>
                            </tr>
                        ) : [...tasks].sort((a, b) =>
                            taskStatusOrder(a) - taskStatusOrder(b) || a.name.localeCompare(b.name)
                        ).map(task => (
                            <tr key={task.id}>
                                <td>
                                    <div style={{fontWeight: 500}}>{task.name}</div>
                                    <div style={{fontSize: 11, color: 'var(--text-muted)'}}>{task.description}</div>
                                </td>
                                <td>{task.schedule}</td>
                                <td>
                                    {task.running && <span className="status-badge status-running">Running</span>}
                                    {!task.running && task.enabled &&
                                        <span className="status-badge status-enabled">Active</span>}
                                    {!task.running && !task.enabled &&
                                        <span className="status-badge status-disabled">Disabled</span>}
                                </td>
                                <td>{task.last_run && !task.last_run.startsWith('0001') ? new Date(task.last_run).toLocaleString() : '—'}</td>
                                <td>{task.next_run && !task.next_run.startsWith('0001') ? new Date(task.next_run).toLocaleString() : '—'}</td>
                                <td>
                                    <div style={{display: 'flex', gap: 6}}>
                                        <button className="admin-btn" onClick={() => handleRunTask(task.id)}
                                                disabled={task.running}>
                                            {task.running ? <><i className="bi bi-arrow-repeat"/> Running</> : <><i
                                                className="bi bi-play-fill"/> Run</>}
                                        </button>
                                        {task.running && (
                                            <button className="admin-btn admin-btn-danger"
                                                    onClick={() => handleStopTask(task.id)}
                                                    title="Cancel the running execution">
                                                <i className="bi bi-stop-fill"/> Stop
                                            </button>
                                        )}
                                        <button className="admin-btn" onClick={() => handleToggleTask(task)}>
                                            {task.enabled ? 'Disable' : 'Enable'}
                                        </button>
                                    </div>
                                </td>
                            </tr>
                        ))}
                        </tbody>
                    </table>
                </div>
            </div>

            {/* Feature 8: HLS Validation Result Modal */}
            {validationResult && (
                <div className="admin-modal-overlay" onClick={() => { setValidationResult(null); }}>
                    <div className="admin-modal-box" onClick={e => { e.stopPropagation(); }}>
                        <div className="admin-modal-header">
                            <h3><i className="bi bi-check2-circle"/> HLS Validation Result</h3>
                            <button className="admin-modal-close" onClick={() => { setValidationResult(null); }}>×</button>
                        </div>
                        <div className="admin-modal-body">
                            <p><strong>Job ID:</strong> {validationResult.job_id}</p>
                            <p><strong>Valid:</strong> {validationResult.valid ? '✓ Yes' : '✗ No'}</p>
                            <p><strong>Variant Streams:</strong> {validationResult.variant_count}</p>
                            <p><strong>Total Segments:</strong> {validationResult.segment_count}</p>
                            {validationResult.errors && validationResult.errors.length > 0 && (
                                <div style={{marginTop: 12}}>
                                    <h4 style={{color: '#ef4444'}}>Errors</h4>
                                    {validationResult.errors.map((e, i) => <p key={`${e}-${i}`} style={{
                                        color: '#ef4444',
                                        fontSize: 13
                                    }}>{e}</p>)}
                                </div>
                            )}
                        </div>
                    </div>
                </div>
            )}
        </div>
    )
}

import {type FormEvent, useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {adminApi} from '@/api/endpoints'
import type {AuditLogEntry, QueryResult} from '@/api/types'
import {useSettingsStore} from '@/stores/settingsStore'
import {UpdatesTab} from './UpdatesTab'
import {errMsg} from './adminUtils'
import {SubTabs} from './helpers'

// ── Tab: Logs ─────────────────────────────────────────────────────────────────

function LogsTab() {
    const [level, setLevel] = useState('')
    const [module, setModule] = useState('')

    const {data: logs = [], refetch, isLoading} = useQuery({
        queryKey: ['admin-logs', level, module],
        queryFn: () => adminApi.getLogs(level || undefined, module || undefined, 500),
        refetchInterval: 15000,
    })

    return (
        <div>
            <div className="admin-card">
                <h2>Server Logs</h2>
                <div className="admin-form-row" style={{marginBottom: 12}}>
                    <div className="admin-form-group">
                        <label>Level</label>
                        <select value={level} onChange={e => { setLevel(e.target.value); }}>
                            <option value="">All</option>
                            <option value="debug">Debug</option>
                            <option value="info">Info</option>
                            <option value="warn">Warn</option>
                            <option value="error">Error</option>
                        </select>
                    </div>
                    <div className="admin-form-group">
                        <label>Module</label>
                        <input value={module} onChange={e => { setModule(e.target.value); }} placeholder="Filter module..."/>
                    </div>
                    <button className="admin-btn" onClick={() => refetch()}><i
                        className="bi bi-arrow-counterclockwise"/> Refresh
                    </button>
                </div>
                {isLoading ? (
                    <p style={{color: 'var(--text-muted)'}}>Loading logs...</p>
                ) : (
                    <div className="log-viewer">
                        {logs.length === 0 && <div style={{color: '#888'}}>No log entries found.</div>}
                        {logs.map((entry, i) => (
                            <div key={i} className={`log-line ${entry.level?.toLowerCase()}`}>
                                <span style={{opacity: 0.5}}>[{entry.timestamp}]</span>{' '}
                                <span style={{fontWeight: 600, opacity: 0.8}}>[{entry.module}]</span>{' '}
                                <span>{entry.message}</span>
                            </div>
                        ))}
                    </div>
                )}
            </div>
        </div>
    )
}

// ── Tab: Settings ─────────────────────────────────────────────────────────────

function SettingsTab() {
    const [configText, setConfigText] = useState('')
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [loading, setLoading] = useState(false)
    const loadServerSettings = useSettingsStore((s) => s.loadServerSettings)

    // Admin password change state
    const [pwCurrent, setPwCurrent] = useState('')
    const [pwNew, setPwNew] = useState('')
    const [pwConfirm, setPwConfirm] = useState('')
    const [pwMsg, setPwMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [pwLoading, setPwLoading] = useState(false)

    useQuery({
        queryKey: ['admin-config'],
        queryFn: async () => {
            const data = await adminApi.getConfig()
            setConfigText(JSON.stringify(data, null, 2))
            return data
        },
    })

    async function handleSave() {
        setMsg(null)
        setLoading(true)
        try {
            const parsed = JSON.parse(configText)
            await adminApi.updateConfig(parsed)
            setMsg({type: 'success', text: 'Configuration saved. Some changes require a restart.'})
            loadServerSettings()
        } catch (err) {
            if (err instanceof SyntaxError) {
                setMsg({type: 'error', text: 'Invalid JSON: ' + err.message})
            } else {
                setMsg({type: 'error', text: errMsg(err)})
            }
        } finally {
            setLoading(false)
        }
    }

    async function handleChangePassword(e: React.FormEvent) {
        e.preventDefault()
        setPwMsg(null)
        if (pwNew.length < 8) {
            setPwMsg({type: 'error', text: 'New password must be at least 8 characters'})
            return
        }
        if (pwNew !== pwConfirm) {
            setPwMsg({type: 'error', text: 'New passwords do not match'})
            return
        }
        setPwLoading(true)
        try {
            await adminApi.changeAdminPassword(pwCurrent, pwNew)
            setPwMsg({type: 'success', text: 'Admin password changed successfully.'})
            setPwCurrent('')
            setPwNew('')
            setPwConfirm('')
        } catch (err) {
            setPwMsg({type: 'error', text: errMsg(err)})
        } finally {
            setPwLoading(false)
        }
    }

    return (
        <div>
            {/* Change Admin Password */}
            <div className="admin-card" style={{maxWidth: 480, marginBottom: 20}}>
                <h2>Change Admin Password</h2>
                {pwMsg && (
                    <div className={`admin-alert admin-alert-${pwMsg.type === 'success' ? 'success' : 'danger'}`}>
                        {pwMsg.text}
                    </div>
                )}
                <form onSubmit={handleChangePassword}>
                    <div className="admin-form-group">
                        <label htmlFor="pw-current">Current Password</label>
                        <input id="pw-current" type="password" className="admin-input"
                               value={pwCurrent} onChange={e => { setPwCurrent(e.target.value); }}
                               autoComplete="current-password" required/>
                    </div>
                    <div className="admin-form-group">
                        <label htmlFor="pw-new">New Password</label>
                        <input id="pw-new" type="password" className="admin-input"
                               value={pwNew} onChange={e => { setPwNew(e.target.value); }}
                               autoComplete="new-password" minLength={8} required/>
                    </div>
                    <div className="admin-form-group">
                        <label htmlFor="pw-confirm">Confirm New Password</label>
                        <input id="pw-confirm" type="password" className="admin-input"
                               value={pwConfirm} onChange={e => setPwConfirm(e.target.value)}
                               autoComplete="new-password" minLength={8} required/>
                    </div>
                    <button type="submit" className="admin-btn admin-btn-primary" disabled={pwLoading}>
                        <i className="bi bi-key-fill"/>
                        {pwLoading ? ' Changing…' : ' Change Password'}
                    </button>
                </form>
            </div>

            {/* Server Configuration */}
            <div className="admin-card">
                <h2>Server Configuration</h2>
                {msg && <div
                    className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}>{msg.text}</div>}
                <div className="admin-alert admin-alert-warning">
                    <i className="bi bi-exclamation-triangle-fill"/> Editing configuration directly can break the
                    server. Know what you're changing.
                </div>
                <textarea className="config-editor" value={configText} onChange={e => { setConfigText(e.target.value); }}/>
                <div className="admin-action-row" style={{marginTop: 10}}>
                    <button className="admin-btn admin-btn-primary" onClick={handleSave} disabled={loading}>
                        {loading ? 'Saving...' : <><i className="bi bi-floppy-fill"/> Save Configuration</>}
                    </button>
                </div>
            </div>
        </div>
    )
}

// ── Tab: Database ─────────────────────────────────────────────────────────────

function DatabaseTab() {
    const [query, setQuery] = useState('')
    const [result, setResult] = useState<QueryResult | null>(null)
    const [querying, setQuerying] = useState(false)
    const [queryMsg, setQueryMsg] = useState('')

    const {data: dbStatus} = useQuery({
        queryKey: ['admin-db-status'],
        queryFn: () => adminApi.getDatabaseStatus(),
        refetchInterval: 30000,
    })

    async function handleQuery(e: FormEvent) {
        e.preventDefault()
        if (!query.trim()) return
        if (/^\s*(DROP|DELETE|TRUNCATE|ALTER|UPDATE|INSERT|CREATE|GRANT|REVOKE|LOAD|CALL)\b/i.test(query) && !window.confirm('This query modifies data. Proceed?')) return
        setQuerying(true)
        setQueryMsg('')
        setResult(null)
        try {
            const r = await adminApi.executeQuery(query)
            setResult(r)
            setQueryMsg(r.rows_affected !== null ? `${r.rows_affected} row(s) affected` : `${r.rows?.length ?? 0} row(s) returned`)
        } catch (err) {
            setQueryMsg('Error: ' + errMsg(err))
        } finally {
            setQuerying(false)
        }
    }

    return (
        <div>
            <div className="admin-card">
                <h2>Database Status</h2>
                {dbStatus && (
                    <div style={{
                        display: 'grid',
                        gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))',
                        gap: 12
                    }}>
                        <div>
                            <strong>Status:</strong>{' '}
                            <span className={`status-badge ${dbStatus.connected ? 'status-healthy' : 'status-failed'}`}>
                {dbStatus.connected ? 'Connected' : 'Disconnected'}
              </span>
                        </div>
                        <div><strong>Host:</strong> {dbStatus.host}</div>
                        <div><strong>Database:</strong> {dbStatus.database}</div>
                        <div><strong>App Version:</strong> v{dbStatus.app_version}</div>
                        <div><strong>Repository:</strong> {dbStatus.repository_type}</div>
                        {!dbStatus.connected && dbStatus.message &&
                            <div style={{color: '#ef4444'}}><strong>Error:</strong> {dbStatus.message}</div>}
                    </div>
                )}
            </div>
            <div className="admin-card">
                <h2>Query Executor</h2>
                <div className="admin-alert admin-alert-warning"><i
                    className="bi bi-exclamation-triangle-fill"/> Queries run directly on the database. Use with
                    caution.
                </div>
                <form onSubmit={handleQuery}>
                    <textarea className="config-editor" style={{minHeight: 100}} value={query}
                              onChange={e => { setQuery(e.target.value); }} placeholder="SELECT * FROM users LIMIT 10;"/>
                    <div className="admin-action-row" style={{marginTop: 8}}>
                        <button type="submit" className="admin-btn admin-btn-primary" disabled={querying}>
                            {querying ? 'Executing...' : <><i className="bi bi-play-fill"/> Execute</>}
                        </button>
                        {queryMsg && <span
                            style={{fontSize: 13, color: 'var(--text-muted)', alignSelf: 'center'}}>{queryMsg}</span>}
                    </div>
                </form>
                {result && !result.error && result.columns && (
                    <div className="query-result">
                        <table>
                            <thead>
                            <tr>{result.columns.map(c => <th key={c}>{c}</th>)}</tr>
                            </thead>
                            <tbody>
                            {(result.rows ?? []).map((row, ri) => (
                                <tr key={`row-${ri}`}>{(Array.isArray(row) ? row : []).map((cell, ci) => <td
                                    key={`${ri}-${ci}`}>{String(cell ?? 'NULL')}</td>)}</tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                )}
            </div>
        </div>
    )
}

// ── Tab: Audit log ────────────────────────────────────────────────────────────

const AUDIT_PAGE_SIZE = 50

function AuditLogTab() {
    const [page, setPage] = useState(1)
    const [userIdFilter, setUserIdFilter] = useState('')
    const [appliedUserId, setAppliedUserId] = useState('')
    const offset = (page - 1) * AUDIT_PAGE_SIZE

    const {data: entries = [], isLoading, isError, refetch} = useQuery({
        queryKey: ['admin-audit-log', offset, appliedUserId],
        queryFn: () =>
            adminApi.getAuditLog(AUDIT_PAGE_SIZE, offset, appliedUserId.trim() || undefined),
    })

    const hasNext = entries.length === AUDIT_PAGE_SIZE
    const hasPrev = page > 1

    function applyUserFilter() {
        setAppliedUserId(userIdFilter.trim())
        setPage(1)
    }

    async function handleExportCsv() {
        const res = await fetch(adminApi.exportAuditLogUrl(), {credentials: 'include'})
        if (!res.ok) return
        const blob = await res.blob()
        const a = document.createElement('a')
        a.href = URL.createObjectURL(blob)
        a.download = `audit-log-${new Date().toISOString().slice(0, 10)}.csv`
        a.click()
        URL.revokeObjectURL(a.href)
    }

    return (
        <div>
            <div className="admin-card">
                <h2>Audit log</h2>
                <p style={{fontSize: 12, color: 'var(--text-muted)', marginBottom: 12}}>
                    Recent admin actions (newest first within each page). Use CSV export for a full archive.
                </p>
                <div style={{display: 'flex', flexWrap: 'wrap', gap: 8, marginBottom: 12, alignItems: 'center'}}>
                    <input
                        type="text"
                        placeholder="Filter by user_id (optional)"
                        value={userIdFilter}
                        onChange={e => { setUserIdFilter(e.target.value); }}
                        style={{
                            flex: 1,
                            minWidth: 200,
                            padding: '6px 10px',
                            border: '1px solid var(--border-color)',
                            borderRadius: 6,
                            background: 'var(--input-bg)',
                            color: 'var(--text-color)',
                            fontSize: 13,
                        }}
                    />
                    <button type="button" className="admin-btn" onClick={applyUserFilter}>Apply filter</button>
                    <button type="button" className="admin-btn" onClick={() => { setUserIdFilter(''); setAppliedUserId(''); setPage(1); }}>
                        Clear
                    </button>
                    <button type="button" className="admin-btn" onClick={() => refetch()}><i className="bi bi-arrow-counterclockwise"/> Refresh</button>
                    <button type="button" className="admin-btn" onClick={() => handleExportCsv()}><i className="bi bi-download"/> Export CSV</button>
                </div>
                {isLoading && <p style={{color: 'var(--text-muted)', fontSize: 13}}>Loading…</p>}
                {isError && <div className="admin-alert admin-alert-danger">Failed to load audit log.</div>}
                {!isLoading && !isError && entries.length === 0 && (
                    <p style={{color: 'var(--text-muted)', fontSize: 13}}>No entries for this page.</p>
                )}
                {!isLoading && !isError && entries.length > 0 && (
                    <>
                        <div className="admin-table-wrapper" style={{maxHeight: 480, overflow: 'auto'}}>
                            <table className="admin-table">
                                <thead>
                                <tr>
                                    <th>Time</th>
                                    <th>User</th>
                                    <th>Action</th>
                                    <th>Resource</th>
                                    <th>OK</th>
                                    <th>IP</th>
                                </tr>
                                </thead>
                                <tbody>
                                {entries.map((e: AuditLogEntry) => (
                                    <tr key={e.id}>
                                        <td style={{fontSize: 11, whiteSpace: 'nowrap'}}>{new Date(e.timestamp).toLocaleString()}</td>
                                        <td style={{fontSize: 12}}>{e.username}</td>
                                        <td style={{fontSize: 12}}>{e.action}</td>
                                        <td style={{fontSize: 11, maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis'}} title={e.resource}>
                                            {e.resource}
                                        </td>
                                        <td>{e.success ? '✓' : '✗'}</td>
                                        <td style={{fontSize: 11}}>{e.ip_address || '—'}</td>
                                    </tr>
                                ))}
                                </tbody>
                            </table>
                        </div>
                        <div style={{display: 'flex', justifyContent: 'center', gap: 8, marginTop: 12, alignItems: 'center'}}>
                            <button type="button" className="admin-btn" disabled={!hasPrev} onClick={() => { setPage(p => p - 1); }}>← Newer</button>
                            <span style={{fontSize: 13, color: 'var(--text-muted)'}}>Page {page}</span>
                            <button type="button" className="admin-btn" disabled={!hasNext} onClick={() => { setPage(p => p + 1); }}>Older →</button>
                        </div>
                    </>
                )}
            </div>
        </div>
    )
}

// ── Tab: System (composite) ──────────────────────────────────────────────────

export function SystemTab() {
    const [sub, setSub] = useState('settings')
    return (<>
        <SubTabs items={[
            {id: 'settings', label: 'Settings'},
            {id: 'logs', label: 'Logs'},
            {id: 'database', label: 'Database'},
            {id: 'audit', label: 'Audit log'},
            {id: 'updates', label: 'Updates'},
        ]} active={sub} onChange={setSub}/>
        {sub === 'settings' && <SettingsTab/>}
        {sub === 'logs' && <LogsTab/>}
        {sub === 'database' && <DatabaseTab/>}
        {sub === 'audit' && <AuditLogTab/>}
        {sub === 'updates' && <UpdatesTab/>}
    </>)
}

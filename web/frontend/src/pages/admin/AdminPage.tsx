import {type FormEvent, useEffect, useRef, useState} from 'react'
import {Link, useLocation, useNavigate} from 'react-router-dom'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import {adminApi, analyticsApi, mediaApi, receiverApi} from '@/api/endpoints'
import {useAuthStore} from '@/stores/authStore'
import {SectionErrorBoundary} from '@/components/ErrorBoundary'
import {useSettingsStore} from '@/stores/settingsStore'
import type {
    AdminMediaListResponse,
    AdminPlaylistStats,
    BannedIP,
    CategorizedItem,
    CategoryStats,
    DiscoverySuggestion,
    EventStats,
    HLSValidationResult,
    IPEntry,
    MediaItem,
    Playlist,
    QueryResult,
    ReceiverMediaItem,
    ReceiverStats,
    RemoteMediaItem,
    RemoteSourceState,
    SlaveNode,
    ScheduledTask,
    SecurityStats,
    SuggestionStats,
    ThumbnailStats,
    User,
} from '@/api/types'
import '@/styles/admin.css'

function errMsg(err: unknown): string {
    return err instanceof Error ? err.message : String(err)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function formatBytes(bytes: number): string {
    if (!bytes) return '0 B'
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(1024))
    return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${sizes[i]}`
}

function formatUptime(secs: number): string {
    const d = Math.floor(secs / 86400)
    const h = Math.floor((secs % 86400) / 3600)
    const m = Math.floor((secs % 3600) / 60)
    if (d > 0) return `${d}d ${h}h ${m}m`
    if (h > 0) return `${h}h ${m}m`
    return `${m}m`
}

// ── Tab: Dashboard ────────────────────────────────────────────────────────────

function DashboardTab() {
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

    const diskPct = stats ? Math.round((stats.disk_usage / (stats.disk_total || 1)) * 100) : 0
    // memory_total = memStats.Sys (bytes obtained from OS) — approximates RSS. Fixed in admin.go.
    const memPct = system ? Math.round((system.memory_used / (system.memory_total || 1)) * 100) : 0

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
                            <span className="admin-stat-value">{stats.total_videos.toLocaleString()}</span>
                            <span className="admin-stat-label">Videos</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{stats.total_audio.toLocaleString()}</span>
                            <span className="admin-stat-label">Audio Files</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{stats.total_users.toLocaleString()}</span>
                            <span className="admin-stat-label">Total Users</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{stats.active_sessions.toLocaleString()}</span>
                            <span className="admin-stat-label">Active Sessions</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{stats.total_views.toLocaleString()}</span>
                            <span className="admin-stat-label">Total Views</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{stats.hls_jobs_running}</span>
                            <span className="admin-stat-label">HLS Running</span>
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
                                <span>{formatBytes(stats.disk_usage)} / {formatBytes(stats.disk_total)}</span>
                            </div>
                            <div className="disk-usage-bar">
                                <div
                                    className={`disk-usage-fill ${diskPct > 90 ? 'danger' : diskPct > 70 ? 'warning' : ''}`}
                                    style={{width: `${diskPct}%`}}
                                />
                            </div>
                            <div style={{fontSize: 12, color: 'var(--text-muted)', marginTop: 4}}>
                                {diskPct}% used · {formatBytes(stats.disk_free)} free
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
                        if (window.confirm('Restart the server? Active streams will be interrupted.'))
                            handleAction(() => adminApi.restartServer(), 'Server restarting...')
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

// ── Tab: Users ────────────────────────────────────────────────────────────────

function CreateUserModal({onClose, onCreated}: { onClose: () => void; onCreated: () => void }) {
    const [username, setUsername] = useState('')
    const [password, setPassword] = useState('')
    const [email, setEmail] = useState('')
    const [role, setRole] = useState<'admin' | 'viewer'>('viewer')
    const [error, setError] = useState('')
    const [loading, setLoading] = useState(false)

    async function handleSubmit(e: FormEvent) {
        e.preventDefault()
        setError('')
        setLoading(true)
        try {
            await adminApi.createUser({username, password, email: email || undefined, role})
            onCreated()
        } catch (err) {
            setError(errMsg(err))
        } finally {
            setLoading(false)
        }
    }

    return (
        <div className="admin-modal-overlay" onClick={e => e.target === e.currentTarget && onClose()}>
            <div className="admin-modal-box">
                <div className="admin-modal-header">
                    <h3>Create User</h3>
                    <button className="admin-modal-close" onClick={onClose}>×</button>
                </div>
                <div className="admin-modal-body">
                    {error && <div className="admin-alert admin-alert-danger">{error}</div>}
                    <form onSubmit={handleSubmit}>
                        <div className="admin-form-group" style={{marginBottom: 10}}>
                            <label>Username *</label>
                            <input value={username} onChange={e => setUsername(e.target.value)} required/>
                        </div>
                        <div className="admin-form-group" style={{marginBottom: 10}}>
                            <label>Password *</label>
                            <input type="password" value={password} onChange={e => setPassword(e.target.value)} required
                                   minLength={8}/>
                        </div>
                        <div className="admin-form-group" style={{marginBottom: 10}}>
                            <label>Email</label>
                            <input type="email" value={email} onChange={e => setEmail(e.target.value)}/>
                        </div>
                        <div className="admin-form-group" style={{marginBottom: 16}}>
                            <label>Role</label>
                            <select value={role} onChange={e => setRole(e.target.value as 'admin' | 'viewer')}>
                                <option value="viewer">Viewer</option>
                                <option value="admin">Admin</option>
                            </select>
                        </div>
                        <div style={{display: 'flex', gap: 8, justifyContent: 'flex-end'}}>
                            <button type="button" className="admin-btn" onClick={onClose}>Cancel</button>
                            <button type="submit" className="admin-btn admin-btn-primary" disabled={loading}>
                                {loading ? 'Creating...' : 'Create User'}
                            </button>
                        </div>
                    </form>
                </div>
            </div>
        </div>
    )
}

function EditUserModal({user, onClose, onSaved}: { user: User; onClose: () => void; onSaved: () => void }) {
    const [role, setRole] = useState<'admin' | 'viewer'>(user.role)
    const [enabled, setEnabled] = useState(user.enabled)
    const [email, setEmail] = useState(user.email ?? '')
    const [newPassword, setNewPassword] = useState('')
    const [permissions, setPermissions] = useState({...user.permissions})
    const [error, setError] = useState('')
    const [loading, setLoading] = useState(false)

    async function handleSubmit(e: FormEvent) {
        e.preventDefault()
        setError('')
        setLoading(true)
        try {
            await adminApi.updateUser(user.username, {role, enabled, permissions, email: email || undefined})
            if (newPassword) await adminApi.changeUserPassword(user.username, newPassword)
            onSaved()
        } catch (err) {
            setError(errMsg(err))
        } finally {
            setLoading(false)
        }
    }

    type PermKey = keyof typeof permissions

    return (
        <div className="admin-modal-overlay" onClick={e => e.target === e.currentTarget && onClose()}>
            <div className="admin-modal-box" style={{maxWidth: 560}}>
                <div className="admin-modal-header">
                    <h3>Edit User: {user.username}</h3>
                    <button className="admin-modal-close" onClick={onClose}>×</button>
                </div>
                <div className="admin-modal-body">
                    {error && <div className="admin-alert admin-alert-danger">{error}</div>}
                    <form onSubmit={handleSubmit}>
                        <div style={{display: 'flex', gap: 12, marginBottom: 12}}>
                            <div className="admin-form-group" style={{flex: 1}}>
                                <label>Role</label>
                                <select value={role} onChange={e => setRole(e.target.value as 'admin' | 'viewer')}>
                                    <option value="viewer">Viewer</option>
                                    <option value="admin">Admin</option>
                                </select>
                            </div>
                            <div className="admin-form-group" style={{flex: 1}}>
                                <label>Status</label>
                                <select value={enabled ? 'enabled' : 'disabled'}
                                        onChange={e => setEnabled(e.target.value === 'enabled')}>
                                    <option value="enabled">Enabled</option>
                                    <option value="disabled">Disabled</option>
                                </select>
                            </div>
                        </div>
                        <div style={{display: 'flex', gap: 12, marginBottom: 12}}>
                            <div className="admin-form-group" style={{flex: 1}}>
                                <label>Email</label>
                                <input type="email" value={email} onChange={e => setEmail(e.target.value)}/>
                            </div>
                            <div className="admin-form-group" style={{flex: 1}}>
                                <label>New Password (blank to keep current)</label>
                                <input type="password" value={newPassword} onChange={e => setNewPassword(e.target.value)}
                                       minLength={8}/>
                            </div>
                        </div>
                        <div style={{marginBottom: 14}}>
                            <label style={{
                                fontSize: 12,
                                fontWeight: 600,
                                color: 'var(--text-muted)',
                                display: 'block',
                                marginBottom: 8
                            }}>Permissions</label>
                            <div style={{display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 6}}>
                                {(Object.keys(permissions) as PermKey[]).map(key => (
                                    <label key={key} style={{
                                        display: 'flex',
                                        alignItems: 'center',
                                        gap: 6,
                                        fontSize: 13,
                                        cursor: 'pointer'
                                    }}>
                                        <input
                                            type="checkbox"
                                            checked={permissions[key]}
                                            onChange={() => setPermissions(p => ({...p, [key]: !p[key]}))}
                                        />
                                        {key.replace('can_', '').replace(/_/g, ' ')}
                                    </label>
                                ))}
                            </div>
                        </div>
                        <div style={{display: 'flex', gap: 8, justifyContent: 'flex-end'}}>
                            <button type="button" className="admin-btn" onClick={onClose}>Cancel</button>
                            <button type="submit" className="admin-btn admin-btn-primary" disabled={loading}>
                                {loading ? 'Saving...' : 'Save Changes'}
                            </button>
                        </div>
                    </form>
                </div>
            </div>
        </div>
    )
}

type UserSortKey = 'username' | 'email' | 'role' | 'enabled' | 'last_login' | 'created_at'

const USER_SORT_COLUMNS: ReadonlyArray<{key: UserSortKey; label: string}> = [
    {key: 'username', label: 'Username'},
    {key: 'email', label: 'Email'},
    {key: 'role', label: 'Role'},
    {key: 'enabled', label: 'Status'},
    {key: 'last_login', label: 'Last Login'},
    {key: 'created_at', label: 'Created'},
]

function UsersTab() {
    const queryClient = useQueryClient()
    const [showCreate, setShowCreate] = useState(false)
    const [editUser, setEditUser] = useState<User | null>(null)
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [selected, setSelected] = useState<Set<string>>(new Set())
    const [bulkWorking, setBulkWorking] = useState(false)

    // Sort and filter state
    const [sortBy, setSortBy] = useState<UserSortKey>('username')
    const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('asc')
    const [filterRole, setFilterRole] = useState<string>('')
    const [filterStatus, setFilterStatus] = useState<string>('')
    const [userSearch, setUserSearch] = useState('')

    const {data: users = [], isLoading} = useQuery({
        queryKey: ['admin-users'],
        queryFn: () => adminApi.listUsers(),
    })

    // Apply filters and sorting client-side
    const filteredUsers = users.filter(u => {
        if (filterRole && u.role !== filterRole) return false
        if (filterStatus === 'enabled' && !u.enabled) return false
        if (filterStatus === 'disabled' && u.enabled) return false
        if (userSearch) {
            const q = userSearch.toLowerCase()
            if (!u.username.toLowerCase().includes(q) && !(u.email || '').toLowerCase().includes(q)) return false
        }
        return true
    }).sort((a, b) => {
        let cmp = 0
        switch (sortBy) {
            case 'username': cmp = a.username.localeCompare(b.username); break
            case 'email': cmp = (a.email || '').localeCompare(b.email || ''); break
            case 'role': cmp = a.role.localeCompare(b.role); break
            case 'enabled': cmp = (a.enabled === b.enabled ? 0 : a.enabled ? -1 : 1); break
            case 'last_login': cmp = (a.last_login || '').localeCompare(b.last_login || ''); break
            case 'created_at': cmp = a.created_at.localeCompare(b.created_at); break
        }
        return sortOrder === 'desc' ? -cmp : cmp
    })

    function handleSort(column: UserSortKey) {
        if (sortBy === column) {
            setSortOrder(prev => prev === 'asc' ? 'desc' : 'asc')
        } else {
            setSortBy(column)
            setSortOrder('asc')
        }
    }

    function sortIndicator(column: UserSortKey) {
        if (sortBy !== column) return <span style={{opacity: 0.3, marginLeft: 4}}>&#x21C5;</span>
        return <span style={{marginLeft: 4}}>{sortOrder === 'asc' ? '\u25B2' : '\u25BC'}</span>
    }

    async function handleDelete(username: string) {
        if (!window.confirm(`Delete user "${username}"? This cannot be undone.`)) return
        try {
            await adminApi.deleteUser(username)
            setMsg({type: 'success', text: `User "${username}" deleted.`})
            await queryClient.invalidateQueries({queryKey: ['admin-users']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleToggle(user: User) {
        try {
            await adminApi.updateUser(user.username, {enabled: !user.enabled})
            await queryClient.invalidateQueries({queryKey: ['admin-users']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleBulkAction(action: 'delete' | 'enable' | 'disable') {
        if (selected.size === 0) return
        const label = {delete: 'Delete', enable: 'Enable', disable: 'Disable'}[action]
        if (action === 'delete' && !window.confirm(`${label} ${selected.size} user(s)? This cannot be undone.`)) return
        setBulkWorking(true)
        setMsg(null)
        try {
            const result = await adminApi.bulkUsers([...selected], action)
            setSelected(new Set())
            await queryClient.invalidateQueries({queryKey: ['admin-users']})
            setMsg({
                type: result.failed > 0 ? 'error' : 'success',
                text: `${label}d ${result.success} user(s)${result.failed > 0 ? `, ${result.failed} failed` : ''}.`
            })
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setBulkWorking(false)
        }
    }

    const selectableUsers = filteredUsers.filter(u => u.username !== 'admin')
    const allSelected = selectableUsers.length > 0 && selectableUsers.every(u => selected.has(u.username))

    function toggleSelectAll() {
        if (allSelected) {
            setSelected(prev => {
                const next = new Set(prev)
                selectableUsers.forEach(u => next.delete(u.username))
                return next
            })
        } else {
            setSelected(prev => {
                const next = new Set(prev)
                selectableUsers.forEach(u => next.add(u.username))
                return next
            })
        }
    }

    const thSortStyle: React.CSSProperties = {cursor: 'pointer', userSelect: 'none', whiteSpace: 'nowrap'}
    const selectStyle: React.CSSProperties = {
        padding: '6px 10px', border: '1px solid var(--border-color)', borderRadius: 6,
        background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13,
    }

    return (
        <div>
            {msg && <div
                className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}>{msg.text}</div>}
            <div className="admin-action-row">
                <button className="admin-btn admin-btn-primary" onClick={() => setShowCreate(true)}><i
                    className="bi bi-person-plus-fill"/> Create User
                </button>
            </div>

            {/* Search and filter controls */}
            <div style={{marginBottom: 10, display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap'}}>
                <input type="text" placeholder="Search users..." value={userSearch}
                       onChange={e => setUserSearch(e.target.value)}
                       style={{...selectStyle, flex: 1, minWidth: 160}} />
                <select value={filterRole} onChange={e => setFilterRole(e.target.value)} style={selectStyle}>
                    <option value="">All Roles</option>
                    <option value="admin">Admin</option>
                    <option value="viewer">Viewer</option>
                </select>
                <select value={filterStatus} onChange={e => setFilterStatus(e.target.value)} style={selectStyle}>
                    <option value="">All Status</option>
                    <option value="enabled">Active</option>
                    <option value="disabled">Disabled</option>
                </select>
                {(filterRole || filterStatus || userSearch) && (
                    <button className="admin-btn" style={{fontSize: 12, padding: '4px 10px'}}
                            onClick={() => { setFilterRole(''); setFilterStatus(''); setUserSearch('') }}>
                        <i className="bi bi-x-circle"/> Clear
                    </button>
                )}
                <span style={{fontSize: 12, color: 'var(--text-muted)'}}>
                    {filteredUsers.length} of {users.length} user{users.length !== 1 ? 's' : ''}
                </span>
            </div>

            {selected.size > 0 && (
                <div style={{
                    marginBottom: 10,
                    padding: '8px 12px',
                    background: 'var(--hover-bg)',
                    border: '1px solid var(--border-color)',
                    borderRadius: 6,
                    display: 'flex',
                    gap: 8,
                    alignItems: 'center',
                    flexWrap: 'wrap',
                }}>
                    <span style={{
                        fontSize: 13,
                        fontWeight: 600,
                        color: 'var(--accent-color)'
                    }}>{selected.size} selected</span>
                    <button className="admin-btn" onClick={() => handleBulkAction('enable')} disabled={bulkWorking}
                            style={{fontSize: 12, padding: '4px 10px'}}>
                        <i className="bi bi-person-check-fill"/> Enable
                    </button>
                    <button className="admin-btn" onClick={() => handleBulkAction('disable')} disabled={bulkWorking}
                            style={{fontSize: 12, padding: '4px 10px'}}>
                        <i className="bi bi-person-dash-fill"/> Disable
                    </button>
                    <button className="admin-btn admin-btn-danger" onClick={() => handleBulkAction('delete')}
                            disabled={bulkWorking} style={{fontSize: 12, padding: '4px 10px'}}>
                        <i className="bi bi-trash-fill"/> Delete
                    </button>
                    <button className="admin-btn" onClick={() => setSelected(new Set())}
                            style={{fontSize: 12, padding: '4px 10px'}}>Clear
                    </button>
                </div>
            )}

            {isLoading ? (
                <p style={{color: 'var(--text-muted)'}}>Loading users...</p>
            ) : (
                <div className="admin-table-wrapper">
                    <table className="admin-table">
                        <thead>
                        <tr>
                            <th style={{width: 32}}>
                                <input type="checkbox" checked={allSelected} onChange={toggleSelectAll}
                                       title={allSelected ? 'Deselect all' : 'Select all (except admin)'}/>
                            </th>
                            {USER_SORT_COLUMNS.map(col => (
                                <th key={col.key} style={thSortStyle} onClick={() => handleSort(col.key)}>
                                    {col.label}{sortIndicator(col.key)}
                                </th>
                            ))}
                            <th>Actions</th>
                        </tr>
                        </thead>
                        <tbody>
                        {filteredUsers.map(user => (
                            <tr key={user.id}
                                style={selected.has(user.username) ? {background: 'color-mix(in srgb, var(--accent-color) 8%, transparent)'} : undefined}>
                                <td>
                                    {user.username !== 'admin' && (
                                        <input type="checkbox" checked={selected.has(user.username)}
                                               onChange={() => setSelected(prev => {
                                                   const next = new Set(prev)
                                                   if (next.has(user.username)) next.delete(user.username)
                                                   else next.add(user.username)
                                                   return next
                                               })}/>
                                    )}
                                </td>
                                <td><strong>{user.username}</strong></td>
                                <td>{user.email || '—'}</td>
                                <td><span className={`status-badge status-${user.role}`}>{user.role}</span></td>
                                <td>
                    <span className={`status-badge ${user.enabled ? 'status-enabled' : 'status-disabled2'}`}>
                      {user.enabled ? 'Active' : 'Disabled'}
                    </span>
                                </td>
                                <td>{user.last_login ? new Date(user.last_login).toLocaleDateString() : '—'}</td>
                                <td>{new Date(user.created_at).toLocaleDateString()}</td>
                                <td>
                                    <div style={{display: 'flex', gap: 6}}>
                                        <button className="admin-btn" onClick={() => setEditUser(user)}>Edit</button>
                                        <button className="admin-btn"
                                                onClick={() => handleToggle(user)}>{user.enabled ? 'Disable' : 'Enable'}</button>
                                        <button className="admin-btn admin-btn-danger"
                                                onClick={() => handleDelete(user.username)}>Delete
                                        </button>
                                    </div>
                                </td>
                            </tr>
                        ))}
                        {filteredUsers.length === 0 && (
                            <tr>
                                <td colSpan={USER_SORT_COLUMNS.length + 2}
                                    style={{textAlign: 'center', color: 'var(--text-muted)', padding: '20px 0'}}>
                                    No users found
                                </td>
                            </tr>
                        )}
                        </tbody>
                    </table>
                </div>
            )}
            {showCreate && (
                <CreateUserModal
                    onClose={() => setShowCreate(false)}
                    onCreated={() => {
                        setShowCreate(false);
                        void queryClient.invalidateQueries({queryKey: ['admin-users']});
                        setMsg({type: 'success', text: 'User created.'})
                    }}
                />
            )}
            {editUser && (
                <EditUserModal
                    user={editUser}
                    onClose={() => setEditUser(null)}
                    onSaved={() => {
                        setEditUser(null);
                        void queryClient.invalidateQueries({queryKey: ['admin-users']});
                        setMsg({type: 'success', text: 'User updated.'})
                    }}
                />
            )}
        </div>
    )
}

// ── Tab: Media ────────────────────────────────────────────────────────────────

function formatDuration(secs: number): string {
    if (!secs || secs <= 0) return '—'
    const h = Math.floor(secs / 3600)
    const m = Math.floor((secs % 3600) / 60)
    const s = Math.floor(secs % 60)
    if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
    return `${m}:${String(s).padStart(2, '0')}`
}

// Sortable column header definitions for the admin media table.
const MEDIA_SORT_COLUMNS = [
    {key: 'name', label: 'Name'},
    {key: 'type', label: 'Type'},
    {key: 'size', label: 'Size'},
    {key: 'duration', label: 'Duration'},
    {key: 'category', label: 'Category'},
    {key: 'date_added', label: 'Date Added'},
    {key: 'date_modified', label: 'Modified'},
    {key: 'views', label: 'Views'},
    {key: 'is_mature', label: 'Mature'},
] as const

type MediaSortKey = typeof MEDIA_SORT_COLUMNS[number]['key']

function MediaTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [scanning, setScanning] = useState(false)
    const [creatingBackup, setCreatingBackup] = useState(false)

    // Media browser state
    const [mediaSearch, setMediaSearch] = useState('')
    const [debouncedMediaSearch, setDebouncedMediaSearch] = useState('')
    const [mediaPage, setMediaPage] = useState(1)
    const [editItem, setEditItem] = useState<MediaItem | null>(null)
    const [mediaLimit, setMediaLimit] = useState(20)
    const mediaSearchTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

    // Sort state
    const [sortBy, setSortBy] = useState<MediaSortKey>('name')
    const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('asc')

    // Filter state
    const [filterType, setFilterType] = useState<string>('')
    const [filterCategory, setFilterCategory] = useState('')
    const [filterMature, setFilterMature] = useState<string>('')
    const [filterTags, setFilterTags] = useState('')

    // Bulk selection state
    const [selected, setSelected] = useState<Set<string>>(new Set())
    const [bulkCategory, setBulkCategory] = useState('')
    const [bulkMature, setBulkMature] = useState<boolean | null>(null)
    const [bulkWorking, setBulkWorking] = useState(false)

    useEffect(() => {
        if (mediaSearchTimer.current) clearTimeout(mediaSearchTimer.current)
        mediaSearchTimer.current = setTimeout(() => setDebouncedMediaSearch(mediaSearch), 300)
        return () => {
            if (mediaSearchTimer.current) clearTimeout(mediaSearchTimer.current)
        }
    }, [mediaSearch])

    // Clear bulk selection whenever the visible result set changes to avoid
    // acting on IDs that are no longer shown in the table.
    useEffect(() => {
        setSelected(new Set())
    }, [debouncedMediaSearch, mediaPage, mediaLimit, sortBy, sortOrder, filterType, filterCategory, filterMature, filterTags])

    const {data: backups = []} = useQuery({
        queryKey: ['admin-backups'],
        queryFn: async () => (await adminApi.listBackups()) ?? [],
    })

    const emptyResponse: AdminMediaListResponse = {items: [], total_items: 0, total_pages: 1}
    const {data: mediaResponse = emptyResponse} = useQuery<AdminMediaListResponse>({
        queryKey: ['admin-media', debouncedMediaSearch, mediaPage, mediaLimit, sortBy, sortOrder, filterType, filterCategory, filterMature, filterTags],
        queryFn: async () => {
            const result = await adminApi.listMedia({
                page: mediaPage,
                limit: mediaLimit,
                search: debouncedMediaSearch || undefined,
                sort: sortBy || undefined,
                sort_order: sortOrder || undefined,
                type: filterType || undefined,
                category: filterCategory || undefined,
                is_mature: filterMature || undefined,
                tags: filterTags || undefined,
            })
            return result ?? emptyResponse
        },
    })
    const mediaItems = mediaResponse.items ?? []
    const totalItems = mediaResponse.total_items ?? 0
    const totalPages = mediaResponse.total_pages ?? 1

    function handleSort(column: MediaSortKey) {
        if (sortBy === column) {
            setSortOrder(prev => prev === 'asc' ? 'desc' : 'asc')
        } else {
            setSortBy(column)
            setSortOrder('asc')
        }
        setMediaPage(1)
    }

    function sortIndicator(column: MediaSortKey) {
        if (sortBy !== column) return <span style={{opacity: 0.3, marginLeft: 4}}>&#x21C5;</span>
        return <span style={{marginLeft: 4}}>{sortOrder === 'asc' ? '\u25B2' : '\u25BC'}</span>
    }

    async function handleScanMedia() {
        setScanning(true)
        setMsg(null)
        try {
            await adminApi.scanMedia()
            setMsg({type: 'success', text: 'Media scan triggered. Files will be indexed in the background.'})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setScanning(false)
        }
    }

    async function handleCreateBackup() {
        setCreatingBackup(true)
        setMsg(null)
        try {
            await adminApi.createBackup()
            setMsg({type: 'success', text: 'Backup created successfully.'})
            await queryClient.invalidateQueries({queryKey: ['admin-backups']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setCreatingBackup(false)
        }
    }

    async function handleRestore(id: string, filename: string) {
        if (!window.confirm(`Restore backup "${filename}"? This will overwrite current data.`)) return
        try {
            await adminApi.restoreBackup(id)
            setMsg({type: 'success', text: 'Restore initiated. Restart the server to apply changes.'})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleDeleteBackup(id: string, filename: string) {
        if (!window.confirm(`Delete backup "${filename}"? This cannot be undone.`)) return
        try {
            await adminApi.deleteBackup(id)
            setMsg({type: 'success', text: `Backup "${filename}" deleted.`})
            await queryClient.invalidateQueries({queryKey: ['admin-backups']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleDeleteMedia(path: string, name: string) {
        if (!window.confirm(`Delete "${name}" from the server? This cannot be undone.`)) return
        try {
            await adminApi.deleteMedia(path)
            setMsg({type: 'success', text: `Deleted "${name}".`})
            await queryClient.invalidateQueries({queryKey: ['admin-media']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleSaveEdit() {
        if (!editItem) return
        try {
            await adminApi.updateMedia(editItem.id, {
                name: editItem.name,
                category: editItem.category,
                tags: editItem.tags ?? [],
                is_mature: editItem.is_mature
            })
            setEditItem(null)
            setMsg({type: 'success', text: 'Media updated.'})
            await queryClient.invalidateQueries({queryKey: ['admin-media']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleBulkDelete() {
        if (selected.size === 0) return
        if (!window.confirm(`Delete ${selected.size} selected item(s)? This cannot be undone.`)) return
        setBulkWorking(true)
        setMsg(null)
        try {
            const result = await adminApi.bulkMedia([...selected], 'delete')
            setSelected(new Set())
            await queryClient.invalidateQueries({queryKey: ['admin-media']})
            setMsg({
                type: result.failed > 0 ? 'error' : 'success',
                text: `Deleted ${result.success} item(s)${result.failed > 0 ? `, ${result.failed} failed` : ''}.`
            })
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setBulkWorking(false)
        }
    }

    async function handleBulkUpdate() {
        if (selected.size === 0) return
        const data: { category?: string; is_mature?: boolean } = {}
        if (bulkCategory.trim()) data.category = bulkCategory.trim()
        if (bulkMature !== null) data.is_mature = bulkMature
        if (!data.category && data.is_mature === undefined) {
            setMsg({type: 'error', text: 'Set a category or mature flag before applying.'})
            return
        }
        setBulkWorking(true)
        setMsg(null)
        try {
            const result = await adminApi.bulkMedia([...selected], 'update', data)
            setSelected(new Set())
            setBulkCategory('')
            setBulkMature(null)
            await queryClient.invalidateQueries({queryKey: ['admin-media']})
            setMsg({
                type: result.failed > 0 ? 'error' : 'success',
                text: `Updated ${result.success} item(s)${result.failed > 0 ? `, ${result.failed} failed` : ''}.`
            })
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setBulkWorking(false)
        }
    }

    const allSelected = mediaItems.length > 0 && mediaItems.every(i => selected.has(i.id))

    function toggleSelectAll() {
        if (allSelected) {
            setSelected(prev => {
                const next = new Set(prev)
                mediaItems.forEach(i => next.delete(i.id))
                return next
            })
        } else {
            setSelected(prev => {
                const next = new Set(prev)
                mediaItems.forEach(i => next.add(i.id))
                return next
            })
        }
    }

    function toggleSelect(id: string) {
        setSelected(prev => {
            const next = new Set(prev)
            if (next.has(id)) next.delete(id)
            else next.add(id)
            return next
        })
    }

    const selectStyle: React.CSSProperties = {
        padding: '6px 10px',
        border: '1px solid var(--border-color)',
        borderRadius: 6,
        background: 'var(--input-bg)',
        color: 'var(--text-color)',
        fontSize: 13,
    }

    const thSortStyle: React.CSSProperties = {
        cursor: 'pointer',
        userSelect: 'none',
        whiteSpace: 'nowrap',
    }

    return (
        <div>
            {msg && <div
                className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}>{msg.text}</div>}
            <div className="admin-card">
                <h2>Media Library</h2>
                <div className="admin-action-row">
                    <button className="admin-btn admin-btn-primary" onClick={handleScanMedia} disabled={scanning}>
                        {scanning ? <><i className="bi bi-arrow-repeat"/> Scanning...</> : <><i
                            className="bi bi-search"/> Scan Media Library</>}
                    </button>
                    <button className="admin-btn" onClick={() => adminApi.clearCache().then(() => setMsg({
                        type: 'success',
                        text: 'Cache cleared.'
                    })).catch(err => setMsg({type: 'error', text: errMsg(err)}))}>
                        <i className="bi bi-trash-fill"/> Clear Cache
                    </button>
                </div>

                {/* Search and filter controls */}
                <div style={{marginTop: 16, display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap'}}>
                    <input
                        type="text"
                        placeholder="Search media..."
                        value={mediaSearch}
                        onChange={e => {
                            setMediaSearch(e.target.value);
                            setMediaPage(1)
                        }}
                        style={{
                            flex: 1,
                            minWidth: 180,
                            padding: '6px 10px',
                            border: '1px solid var(--border-color)',
                            borderRadius: 6,
                            background: 'var(--input-bg)',
                            color: 'var(--text-color)',
                            fontSize: 13
                        }}
                    />
                    <select value={filterType} onChange={e => { setFilterType(e.target.value); setMediaPage(1) }} style={selectStyle}>
                        <option value="">All Types</option>
                        <option value="video">Video</option>
                        <option value="audio">Audio</option>
                    </select>
                    <select value={filterMature} onChange={e => { setFilterMature(e.target.value); setMediaPage(1) }} style={selectStyle}>
                        <option value="">All Content</option>
                        <option value="true">Mature Only</option>
                        <option value="false">Non-Mature Only</option>
                    </select>
                    <input
                        type="text"
                        placeholder="Filter category..."
                        value={filterCategory}
                        onChange={e => { setFilterCategory(e.target.value); setMediaPage(1) }}
                        style={{...selectStyle, width: 140}}
                    />
                    <input
                        type="text"
                        placeholder="Filter tags..."
                        value={filterTags}
                        onChange={e => { setFilterTags(e.target.value); setMediaPage(1) }}
                        style={{...selectStyle, width: 120}}
                    />
                    <select value={mediaLimit} onChange={e => { setMediaLimit(Number(e.target.value)); setMediaPage(1) }} style={selectStyle}>
                        <option value={20}>20 per page</option>
                        <option value={50}>50 per page</option>
                        <option value={100}>100 per page</option>
                        <option value={200}>200 per page</option>
                    </select>
                    {(filterType || filterCategory || filterMature || filterTags || debouncedMediaSearch) && (
                        <button className="admin-btn" style={{fontSize: 12, padding: '4px 10px'}}
                                onClick={() => { setFilterType(''); setFilterCategory(''); setFilterMature(''); setFilterTags(''); setMediaSearch(''); setMediaPage(1) }}>
                            <i className="bi bi-x-circle"/> Clear Filters
                        </button>
                    )}
                </div>

                {totalItems > 0 && (
                    <div style={{marginTop: 8, fontSize: 12, color: 'var(--text-muted)'}}>
                        {totalItems.toLocaleString()} item{totalItems !== 1 ? 's' : ''} found
                    </div>
                )}

                {selected.size > 0 && (
                    <div style={{
                        marginTop: 12,
                        padding: '10px 14px',
                        background: 'var(--hover-bg)',
                        border: '1px solid var(--border-color)',
                        borderRadius: 6,
                        display: 'flex',
                        flexWrap: 'wrap',
                        gap: 10,
                        alignItems: 'center',
                    }}>
                        <span style={{fontSize: 13, fontWeight: 600, color: 'var(--accent-color)'}}>
                            {selected.size} selected
                        </span>
                        <input
                            type="text"
                            placeholder="Category…"
                            value={bulkCategory}
                            onChange={e => setBulkCategory(e.target.value)}
                            style={{
                                padding: '4px 8px',
                                border: '1px solid var(--border-color)',
                                borderRadius: 4,
                                background: 'var(--input-bg)',
                                color: 'var(--text-color)',
                                fontSize: 12,
                                width: 120,
                            }}
                        />
                        <select
                            value={bulkMature === null ? '' : String(bulkMature)}
                            onChange={e => setBulkMature(e.target.value === '' ? null : e.target.value === 'true')}
                            style={{
                                padding: '4px 8px',
                                border: '1px solid var(--border-color)',
                                borderRadius: 4,
                                background: 'var(--input-bg)',
                                color: 'var(--text-color)',
                                fontSize: 12,
                            }}
                        >
                            <option value="">Mature: no change</option>
                            <option value="true">Mark mature</option>
                            <option value="false">Mark not mature</option>
                        </select>
                        <button className="admin-btn admin-btn-primary" onClick={handleBulkUpdate}
                                disabled={bulkWorking} style={{fontSize: 12, padding: '4px 10px'}}>
                            <i className="bi bi-pencil-fill"/> Apply Update
                        </button>
                        <button className="admin-btn admin-btn-danger" onClick={handleBulkDelete} disabled={bulkWorking}
                                style={{fontSize: 12, padding: '4px 10px'}}>
                            <i className="bi bi-trash-fill"/> Delete Selected
                        </button>
                        <button className="admin-btn" onClick={() => setSelected(new Set())}
                                style={{fontSize: 12, padding: '4px 10px'}}>
                            Clear
                        </button>
                    </div>
                )}

                {editItem && (
                    <div className="admin-card" style={{marginTop: 12, background: 'var(--hover-bg)'}}>
                        <h3 style={{marginBottom: 10}}>Edit Media</h3>
                        <div style={{display: 'grid', gap: 8}}>
                            <label style={{fontSize: 13}}>Name
                                <input value={editItem.name}
                                       onChange={e => setEditItem({...editItem, name: e.target.value})}
                                       style={{
                                           display: 'block',
                                           width: '100%',
                                           marginTop: 4,
                                           padding: '6px 8px',
                                           border: '1px solid var(--border-color)',
                                           borderRadius: 4,
                                           background: 'var(--input-bg)',
                                           color: 'var(--text-color)'
                                       }}/>
                            </label>
                            <label style={{fontSize: 13}}>Category
                                <input value={editItem.category ?? ''}
                                       onChange={e => setEditItem({...editItem, category: e.target.value})}
                                       style={{
                                           display: 'block',
                                           width: '100%',
                                           marginTop: 4,
                                           padding: '6px 8px',
                                           border: '1px solid var(--border-color)',
                                           borderRadius: 4,
                                           background: 'var(--input-bg)',
                                           color: 'var(--text-color)'
                                       }}/>
                            </label>
                            <label style={{fontSize: 13}}>Tags (comma-separated)
                                <input value={(editItem.tags ?? []).join(', ')}
                                       onChange={e => setEditItem({...editItem, tags: e.target.value.split(',').map(t => t.trim()).filter(Boolean)})}
                                       style={{
                                           display: 'block',
                                           width: '100%',
                                           marginTop: 4,
                                           padding: '6px 8px',
                                           border: '1px solid var(--border-color)',
                                           borderRadius: 4,
                                           background: 'var(--input-bg)',
                                           color: 'var(--text-color)'
                                       }}/>
                            </label>
                            <label style={{fontSize: 13, display: 'flex', alignItems: 'center', gap: 8}}>
                                <input type="checkbox" checked={editItem.is_mature}
                                       onChange={e => setEditItem({...editItem, is_mature: e.target.checked})}/>
                                Mature content (18+)
                            </label>
                        </div>
                        <div className="admin-action-row" style={{marginTop: 10}}>
                            <button className="admin-btn admin-btn-primary" onClick={handleSaveEdit}><i
                                className="bi bi-check-lg"/> Save
                            </button>
                            <button className="admin-btn" onClick={() => setEditItem(null)}>Cancel</button>
                        </div>
                    </div>
                )}

                <div className="admin-table-wrapper" style={{marginTop: 12}}>
                    <table className="admin-table">
                        <thead>
                        <tr>
                            <th style={{width: 32}}>
                                <input type="checkbox" checked={allSelected} onChange={toggleSelectAll}
                                       title={allSelected ? 'Deselect all' : 'Select all on page'}/>
                            </th>
                            {MEDIA_SORT_COLUMNS.map(col => (
                                <th key={col.key} style={thSortStyle} onClick={() => handleSort(col.key)}>
                                    {col.label}{sortIndicator(col.key)}
                                </th>
                            ))}
                            <th>Actions</th>
                        </tr>
                        </thead>
                        <tbody>
                        {mediaItems.map(item => (
                            <tr key={item.id}
                                style={selected.has(item.id) ? {background: 'color-mix(in srgb, var(--accent-color) 8%, transparent)'} : undefined}>
                                <td>
                                    <input type="checkbox" checked={selected.has(item.id)}
                                           onChange={() => toggleSelect(item.id)}/>
                                </td>
                                <td style={{
                                    maxWidth: 240,
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap'
                                }}>{item.name}</td>
                                <td>{item.type}</td>
                                <td style={{whiteSpace: 'nowrap'}}>{formatBytes(item.size)}</td>
                                <td style={{whiteSpace: 'nowrap'}}>{formatDuration(item.duration)}</td>
                                <td>{item.category || '—'}</td>
                                <td style={{whiteSpace: 'nowrap'}}>{new Date(item.date_added).toLocaleDateString()}</td>
                                <td style={{whiteSpace: 'nowrap'}}>{new Date(item.date_modified).toLocaleDateString()}</td>
                                <td>{item.views}</td>
                                <td>{item.is_mature ? <span style={{color: '#ef4444'}}>Yes</span> : 'No'}</td>
                                <td>
                                    <div style={{display: 'flex', gap: 4}}>
                                        <button className="admin-btn" style={{padding: '3px 8px', fontSize: 12}}
                                                onClick={() => setEditItem(item)}>
                                            <i className="bi bi-pencil-fill"/>
                                        </button>
                                        <button className="admin-btn admin-btn-danger"
                                                style={{padding: '3px 8px', fontSize: 12}}
                                                onClick={() => handleDeleteMedia(item.id, item.name)}>
                                            <i className="bi bi-trash-fill"/>
                                        </button>
                                    </div>
                                </td>
                            </tr>
                        ))}
                        {mediaItems.length === 0 && (
                            <tr>
                                <td colSpan={MEDIA_SORT_COLUMNS.length + 2}
                                    style={{textAlign: 'center', color: 'var(--text-muted)', padding: '20px 0'}}>No
                                    media found
                                </td>
                            </tr>
                        )}
                        </tbody>
                    </table>
                </div>
                <div style={{display: 'flex', justifyContent: 'center', gap: 8, marginTop: 8, alignItems: 'center'}}>
                    <button className="admin-btn" disabled={mediaPage <= 1} onClick={() => setMediaPage(p => p - 1)}>←
                        Prev
                    </button>
                    <span style={{fontSize: 13, color: 'var(--text-muted)', padding: '4px 0'}}>
                        Page {mediaPage} of {totalPages}
                    </span>
                    <button className="admin-btn" disabled={mediaPage >= totalPages}
                            onClick={() => setMediaPage(p => p + 1)}>Next →
                    </button>
                </div>
            </div>
            <div className="admin-card">
                <h2>Backups</h2>
                <div className="admin-action-row">
                    <button className="admin-btn admin-btn-primary" onClick={handleCreateBackup}
                            disabled={creatingBackup}>
                        {creatingBackup ? 'Creating...' : <><i className="bi bi-plus-circle"/> Create Backup</>}
                    </button>
                </div>
                {backups.length === 0 ? (
                    <p style={{color: 'var(--text-muted)', fontSize: 13}}>No backups found.</p>
                ) : <SortableBackupsTable backups={backups} onRestore={handleRestore} onDelete={handleDeleteBackup} />}
            </div>

            {/* Feature 7: Thumbnail Stats */}
            <ThumbnailStatsCard/>
        </div>
    )
}

// ── Sortable Backups Table ────────────────────────────────────────────────────

type BackupSortKey = 'filename' | 'size' | 'created_at'
function SortableBackupsTable({backups, onRestore, onDelete}: {
    backups: Array<{id: string; filename: string; size: number; created_at: string}>;
    onRestore: (id: string, filename: string) => void;
    onDelete: (id: string, filename: string) => void;
}) {
    const [sortBy, setSortBy] = useState<BackupSortKey>('created_at')
    const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('desc')

    const sorted = [...backups].sort((a, b) => {
        let cmp = 0
        switch (sortBy) {
            case 'filename': cmp = a.filename.localeCompare(b.filename); break
            case 'size': cmp = a.size - b.size; break
            case 'created_at': cmp = a.created_at.localeCompare(b.created_at); break
        }
        return sortOrder === 'desc' ? -cmp : cmp
    })

    function handleSort(col: BackupSortKey) {
        if (sortBy === col) setSortOrder(p => p === 'asc' ? 'desc' : 'asc')
        else { setSortBy(col); setSortOrder('asc') }
    }

    function ind(col: BackupSortKey) {
        if (sortBy !== col) return <span style={{opacity: 0.3, marginLeft: 4}}>&#x21C5;</span>
        return <span style={{marginLeft: 4}}>{sortOrder === 'asc' ? '\u25B2' : '\u25BC'}</span>
    }

    const thS: React.CSSProperties = {cursor: 'pointer', userSelect: 'none', whiteSpace: 'nowrap'}

    return (
        <div className="admin-table-wrapper">
            <table className="admin-table">
                <thead>
                <tr>
                    <th style={thS} onClick={() => handleSort('filename')}>Name{ind('filename')}</th>
                    <th style={thS} onClick={() => handleSort('size')}>Size{ind('size')}</th>
                    <th style={thS} onClick={() => handleSort('created_at')}>Created{ind('created_at')}</th>
                    <th>Actions</th>
                </tr>
                </thead>
                <tbody>
                {sorted.map(b => (
                    <tr key={b.id}>
                        <td>{b.filename}</td>
                        <td>{formatBytes(b.size)}</td>
                        <td>{new Date(b.created_at).toLocaleString()}</td>
                        <td>
                            <div style={{display: 'flex', gap: 6}}>
                                <button className="admin-btn admin-btn-warning"
                                        onClick={() => onRestore(b.id, b.filename)}>Restore
                                </button>
                                <button className="admin-btn admin-btn-danger"
                                        onClick={() => onDelete(b.id, b.filename)}>
                                    <i className="bi bi-trash-fill"/>
                                </button>
                            </div>
                        </td>
                    </tr>
                ))}
                </tbody>
            </table>
        </div>
    )
}

// ── Thumbnail Stats Card (Feature 7) ─────────────────────────────────────────

function ThumbnailStatsCard() {
    const [thumbPath, setThumbPath] = useState('')
    const [generatingThumb, setGeneratingThumb] = useState(false)
    const [thumbMsg, setThumbMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

    const {data: thumbStats} = useQuery<ThumbnailStats>({
        queryKey: ['admin-thumbnail-stats'],
        queryFn: () => adminApi.getThumbnailStats(),
    })

    async function handleGenerateThumb(e: React.FormEvent) {
        e.preventDefault()
        if (!thumbPath.trim()) return
        setGeneratingThumb(true)
        setThumbMsg(null)
        try {
            await adminApi.generateThumbnail(thumbPath.trim())
            setThumbMsg({type: 'success', text: 'Thumbnail generation triggered.'})
            setThumbPath('')
        } catch (err) {
            setThumbMsg({type: 'error', text: errMsg(err)})
        } finally {
            setGeneratingThumb(false)
        }
    }

    return (
        <div className="admin-card">
            <h2>Thumbnails</h2>
            {thumbStats && (
                <div className="admin-stats-grid" style={{marginBottom: 16}}>
                    <div className="admin-stat-card">
                        <span className="admin-stat-value">{thumbStats.total_thumbnails.toLocaleString()}</span>
                        <span className="admin-stat-label">Thumbnails</span>
                    </div>
                    <div className="admin-stat-card">
                        <span className="admin-stat-value">{thumbStats.total_size_mb.toFixed(1)} MB</span>
                        <span className="admin-stat-label">Total Size</span>
                    </div>
                    <div className="admin-stat-card">
                        <span className="admin-stat-value">{thumbStats.pending_generation.toLocaleString()}</span>
                        <span className="admin-stat-label">Pending</span>
                    </div>
                    <div className="admin-stat-card">
                        <span className="admin-stat-value"
                              style={{color: thumbStats.generation_errors > 0 ? '#ef4444' : 'inherit'}}>
                            {thumbStats.generation_errors.toLocaleString()}
                        </span>
                        <span className="admin-stat-label">Errors</span>
                    </div>
                </div>
            )}
            {thumbMsg && (
                <div className={`admin-alert admin-alert-${thumbMsg.type === 'success' ? 'success' : 'danger'}`}
                     style={{marginBottom: 8}}>
                    {thumbMsg.text}
                </div>
            )}
            <form onSubmit={handleGenerateThumb} style={{display: 'flex', gap: 8}}>
                <input
                    type="text"
                    value={thumbPath}
                    onChange={e => setThumbPath(e.target.value)}
                    placeholder="Media ID to generate thumbnail..."
                    style={{
                        flex: 1,
                        padding: '6px 10px',
                        border: '1px solid var(--border-color)',
                        borderRadius: 6,
                        background: 'var(--input-bg)',
                        color: 'var(--text-color)',
                        fontSize: 13
                    }}
                />
                <button type="submit" className="admin-btn admin-btn-primary"
                        disabled={generatingThumb || !thumbPath.trim()}>
                    <i className="bi bi-image"/> {generatingThumb ? 'Generating...' : 'Generate'}
                </button>
            </form>
        </div>
    )
}

// ── Tab: Streaming ────────────────────────────────────────────────────────────

type HLSSortKey = 'id' | 'status' | 'progress'
function StreamingTab() {
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
                    <select value={hlsFilterStatus} onChange={e => setHlsFilterStatus(e.target.value)}
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
                                onClick={() => setHlsFilterStatus('')}>
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
                                        <span className={`status-badge status-${job.status === 'completed' ? 'enabled' : job.status === 'failed' ? 'error' : job.status === 'running' ? 'running' : 'disabled'}`}>
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
                        ) : [...tasks].sort((a, b) => {
                            const statusOrder = (t: ScheduledTask) => t.running ? 0 : t.enabled ? 1 : 2
                            return statusOrder(a) - statusOrder(b) || a.name.localeCompare(b.name)
                        }).map(task => (
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
                <div className="admin-modal-overlay" onClick={() => setValidationResult(null)}>
                    <div className="admin-modal-box" onClick={e => e.stopPropagation()}>
                        <div className="admin-modal-header">
                            <h3><i className="bi bi-check2-circle"/> HLS Validation Result</h3>
                            <button className="admin-modal-close" onClick={() => setValidationResult(null)}>×</button>
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

// ── Tab: Analytics ────────────────────────────────────────────────────────────

function AnalyticsTab() {
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
                            <span className="admin-stat-value">{eventStats.total_events.toLocaleString()}</span>
                            <span className="admin-stat-label">Total Events</span>
                        </div>
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
                                           style={{color: 'var(--text-primary)'}}>
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
                        <select value={level} onChange={e => setLevel(e.target.value)}>
                            <option value="">All</option>
                            <option value="debug">Debug</option>
                            <option value="info">Info</option>
                            <option value="warn">Warn</option>
                            <option value="error">Error</option>
                        </select>
                    </div>
                    <div className="admin-form-group">
                        <label>Module</label>
                        <input value={module} onChange={e => setModule(e.target.value)} placeholder="Filter module..."/>
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
                               value={pwCurrent} onChange={e => setPwCurrent(e.target.value)}
                               autoComplete="current-password" required/>
                    </div>
                    <div className="admin-form-group">
                        <label htmlFor="pw-new">New Password</label>
                        <input id="pw-new" type="password" className="admin-input"
                               value={pwNew} onChange={e => setPwNew(e.target.value)}
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
                <textarea className="config-editor" value={configText} onChange={e => setConfigText(e.target.value)}/>
                <div className="admin-action-row" style={{marginTop: 10}}>
                    <button className="admin-btn admin-btn-primary" onClick={handleSave} disabled={loading}>
                        {loading ? 'Saving...' : <><i className="bi bi-floppy-fill"/> Save Configuration</>}
                    </button>
                </div>
            </div>
        </div>
    )
}

// ── Tab: Remote Sources ───────────────────────────────────────────────────────

function RemoteTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [syncing, setSyncing] = useState<string | null>(null)
    const [showAdd, setShowAdd] = useState(false)
    const [addName, setAddName] = useState('')
    const [addURL, setAddURL] = useState('')
    const [addUser, setAddUser] = useState('')
    const [addPass, setAddPass] = useState('')
    const [adding, setAdding] = useState(false)
    const [cleaningCache, setCleaningCache] = useState(false)
    // Feature 13: Remote media browsing
    const [browseSource, setBrowseSource] = useState<string | null>(null)
    const [browseMedia, setBrowseMedia] = useState<RemoteMediaItem[] | null>(null)
    const [browseLoading, setBrowseLoading] = useState(false)
    const [cachingUrl, setCachingUrl] = useState<string | null>(null)

    const {data: sources, isLoading, isError, error} = useQuery<RemoteSourceState[]>({
        queryKey: ['admin-remote-sources'],
        queryFn: () => adminApi.getRemoteSources(),
        refetchInterval: 15000,
        retry: false, // don't retry — feature-disabled 404s are permanent until config changes
    })

    const {data: stats} = useQuery({
        queryKey: ['admin-remote-stats'],
        queryFn: () => adminApi.getRemoteStats(),
        refetchInterval: 15000,
    })

    async function handleSync(name: string) {
        setSyncing(name)
        setMsg(null)
        try {
            await adminApi.syncRemoteSource(name)
            setMsg({type: 'success', text: `Sync started for "${name}"`})
            setTimeout(() => queryClient.invalidateQueries({queryKey: ['admin-remote-sources']}), 1500)
        } catch (err) {
            setMsg({type: 'error', text: `Sync failed: ${errMsg(err)}`})
        } finally {
            setSyncing(null)
        }
    }

    async function handleDelete(name: string) {
        if (!window.confirm(`Remove remote source "${name}"? This cannot be undone.`)) return
        setMsg(null)
        try {
            await adminApi.deleteRemoteSource(name)
            setMsg({type: 'success', text: `Source "${name}" removed`})
            await queryClient.invalidateQueries({queryKey: ['admin-remote-sources']})
            await queryClient.invalidateQueries({queryKey: ['admin-remote-stats']})
        } catch (err) {
            setMsg({type: 'error', text: `Delete failed: ${errMsg(err)}`})
        }
    }

    async function handleCleanCache() {
        if (!window.confirm('Remove all cached remote media files? Media will be re-fetched on demand.')) return
        setCleaningCache(true)
        setMsg(null)
        try {
            const res = await adminApi.cleanRemoteCache()
            setMsg({type: 'success', text: `Cache cleaned: ${res.removed} file(s) removed.`})
            await queryClient.invalidateQueries({queryKey: ['admin-remote-stats']})
        } catch (err) {
            setMsg({type: 'error', text: `Clean cache failed: ${errMsg(err)}`})
        } finally {
            setCleaningCache(false)
        }
    }

    async function handleBrowseSource(name: string) {
        setBrowseSource(name)
        setBrowseLoading(true)
        setBrowseMedia(null)
        try {
            const media = await adminApi.getSourceMedia(name)
            setBrowseMedia(media)
        } catch (err) {
            setMsg({type: 'error', text: `Browse failed: ${errMsg(err)}`})
        } finally {
            setBrowseLoading(false)
        }
    }

    async function handleBrowseAll() {
        setBrowseSource('all')
        setBrowseLoading(true)
        setBrowseMedia(null)
        try {
            const media = await adminApi.getAllRemoteMedia()
            setBrowseMedia(media)
        } catch (err) {
            setMsg({type: 'error', text: `Browse failed: ${errMsg(err)}`})
        } finally {
            setBrowseLoading(false)
        }
    }

    async function handleCacheMedia(url: string, sourceName: string) {
        setCachingUrl(url)
        try {
            await adminApi.cacheRemoteMedia(url, sourceName)
            setMsg({type: 'success', text: 'Cache request queued.'})
        } catch (err) {
            setMsg({type: 'error', text: `Cache failed: ${errMsg(err)}`})
        } finally {
            setCachingUrl(null)
        }
    }

    async function handleAdd(e: FormEvent) {
        e.preventDefault()
        if (!addName.trim() || !addURL.trim()) return
        setAdding(true)
        setMsg(null)
        try {
            await adminApi.createRemoteSource({
                name: addName.trim(),
                url: addURL.trim(),
                username: addUser.trim() || undefined,
                password: addPass.trim() || undefined,
            })
            setMsg({type: 'success', text: `Source "${addName.trim()}" added`})
            setAddName('');
            setAddURL('');
            setAddUser('');
            setAddPass('')
            setShowAdd(false)
            await queryClient.invalidateQueries({queryKey: ['admin-remote-sources']})
            await queryClient.invalidateQueries({queryKey: ['admin-remote-stats']})
        } catch (err) {
            setMsg({type: 'error', text: `Failed to add source: ${errMsg(err)}`})
        } finally {
            setAdding(false)
        }
    }

    function statusBadge(status: string) {
        const color = status === 'idle' ? '#22c55e' : status === 'syncing' ? '#f59e0b' : '#ef4444'
        return <span style={{color, fontWeight: 600, fontSize: 12}}>{status}</span>
    }

    return (
        <div>
            {msg && (
                <div className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}>
                    {msg.text}
                </div>
            )}

            {/* Stats summary */}
            {stats && (
                <div style={{marginBottom: 20}}>
                    <div className="admin-stats-grid" style={{marginBottom: 10}}>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{stats.source_count}</span>
                            <span className="admin-stat-label">Sources</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{stats.total_media_count}</span>
                            <span className="admin-stat-label">Remote Media</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{stats.cached_item_count}</span>
                            <span className="admin-stat-label">Cached Items</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{formatBytes(stats.cache_size)}</span>
                            <span className="admin-stat-label">Cache Size</span>
                        </div>
                    </div>
                    {stats.cached_item_count > 0 && (
                        <button className="admin-btn admin-btn-warning" onClick={handleCleanCache}
                                disabled={cleaningCache} style={{fontSize: 12}}>
                            <i className="bi bi-trash"/> {cleaningCache ? 'Cleaning...' : 'Clean Cache'}
                        </button>
                    )}
                </div>
            )}

            <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12}}>
                <h3 style={{margin: 0}}>Remote Sources</h3>
                <button className="admin-btn admin-btn-primary" onClick={() => setShowAdd(s => !s)}>
                    <i className={`bi bi-${showAdd ? 'x-lg' : 'plus-lg'}`}/> {showAdd ? 'Cancel' : 'Add Source'}
                </button>
            </div>

            {/* Add source form */}
            {showAdd && (
                <form onSubmit={handleAdd} style={{
                    background: 'var(--card-bg)',
                    border: '1px solid var(--border-color)',
                    borderRadius: 8,
                    padding: 16,
                    marginBottom: 16
                }}>
                    <h4 style={{margin: '0 0 12px 0'}}>New Remote Source</h4>
                    <div style={{display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginBottom: 10}}>
                        <div>
                            <label
                                style={{display: 'block', fontSize: 12, color: 'var(--text-muted)', marginBottom: 4}}>Name
                                *</label>
                            <input className="admin-input" placeholder="my-source" value={addName}
                                   onChange={e => setAddName(e.target.value)} required/>
                        </div>
                        <div>
                            <label
                                style={{display: 'block', fontSize: 12, color: 'var(--text-muted)', marginBottom: 4}}>URL
                                *</label>
                            <input className="admin-input" placeholder="https://example.com/media" value={addURL}
                                   onChange={e => setAddURL(e.target.value)} required/>
                        </div>
                        <div>
                            <label
                                style={{display: 'block', fontSize: 12, color: 'var(--text-muted)', marginBottom: 4}}>Username
                                (optional)</label>
                            <input className="admin-input" placeholder="user" value={addUser}
                                   onChange={e => setAddUser(e.target.value)} autoComplete="off"/>
                        </div>
                        <div>
                            <label
                                style={{display: 'block', fontSize: 12, color: 'var(--text-muted)', marginBottom: 4}}>Password
                                (optional)</label>
                            <input className="admin-input" type="password" placeholder="••••••" value={addPass}
                                   onChange={e => setAddPass(e.target.value)} autoComplete="new-password"/>
                        </div>
                    </div>
                    <button className="admin-btn admin-btn-primary" type="submit" disabled={adding}>
                        {adding ? 'Adding...' : <><i className="bi bi-plus-circle"/> Add Source</>}
                    </button>
                </form>
            )}

            {/* Sources table */}
            {isLoading ? (
                <p style={{color: 'var(--text-muted)'}}>Loading sources...</p>
            ) : isError ? (
                <div className="admin-alert admin-alert-danger">
                    {String(error).includes('disabled') || String(error).includes('404')
                        ? 'Remote media is disabled. Enable it in Settings → Features → Remote Media.'
                        : `Failed to load remote sources: ${String(error)}`}
                </div>
            ) : !sources || sources.length === 0 ? (
                <div style={{textAlign: 'center', padding: '40px 0', color: 'var(--text-muted)'}}>
                    <i className="bi bi-cloud-slash" style={{fontSize: 32}}/>
                    <p style={{marginTop: 8}}>No remote sources configured.</p>
                    <p style={{fontSize: 13}}>Add a source above to stream media from remote servers.</p>
                </div>
            ) : (
                <div className="admin-table-wrapper">
                    <table className="admin-table">
                        <thead>
                        <tr>
                            <th>Name</th>
                            <th>URL</th>
                            <th>Status</th>
                            <th>Media</th>
                            <th>Last Sync</th>
                            <th>Actions</th>
                        </tr>
                        </thead>
                        <tbody>
                        {[...sources].sort((a, b) => a.source.name.localeCompare(b.source.name)).map(s => (
                            <tr key={s.source.name}>
                                <td><strong>{s.source.name}</strong></td>
                                <td style={{
                                    maxWidth: 220,
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap'
                                }}>
                                    <a href={s.source.url} target="_blank" rel="noopener noreferrer"
                                       style={{color: 'var(--accent-color)'}}>{s.source.url}</a>
                                </td>
                                <td>{statusBadge(s.status)}{s.error && <span title={s.error} style={{
                                    marginLeft: 4,
                                    cursor: 'help',
                                    color: '#ef4444'
                                }}>⚠</span>}</td>
                                <td>{s.media_count}</td>
                                <td style={{fontSize: 12, color: 'var(--text-muted)'}}>
                                    {s.last_sync && !s.last_sync.startsWith('0001-')
                                        ? new Date(s.last_sync).toLocaleString()
                                        : '—'}
                                </td>
                                <td>
                                    <button
                                        className="admin-btn admin-btn-sm"
                                        onClick={() => handleSync(s.source.name)}
                                        disabled={syncing === s.source.name}
                                        title="Trigger sync"
                                    >
                                        <i className={`bi bi-arrow-repeat ${syncing === s.source.name ? 'spinning' : ''}`}/>
                                    </button>
                                    <button
                                        className="admin-btn admin-btn-sm admin-btn-danger"
                                        onClick={() => handleDelete(s.source.name)}
                                        title="Remove source"
                                        style={{marginLeft: 4}}
                                    >
                                        <i className="bi bi-trash"/>
                                    </button>
                                </td>
                            </tr>
                        ))}
                        </tbody>
                    </table>
                </div>
            )}

            {/* Feature 13: Browse Remote Media */}
            {sources && sources.length > 0 && (
                <div className="admin-card" style={{marginTop: 16}}>
                    <div style={{display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12}}>
                        <h3 style={{margin: 0}}>Browse Media</h3>
                        <button className="admin-btn" onClick={handleBrowseAll} disabled={browseLoading}>
                            <i className="bi bi-grid-fill"/> Browse All
                        </button>
                        {sources.map(s => (
                            <button key={s.source.name} className="admin-btn"
                                    onClick={() => handleBrowseSource(s.source.name)} disabled={browseLoading}>
                                <i className="bi bi-cloud"/> {s.source.name}
                            </button>
                        ))}
                        {browseSource && (
                            <button className="admin-btn" onClick={() => {
                                setBrowseSource(null);
                                setBrowseMedia(null)
                            }}>
                                <i className="bi bi-x-lg"/> Clear
                            </button>
                        )}
                    </div>
                    {browseLoading && <p style={{color: 'var(--text-muted)'}}>Loading...</p>}
                    {browseMedia && (
                        <div className="admin-table-wrapper">
                            <table className="admin-table">
                                <thead>
                                <tr>
                                    <th>Name</th>
                                    <th>Source</th>
                                    <th>Size</th>
                                    <th>Cached</th>
                                    <th>Actions</th>
                                </tr>
                                </thead>
                                <tbody>
                                {browseMedia.length === 0 ? (
                                    <tr>
                                        <td colSpan={5} style={{textAlign: 'center', color: 'var(--text-muted)'}}>No
                                            media found
                                        </td>
                                    </tr>
                                ) : [...browseMedia].sort((a, b) => a.name.localeCompare(b.name)).map(item => (
                                    <tr key={item.id}>
                                        <td style={{
                                            maxWidth: 200,
                                            overflow: 'hidden',
                                            textOverflow: 'ellipsis',
                                            whiteSpace: 'nowrap'
                                        }} title={item.name}>{item.name}</td>
                                        <td>{item.source_name}</td>
                                        <td>{item.size > 0 ? formatBytes(item.size) : '—'}</td>
                                        <td>{item.cached_at ? new Date(item.cached_at).toLocaleDateString() : '—'}</td>
                                        <td>
                                            <button
                                                className="admin-btn admin-btn-primary"
                                                style={{padding: '3px 8px'}}
                                                disabled={cachingUrl === item.url}
                                                onClick={() => handleCacheMedia(item.url, item.source_name)}
                                            >
                                                <i className="bi bi-cloud-download"/> {cachingUrl === item.url ? '...' : 'Cache'}
                                            </button>
                                        </td>
                                    </tr>
                                ))}
                                </tbody>
                            </table>
                        </div>
                    )}
                </div>
            )}
        </div>
    )
}

// ── Tab: Receiver (master/slave node management) ──────────────────────────────

function ReceiverTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [removingId, setRemovingId] = useState<string | null>(null)
    const [browseSlaveId, setBrowseSlaveId] = useState<string | null>(null)
    const [browseMedia, setBrowseMedia] = useState<ReceiverMediaItem[] | null>(null)
    const [browseLoading, setBrowseLoading] = useState(false)

    const {data: slaves, isLoading, isError} = useQuery<SlaveNode[]>({
        queryKey: ['admin-receiver-slaves'],
        queryFn: () => adminApi.getReceiverSlaves(),
        refetchInterval: 10000,
        retry: false,
    })

    const {data: stats} = useQuery<ReceiverStats>({
        queryKey: ['admin-receiver-stats'],
        queryFn: () => adminApi.getReceiverStats(),
        refetchInterval: 10000,
        retry: false,
    })

    async function handleRemove(id: string, name: string) {
        if (!window.confirm(`Remove slave node "${name}"? It will need to re-register to reconnect.`)) return
        setRemovingId(id)
        setMsg(null)
        try {
            await adminApi.removeReceiverSlave(id)
            setMsg({type: 'success', text: `Slave "${name}" removed`})
            await queryClient.invalidateQueries({queryKey: ['admin-receiver-slaves']})
            await queryClient.invalidateQueries({queryKey: ['admin-receiver-stats']})
            if (browseSlaveId === id) {
                setBrowseSlaveId(null)
                setBrowseMedia(null)
            }
        } catch (err) {
            setMsg({type: 'error', text: `Remove failed: ${errMsg(err)}`})
        } finally {
            setRemovingId(null)
        }
    }

    async function handleBrowse(slave: SlaveNode) {
        if (browseSlaveId === slave.id) {
            setBrowseSlaveId(null)
            setBrowseMedia(null)
            return
        }
        setBrowseSlaveId(slave.id)
        setBrowseLoading(true)
        setBrowseMedia(null)
        try {
            const all = await receiverApi.listMedia()
            setBrowseMedia(all.filter(m => m.slave_id === slave.id))
        } catch (err) {
            setMsg({type: 'error', text: `Browse failed: ${errMsg(err)}`})
            setBrowseSlaveId(null)
        } finally {
            setBrowseLoading(false)
        }
    }

    function statusBadge(status: string) {
        const cls = status === 'online' ? 'badge-active' : status === 'stale' ? 'badge-mature' : 'badge-inactive'
        const icon = status === 'online' ? 'bi-circle-fill' : status === 'stale' ? 'bi-exclamation-circle-fill' : 'bi-circle'
        return <span className={`media-card-type-badge ${cls}`}><i className={`bi ${icon}`}/> {status}</span>
    }

    function relativeTime(iso: string): string {
        const ms = Date.now() - new Date(iso).getTime()
        if (ms < 0 || iso.startsWith('0001')) return 'never'
        const s = Math.floor(ms / 1000)
        if (s < 60) return `${s}s ago`
        if (s < 3600) return `${Math.floor(s / 60)}m ago`
        if (s < 86400) return `${Math.floor(s / 3600)}h ago`
        return `${Math.floor(s / 86400)}d ago`
    }

    const apiKey = '(set in RECEIVER_API_KEYS env var)'

    return (
        <div>
            <h2 style={{margin: '0 0 4px 0', fontSize: 20}}><i className="bi bi-diagram-3-fill"/> Receiver — Slave Nodes</h2>
            <p style={{margin: '0 0 20px 0', color: 'var(--text-muted)', fontSize: 13}}>
                This server is the <strong>master</strong>. Slave nodes register here, push their media
                catalogs, and their streams are proxied to users on demand. No media is stored locally —
                streams pass through in real time.
            </p>

            {msg && (
                <div className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}
                     style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center'}}>
                    <span><i className={`bi ${msg.type === 'success' ? 'bi-check-circle-fill' : 'bi-exclamation-triangle-fill'}`}/> {msg.text}</span>
                    <button onClick={() => setMsg(null)} style={{background: 'none', border: 'none', cursor: 'pointer', fontSize: 16, opacity: 0.7}}>×</button>
                </div>
            )}

            {/* Stats */}
            <div className="admin-stats-grid">
                <div className="admin-stat-card">
                    <span className="admin-stat-value">{stats?.slave_count ?? '—'}</span>
                    <span className="admin-stat-label"><i className="bi bi-hdd-network"/> Total Slaves</span>
                </div>
                <div className="admin-stat-card">
                    <span className="admin-stat-value" style={{color: '#10b981'}}>{stats?.online_slaves ?? '—'}</span>
                    <span className="admin-stat-label"><i className="bi bi-circle-fill"/> Online</span>
                </div>
                <div className="admin-stat-card">
                    <span className="admin-stat-value">{stats?.media_count ?? '—'}</span>
                    <span className="admin-stat-label"><i className="bi bi-collection-fill"/> Total Media</span>
                </div>
            </div>

            {/* Registration guide */}
            <div className="admin-alert admin-alert-info" style={{marginBottom: 16}}>
                <strong><i className="bi bi-info-circle-fill"/> How to register a slave node</strong>
                <ol style={{margin: '8px 0 0 0', paddingLeft: 20, lineHeight: 1.8}}>
                    <li>Deploy Media Server Pro on the slave, set <code>REMOTE_MEDIA_ENABLED=true</code>.</li>
                    <li><code>POST /api/receiver/register</code> → <code>{`{"name":"slave-name","base_url":"http://slave-host:port"}`}</code> → returns <code>slave_id</code></li>
                    <li><code>POST /api/receiver/catalog</code> with <code>X-API-Key: {apiKey}</code> header and <code>{`{"slave_id":"...","full":true,"items":[...]}`}</code></li>
                    <li>Send periodic <code>POST /api/receiver/heartbeat</code> with <code>{`{"slave_id":"..."}`}</code></li>
                </ol>
            </div>

            {/* Slave table */}
            <div className="admin-card">
                <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12}}>
                    <h3 style={{margin: 0}}><i className="bi bi-hdd-network-fill"/> Registered Slaves</h3>
                    <button className="admin-btn" style={{fontSize: 12, padding: '5px 10px'}}
                            onClick={() => queryClient.invalidateQueries({queryKey: ['admin-receiver-slaves']})}>
                        <i className="bi bi-arrow-clockwise"/> Refresh
                    </button>
                </div>

                {isLoading && <p style={{color: 'var(--text-muted)', fontSize: 13}}><i className="bi bi-hourglass-split"/> Loading slaves…</p>}

                {isError && (
                    <div className="admin-alert admin-alert-warning">
                        <i className="bi bi-exclamation-triangle-fill"/> Receiver feature may be disabled.
                        Set <code>RECEIVER_ENABLED=true</code> and restart the service.
                    </div>
                )}

                {!isLoading && !isError && (!slaves || slaves.length === 0) && (
                    <div style={{textAlign: 'center', padding: '32px 16px', color: 'var(--text-muted)'}}>
                        <i className="bi bi-hdd-network" style={{fontSize: 32, display: 'block', marginBottom: 8}}/>
                        <p style={{margin: '0 0 4px 0'}}>No slave nodes registered yet.</p>
                        <p style={{margin: 0, fontSize: 12}}>Run <code>./deploy.sh --setup-receiver</code> on this master, then configure your slave servers.</p>
                    </div>
                )}

                {slaves && slaves.length > 0 && (
                    <div className="admin-table-wrapper">
                        <table className="admin-table">
                            <thead>
                                <tr>
                                    <th>Name</th>
                                    <th>Base URL</th>
                                    <th>Status</th>
                                    <th>Media</th>
                                    <th>Last Seen</th>
                                    <th>Registered</th>
                                    <th>Actions</th>
                                </tr>
                            </thead>
                            <tbody>
                                {slaves.map(slave => (
                                    <>
                                        <tr key={slave.id}>
                                            <td>
                                                <strong>{slave.name}</strong>
                                                <br/>
                                                <span style={{fontSize: 11, color: 'var(--text-muted)'}}>{slave.id}</span>
                                            </td>
                                            <td><a href={slave.base_url} target="_blank" rel="noreferrer">{slave.base_url}</a></td>
                                            <td>{statusBadge(slave.status)}</td>
                                            <td>{slave.media_count}</td>
                                            <td>{relativeTime(slave.last_seen)}</td>
                                            <td>{new Date(slave.registered_at).toLocaleDateString()}</td>
                                            <td style={{display: 'flex', gap: 6, flexWrap: 'wrap'}}>
                                                <button
                                                    className="admin-btn"
                                                    style={{fontSize: 12, padding: '4px 8px'}}
                                                    onClick={() => handleBrowse(slave)}
                                                    disabled={browseLoading && browseSlaveId === slave.id}
                                                >
                                                    <i className={`bi ${browseSlaveId === slave.id ? 'bi-chevron-up' : 'bi-folder2-open'}`}/>
                                                    {browseSlaveId === slave.id ? ' Hide' : ' Browse'}
                                                </button>
                                                <button
                                                    className="admin-btn admin-btn-danger"
                                                    style={{fontSize: 12, padding: '4px 8px'}}
                                                    onClick={() => handleRemove(slave.id, slave.name)}
                                                    disabled={removingId === slave.id}
                                                >
                                                    {removingId === slave.id
                                                        ? <><i className="bi bi-hourglass-split"/> Removing…</>
                                                        : <><i className="bi bi-trash3-fill"/> Remove</>}
                                                </button>
                                            </td>
                                        </tr>
                                        {browseSlaveId === slave.id && (
                                            <tr key={`${slave.id}-browse`}>
                                                <td colSpan={7} style={{padding: '12px 16px', background: 'var(--input-bg)'}}>
                                                    {browseLoading && (
                                                        <span style={{color: 'var(--text-muted)', fontSize: 13}}>
                                                            <i className="bi bi-hourglass-split"/> Loading media…
                                                        </span>
                                                    )}
                                                    {!browseLoading && browseMedia && browseMedia.length === 0 && (
                                                        <span style={{color: 'var(--text-muted)', fontSize: 13}}>No media items in catalog yet.</span>
                                                    )}
                                                    {!browseLoading && browseMedia && browseMedia.length > 0 && (
                                                        <div className="admin-table-wrapper" style={{maxHeight: 280, overflowY: 'auto'}}>
                                                            <table className="admin-table" style={{fontSize: 12}}>
                                                                <thead>
                                                                    <tr>
                                                                        <th>Name</th>
                                                                        <th>Type</th>
                                                                        <th>Size</th>
                                                                        <th>Duration</th>
                                                                        <th>Stream</th>
                                                                    </tr>
                                                                </thead>
                                                                <tbody>
                                                                    {browseMedia.map(m => (
                                                                        <tr key={m.id}>
                                                                            <td>{m.name}</td>
                                                                            <td><span className={`media-card-type-badge badge-${m.media_type}`}>{m.media_type}</span></td>
                                                                            <td>{formatBytes(m.size)}</td>
                                                                            <td>{m.duration > 0 ? `${Math.floor(m.duration / 60)}:${String(Math.floor(m.duration % 60)).padStart(2, '0')}` : '—'}</td>
                                                                            <td>
                                                                                <a href={mediaApi.getStreamUrl(m.id)} target="_blank" rel="noreferrer"
                                                                                   className="admin-btn" style={{fontSize: 11, padding: '3px 7px'}}>
                                                                                    <i className="bi bi-play-fill"/> Play
                                                                                </a>
                                                                            </td>
                                                                        </tr>
                                                                    ))}
                                                                </tbody>
                                                            </table>
                                                        </div>
                                                    )}
                                                </td>
                                            </tr>
                                        )}
                                    </>
                                ))}
                            </tbody>
                        </table>
                    </div>
                )}
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
        if (/^\s*(DROP|DELETE|TRUNCATE|ALTER|UPDATE)\b/i.test(query) && !window.confirm('This query modifies data. Proceed?')) return
        setQuerying(true)
        setQueryMsg('')
        setResult(null)
        try {
            const r = await adminApi.executeQuery(query)
            setResult(r)
            setQueryMsg(r.rows_affected != null ? `${r.rows_affected} row(s) affected` : `${r.rows?.length ?? 0} row(s) returned`)
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
                        <div><strong>Schema Version:</strong> v{dbStatus.schema_version}</div>
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
                              onChange={e => setQuery(e.target.value)} placeholder="SELECT * FROM users LIMIT 10;"/>
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

// ── Tab: Content Review ───────────────────────────────────────────────────────

type ReviewSortKey = 'name' | 'detected_at' | 'confidence'
function ContentReviewTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [selected, setSelected] = useState<Set<string>>(new Set())
    const [scanning, setScanning] = useState(false)
    const [processingPaths, setProcessingPaths] = useState<Set<string>>(new Set())
    const [reviewSortBy, setReviewSortBy] = useState<ReviewSortKey>('confidence')
    const [reviewSortOrder, setReviewSortOrder] = useState<'asc' | 'desc'>('desc')
    const [reviewSearch, setReviewSearch] = useState('')

    const {data: scanStats} = useQuery({
        queryKey: ['scanner-stats'],
        queryFn: () => adminApi.getScannerStats(),
    })

    const {data: queue = [], isLoading} = useQuery({
        queryKey: ['review-queue'],
        queryFn: () => adminApi.getReviewQueue(),
    })

    async function handleBatchAction(action: 'approve' | 'reject') {
        const ids = selected.size > 0 ? Array.from(selected) : queue.map(i => i.id)
        if (!ids.length || !window.confirm(`Apply "${action}" to ${ids.length} item(s)?`)) return
        try {
            await adminApi.batchReview(action, ids)
            setSelected(new Set())
            setMsg({type: 'success', text: `"${action}" applied to ${ids.length} item(s).`})
            await queryClient.invalidateQueries({queryKey: ['review-queue']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleRunScan() {
        setScanning(true)
        try {
            await adminApi.runScan()
            setMsg({type: 'success', text: 'Content scan triggered.'})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setScanning(false)
        }
    }

    return (
        <div>
            {msg && <div
                className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}>{msg.text}</div>}
            {scanStats && (
                <div className="admin-stats-grid" style={{marginBottom: 16}}>
                    <div className="admin-stat-card"><span
                        className="admin-stat-value">{scanStats.total_scanned.toLocaleString()}</span><span
                        className="admin-stat-label">Scanned</span></div>
                    <div className="admin-stat-card"><span className="admin-stat-value"
                                                           style={{color: '#ef4444'}}>{scanStats.mature_count.toLocaleString()}</span><span
                        className="admin-stat-label">Flagged</span></div>
                    <div className="admin-stat-card"><span className="admin-stat-value"
                                                           style={{color: '#10b981'}}>{(scanStats.total_scanned - scanStats.mature_count).toLocaleString()}</span><span
                        className="admin-stat-label">Clean</span></div>
                    <div className="admin-stat-card"><span className="admin-stat-value"
                                                           style={{color: '#f59e0b'}}>{scanStats.pending_review.toLocaleString()}</span><span
                        className="admin-stat-label">Pending</span></div>
                </div>
            )}
            <div className="admin-action-row">
                <button className="admin-btn admin-btn-primary" onClick={handleRunScan} disabled={scanning}>
                    {scanning ? <><i className="bi bi-arrow-repeat"/> Scanning...</> : <><i
                        className="bi bi-search"/> Run Scan</>}
                </button>
                {queue.length > 0 && (
                    <>
                        <button className="admin-btn admin-btn-success" onClick={() => handleBatchAction('reject')}>
                            <i className="bi bi-check-circle"/> Not
                            Mature {selected.size > 0 ? `${selected.size}` : 'All'}
                        </button>
                        <button className="admin-btn admin-btn-warning" onClick={() => handleBatchAction('approve')}>
                            <i className="bi bi-exclamation-triangle-fill"/> Confirm
                            Mature {selected.size > 0 ? `${selected.size}` : 'All'}
                        </button>
                        <button className="admin-btn admin-btn-danger" onClick={() => {
                            if (window.confirm('Clear review queue?'))
                                adminApi.clearReviewQueue().then(() => {
                                    void queryClient.invalidateQueries({queryKey: ['review-queue']});
                                    setMsg({type: 'success', text: 'Queue cleared.'})
                                }).catch(err => setMsg({type: 'error', text: errMsg(err)}))
                        }}>
                            <i className="bi bi-trash-fill"/> Clear Queue
                        </button>
                    </>
                )}
            </div>
            {isLoading ? (
                <p style={{color: 'var(--text-muted)'}}>Loading queue...</p>
            ) : queue.length === 0 ? (
                <div className="admin-card" style={{textAlign: 'center', padding: 40, color: 'var(--text-muted)'}}>
                    <div style={{fontSize: 40, marginBottom: 12}}><i className="bi bi-check-circle"/></div>
                    <h3>Queue Empty</h3>
                    <p>No content pending review.</p>
                </div>
            ) : (() => {
                const sortedQueue = queue
                    .filter(i => !reviewSearch || i.name.toLowerCase().includes(reviewSearch.toLowerCase()))
                    .sort((a, b) => {
                        let cmp = 0
                        switch (reviewSortBy) {
                            case 'name': cmp = a.name.localeCompare(b.name); break
                            case 'detected_at': cmp = (a.detected_at || '').localeCompare(b.detected_at || ''); break
                            case 'confidence': cmp = a.confidence - b.confidence; break
                        }
                        return reviewSortOrder === 'desc' ? -cmp : cmp
                    })
                const reviewThStyle: React.CSSProperties = {cursor: 'pointer', userSelect: 'none', whiteSpace: 'nowrap'}
                function reviewSortIndicator(col: ReviewSortKey) {
                    if (reviewSortBy !== col) return <span style={{opacity: 0.3, marginLeft: 4}}>&#x21C5;</span>
                    return <span style={{marginLeft: 4}}>{reviewSortOrder === 'asc' ? '\u25B2' : '\u25BC'}</span>
                }
                function handleReviewSort(col: ReviewSortKey) {
                    if (reviewSortBy === col) setReviewSortOrder(p => p === 'asc' ? 'desc' : 'asc')
                    else { setReviewSortBy(col); setReviewSortOrder('asc') }
                }
                return (<>
                <div style={{marginBottom: 8}}>
                    <input type="text" placeholder="Search files..." value={reviewSearch}
                           onChange={e => setReviewSearch(e.target.value)}
                           style={{padding: '6px 10px', border: '1px solid var(--border-color)', borderRadius: 6,
                                   background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13, width: '100%', maxWidth: 300}} />
                </div>
                <div className="admin-table-wrapper">
                    <table className="admin-table">
                        <thead>
                        <tr>
                            <th>
                                <input type="checkbox"
                                       onChange={e => setSelected(e.target.checked ? new Set(sortedQueue.map(i => i.id)) : new Set())}/>
                            </th>
                            <th style={reviewThStyle} onClick={() => handleReviewSort('name')}>File{reviewSortIndicator('name')}</th>
                            <th style={reviewThStyle} onClick={() => handleReviewSort('detected_at')}>Detected{reviewSortIndicator('detected_at')}</th>
                            <th style={reviewThStyle} onClick={() => handleReviewSort('confidence')}>Confidence{reviewSortIndicator('confidence')}</th>
                            <th>Reasons</th>
                            <th>Actions</th>
                        </tr>
                        </thead>
                        <tbody>
                        {sortedQueue.map(item => (
                            <tr key={item.id}>
                                <td><input type="checkbox" checked={selected.has(item.id)} onChange={() => {
                                    const next = new Set(selected)
                                    if (next.has(item.id)) next.delete(item.id)
                                    else next.add(item.id)
                                    setSelected(next)
                                }}/></td>
                                <td style={{
                                    maxWidth: 200,
                                    overflow: 'hidden',
                                    textOverflow: 'ellipsis',
                                    whiteSpace: 'nowrap'
                                }}>
                                    {item.name}
                                </td>
                                <td style={{fontSize: 11, color: 'var(--text-muted)'}}>
                                    {item.detected_at ? new Date(item.detected_at).toLocaleDateString() : '—'}
                                </td>
                                <td>
                                    <div style={{fontSize: 12}}>{Math.round(item.confidence * 100)}%</div>
                                    <div className="admin-progress-bg" style={{marginTop: 3}}>
                                        <div className="admin-progress-fill" style={{
                                            width: `${item.confidence * 100}%`,
                                            background: item.confidence > 0.8 ? '#ef4444' : item.confidence > 0.5 ? '#f59e0b' : '#10b981'
                                        }}/>
                                    </div>
                                </td>
                                <td style={{maxWidth: 160, fontSize: 11, color: 'var(--text-muted)'}}>
                                    {(item.reasons ?? []).join(', ') || '—'}
                                </td>
                                <td>
                                    <div style={{display: 'flex', gap: 6}}>
                                        <button className="admin-btn admin-btn-success" style={{padding: '3px 7px'}}
                                                title="Not mature"
                                                disabled={processingPaths.has(item.id)}
                                                onClick={() => {
                                                    setProcessingPaths(prev => new Set(prev).add(item.id))
                                                    adminApi.batchReview('reject', [item.id])
                                                        .then(() => void queryClient.invalidateQueries({queryKey: ['review-queue']}))
                                                        .catch(err => setMsg({type: 'error', text: errMsg(err)}))
                                                        .finally(() => setProcessingPaths(prev => {
                                                            const next = new Set(prev);
                                                            next.delete(item.id);
                                                            return next
                                                        }))
                                                }}>
                                            {processingPaths.has(item.id) ?
                                                <i className="bi bi-arrow-repeat"/> : <i className="bi bi-check-lg"/>}
                                        </button>
                                        <button className="admin-btn admin-btn-warning" style={{padding: '3px 7px'}}
                                                title="Confirm mature"
                                                disabled={processingPaths.has(item.id)}
                                                onClick={() => {
                                                    setProcessingPaths(prev => new Set(prev).add(item.id))
                                                    adminApi.batchReview('approve', [item.id])
                                                        .then(() => void queryClient.invalidateQueries({queryKey: ['review-queue']}))
                                                        .catch(err => setMsg({type: 'error', text: errMsg(err)}))
                                                        .finally(() => setProcessingPaths(prev => {
                                                            const next = new Set(prev);
                                                            next.delete(item.id);
                                                            return next
                                                        }))
                                                }}>
                                            <i className="bi bi-exclamation-triangle-fill"/>
                                        </button>
                                        <button className="admin-btn admin-btn-danger" style={{padding: '3px 7px'}}
                                                title="Reject and remove from library"
                                                disabled={processingPaths.has(item.id)}
                                                onClick={() => {
                                                    if (!window.confirm(`Remove "${item.name}" from library?`)) return
                                                    setProcessingPaths(prev => new Set(prev).add(item.id))
                                                    adminApi.rejectContent(item.id)
                                                        .then(() => void queryClient.invalidateQueries({queryKey: ['review-queue']}))
                                                        .catch(err => setMsg({type: 'error', text: errMsg(err)}))
                                                        .finally(() => setProcessingPaths(prev => {
                                                            const next = new Set(prev);
                                                            next.delete(item.id);
                                                            return next
                                                        }))
                                                }}>
                                            <i className="bi bi-trash-fill"/>
                                        </button>
                                    </div>
                                </td>
                            </tr>
                        ))}
                        </tbody>
                    </table>
                </div>
                </>)
            })()}
        </div>
    )
}

// ── Tab: Updates ──────────────────────────────────────────────────────────────

type BuildProgress = {
    inProgress: boolean
    stage: string
    progress: number
    error?: string
    done: boolean   // completed (success or error)
    success: boolean
}

function UpdatesTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [checking, setChecking] = useState(false)
    const [applying, setApplying] = useState(false)
    const [updateApplied, setUpdateApplied] = useState(false)
    const [checkingSource, setCheckingSource] = useState(false)
    const [sourceStatus, setSourceStatus] = useState<{ updates_available: boolean; remote_commit: string } | null>(null)
    const [build, setBuild] = useState<BuildProgress | null>(null)
    const [savingConfig, setSavingConfig] = useState(false)
    const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

    // Fetch update config (method + branch)
    const {data: updateCfg} = useQuery({
        queryKey: ['admin-update-config'],
        queryFn: () => adminApi.getUpdateConfig(),
        staleTime: 60_000,
    })

    const activeMethod = updateCfg?.update_method || 'source'
    const activeBranch = updateCfg?.branch || 'main'

    async function handleSaveConfig(method: 'source' | 'binary', branch: string) {
        setSavingConfig(true)
        setMsg(null)
        try {
            await adminApi.setUpdateConfig({update_method: method, branch})
            await queryClient.invalidateQueries({queryKey: ['admin-update-config']})
            setMsg({type: 'success', text: `Update settings saved: method=${method}, branch=${branch}`})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setSavingConfig(false)
        }
    }

    // Poll /api/admin/update/source/progress every 2s while a build is running
    useEffect(() => {
        if (!build?.inProgress) {
            if (pollRef.current) {
                clearInterval(pollRef.current)
                pollRef.current = null
            }
            return
        }
        pollRef.current = setInterval(async () => {
            try {
                const p = await adminApi.getSourceUpdateProgress()
                const done = !p.in_progress
                const success = done && !p.error
                setBuild({
                    inProgress: p.in_progress,
                    stage: p.stage,
                    progress: p.progress,
                    error: p.error,
                    done,
                    success,
                })
                if (done) {
                    clearInterval(pollRef.current!)
                    pollRef.current = null
                    if (p.error) {
                        setMsg({type: 'error', text: `Build failed at "${p.stage}": ${p.error}`})
                    } else if (p.stage.includes('up to date')) {
                        setMsg({type: 'success', text: 'Already up to date — no changes applied.'})
                    } else {
                        setMsg({type: 'success', text: `Build complete (${p.stage}). Restart the service to apply.`})
                    }
                }
            } catch {
                // network blip — keep polling
            }
        }, 2000)
        return () => {
            if (pollRef.current) clearInterval(pollRef.current)
        }
    }, [build?.inProgress])

    const {data: status, refetch} = useQuery({
        queryKey: ['admin-update-status'],
        queryFn: () => adminApi.getUpdateStatus(),
        staleTime: 60_000,
    })

    async function handleCheck() {
        setChecking(true)
        setMsg(null)
        try {
            await adminApi.checkUpdates()
            await refetch()
            setMsg({type: 'success', text: 'Update check complete.'})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setChecking(false)
        }
    }

    async function handleApply() {
        if (!window.confirm(
            'Download and install the new binary now?\n\n' +
            'You will need to restart the service afterwards to run the new version.'
        )) return
        setApplying(true)
        setUpdateApplied(false)
        setMsg(null)
        try {
            await adminApi.applyUpdate()
            setUpdateApplied(true)
            setMsg({type: 'success', text: 'Update installed. Click "Restart Server" to run the new version.'})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setApplying(false)
        }
    }

    async function handleCheckSource() {
        setCheckingSource(true)
        setMsg(null)
        try {
            const result = await adminApi.checkSourceUpdates()
            setSourceStatus(result)
            setMsg({
                type: 'success',
                text: result.updates_available
                    ? `New commits available (remote: ${result.remote_commit})`
                    : 'Already up to date with the remote branch.',
            })
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setCheckingSource(false)
        }
    }

    async function handleApplySource() {
        if (!window.confirm(
            'This will run git pull + go build on the server and replace the running binary.\n\n' +
            'You will need to restart the service afterwards.\n\n' +
            'Proceed?'
        )) return
        setMsg(null)
        setBuild({inProgress: true, stage: 'starting', progress: 0, done: false, success: false})
        try {
            const result = await adminApi.applySourceUpdate()
            setBuild({
                inProgress: result.in_progress,
                stage: result.stage,
                progress: result.progress,
                error: result.error,
                done: !result.in_progress,
                success: !result.in_progress && !result.error,
            })
            setSourceStatus(null)
        } catch (err) {
            setBuild(null)
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleRestart() {
        if (!window.confirm('Restart the server now?')) return
        try {
            await adminApi.restartServer()
            setMsg({type: 'success', text: 'Restart initiated. The page will reload shortly…'})
            setTimeout(() => window.location.reload(), 5000)
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    const buildRunning = build?.inProgress === true

    return (
        <div>
            {msg && (
                <div className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}>
                    {msg.text}
                </div>
            )}

            {/* Update Settings */}
            <div className="admin-card" style={{maxWidth: 640, marginBottom: 20}}>
                <h2>Update Settings</h2>
                <p style={{fontSize: 13, color: 'var(--text-muted)', margin: '0 0 16px 0'}}>
                    Configure how updates are applied. For <strong>main</strong> branch releases,
                    you can choose between downloading a pre-built binary or building from source.
                    The <strong>development</strong> branch always builds from source.
                </p>

                <div style={{display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, marginBottom: 16}}>
                    <div>
                        <label style={{fontSize: 12, color: 'var(--text-muted)', display: 'block', marginBottom: 4}}>
                            Update Method
                        </label>
                        <select
                            aria-label="Update method"
                            value={activeMethod}
                            onChange={e => handleSaveConfig(e.target.value as 'source' | 'binary', activeBranch)}
                            disabled={savingConfig || activeBranch === 'development'}
                            style={{
                                width: '100%',
                                padding: '8px 12px',
                                borderRadius: 6,
                                border: '1px solid var(--border-color)',
                                background: 'var(--bg-color)',
                                color: 'var(--text-color)',
                                fontSize: 14,
                            }}
                        >
                            <option value="source">Source Build (git pull + go build)</option>
                            <option value="binary">Binary Download (GitHub Release)</option>
                        </select>
                        {activeBranch === 'development' && (
                            <p style={{fontSize: 11, color: 'var(--text-muted)', marginTop: 4}}>
                                Development branch always uses source builds.
                            </p>
                        )}
                    </div>
                    <div>
                        <label style={{fontSize: 12, color: 'var(--text-muted)', display: 'block', marginBottom: 4}}>
                            Branch
                        </label>
                        <select
                            aria-label="Branch"
                            value={activeBranch}
                            onChange={e => {
                                const newBranch = e.target.value
                                const method = newBranch === 'development' ? 'source' : activeMethod
                                handleSaveConfig(method, newBranch)
                            }}
                            disabled={savingConfig}
                            style={{
                                width: '100%',
                                padding: '8px 12px',
                                borderRadius: 6,
                                border: '1px solid var(--border-color)',
                                background: 'var(--bg-color)',
                                color: 'var(--text-color)',
                                fontSize: 14,
                            }}
                        >
                            <option value="main">main (stable releases)</option>
                            <option value="development">development (latest features)</option>
                        </select>
                    </div>
                </div>

                <div style={{
                    background: 'var(--hover-bg)',
                    border: '1px solid var(--border-color)',
                    borderRadius: 6,
                    padding: '10px 14px',
                    fontSize: 12,
                    color: 'var(--text-muted)',
                }}>
                    <strong style={{color: 'var(--text-color)'}}>Current config:</strong>{' '}
                    {activeMethod === 'binary' ? 'Binary download' : 'Source build'} from <code>{activeBranch}</code> branch
                </div>
            </div>

            {/* GitHub Releases update — shown when method is "binary" or always for release checks */}
            <div className="admin-card" style={{maxWidth: 640, marginBottom: 20}}>
                <h2>Software Updates <span style={{fontSize: 13, fontWeight: 400, color: 'var(--text-muted)'}}>— GitHub Releases</span>
                </h2>

                <div style={{display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, marginBottom: 20}}>
                    <div>
                        <div style={{fontSize: 12, color: 'var(--text-muted)', marginBottom: 4}}>Current Version</div>
                        <div style={{fontWeight: 600, fontSize: 18}}>{status?.current_version || '—'}</div>
                    </div>
                    {status?.checked_at && (
                        <div>
                            <div style={{fontSize: 12, color: 'var(--text-muted)', marginBottom: 4}}>Latest Version
                            </div>
                            <div style={{fontWeight: 600, fontSize: 18}}>
                                {status.latest_version || '—'}
                                {status.update_available && (
                                    <span style={{
                                        marginLeft: 8,
                                        fontSize: 12,
                                        background: '#22c55e',
                                        color: '#fff',
                                        borderRadius: 4,
                                        padding: '2px 6px'
                                    }}>New!</span>
                                )}
                            </div>
                        </div>
                    )}
                </div>

                {status?.checked_at && (
                    <p style={{fontSize: 12, color: 'var(--text-muted)', margin: '0 0 16px 0'}}>
                        Last checked: {new Date(status.checked_at).toLocaleString()}
                        {status.error && <span style={{color: '#ef4444', marginLeft: 8}}>— {status.error}</span>}
                    </p>
                )}

                {!status?.checked_at && (
                    <p style={{fontSize: 13, color: 'var(--text-muted)', marginBottom: 16}}>
                        No update check has been performed yet.
                    </p>
                )}

                {status?.update_available && status.release_notes && (
                    <div style={{
                        background: 'var(--hover-bg)',
                        border: '1px solid var(--border-color)',
                        borderRadius: 6,
                        padding: '10px 14px',
                        marginBottom: 16,
                        fontSize: 13,
                        maxHeight: 160,
                        overflowY: 'auto',
                        whiteSpace: 'pre-wrap',
                    }}>
                        <strong>Release Notes</strong>
                        <div style={{marginTop: 6, color: 'var(--text-muted)'}}>{status.release_notes}</div>
                    </div>
                )}

                <div style={{display: 'flex', gap: 10, flexWrap: 'wrap'}}>
                    <button className="admin-btn admin-btn-primary" onClick={handleCheck} disabled={checking}>
                        <i className="bi bi-arrow-repeat"/> {checking ? 'Checking…' : 'Check for Updates'}
                    </button>
                    {status?.update_available && activeMethod === 'binary' && !updateApplied && (
                        <button className="admin-btn admin-btn-success" onClick={handleApply} disabled={applying}>
                            <i className="bi bi-download"/>
                            {applying ? 'Downloading…' : `Download ${status.latest_version}`}
                        </button>
                    )}
                    {updateApplied && (
                        <button className="admin-btn admin-btn-warning" onClick={handleRestart}>
                            <i className="bi bi-arrow-clockwise"/> Restart Server
                        </button>
                    )}
                    {status?.release_url && (
                        <a href={status.release_url} target="_blank" rel="noopener noreferrer"
                           className="admin-btn">
                            <i className="bi bi-box-arrow-up-right"/> View Release
                        </a>
                    )}
                </div>

                {activeMethod === 'source' && status?.update_available && (
                    <p style={{fontSize: 12, color: 'var(--text-muted)', marginTop: 12}}>
                        Update method is set to <strong>Source Build</strong> — use the Source Update
                        section below to pull and build the new version.
                    </p>
                )}
            </div>

            {/* Source-based update (git pull + go build) */}
            <div className="admin-card" style={{maxWidth: 640}}>
                <h2>Source Update <span style={{fontSize: 13, fontWeight: 400, color: 'var(--text-muted)'}}>
                    — git pull + build ({activeBranch})
                </span></h2>
                <p style={{fontSize: 13, color: 'var(--text-muted)', margin: '0 0 16px 0'}}>
                    Pull the latest code from the <code>{activeBranch}</code> branch, rebuild the frontend and
                    server binary in-place. Requires build tools (git, npm, go) and the GitHub
                    token (UPDATER_GITHUB_TOKEN) to be configured on the server.
                </p>

                {sourceStatus && !build && (
                    <div style={{
                        background: sourceStatus.updates_available ? 'rgba(34,197,94,0.1)' : 'var(--hover-bg)',
                        border: `1px solid ${sourceStatus.updates_available ? '#22c55e' : 'var(--border-color)'}`,
                        borderRadius: 6,
                        padding: '8px 14px',
                        marginBottom: 16,
                        fontSize: 13,
                    }}>
                        {sourceStatus.updates_available
                            ? <>New commits available on <code>{activeBranch}</code> — remote
                                HEAD: <code>{sourceStatus.remote_commit}</code></>
                            : <>Repository is up to date with the <code>{activeBranch}</code> branch.</>}
                    </div>
                )}

                {/* Live build progress */}
                {build && (
                    <div style={{marginBottom: 16}}>
                        <div style={{
                            display: 'flex',
                            justifyContent: 'space-between',
                            fontSize: 13,
                            marginBottom: 6,
                        }}>
                            <span style={{
                                color: build.error ? '#ef4444' : build.done ? '#22c55e' : 'var(--text-color)',
                                fontWeight: 500,
                            }}>
                                {build.error
                                    ? `Failed: ${build.stage}`
                                    : build.done
                                        ? `Done: ${build.stage}`
                                        : build.stage || 'starting…'}
                            </span>
                            <span style={{color: 'var(--text-muted)'}}>{Math.round(build.progress)}%</span>
                        </div>
                        <div style={{
                            height: 8,
                            background: 'var(--hover-bg)',
                            borderRadius: 4,
                            overflow: 'hidden',
                        }}>
                            <div style={{
                                height: '100%',
                                width: `${build.progress}%`,
                                background: build.error ? '#ef4444' : build.done ? '#22c55e' : '#3b82f6',
                                borderRadius: 4,
                                transition: 'width 0.4s ease',
                            }}/>
                        </div>
                    </div>
                )}

                <div style={{display: 'flex', gap: 10, flexWrap: 'wrap'}}>
                    <button className="admin-btn admin-btn-primary" onClick={handleCheckSource}
                            disabled={checkingSource || buildRunning}>
                        <i className="bi bi-git"/> {checkingSource ? 'Checking…' : `Check ${activeBranch}`}
                    </button>
                    <button
                        className="admin-btn admin-btn-success"
                        onClick={handleApplySource}
                        disabled={buildRunning}
                        title={`git pull origin ${activeBranch} + npm build + go build, then replace binary`}
                    >
                        <i className="bi bi-arrow-up-circle"/>
                        {buildRunning ? 'Building…' : 'Pull & Build'}
                    </button>
                    {build?.success && (
                        <button className="admin-btn admin-btn-warning" onClick={handleRestart}>
                            <i className="bi bi-arrow-clockwise"/> Restart Server
                        </button>
                    )}
                    {build?.done && (
                        <button className="admin-btn" onClick={() => setBuild(null)}>
                            Dismiss
                        </button>
                    )}
                </div>
                {!build && (
                    <p style={{fontSize: 12, color: 'var(--text-muted)', marginTop: 12}}>
                        After a successful build, use the{' '}
                        <strong>Restart Server</strong> button or run:{' '}
                        <code style={{background: 'var(--hover-bg)', padding: '2px 6px', borderRadius: 4}}>
                            systemctl restart mediaserver
                        </code>
                    </p>
                )}
            </div>
        </div>
    )
}

// ── Tab: Playlists (Feature 6) ────────────────────────────────────────────────

type PlaylistSortKey = 'name' | 'user_id' | 'items' | 'created_at' | 'is_public'

const PLAYLIST_SORT_COLUMNS: ReadonlyArray<{key: PlaylistSortKey; label: string}> = [
    {key: 'name', label: 'Name'},
    {key: 'user_id', label: 'Owner'},
    {key: 'items', label: 'Items'},
    {key: 'is_public', label: 'Public'},
    {key: 'created_at', label: 'Created'},
]

function PlaylistsTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [search, setSearch] = useState('')
    const [selected, setSelected] = useState<Set<string>>(new Set())
    const [bulkWorking, setBulkWorking] = useState(false)
    const [sortBy, setSortBy] = useState<PlaylistSortKey>('name')
    const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('asc')
    const [filterVisibility, setFilterVisibility] = useState<string>('')

    const {data: playlists = []} = useQuery<Playlist[]>({
        queryKey: ['admin-playlists'],
        queryFn: () => adminApi.listAllPlaylists(),
    })

    const {data: playlistStats} = useQuery<AdminPlaylistStats>({
        queryKey: ['admin-playlist-stats'],
        queryFn: () => adminApi.getPlaylistStats(),
    })

    async function handleDelete(id: string, name: string) {
        if (!window.confirm(`Delete playlist "${name}"?`)) return
        try {
            await adminApi.deletePlaylist(id)
            setMsg({type: 'success', text: `Playlist "${name}" deleted.`})
            await queryClient.invalidateQueries({queryKey: ['admin-playlists']})
            await queryClient.invalidateQueries({queryKey: ['admin-playlist-stats']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleBulkDelete() {
        if (selected.size === 0) return
        if (!window.confirm(`Delete ${selected.size} playlist(s)? This cannot be undone.`)) return
        setBulkWorking(true)
        setMsg(null)
        try {
            const result = await adminApi.bulkDeletePlaylists([...selected])
            setSelected(new Set())
            await queryClient.invalidateQueries({queryKey: ['admin-playlists']})
            await queryClient.invalidateQueries({queryKey: ['admin-playlist-stats']})
            setMsg({
                type: result.failed > 0 ? 'error' : 'success',
                text: `Deleted ${result.success} playlist(s)${result.failed > 0 ? `, ${result.failed} failed` : ''}.`
            })
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setBulkWorking(false)
        }
    }

    const filtered = playlists.filter(p => {
        if (search && !p.name.toLowerCase().includes(search.toLowerCase()) && !p.user_id.toLowerCase().includes(search.toLowerCase())) return false
        if (filterVisibility === 'public' && !p.is_public) return false
        if (filterVisibility === 'private' && p.is_public) return false
        return true
    }).sort((a, b) => {
        let cmp = 0
        switch (sortBy) {
            case 'name': cmp = a.name.localeCompare(b.name); break
            case 'user_id': cmp = a.user_id.localeCompare(b.user_id); break
            case 'items': cmp = (a.items?.length ?? 0) - (b.items?.length ?? 0); break
            case 'is_public': cmp = (a.is_public === b.is_public ? 0 : a.is_public ? -1 : 1); break
            case 'created_at': cmp = a.created_at.localeCompare(b.created_at); break
        }
        return sortOrder === 'desc' ? -cmp : cmp
    })

    const allSelected = filtered.length > 0 && filtered.every(p => selected.has(p.id))

    function handleSort(column: PlaylistSortKey) {
        if (sortBy === column) setSortOrder(prev => prev === 'asc' ? 'desc' : 'asc')
        else { setSortBy(column); setSortOrder('asc') }
    }

    function sortIndicator(column: PlaylistSortKey) {
        if (sortBy !== column) return <span style={{opacity: 0.3, marginLeft: 4}}>&#x21C5;</span>
        return <span style={{marginLeft: 4}}>{sortOrder === 'asc' ? '\u25B2' : '\u25BC'}</span>
    }

    function toggleSelectAll() {
        if (allSelected) {
            setSelected(prev => {
                const next = new Set(prev);
                filtered.forEach(p => next.delete(p.id));
                return next
            })
        } else {
            setSelected(prev => {
                const next = new Set(prev);
                filtered.forEach(p => next.add(p.id));
                return next
            })
        }
    }

    const selectStyle: React.CSSProperties = {
        padding: '6px 10px', border: '1px solid var(--border-color)', borderRadius: 6,
        background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13,
    }
    const thSortStyle: React.CSSProperties = {cursor: 'pointer', userSelect: 'none', whiteSpace: 'nowrap'}

    return (
        <div>
            {msg && <div
                className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}>{msg.text}</div>}
            {playlistStats && (
                <div className="admin-stats-grid" style={{marginBottom: 16}}>
                    <div className="admin-stat-card">
                        <span className="admin-stat-value">{playlistStats.total_playlists.toLocaleString()}</span>
                        <span className="admin-stat-label">Total Playlists</span>
                    </div>
                    <div className="admin-stat-card">
                        <span className="admin-stat-value">{playlistStats.total_items.toLocaleString()}</span>
                        <span className="admin-stat-label">Total Items</span>
                    </div>
                    <div className="admin-stat-card">
                        <span className="admin-stat-value">{playlistStats.public_playlists.toLocaleString()}</span>
                        <span className="admin-stat-label">Public Playlists</span>
                    </div>
                </div>
            )}
            <div className="admin-card">
                <div style={{display: 'flex', gap: 8, marginBottom: 12, flexWrap: 'wrap', alignItems: 'center'}}>
                    <input
                        type="search"
                        placeholder="Search playlists..."
                        value={search}
                        onChange={e => setSearch(e.target.value)}
                        style={{...selectStyle, flex: 1, minWidth: 160}}
                    />
                    <select value={filterVisibility} onChange={e => setFilterVisibility(e.target.value)} style={selectStyle}>
                        <option value="">All Visibility</option>
                        <option value="public">Public Only</option>
                        <option value="private">Private Only</option>
                    </select>
                    {(search || filterVisibility) && (
                        <button className="admin-btn" style={{fontSize: 12, padding: '4px 10px'}}
                                onClick={() => { setSearch(''); setFilterVisibility('') }}>
                            <i className="bi bi-x-circle"/> Clear
                        </button>
                    )}
                    <span style={{fontSize: 12, color: 'var(--text-muted)'}}>
                        {filtered.length} of {playlists.length} playlist{playlists.length !== 1 ? 's' : ''}
                    </span>
                </div>
                {selected.size > 0 && (
                    <div style={{
                        marginBottom: 10, padding: '8px 12px', background: 'var(--hover-bg)',
                        border: '1px solid var(--border-color)', borderRadius: 6,
                        display: 'flex', gap: 8, alignItems: 'center',
                    }}>
                        <span style={{
                            fontSize: 13,
                            fontWeight: 600,
                            color: 'var(--accent-color)'
                        }}>{selected.size} selected</span>
                        <button className="admin-btn admin-btn-danger" onClick={handleBulkDelete} disabled={bulkWorking}
                                style={{fontSize: 12, padding: '4px 10px'}}>
                            <i className="bi bi-trash-fill"/> Delete Selected
                        </button>
                        <button className="admin-btn" onClick={() => setSelected(new Set())}
                                style={{fontSize: 12, padding: '4px 10px'}}>Clear
                        </button>
                    </div>
                )}
                <div className="admin-table-wrapper">
                    <table className="admin-table">
                        <thead>
                        <tr>
                            <th style={{width: 32}}>
                                <input type="checkbox" checked={allSelected} onChange={toggleSelectAll}/>
                            </th>
                            {PLAYLIST_SORT_COLUMNS.map(col => (
                                <th key={col.key} style={thSortStyle} onClick={() => handleSort(col.key)}>
                                    {col.label}{sortIndicator(col.key)}
                                </th>
                            ))}
                            <th>Actions</th>
                        </tr>
                        </thead>
                        <tbody>
                        {filtered.length === 0 ? (
                            <tr>
                                <td colSpan={PLAYLIST_SORT_COLUMNS.length + 2} style={{textAlign: 'center', color: 'var(--text-muted)'}}>No playlists
                                    found
                                </td>
                            </tr>
                        ) : filtered.map(pl => (
                            <tr key={pl.id}
                                style={selected.has(pl.id) ? {background: 'color-mix(in srgb, var(--accent-color) 8%, transparent)'} : undefined}>
                                <td>
                                    <input type="checkbox" checked={selected.has(pl.id)}
                                           onChange={() => setSelected(prev => {
                                               const next = new Set(prev)
                                               if (next.has(pl.id)) next.delete(pl.id)
                                               else next.add(pl.id)
                                               return next
                                           })}/>
                                </td>
                                <td>{pl.name}</td>
                                <td style={{fontSize: 12, color: 'var(--text-muted)'}}>{pl.user_id}</td>
                                <td>{pl.items?.length ?? 0}</td>
                                <td>{pl.is_public ? 'Yes' : 'No'}</td>
                                <td style={{
                                    fontSize: 12,
                                    color: 'var(--text-muted)'
                                }}>{new Date(pl.created_at).toLocaleDateString()}</td>
                                <td>
                                    <button className="admin-btn admin-btn-danger" style={{padding: '3px 8px'}}
                                            onClick={() => handleDelete(pl.id, pl.name)}>
                                        <i className="bi bi-trash-fill"/>
                                    </button>
                                </td>
                            </tr>
                        ))}
                        </tbody>
                    </table>
                </div>
            </div>
        </div>
    )
}

// ── Tab: Security (Feature 10) ────────────────────────────────────────────────

type IPSortKey = 'ip' | 'comment' | 'added_at'
type BanSortKey = 'ip' | 'reason' | 'banned_at' | 'expires_at'

function SecurityTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

    // Whitelist state
    const [wlIp, setWlIp] = useState('')
    const [wlComment, setWlComment] = useState('')
    // Blacklist state
    const [blIp, setBlIp] = useState('')
    const [blComment, setBlComment] = useState('')
    // Ban state
    const [banIp, setBanIp] = useState('')
    const [banDuration, setBanDuration] = useState(60)

    // Sort state for IP tables
    const [wlSortBy, setWlSortBy] = useState<IPSortKey>('ip')
    const [wlSortOrder, setWlSortOrder] = useState<'asc' | 'desc'>('asc')
    const [blSortBy, setBlSortBy] = useState<IPSortKey>('ip')
    const [blSortOrder, setBlSortOrder] = useState<'asc' | 'desc'>('asc')
    const [banSortBy, setBanSortBy] = useState<BanSortKey>('banned_at')
    const [banSortOrder, setBanSortOrder] = useState<'asc' | 'desc'>('desc')
    const [ipSearch, setIpSearch] = useState('')

    const {data: secStats} = useQuery<SecurityStats>({
        queryKey: ['admin-security-stats'],
        queryFn: () => adminApi.getSecurityStats(),
        refetchInterval: 30000,
    })

    const {data: whitelist = []} = useQuery<IPEntry[]>({
        queryKey: ['admin-security-whitelist'],
        queryFn: () => adminApi.getWhitelist(),
    })

    const {data: blacklist = []} = useQuery<IPEntry[]>({
        queryKey: ['admin-security-blacklist'],
        queryFn: () => adminApi.getBlacklist(),
    })

    const {data: bannedIPs = []} = useQuery<BannedIP[]>({
        queryKey: ['admin-security-banned'],
        queryFn: () => adminApi.getBannedIPs(),
        refetchInterval: 30000,
    })

    async function handleAddWhitelist(e: React.FormEvent) {
        e.preventDefault()
        if (!wlIp.trim()) return
        try {
            await adminApi.addToWhitelist(wlIp.trim(), wlComment.trim() || undefined)
            setMsg({type: 'success', text: `${wlIp} added to whitelist`})
            setWlIp('');
            setWlComment('')
            await queryClient.invalidateQueries({queryKey: ['admin-security-whitelist']})
            await queryClient.invalidateQueries({queryKey: ['admin-security-stats']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleAddBlacklist(e: React.FormEvent) {
        e.preventDefault()
        if (!blIp.trim()) return
        try {
            await adminApi.addToBlacklist(blIp.trim(), blComment.trim() || undefined)
            setMsg({type: 'success', text: `${blIp} added to blacklist`})
            setBlIp('');
            setBlComment('')
            await queryClient.invalidateQueries({queryKey: ['admin-security-blacklist']})
            await queryClient.invalidateQueries({queryKey: ['admin-security-stats']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleBan(e: React.FormEvent) {
        e.preventDefault()
        if (!banIp.trim()) return
        try {
            await adminApi.banIP(banIp.trim(), banDuration)
            setMsg({type: 'success', text: `${banIp} banned for ${banDuration} minutes`})
            setBanIp('')
            await queryClient.invalidateQueries({queryKey: ['admin-security-banned']})
            await queryClient.invalidateQueries({queryKey: ['admin-security-stats']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    return (
        <div>
            {msg && <div
                className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}>{msg.text}</div>}
            {secStats && (
                <div className="admin-stats-grid" style={{marginBottom: 16}}>
                    <div className="admin-stat-card"><span
                        className="admin-stat-value">{secStats.banned_ips}</span><span className="admin-stat-label">Banned IPs</span>
                    </div>
                    <div className="admin-stat-card"><span
                        className="admin-stat-value">{secStats.whitelisted_ips}</span><span
                        className="admin-stat-label">Whitelisted</span></div>
                    <div className="admin-stat-card"><span
                        className="admin-stat-value">{secStats.blacklisted_ips}</span><span
                        className="admin-stat-label">Blacklisted</span></div>
                    <div className="admin-stat-card"><span
                        className="admin-stat-value">{secStats.active_rate_limits}</span><span
                        className="admin-stat-label">Active Rate Limits</span></div>
                    <div className="admin-stat-card"><span
                        className="admin-stat-value">{secStats.total_blocks_today}</span><span
                        className="admin-stat-label">Blocks Today</span></div>
                </div>
            )}

            {/* IP search across all lists */}
            <div style={{marginBottom: 16}}>
                <input type="text" placeholder="Search IPs across all lists..." value={ipSearch}
                       onChange={e => setIpSearch(e.target.value)}
                       style={{width: '100%', maxWidth: 300, padding: '6px 10px', border: '1px solid var(--border-color)',
                               borderRadius: 6, background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13}} />
            </div>

            {/* Whitelist */}
            <div className="admin-card">
                <h3>Whitelist <span
                    style={{fontSize: 13, fontWeight: 400, color: 'var(--text-muted)'}}>({whitelist.length} IPs)</span>
                </h3>
                <form onSubmit={handleAddWhitelist} style={{display: 'flex', gap: 8, marginBottom: 12}}>
                    <input type="text" value={wlIp} onChange={e => setWlIp(e.target.value)} placeholder="IP address"
                           style={{flex: 1, padding: '6px 10px', border: '1px solid var(--border-color)', borderRadius: 6,
                               background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13}}/>
                    <input type="text" value={wlComment} onChange={e => setWlComment(e.target.value)}
                           placeholder="Comment (optional)" style={{flex: 1, padding: '6px 10px', border: '1px solid var(--border-color)',
                        borderRadius: 6, background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13}}/>
                    <button type="submit" className="admin-btn admin-btn-primary"><i className="bi bi-plus-lg"/> Add
                    </button>
                </form>
                {whitelist.length > 0 && (() => {
                    const sortedWl = whitelist
                        .filter(e => !ipSearch || e.ip.includes(ipSearch) || (e.comment || '').toLowerCase().includes(ipSearch.toLowerCase()))
                        .sort((a, b) => {
                            let cmp = 0
                            switch (wlSortBy) {
                                case 'ip': cmp = a.ip.localeCompare(b.ip); break
                                case 'comment': cmp = (a.comment || '').localeCompare(b.comment || ''); break
                                case 'added_at': cmp = a.added_at.localeCompare(b.added_at); break
                            }
                            return wlSortOrder === 'desc' ? -cmp : cmp
                        })
                    const thS: React.CSSProperties = {cursor: 'pointer', userSelect: 'none', whiteSpace: 'nowrap'}
                    function wlInd(col: IPSortKey) {
                        if (wlSortBy !== col) return <span style={{opacity: 0.3, marginLeft: 4}}>&#x21C5;</span>
                        return <span style={{marginLeft: 4}}>{wlSortOrder === 'asc' ? '\u25B2' : '\u25BC'}</span>
                    }
                    function handleWlSort(col: IPSortKey) {
                        if (wlSortBy === col) setWlSortOrder(p => p === 'asc' ? 'desc' : 'asc')
                        else { setWlSortBy(col); setWlSortOrder('asc') }
                    }
                    return (
                    <div className="admin-table-wrapper">
                        <table className="admin-table">
                            <thead>
                            <tr>
                                <th style={thS} onClick={() => handleWlSort('ip')}>IP{wlInd('ip')}</th>
                                <th style={thS} onClick={() => handleWlSort('comment')}>Comment{wlInd('comment')}</th>
                                <th style={thS} onClick={() => handleWlSort('added_at')}>Added{wlInd('added_at')}</th>
                                <th>Actions</th>
                            </tr>
                            </thead>
                            <tbody>
                            {sortedWl.map(entry => (
                                <tr key={entry.ip}>
                                    <td><code>{entry.ip}</code></td>
                                    <td style={{color: 'var(--text-muted)', fontSize: 12}}>{entry.comment || '—'}</td>
                                    <td style={{fontSize: 12, color: 'var(--text-muted)'}}>{new Date(entry.added_at).toLocaleDateString()}</td>
                                    <td>
                                        <button className="admin-btn admin-btn-danger" style={{padding: '3px 8px'}}
                                                onClick={() => adminApi.removeFromWhitelist(entry.ip).then(() => void queryClient.invalidateQueries({queryKey: ['admin-security-whitelist']})).catch(err => setMsg({
                                                    type: 'error', text: errMsg(err)
                                                }))}>
                                            <i className="bi bi-trash-fill"/>
                                        </button>
                                    </td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>)
                })()}
            </div>

            {/* Blacklist */}
            <div className="admin-card">
                <h3>Blacklist <span
                    style={{fontSize: 13, fontWeight: 400, color: 'var(--text-muted)'}}>({blacklist.length} IPs)</span>
                </h3>
                <form onSubmit={handleAddBlacklist} style={{display: 'flex', gap: 8, marginBottom: 12}}>
                    <input type="text" value={blIp} onChange={e => setBlIp(e.target.value)} placeholder="IP address"
                           style={{flex: 1, padding: '6px 10px', border: '1px solid var(--border-color)', borderRadius: 6,
                               background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13}}/>
                    <input type="text" value={blComment} onChange={e => setBlComment(e.target.value)}
                           placeholder="Comment (optional)" style={{flex: 1, padding: '6px 10px', border: '1px solid var(--border-color)',
                        borderRadius: 6, background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13}}/>
                    <button type="submit" className="admin-btn admin-btn-danger"><i className="bi bi-plus-lg"/> Block
                    </button>
                </form>
                {blacklist.length > 0 && (() => {
                    const sortedBl = blacklist
                        .filter(e => !ipSearch || e.ip.includes(ipSearch) || (e.comment || '').toLowerCase().includes(ipSearch.toLowerCase()))
                        .sort((a, b) => {
                            let cmp = 0
                            switch (blSortBy) {
                                case 'ip': cmp = a.ip.localeCompare(b.ip); break
                                case 'comment': cmp = (a.comment || '').localeCompare(b.comment || ''); break
                                case 'added_at': cmp = a.added_at.localeCompare(b.added_at); break
                            }
                            return blSortOrder === 'desc' ? -cmp : cmp
                        })
                    const thS: React.CSSProperties = {cursor: 'pointer', userSelect: 'none', whiteSpace: 'nowrap'}
                    function blInd(col: IPSortKey) {
                        if (blSortBy !== col) return <span style={{opacity: 0.3, marginLeft: 4}}>&#x21C5;</span>
                        return <span style={{marginLeft: 4}}>{blSortOrder === 'asc' ? '\u25B2' : '\u25BC'}</span>
                    }
                    function handleBlSort(col: IPSortKey) {
                        if (blSortBy === col) setBlSortOrder(p => p === 'asc' ? 'desc' : 'asc')
                        else { setBlSortBy(col); setBlSortOrder('asc') }
                    }
                    return (
                    <div className="admin-table-wrapper">
                        <table className="admin-table">
                            <thead>
                            <tr>
                                <th style={thS} onClick={() => handleBlSort('ip')}>IP{blInd('ip')}</th>
                                <th style={thS} onClick={() => handleBlSort('comment')}>Comment{blInd('comment')}</th>
                                <th style={thS} onClick={() => handleBlSort('added_at')}>Added{blInd('added_at')}</th>
                                <th>Actions</th>
                            </tr>
                            </thead>
                            <tbody>
                            {sortedBl.map(entry => (
                                <tr key={entry.ip}>
                                    <td><code>{entry.ip}</code></td>
                                    <td style={{color: 'var(--text-muted)', fontSize: 12}}>{entry.comment || '—'}</td>
                                    <td style={{fontSize: 12, color: 'var(--text-muted)'}}>{new Date(entry.added_at).toLocaleDateString()}</td>
                                    <td>
                                        <button className="admin-btn admin-btn-success" style={{padding: '3px 8px'}}
                                                onClick={() => adminApi.removeFromBlacklist(entry.ip).then(() => void queryClient.invalidateQueries({queryKey: ['admin-security-blacklist']})).catch(err => setMsg({
                                                    type: 'error', text: errMsg(err)
                                                }))}>
                                            <i className="bi bi-check-lg"/> Unblock
                                        </button>
                                    </td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>)
                })()}
            </div>

            {/* Banned IPs */}
            <div className="admin-card">
                <h3>Banned IPs <span style={{fontSize: 13, fontWeight: 400, color: 'var(--text-muted)'}}>({bannedIPs.length} active)</span></h3>
                <form onSubmit={handleBan} style={{display: 'flex', gap: 8, marginBottom: 12, flexWrap: 'wrap'}}>
                    <input type="text" value={banIp} onChange={e => setBanIp(e.target.value)} placeholder="IP address"
                           style={{flex: 1, minWidth: 140, padding: '6px 10px', border: '1px solid var(--border-color)',
                               borderRadius: 6, background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13}}/>
                    <select value={banDuration} onChange={e => setBanDuration(Number(e.target.value))} style={{
                        padding: '6px 10px', border: '1px solid var(--border-color)', borderRadius: 6,
                        background: 'var(--input-bg)', color: 'var(--text-color)', fontSize: 13}}>
                        <option value={15}>15 min</option>
                        <option value={60}>1 hour</option>
                        <option value={1440}>24 hours</option>
                        <option value={10080}>7 days</option>
                    </select>
                    <button type="submit" className="admin-btn admin-btn-danger"><i className="bi bi-ban"/> Ban</button>
                </form>
                {bannedIPs.length > 0 && (() => {
                    const sortedBans = bannedIPs
                        .filter(b => !ipSearch || b.ip.includes(ipSearch) || (b.reason || '').toLowerCase().includes(ipSearch.toLowerCase()))
                        .sort((a, b) => {
                            let cmp = 0
                            switch (banSortBy) {
                                case 'ip': cmp = a.ip.localeCompare(b.ip); break
                                case 'reason': cmp = (a.reason || '').localeCompare(b.reason || ''); break
                                case 'banned_at': cmp = a.banned_at.localeCompare(b.banned_at); break
                                case 'expires_at': cmp = (a.expires_at || 'z').localeCompare(b.expires_at || 'z'); break
                            }
                            return banSortOrder === 'desc' ? -cmp : cmp
                        })
                    const thS: React.CSSProperties = {cursor: 'pointer', userSelect: 'none', whiteSpace: 'nowrap'}
                    function banInd(col: BanSortKey) {
                        if (banSortBy !== col) return <span style={{opacity: 0.3, marginLeft: 4}}>&#x21C5;</span>
                        return <span style={{marginLeft: 4}}>{banSortOrder === 'asc' ? '\u25B2' : '\u25BC'}</span>
                    }
                    function handleBanSort(col: BanSortKey) {
                        if (banSortBy === col) setBanSortOrder(p => p === 'asc' ? 'desc' : 'asc')
                        else { setBanSortBy(col); setBanSortOrder('asc') }
                    }
                    return (
                    <div className="admin-table-wrapper">
                        <table className="admin-table">
                            <thead>
                            <tr>
                                <th style={thS} onClick={() => handleBanSort('ip')}>IP{banInd('ip')}</th>
                                <th style={thS} onClick={() => handleBanSort('reason')}>Reason{banInd('reason')}</th>
                                <th style={thS} onClick={() => handleBanSort('banned_at')}>Banned At{banInd('banned_at')}</th>
                                <th style={thS} onClick={() => handleBanSort('expires_at')}>Expires{banInd('expires_at')}</th>
                                <th>Actions</th>
                            </tr>
                            </thead>
                            <tbody>
                            {sortedBans.map(ban => (
                                <tr key={ban.ip}>
                                    <td><code>{ban.ip}</code></td>
                                    <td style={{fontSize: 12, color: 'var(--text-muted)'}}>{ban.reason || '—'}</td>
                                    <td style={{fontSize: 12, color: 'var(--text-muted)'}}>{new Date(ban.banned_at).toLocaleString()}</td>
                                    <td style={{fontSize: 12, color: 'var(--text-muted)'}}>{ban.expires_at ? new Date(ban.expires_at).toLocaleString() : 'Permanent'}</td>
                                    <td>
                                        <button className="admin-btn admin-btn-success" style={{padding: '3px 8px'}}
                                                onClick={() => adminApi.unbanIP(ban.ip).then(() => void queryClient.invalidateQueries({queryKey: ['admin-security-banned']})).catch(err => setMsg({
                                                    type: 'error', text: errMsg(err)
                                                }))}>
                                            <i className="bi bi-check-lg"/> Unban
                                        </button>
                                    </td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>)
                })()}
            </div>
        </div>
    )
}

// ── Tab: Categorizer (Feature 11) ─────────────────────────────────────────────

function CategorizerTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [catPath, setCatPath] = useState('')
    const [categorizing, setCategorizing] = useState(false)
    const [catResult, setCatResult] = useState<CategorizedItem | null>(null)
    const [browseCat, setBrowseCat] = useState('')
    const [browseResults, setBrowseResults] = useState<CategorizedItem[] | null>(null)
    const [setPath, setSetPath] = useState('')
    const [setCategory, setSetCategoryValue] = useState('')
    const [cleaning, setCleaning] = useState(false)

    const {data: catStats} = useQuery<CategoryStats>({
        queryKey: ['admin-category-stats'],
        queryFn: () => adminApi.getCategoryStats(),
    })

    async function handleCategorize(e: React.FormEvent) {
        e.preventDefault()
        if (!catPath.trim()) return
        setCategorizing(true)
        setCatResult(null)
        setMsg(null)
        try {
            const result = await adminApi.categorizeFile(catPath.trim())
            setCatResult(result)
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setCategorizing(false)
        }
    }

    async function handleBrowseCategory() {
        if (!browseCat) return
        try {
            const results = await adminApi.getByCategory(browseCat)
            setBrowseResults(results)
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleSetCategory(e: React.FormEvent) {
        e.preventDefault()
        if (!setPath.trim() || !setCategory.trim()) return
        try {
            await adminApi.setMediaCategory(setPath.trim(), setCategory.trim())
            setMsg({type: 'success', text: `Category set to "${setCategory}" for "${setPath.split(/[\\/]/).pop()}"`})
            setSetPath('');
            setSetCategoryValue('')
            await queryClient.invalidateQueries({queryKey: ['admin-category-stats']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleClean() {
        if (!window.confirm('Remove stale category entries?')) return
        setCleaning(true)
        try {
            const res = await adminApi.cleanStaleCategories()
            setMsg({type: 'success', text: `Cleaned ${res.removed} stale entries.`})
            await queryClient.invalidateQueries({queryKey: ['admin-category-stats']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setCleaning(false)
        }
    }

    const categories = catStats ? Object.keys(catStats.by_category).sort() : []

    return (
        <div>
            {msg && <div
                className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}>{msg.text}</div>}

            {/* Stats */}
            {catStats && (
                <div className="admin-card">
                    <div style={{
                        display: 'flex',
                        justifyContent: 'space-between',
                        alignItems: 'center',
                        marginBottom: 12
                    }}>
                        <h2 style={{margin: 0}}>Category Stats</h2>
                        <button className="admin-btn admin-btn-warning" onClick={handleClean} disabled={cleaning}>
                            <i className="bi bi-trash"/> {cleaning ? 'Cleaning...' : 'Clean Stale'}
                        </button>
                    </div>
                    <div className="admin-stats-grid" style={{marginBottom: 12}}>
                        <div className="admin-stat-card"><span
                            className="admin-stat-value">{catStats.total_items.toLocaleString()}</span><span
                            className="admin-stat-label">Categorized</span></div>
                        <div className="admin-stat-card"><span
                            className="admin-stat-value">{catStats.manual_overrides.toLocaleString()}</span><span
                            className="admin-stat-label">Manual Overrides</span></div>
                        <div className="admin-stat-card"><span
                            className="admin-stat-value">{Object.keys(catStats.by_category).length}</span><span
                            className="admin-stat-label">Categories</span></div>
                    </div>
                    {Object.keys(catStats.by_category).length > 0 && (
                        <div className="admin-table-wrapper">
                            <table className="admin-table">
                                <thead>
                                <tr>
                                    <th>Category</th>
                                    <th>Count</th>
                                </tr>
                                </thead>
                                <tbody>
                                {Object.entries(catStats.by_category).sort((a, b) => b[1] - a[1]).map(([cat, count]) => (
                                    <tr key={cat}>
                                        <td>{cat}</td>
                                        <td>{count.toLocaleString()}</td>
                                    </tr>
                                ))}
                                </tbody>
                            </table>
                        </div>
                    )}
                </div>
            )}

            {/* Categorize file */}
            <div className="admin-card">
                <h3>Categorize File</h3>
                <form onSubmit={handleCategorize} style={{display: 'flex', gap: 8, marginBottom: 12}}>
                    <input type="text" value={catPath} onChange={e => setCatPath(e.target.value)}
                           placeholder="Media file path..." style={{
                        flex: 1,
                        padding: '6px 10px',
                        border: '1px solid var(--border-color)',
                        borderRadius: 6,
                        background: 'var(--input-bg)',
                        color: 'var(--text-color)',
                        fontSize: 13
                    }}/>
                    <button type="submit" className="admin-btn admin-btn-primary"
                            disabled={categorizing || !catPath.trim()}>
                        <i className="bi bi-tag"/> {categorizing ? 'Analyzing...' : 'Categorize'}
                    </button>
                </form>
                {catResult && (
                    <div style={{
                        padding: '10px 12px',
                        background: 'var(--card-bg)',
                        border: '1px solid var(--border-color)',
                        borderRadius: 6,
                        fontSize: 13
                    }}>
                        <p><strong>Category:</strong> {catResult.category}</p>
                        <p><strong>Confidence:</strong> {(catResult.confidence * 100).toFixed(0)}%</p>
                        <p><strong>Manual Override:</strong> {catResult.manual_override ? 'Yes' : 'No'}</p>
                    </div>
                )}
            </div>

            {/* Set category manually */}
            <div className="admin-card">
                <h3>Set Category Manually</h3>
                <form onSubmit={handleSetCategory} style={{display: 'flex', gap: 8}}>
                    <input type="text" value={setPath} onChange={e => setSetPath(e.target.value)}
                           placeholder="Media file path..." style={{
                        flex: 2,
                        padding: '6px 10px',
                        border: '1px solid var(--border-color)',
                        borderRadius: 6,
                        background: 'var(--input-bg)',
                        color: 'var(--text-color)',
                        fontSize: 13
                    }}/>
                    <input type="text" value={setCategory} onChange={e => setSetCategoryValue(e.target.value)}
                           placeholder="Category name..." style={{
                        flex: 1,
                        padding: '6px 10px',
                        border: '1px solid var(--border-color)',
                        borderRadius: 6,
                        background: 'var(--input-bg)',
                        color: 'var(--text-color)',
                        fontSize: 13
                    }}/>
                    <button type="submit" className="admin-btn admin-btn-primary"
                            disabled={!setPath.trim() || !setCategory.trim()}>
                        <i className="bi bi-check-lg"/> Set
                    </button>
                </form>
            </div>

            {/* Browse by category */}
            <div className="admin-card">
                <h3>Browse by Category</h3>
                <div style={{display: 'flex', gap: 8, marginBottom: 12}}>
                    <select value={browseCat} onChange={e => setBrowseCat(e.target.value)} style={{
                        flex: 1,
                        padding: '6px 10px',
                        border: '1px solid var(--border-color)',
                        borderRadius: 6,
                        background: 'var(--input-bg)',
                        color: 'var(--text-color)',
                        fontSize: 13
                    }}>
                        <option value="">Select category...</option>
                        {categories.map(c => <option key={c} value={c}>{c}</option>)}
                    </select>
                    <button className="admin-btn admin-btn-primary" onClick={handleBrowseCategory}
                            disabled={!browseCat}>
                        <i className="bi bi-search"/> Browse
                    </button>
                </div>
                {browseResults && (
                    <div className="admin-table-wrapper">
                        <table className="admin-table">
                            <thead>
                            <tr>
                                <th>File</th>
                                <th>Category</th>
                                <th>Confidence</th>
                                <th>Manual</th>
                            </tr>
                            </thead>
                            <tbody>
                            {browseResults.length === 0 ? (
                                <tr>
                                    <td colSpan={4} style={{textAlign: 'center', color: 'var(--text-muted)'}}>No items
                                        in this category
                                    </td>
                                </tr>
                            ) : [...browseResults].sort((a, b) => a.name.localeCompare(b.name)).map(item => (
                                <tr key={item.id}>
                                    <td style={{
                                        maxWidth: 200,
                                        overflow: 'hidden',
                                        textOverflow: 'ellipsis',
                                        whiteSpace: 'nowrap'
                                    }} title={item.name}>{item.name}</td>
                                    <td>{item.category}</td>
                                    <td>{(item.confidence * 100).toFixed(0)}%</td>
                                    <td>{item.manual_override ? 'Yes' : 'No'}</td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                )}
            </div>
        </div>
    )
}

// ── Tab: Auto-Discovery (Feature 12) ──────────────────────────────────────────

function DiscoveryTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [scanDir, setScanDir] = useState('')
    const [scanning, setScanning] = useState(false)

    const {data: suggestions = []} = useQuery<DiscoverySuggestion[]>({
        queryKey: ['admin-discovery-suggestions'],
        queryFn: () => adminApi.getDiscoverySuggestions(),
    })

    async function handleScan(e: React.FormEvent) {
        e.preventDefault()
        if (!scanDir.trim()) return
        setScanning(true)
        setMsg(null)
        try {
            await adminApi.discoveryScan(scanDir.trim())
            setMsg({type: 'success', text: 'Discovery scan complete. Refresh suggestions below.'})
            await queryClient.invalidateQueries({queryKey: ['admin-discovery-suggestions']})
            setScanDir('')
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setScanning(false)
        }
    }

    async function handleApply(originalPath: string) {
        try {
            await adminApi.applyDiscoverySuggestion(originalPath)
            setMsg({type: 'success', text: 'Suggestion applied.'})
            await queryClient.invalidateQueries({queryKey: ['admin-discovery-suggestions']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleDismiss(originalPath: string) {
        try {
            await adminApi.dismissDiscoverySuggestion(originalPath)
            await queryClient.invalidateQueries({queryKey: ['admin-discovery-suggestions']})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    return (
        <div>
            {msg && <div
                className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}>{msg.text}</div>}

            <div className="admin-card">
                <h2>Scan for New Media</h2>
                <p style={{fontSize: 13, color: 'var(--text-muted)', marginBottom: 12}}>
                    Scan a directory for media files and get suggestions for organizing them.
                </p>
                <form onSubmit={handleScan} style={{display: 'flex', gap: 8}}>
                    <input
                        type="text"
                        value={scanDir}
                        onChange={e => setScanDir(e.target.value)}
                        placeholder="Directory path to scan..."
                        style={{
                            flex: 1,
                            padding: '6px 10px',
                            border: '1px solid var(--border-color)',
                            borderRadius: 6,
                            background: 'var(--input-bg)',
                            color: 'var(--text-color)',
                            fontSize: 13
                        }}
                    />
                    <button type="submit" className="admin-btn admin-btn-primary"
                            disabled={scanning || !scanDir.trim()}>
                        <i className="bi bi-search"/> {scanning ? 'Scanning...' : 'Scan Directory'}
                    </button>
                </form>
            </div>

            <div className="admin-card">
                <h2>Pending Suggestions <span
                    style={{fontSize: 13, fontWeight: 400, color: 'var(--text-muted)'}}>({suggestions.length})</span>
                </h2>
                {suggestions.length === 0 ? (
                    <div style={{textAlign: 'center', padding: '40px 0', color: 'var(--text-muted)'}}>
                        <i className="bi bi-check-circle" style={{fontSize: 32}}/>
                        <p style={{marginTop: 8}}>No pending suggestions. Run a directory scan to get started.</p>
                    </div>
                ) : (
                    <div className="admin-table-wrapper">
                        <table className="admin-table">
                            <thead>
                            <tr>
                                <th>Original File</th>
                                <th>Suggested Name</th>
                                <th>Category</th>
                                <th>Confidence</th>
                                <th>Actions</th>
                            </tr>
                            </thead>
                            <tbody>
                            {[...suggestions].sort((a, b) => b.confidence - a.confidence).map(s => (
                                <tr key={s.original_path}>
                                    <td style={{
                                        maxWidth: 180,
                                        overflow: 'hidden',
                                        textOverflow: 'ellipsis',
                                        whiteSpace: 'nowrap'
                                    }} title={s.original_path}>{s.original_path.split(/[\\/]/).pop()}</td>
                                    <td style={{fontWeight: 500}}>{s.suggested_name}</td>
                                    <td>{s.type}</td>
                                    <td>{(s.confidence * 100).toFixed(0)}%</td>
                                    <td>
                                        <div style={{display: 'flex', gap: 6}}>
                                            <button className="admin-btn admin-btn-success" style={{padding: '3px 8px'}}
                                                    onClick={() => handleApply(s.original_path)}>
                                                <i className="bi bi-check-lg"/> Apply
                                            </button>
                                            <button className="admin-btn" style={{padding: '3px 8px'}}
                                                    onClick={() => handleDismiss(s.original_path)}>
                                                <i className="bi bi-x-lg"/> Dismiss
                                            </button>
                                        </div>
                                    </td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                )}
            </div>
        </div>
    )
}

// ── Sub-tab navigation (used by consolidated tabs) ──────────────────────────

function SubTabs({items, active, onChange}: {
    items: { id: string; label: string }[]
    active: string
    onChange: (id: string) => void
}) {
    return (
        <div className="admin-subtab-nav">
            {items.map(item => (
                <button key={item.id}
                        className={`admin-subtab-btn ${active === item.id ? 'active' : ''}`}
                        onClick={() => onChange(item.id)}>
                    {item.label}
                </button>
            ))}
        </div>
    )
}

// ── Consolidated tabs ────────────────────────────────────────────────────────

function ContentTab() {
    const [sub, setSub] = useState('review')
    return (<>
        <SubTabs items={[
            {id: 'review', label: 'Review'},
            {id: 'categorizer', label: 'Categorizer'},
            {id: 'discovery', label: 'Discovery'},
        ]} active={sub} onChange={setSub}/>
        {sub === 'review' && <ContentReviewTab/>}
        {sub === 'categorizer' && <CategorizerTab/>}
        {sub === 'discovery' && <DiscoveryTab/>}
    </>)
}

function SourcesTab() {
    const [sub, setSub] = useState('remote')
    return (<>
        <SubTabs items={[
            {id: 'remote', label: 'Remote'},
            {id: 'slaves', label: 'Slaves'},
        ]} active={sub} onChange={setSub}/>
        {sub === 'remote' && <RemoteTab/>}
        {sub === 'slaves' && <ReceiverTab/>}
    </>)
}

function SystemTab() {
    const [sub, setSub] = useState('settings')
    return (<>
        <SubTabs items={[
            {id: 'settings', label: 'Settings'},
            {id: 'logs', label: 'Logs'},
            {id: 'database', label: 'Database'},
        ]} active={sub} onChange={setSub}/>
        {sub === 'settings' && <SettingsTab/>}
        {sub === 'logs' && <LogsTab/>}
        {sub === 'database' && <DatabaseTab/>}
    </>)
}

// ── Main AdminPage ────────────────────────────────────────────────────────────

type Tab =
    'dashboard'
    | 'users'
    | 'media'
    | 'streaming'
    | 'analytics'
    | 'content'
    | 'sources'
    | 'system'
    | 'playlists'
    | 'security'
    | 'updates'

const VALID_TABS: Tab[] = ['dashboard', 'users', 'media', 'streaming', 'analytics', 'content', 'sources', 'system', 'playlists', 'security', 'updates']

export function AdminPage() {
    const navigate = useNavigate()
    const location = useLocation()
    const logout = useAuthStore((s) => s.logout)
    const isAdmin = useAuthStore((s) => s.isAdmin)
    const isLoading = useAuthStore((s) => s.isLoading)
    const initialTab = (location.state as { tab?: string } | null)?.tab
    const [activeTab, setActiveTab] = useState<Tab>(
        VALID_TABS.includes(initialTab as Tab) ? (initialTab as Tab) : 'dashboard'
    )

    if (!isLoading && !isAdmin) {
        navigate('/login', {replace: true})
        return null
    }

    const tabs: Array<{ id: Tab; label: string; icon: string }> = [
        {id: 'dashboard', label: 'Dashboard', icon: 'bi-speedometer2'},
        {id: 'users', label: 'Users', icon: 'bi-people-fill'},
        {id: 'media', label: 'Media', icon: 'bi-folder-fill'},
        {id: 'streaming', label: 'Streaming', icon: 'bi-broadcast'},
        {id: 'analytics', label: 'Analytics', icon: 'bi-bar-chart-fill'},
        {id: 'content', label: 'Content', icon: 'bi-shield-fill'},
        {id: 'sources', label: 'Sources', icon: 'bi-cloud-arrow-down-fill'},
        {id: 'playlists', label: 'Playlists', icon: 'bi-collection-fill'},
        {id: 'security', label: 'Security', icon: 'bi-lock-fill'},
        {id: 'updates', label: 'Updates', icon: 'bi-arrow-up-circle-fill'},
        {id: 'system', label: 'System', icon: 'bi-gear-fill'},
    ]

    async function handleLogout() {
        await logout()
        navigate('/login', {replace: true})
    }

    return (
        <div className="admin-page">
            <div className="admin-header-bar">
                <h1><i className="bi bi-shield-fill"/> Admin Panel</h1>
                <div className="admin-header-actions">
                    <Link to="/" className="admin-nav-btn"><i className="bi bi-house-fill"/> Home</Link>
                    <button className="admin-nav-btn" onClick={handleLogout}><i
                        className="bi bi-box-arrow-right"/> Logout
                    </button>
                </div>
            </div>

            <div className="admin-tab-nav">
                {tabs.map(tab => (
                    <button
                        key={tab.id}
                        className={`admin-tab-btn ${activeTab === tab.id ? 'active' : ''}`}
                        onClick={() => setActiveTab(tab.id)}
                    >
                        <i className={`bi ${tab.icon}`}/> {tab.label}
                    </button>
                ))}
            </div>

            <div className="admin-content">
                <SectionErrorBoundary title="Admin panel section unavailable">
                    {activeTab === 'dashboard' && <DashboardTab/>}
                    {activeTab === 'users' && <UsersTab/>}
                    {activeTab === 'media' && <MediaTab/>}
                    {activeTab === 'streaming' && <StreamingTab/>}
                    {activeTab === 'analytics' && <AnalyticsTab/>}
                    {activeTab === 'content' && <ContentTab/>}
                    {activeTab === 'sources' && <SourcesTab/>}
                    {activeTab === 'system' && <SystemTab/>}
                    {activeTab === 'playlists' && <PlaylistsTab/>}
                    {activeTab === 'security' && <SecurityTab/>}
                    {activeTab === 'updates' && <UpdatesTab/>}
                </SectionErrorBoundary>
            </div>
        </div>
    )
}

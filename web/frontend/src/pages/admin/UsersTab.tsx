import {type FormEvent, useState} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import {adminApi} from '@/api/endpoints'
import type {User} from '@/api/types'
import {SecurityTab} from './SecurityTab'
import {errMsg} from './helpers'
import {SubTabs} from './helpers'

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

function UsersListTab() {
    const queryClient = useQueryClient()
    const [showCreate, setShowCreate] = useState(false)
    const [editUser, setEditUser] = useState<User | null>(null)
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [selected, setSelected] = useState<Set<string>>(new Set())
    const [bulkWorking, setBulkWorking] = useState(false)

    const [sortBy, setSortBy] = useState<UserSortKey>('username')
    const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('asc')
    const [filterRole, setFilterRole] = useState<string>('')
    const [filterStatus, setFilterStatus] = useState<string>('')
    const [userSearch, setUserSearch] = useState('')

    const {data: users = [], isLoading} = useQuery({
        queryKey: ['admin-users'],
        queryFn: () => adminApi.listUsers(),
    })

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

export function UsersTab() {
    const [sub, setSub] = useState('users')
    return (
        <>
            <SubTabs items={[{id: 'users', label: 'Users'}, {id: 'security', label: 'Security'}]} active={sub} onChange={setSub}/>
            {sub === 'users' && <UsersListTab/>}
            {sub === 'security' && <SecurityTab/>}
        </>
    )
}

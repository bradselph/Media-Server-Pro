import {useState} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import {adminApi} from '@/api/endpoints'
import type {AdminPlaylistStats, Playlist} from '@/api/types'
import {errMsg} from './adminUtils'

// ── Tab: Playlists (Feature 6) ────────────────────────────────────────────────

type PlaylistSortKey = 'name' | 'user_id' | 'items' | 'created_at' | 'is_public'

const PLAYLIST_SORT_COLUMNS: ReadonlyArray<{key: PlaylistSortKey; label: string}> = [
    {key: 'name', label: 'Name'},
    {key: 'user_id', label: 'Owner'},
    {key: 'items', label: 'Items'},
    {key: 'is_public', label: 'Public'},
    {key: 'created_at', label: 'Created'},
]

export function PlaylistsTab() {
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
        queryFn: async () => {
            const res = await adminApi.listAllPlaylists()
            return res.items
        },
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
        return !(filterVisibility === 'private' && p.is_public)
    }).sort((a, b) => {
        let cmp = 0
        switch (sortBy) {
            case 'name': cmp = a.name.localeCompare(b.name); break
            case 'user_id': cmp = a.user_id.localeCompare(b.user_id); break
            case 'items': cmp = (a.items?.length ?? 0) - (b.items?.length ?? 0); break
            case 'is_public': {
                cmp = a.is_public === b.is_public ? 0 : a.is_public ? -1 : 1
                break
            }
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
        const arrow = sortOrder === 'asc' ? '\u25B2' : '\u25BC'
        return <span style={{marginLeft: 4}}>{arrow}</span>
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
                        onChange={e => { setSearch(e.target.value); }}
                        style={{...selectStyle, flex: 1, minWidth: 160}}
                    />
                    <select value={filterVisibility} onChange={e => { setFilterVisibility(e.target.value); }} style={selectStyle}>
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
                        <button className="admin-btn" onClick={() => { setSelected(new Set()); }}
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
                                <th key={col.key} style={thSortStyle} onClick={() => { handleSort(col.key); }}>
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

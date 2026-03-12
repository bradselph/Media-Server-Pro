import {useEffect, useRef, useState} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import {adminApi} from '@/api/endpoints'
import type {AdminMediaListResponse, MediaItem, ThumbnailStats} from '@/api/types'
import {ContentReviewTab, CategorizerTab, DiscoveryTab, HuggingFaceTab} from './ContentTab'
import {ValidatorTab} from './ValidatorTab'
import {errMsg, formatBytes} from './adminUtils'
import {SubTabs} from './helpers'

// ── Tab: Media ────────────────────────────────────────────────────────────────

// TODO: Duplicate — this local `formatDuration` duplicates the shared version in
// `@/utils/formatters.ts`. Another copy exists in `ProfilePage.tsx`.
// WHY: Multiple copies cause maintenance burden and inconsistent fallback values
// ('—' here vs '0:00' in formatters.ts vs '0m' in ProfilePage.tsx).
// FIX: Import `formatDuration` from `@/utils/formatters.ts` and use it:
// `formatDuration({ seconds: secs })`. Adjust the fallback in formatters.ts or
// add a fallback parameter if different defaults are needed per context.
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

function MediaLibraryTab() {
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
        mediaSearchTimer.current = setTimeout(() => { setDebouncedMediaSearch(mediaSearch); }, 300)
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
                    })).catch(err => { setMsg({type: 'error', text: errMsg(err)}); })}>
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
                            onChange={e => { setBulkCategory(e.target.value); }}
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
                            onChange={e => { setBulkMature(e.target.value === '' ? null : e.target.value === 'true'); }}
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
                        <button className="admin-btn" onClick={() => { setSelected(new Set()); }}
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
                                       onChange={e => { setEditItem({...editItem, name: e.target.value}); }}
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
                                       onChange={e => { setEditItem({...editItem, category: e.target.value}); }}
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
                                       onChange={e => { setEditItem({...editItem, tags: e.target.value.split(',').map(t => t.trim()).filter(Boolean)}); }}
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
                                       onChange={e => { setEditItem({...editItem, is_mature: e.target.checked}); }}/>
                                Mature content (18+)
                            </label>
                        </div>
                        <div className="admin-action-row" style={{marginTop: 10}}>
                            <button className="admin-btn admin-btn-primary" onClick={handleSaveEdit}><i
                                className="bi bi-check-lg"/> Save
                            </button>
                            <button className="admin-btn" onClick={() => { setEditItem(null); }}>Cancel</button>
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
                                <th key={col.key} style={thSortStyle} onClick={() => { handleSort(col.key); }}>
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
                                           onChange={() => { toggleSelect(item.id); }}/>
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
                                                onClick={() => { setEditItem(item); }}>
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
                    <button className="admin-btn" disabled={mediaPage <= 1} onClick={() => { setMediaPage(p => p - 1); }}>←
                        Prev
                    </button>
                    <span style={{fontSize: 13, color: 'var(--text-muted)', padding: '4px 0'}}>
                        Page {mediaPage} of {totalPages}
                    </span>
                    <button className="admin-btn" disabled={mediaPage >= totalPages}
                            onClick={() => { setMediaPage(p => p + 1); }}>Next →
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
                    <th style={thS} onClick={() => { handleSort('filename'); }}>Name{ind('filename')}</th>
                    <th style={thS} onClick={() => { handleSort('size'); }}>Size{ind('size')}</th>
                    <th style={thS} onClick={() => { handleSort('created_at'); }}>Created{ind('created_at')}</th>
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
                                        onClick={() => { onRestore(b.id, b.filename); }}>Restore
                                </button>
                                <button className="admin-btn admin-btn-danger"
                                        onClick={() => { onDelete(b.id, b.filename); }}>
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
                    onChange={e => { setThumbPath(e.target.value); }}
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

export function MediaTab() {
    const [sub, setSub] = useState('library')
    return (
        <>
            <SubTabs
                items={[
                    {id: 'library', label: 'Library'},
                    {id: 'review', label: 'Review'},
                    {id: 'categorizer', label: 'Categorizer'},
                    {id: 'classification', label: 'Hugging Face'},
                    {id: 'discovery', label: 'Discovery'},
                    {id: 'validator', label: 'Validator'},
                ]}
                active={sub}
                onChange={setSub}
            />
            {sub === 'library' && <MediaLibraryTab/>}
            {sub === 'review' && <ContentReviewTab/>}
            {sub === 'categorizer' && <CategorizerTab/>}
            {sub === 'classification' && <HuggingFaceTab/>}
            {sub === 'discovery' && <DiscoveryTab/>}
            {sub === 'validator' && <ValidatorTab/>}
        </>
    )
}

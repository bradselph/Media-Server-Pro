import {type FormEvent, useState} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import {adminApi, mediaApi, receiverApi} from '@/api/endpoints'
import type {
    CrawlTarget,
    CrawlerDiscovery,
    CrawlerStats,
    DuplicateItem,
    ExtractorItem,
    ExtractorStats,
    ReceiverDuplicate,
    ReceiverMediaItem,
    ReceiverStats,
    RemoteMediaItem,
    RemoteSourceState,
    SlaveNode,
} from '@/api/types'
import {StreamingTab} from './StreamingTab'
import {errMsg, formatBytes} from './helpers'
import {SubTabs} from './helpers'

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
                                        className="admin-btn"
                                        style={{padding: '4px 7px', fontSize: 13}}
                                        onClick={() => handleSync(s.source.name)}
                                        disabled={syncing === s.source.name}
                                        title="Trigger sync"
                                    >
                                        <i className={`bi bi-arrow-repeat ${syncing === s.source.name ? 'spinning' : ''}`}/>
                                    </button>
                                    <button
                                        className="admin-btn admin-btn-danger"
                                        onClick={() => handleDelete(s.source.name)}
                                        title="Remove source"
                                        style={{marginLeft: 4, padding: '4px 7px', fontSize: 13}}
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
                                                                        <th>Resolution</th>
                                                                        <th>Content Type</th>
                                                                        <th>Stream</th>
                                                                    </tr>
                                                                </thead>
                                                                <tbody>
                                                                    {browseMedia.map(m => (
                                                                        <tr key={m.id}>
                                                                            <td title={m.path}>{m.name}</td>
                                                                            <td><span className={`media-card-type-badge badge-${m.media_type}`}>{m.media_type}</span></td>
                                                                            <td>{formatBytes(m.size)}</td>
                                                                            <td>{m.duration > 0 ? `${Math.floor(m.duration / 60)}:${String(Math.floor(m.duration % 60)).padStart(2, '0')}` : '—'}</td>
                                                                            <td>{m.width > 0 && m.height > 0 ? `${m.width}x${m.height}` : '—'}</td>
                                                                            <td style={{fontSize: 11}}>{m.content_type || '—'}</td>
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

function ExtractorTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [newUrl, setNewUrl] = useState('')
    const [newTitle, setNewTitle] = useState('')
    const [adding, setAdding] = useState(false)

    const {data: items, isLoading} = useQuery<ExtractorItem[]>({
        queryKey: ['admin-extractor-items'],
        queryFn: () => adminApi.getExtractorItems(),
        refetchInterval: 15000,
        retry: false,
    })

    const {data: stats} = useQuery<ExtractorStats>({
        queryKey: ['admin-extractor-stats'],
        queryFn: () => adminApi.getExtractorStats(),
        refetchInterval: 30000,
        retry: false,
    })

    async function handleAdd(e: FormEvent) {
        e.preventDefault()
        if (!newUrl.trim()) return
        setAdding(true)
        setMsg(null)
        try {
            const item = await adminApi.addExtractorItem(newUrl.trim(), newTitle.trim() || undefined)
            setMsg({type: 'success', text: `Added: ${item.title}`})
            setNewUrl('')
            setNewTitle('')
            await queryClient.invalidateQueries({queryKey: ['admin-extractor-items']})
            await queryClient.invalidateQueries({queryKey: ['admin-extractor-stats']})
        } catch (err) {
            setMsg({type: 'error', text: `Failed: ${errMsg(err)}`})
        } finally {
            setAdding(false)
        }
    }

    async function handleRemove(id: string, title: string) {
        if (!window.confirm(`Remove "${title}" from the library?`)) return
        setMsg(null)
        try {
            await adminApi.removeExtractorItem(id)
            setMsg({type: 'success', text: `Removed: ${title}`})
            await queryClient.invalidateQueries({queryKey: ['admin-extractor-items']})
            await queryClient.invalidateQueries({queryKey: ['admin-extractor-stats']})
        } catch (err) {
            setMsg({type: 'error', text: `Remove failed: ${errMsg(err)}`})
        }
    }

    function statusBadge(status: string) {
        const cls = status === 'active' ? 'badge-active' : 'badge-mature'
        return <span className={`media-card-type-badge ${cls}`}>{status}</span>
    }

    return (
        <div>
            <h3>HLS Stream Proxy</h3>
            <p style={{color: 'var(--text-muted)', marginBottom: 12}}>
                Add M3U8 playlist URLs to proxy HLS streams through the server.
                Streams appear in the media library — no files are downloaded to disk.
            </p>

            {msg && <div className={`admin-alert ${msg.type === 'success' ? 'admin-alert-success' : 'admin-alert-danger'}`}>{msg.text}</div>}

            {/* Stats */}
            {stats && (
                <div className="admin-stats-grid" style={{marginBottom: '1.5rem'}}>
                    <div className="admin-stat-card"><div className="admin-stat-value">{stats.total_items}</div><div className="admin-stat-label">Total Streams</div></div>
                    <div className="admin-stat-card"><div className="admin-stat-value">{stats.active_items}</div><div className="admin-stat-label">Active</div></div>
                    <div className="admin-stat-card"><div className="admin-stat-value">{stats.error_items}</div><div className="admin-stat-label">Errors</div></div>
                </div>
            )}

            {/* Add URL form */}
            <form onSubmit={handleAdd} style={{display: 'flex', gap: '0.5rem', marginBottom: '1.5rem', flexWrap: 'wrap'}}>
                <input
                    type="url"
                    value={newUrl}
                    onChange={e => setNewUrl(e.target.value)}
                    placeholder="M3U8 playlist URL..."
                    required
                    style={{flex: 2, minWidth: '250px', padding: '6px 10px', borderRadius: '6px', border: '1px solid var(--border-color)', background: 'var(--input-bg)', color: 'var(--text-color)'}}
                />
                <input
                    type="text"
                    value={newTitle}
                    onChange={e => setNewTitle(e.target.value)}
                    placeholder="Title (optional)"
                    style={{flex: 1, minWidth: '150px', padding: '6px 10px', borderRadius: '6px', border: '1px solid var(--border-color)', background: 'var(--input-bg)', color: 'var(--text-color)'}}
                />
                <button type="submit" className="admin-btn admin-btn-primary" disabled={adding || !newUrl.trim()}>
                    {adding ? 'Adding...' : 'Add Stream'}
                </button>
            </form>

            {/* Items table */}
            {isLoading ? (
                <p style={{color: 'var(--text-muted)'}}>Loading...</p>
            ) : !items || items.length === 0 ? (
                <p style={{color: 'var(--text-muted)'}}>No streams added yet. Paste an M3U8 URL above to get started.</p>
            ) : (
                <div className="admin-table-wrapper">
                    <table className="admin-table">
                        <thead>
                            <tr>
                                <th>Title</th>
                                <th>Stream URL</th>
                                <th>Status</th>
                                <th>Added By</th>
                                <th>Added</th>
                                <th>Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                            {items.map(item => (
                                <tr key={item.id}>
                                    <td style={{maxWidth: '200px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'}}>
                                        {item.title}
                                    </td>
                                    <td title={item.stream_url} style={{maxWidth: '300px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontFamily: 'monospace', fontSize: '0.85em'}}>
                                        {item.stream_url}
                                    </td>
                                    <td>
                                        {statusBadge(item.status)}
                                        {item.status === 'error' && item.error_message && (
                                            <span title={item.error_message} style={{marginLeft: 4, cursor: 'help', color: '#ef4444'}}>&#9888;</span>
                                        )}
                                    </td>
                                    <td style={{fontSize: 12, color: 'var(--text-muted)'}}>{item.added_by || '—'}</td>
                                    <td>{new Date(item.created_at).toLocaleDateString()}</td>
                                    <td>
                                        <button
                                            className="admin-btn admin-btn-danger"
                                            style={{fontSize: 12, padding: '3px 8px'}}
                                            onClick={() => handleRemove(item.id, item.title)}
                                            title="Remove from library"
                                        >
                                            Remove
                                        </button>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    )
}

function CrawlerTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [newUrl, setNewUrl] = useState('')
    const [newName, setNewName] = useState('')
    const [adding, setAdding] = useState(false)
    const [crawlingId, setCrawlingId] = useState<string | null>(null)
    const [reviewView, setReviewView] = useState<'pending' | 'all'>('pending')
    const [approvingId, setApprovingId] = useState<string | null>(null)

    const {data: targets, isLoading: targetsLoading} = useQuery<CrawlTarget[]>({
        queryKey: ['admin-crawler-targets'],
        queryFn: () => adminApi.getCrawlerTargets(),
        refetchInterval: 15000,
        retry: false,
    })

    const {data: discoveries, isLoading: discoveriesLoading} = useQuery<CrawlerDiscovery[]>({
        queryKey: ['admin-crawler-discoveries', reviewView],
        queryFn: () => adminApi.getCrawlerDiscoveries(reviewView === 'pending' ? 'pending' : undefined),
        refetchInterval: 10000,
        retry: false,
    })

    const {data: stats} = useQuery<CrawlerStats>({
        queryKey: ['admin-crawler-stats'],
        queryFn: () => adminApi.getCrawlerStats(),
        refetchInterval: 10000,
        retry: false,
    })

    async function handleAddTarget(e: FormEvent) {
        e.preventDefault()
        if (!newUrl.trim()) return
        setAdding(true)
        setMsg(null)
        try {
            const target = await adminApi.addCrawlerTarget(newUrl.trim(), newName.trim() || undefined)
            setMsg({type: 'success', text: `Added target: ${target.name}`})
            setNewUrl('')
            setNewName('')
            await queryClient.invalidateQueries({queryKey: ['admin-crawler-targets']})
            await queryClient.invalidateQueries({queryKey: ['admin-crawler-stats']})
        } catch (err) {
            setMsg({type: 'error', text: `Failed: ${errMsg(err)}`})
        } finally {
            setAdding(false)
        }
    }

    async function handleCrawl(id: string) {
        setCrawlingId(id)
        setMsg(null)
        try {
            const result = await adminApi.crawlTarget(id)
            setMsg({type: 'success', text: `Crawl complete: ${result.new_discoveries} new streams found`})
            await queryClient.invalidateQueries({queryKey: ['admin-crawler-targets']})
            await queryClient.invalidateQueries({queryKey: ['admin-crawler-discoveries']})
            await queryClient.invalidateQueries({queryKey: ['admin-crawler-stats']})
        } catch (err) {
            setMsg({type: 'error', text: `Crawl failed: ${errMsg(err)}`})
        } finally {
            setCrawlingId(null)
        }
    }

    async function handleRemoveTarget(id: string, name: string) {
        if (!window.confirm(`Remove target "${name}" and all its discoveries?`)) return
        try {
            await adminApi.removeCrawlerTarget(id)
            setMsg({type: 'success', text: `Removed: ${name}`})
            await queryClient.invalidateQueries({queryKey: ['admin-crawler-targets']})
            await queryClient.invalidateQueries({queryKey: ['admin-crawler-discoveries']})
            await queryClient.invalidateQueries({queryKey: ['admin-crawler-stats']})
        } catch (err) {
            setMsg({type: 'error', text: `Remove failed: ${errMsg(err)}`})
        }
    }

    async function handleApprove(id: string) {
        setApprovingId(id)
        try {
            const disc = await adminApi.approveCrawlerDiscovery(id)
            setMsg({type: 'success', text: `Added to library: ${disc.title}`})
            await queryClient.invalidateQueries({queryKey: ['admin-crawler-discoveries']})
            await queryClient.invalidateQueries({queryKey: ['admin-crawler-stats']})
        } catch (err) {
            setMsg({type: 'error', text: `Approve failed: ${errMsg(err)}`})
        } finally {
            setApprovingId(null)
        }
    }

    async function handleIgnore(id: string) {
        try {
            await adminApi.ignoreCrawlerDiscovery(id)
            await queryClient.invalidateQueries({queryKey: ['admin-crawler-discoveries']})
            await queryClient.invalidateQueries({queryKey: ['admin-crawler-stats']})
        } catch (err) {
            setMsg({type: 'error', text: `Ignore failed: ${errMsg(err)}`})
        }
    }

    function statusBadge(status: string) {
        const cls = status === 'pending' ? 'badge-inactive' : status === 'added' ? 'badge-active' : 'badge-mature'
        return <span className={`media-card-type-badge ${cls}`}>{status}</span>
    }

    return (
        <div>
            <h3>Stream Crawler</h3>
            <p style={{color: 'var(--text-muted)', marginBottom: 12}}>
                Crawl target site pages to discover M3U8 streams. Review discovered streams and approve them to add to the media library.
            </p>

            {msg && <div className={`admin-alert ${msg.type === 'success' ? 'admin-alert-success' : 'admin-alert-danger'}`}>{msg.text}</div>}

            {/* Stats */}
            {stats && (
                <div className="admin-stats-grid" style={{marginBottom: '1.5rem'}}>
                    <div className="admin-stat-card"><div className="admin-stat-value">{stats.total_targets}</div><div className="admin-stat-label">Targets</div></div>
                    <div className="admin-stat-card"><div className="admin-stat-value">{stats.enabled_targets}</div><div className="admin-stat-label">Enabled</div></div>
                    <div className="admin-stat-card"><div className="admin-stat-value">{stats.pending_discoveries}</div><div className="admin-stat-label">Pending Review</div></div>
                    <div className="admin-stat-card"><div className="admin-stat-value">{stats.total_discoveries}</div><div className="admin-stat-label">Total Found</div></div>
                    <div className="admin-stat-card"><div className="admin-stat-value">{stats.crawling ? 'Running' : 'Idle'}</div><div className="admin-stat-label">Status</div></div>
                </div>
            )}

            {/* Add Target */}
            <h4 style={{marginBottom: '0.5rem'}}>Crawl Targets</h4>
            <form onSubmit={handleAddTarget} style={{display: 'flex', gap: '0.5rem', marginBottom: '1rem', flexWrap: 'wrap'}}>
                <input
                    type="url"
                    value={newUrl}
                    onChange={e => setNewUrl(e.target.value)}
                    placeholder="Site URL to crawl..."
                    required
                    style={{flex: 2, minWidth: '250px', padding: '6px 10px', borderRadius: '6px', border: '1px solid var(--border-color)', background: 'var(--input-bg)', color: 'var(--text-color)'}}
                />
                <input
                    type="text"
                    value={newName}
                    onChange={e => setNewName(e.target.value)}
                    placeholder="Name (optional)"
                    style={{flex: 1, minWidth: '120px', padding: '6px 10px', borderRadius: '6px', border: '1px solid var(--border-color)', background: 'var(--input-bg)', color: 'var(--text-color)'}}
                />
                <button type="submit" className="admin-btn admin-btn-primary" disabled={adding || !newUrl.trim()}>
                    {adding ? 'Adding...' : 'Add Target'}
                </button>
            </form>

            {/* Targets Table */}
            {targetsLoading ? (
                <p style={{color: 'var(--text-muted)'}}>Loading...</p>
            ) : !targets || targets.length === 0 ? (
                <p style={{color: 'var(--text-muted)'}}>No crawl targets. Add a site URL above.</p>
            ) : (
                <div className="admin-table-wrapper" style={{marginBottom: 24}}>
                    <table className="admin-table">
                        <thead><tr><th>Name</th><th>Site</th><th>URL</th><th>Enabled</th><th>Last Crawled</th><th>Actions</th></tr></thead>
                        <tbody>
                            {targets.map(t => (
                                <tr key={t.id}>
                                    <td>{t.name}</td>
                                    <td style={{fontSize: 12, color: 'var(--text-muted)'}}>{t.site || '—'}</td>
                                    <td style={{maxWidth: '300px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontFamily: 'monospace', fontSize: '0.85em'}}>{t.url}</td>
                                    <td>{t.enabled ? <span style={{color: '#22c55e'}}>Yes</span> : <span style={{color: '#ef4444'}}>No</span>}</td>
                                    <td>{t.last_crawled ? new Date(t.last_crawled).toLocaleString() : 'Never'}</td>
                                    <td>
                                        <button
                                            className="admin-btn admin-btn-primary"
                                            style={{fontSize: 12, padding: '4px 8px'}}
                                            onClick={() => handleCrawl(t.id)}
                                            disabled={crawlingId === t.id || (stats?.crawling ?? false)}
                                        >
                                            {crawlingId === t.id ? 'Crawling...' : 'Crawl'}
                                        </button>
                                        {' '}
                                        <button className="admin-btn admin-btn-danger" style={{fontSize: 12, padding: '4px 8px'}} onClick={() => handleRemoveTarget(t.id, t.name)}>
                                            Remove
                                        </button>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}

            {/* Discoveries */}
            <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.5rem'}}>
                <h4>Discovered Streams</h4>
                <div style={{display: 'flex', gap: '0.5rem'}}>
                    <button
                        className={`admin-btn ${reviewView === 'pending' ? 'admin-btn-primary' : ''}`}
                        style={{fontSize: 12, padding: '4px 10px'}}
                        onClick={() => setReviewView('pending')}
                    >Pending</button>
                    <button
                        className={`admin-btn ${reviewView === 'all' ? 'admin-btn-primary' : ''}`}
                        style={{fontSize: 12, padding: '4px 10px'}}
                        onClick={() => setReviewView('all')}
                    >All</button>
                </div>
            </div>

            {discoveriesLoading ? (
                <p style={{color: 'var(--text-muted)'}}>Loading...</p>
            ) : !discoveries || discoveries.length === 0 ? (
                <p style={{color: 'var(--text-muted)'}}>
                    {reviewView === 'pending' ? 'No pending discoveries. Crawl a target to find streams.' : 'No discoveries yet.'}
                </p>
            ) : (
                <div className="admin-table-wrapper">
                    <table className="admin-table">
                        <thead><tr><th>Title</th><th>Stream URL</th><th>Type</th><th>Quality</th><th>Status</th><th>Found</th><th>Actions</th></tr></thead>
                        <tbody>
                            {discoveries.map(d => (
                                <tr key={d.id}>
                                    <td title={d.page_url} style={{maxWidth: '200px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap'}}>
                                        {d.title}
                                    </td>
                                    <td title={d.stream_url} style={{maxWidth: '250px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontFamily: 'monospace', fontSize: '0.85em'}}>
                                        {d.stream_url}
                                    </td>
                                    <td style={{fontSize: 12}}>{d.stream_type || '—'}</td>
                                    <td style={{fontSize: 12}}>{d.quality > 0 ? `${d.quality}p` : '—'}</td>
                                    <td>{statusBadge(d.status)}</td>
                                    <td>{new Date(d.discovered_at).toLocaleDateString()}</td>
                                    <td>
                                        {d.status === 'pending' && (<>
                                            <button
                                                className="admin-btn admin-btn-primary"
                                                style={{fontSize: 12, padding: '4px 8px'}}
                                                onClick={() => handleApprove(d.id)}
                                                disabled={approvingId === d.id}
                                                title="Add to media library"
                                            >
                                                {approvingId === d.id ? '...' : 'Add'}
                                            </button>
                                            {' '}
                                            <button className="admin-btn" style={{fontSize: 12, padding: '4px 8px'}} onClick={() => handleIgnore(d.id)} title="Ignore">
                                                Ignore
                                            </button>
                                        </>)}
                                        {d.status !== 'pending' && (
                                            <span style={{color: 'var(--text-muted)', fontSize: 12}}>
                                                {d.reviewed_by || '-'}
                                                {d.reviewed_at && ` on ${new Date(d.reviewed_at).toLocaleDateString()}`}
                                            </span>
                                        )}
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    )
}

// ── Tab: Receiver Duplicates ──────────────────────────────────────────────────

function DuplicatesTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{type: 'success' | 'error'; text: string} | null>(null)
    const [showAll, setShowAll] = useState(false)
    const [resolving, setResolving] = useState<string | null>(null)

    const {data: dupes, isLoading, isError} = useQuery<ReceiverDuplicate[]>({
        queryKey: ['receiver-duplicates', showAll ? 'all' : 'pending'],
        queryFn: () => adminApi.listReceiverDuplicates(showAll ? 'all' : 'pending'),
        refetchInterval: 30000,
        retry: false,
    })

    async function handleResolve(id: string, action: string) {
        setResolving(id)
        setMsg(null)
        try {
            const res = await adminApi.resolveReceiverDuplicate(id, action)
            setMsg({type: 'success', text: res.message ?? 'Resolved'})
            await queryClient.invalidateQueries({queryKey: ['receiver-duplicates']})
            await queryClient.invalidateQueries({queryKey: ['admin-receiver-stats']})
        } catch (err) {
            setMsg({type: 'error', text: `Failed: ${errMsg(err)}`})
        } finally {
            setResolving(null)
        }
    }

    function itemCard(item: DuplicateItem, label: string) {
        return (
            <div style={{flex: 1, minWidth: 0, background: 'var(--input-bg)', borderRadius: 6, padding: '10px 14px'}}>
                <div style={{fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', marginBottom: 4, textTransform: 'uppercase', letterSpacing: 1}}>{label}</div>
                <div style={{fontWeight: 600, wordBreak: 'break-all', marginBottom: 4}}>{item.name}</div>
                <div style={{fontSize: 12, color: 'var(--text-muted)', display: 'flex', flexWrap: 'wrap', gap: '6px 16px'}}>
                    {item.source === 'receiver'
                        ? <span><i className="bi bi-hdd-network"/> Receiver {item.slave_id ? item.slave_id.slice(0, 12) + '…' : ''}</span>
                        : <span><i className="bi bi-hdd"/> Local</span>
                    }
                </div>
            </div>
        )
    }

    function statusBadge(status: string) {
        if (status === 'pending') return <span className="media-card-type-badge badge-mature"><i className="bi bi-exclamation-circle-fill"/> Pending</span>
        if (status === 'remove_a') return <span className="media-card-type-badge badge-inactive"><i className="bi bi-trash3-fill"/> A removed</span>
        if (status === 'remove_b') return <span className="media-card-type-badge badge-inactive"><i className="bi bi-trash3-fill"/> B removed</span>
        if (status === 'keep_both') return <span className="media-card-type-badge badge-active"><i className="bi bi-check-circle-fill"/> Keep both</span>
        if (status === 'ignore') return <span className="media-card-type-badge badge-active"><i className="bi bi-eye-slash-fill"/> Ignored</span>
        return <span className="media-card-type-badge">{status}</span>
    }

    return (
        <div>
            <h2 style={{margin: '0 0 4px 0', fontSize: 20}}><i className="bi bi-copy"/> Duplicate Detection</h2>
            <p style={{margin: '0 0 20px 0', color: 'var(--text-muted)', fontSize: 13}}>
                Local and receiver media items sharing the same content fingerprint (SHA-256 of sampled file content).
                Choose how to handle each pair.
            </p>

            {msg && (
                <div className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}
                     style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16}}>
                    <span><i className={`bi ${msg.type === 'success' ? 'bi-check-circle-fill' : 'bi-exclamation-triangle-fill'}`}/> {msg.text}</span>
                    <button onClick={() => setMsg(null)} style={{background: 'none', border: 'none', cursor: 'pointer', fontSize: 16, opacity: 0.7}}>×</button>
                </div>
            )}

            <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16}}>
                <div style={{fontSize: 13, color: 'var(--text-muted)'}}>
                    {isLoading ? 'Loading…' : isError ? 'Feature may be disabled.' : `${dupes?.length ?? 0} ${showAll ? 'total' : 'pending'} duplicate pair${dupes?.length !== 1 ? 's' : ''}`}
                </div>
                <div style={{display: 'flex', gap: 8}}>
                    <button className={`admin-btn ${showAll ? 'admin-btn-primary' : ''}`} style={{fontSize: 12, padding: '5px 10px'}}
                            onClick={() => setShowAll(v => !v)}>
                        <i className={`bi ${showAll ? 'bi-funnel-fill' : 'bi-funnel'}`}/> {showAll ? 'Showing All' : 'Show All'}
                    </button>
                    <button className="admin-btn" style={{fontSize: 12, padding: '5px 10px'}}
                            onClick={() => queryClient.invalidateQueries({queryKey: ['receiver-duplicates']})}>
                        <i className="bi bi-arrow-clockwise"/> Refresh
                    </button>
                </div>
            </div>

            {isLoading && <p style={{color: 'var(--text-muted)', fontSize: 13}}><i className="bi bi-hourglass-split"/> Checking for duplicates…</p>}

            {isError && (
                <div className="admin-alert admin-alert-warning">
                    <i className="bi bi-exclamation-triangle-fill"/> Duplicate detection may be disabled. Set <code>FEATURE_DUPLICATE_DETECTION=true</code> and restart.
                </div>
            )}

            {!isLoading && !isError && (!dupes || dupes.length === 0) && (
                <div style={{textAlign: 'center', padding: '40px 16px', color: 'var(--text-muted)'}}>
                    <i className="bi bi-check2-circle" style={{fontSize: 36, display: 'block', marginBottom: 8, color: '#10b981'}}/>
                    <p style={{margin: 0}}>{showAll ? 'No duplicate records found.' : 'No pending duplicates — all clear!'}</p>
                </div>
            )}

            {dupes && dupes.length > 0 && (
                <div style={{display: 'flex', flexDirection: 'column', gap: 12}}>
                    {dupes.map(dup => (
                        <div key={dup.id} className="admin-card" style={{padding: '14px 16px'}}>
                            <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 10, flexWrap: 'wrap', gap: 8}}>
                                <div style={{display: 'flex', alignItems: 'center', gap: 8}}>
                                    {statusBadge(dup.status)}
                                    <span style={{fontSize: 11, color: 'var(--text-muted)', fontFamily: 'monospace'}}>
                                        fp: {dup.fingerprint.slice(0, 16)}…
                                    </span>
                                </div>
                                <span style={{fontSize: 11, color: 'var(--text-muted)'}}>
                                    detected {new Date(dup.detected_at).toLocaleString()}
                                    {dup.resolved_by && ` · resolved by ${dup.resolved_by}`}
                                    {dup.resolved_at && ` on ${new Date(dup.resolved_at).toLocaleDateString()}`}
                                </span>
                            </div>

                            <div style={{display: 'flex', gap: 10, marginBottom: 12, flexWrap: 'wrap'}}>
                                {itemCard(dup.item_a, 'Item A')}
                                <div style={{display: 'flex', alignItems: 'center', flexShrink: 0, color: 'var(--text-muted)', fontSize: 20}}>
                                    <i className="bi bi-arrow-left-right"/>
                                </div>
                                {itemCard(dup.item_b, 'Item B')}
                            </div>

                            {dup.status === 'pending' && (
                                <div style={{display: 'flex', gap: 8, flexWrap: 'wrap'}}>
                                    <button className="admin-btn admin-btn-danger" style={{fontSize: 12}}
                                            disabled={resolving === dup.id}
                                            onClick={() => handleResolve(dup.id, 'remove_a')}>
                                        <i className="bi bi-trash3-fill"/> Remove A
                                    </button>
                                    <button className="admin-btn admin-btn-danger" style={{fontSize: 12}}
                                            disabled={resolving === dup.id}
                                            onClick={() => handleResolve(dup.id, 'remove_b')}>
                                        <i className="bi bi-trash3-fill"/> Remove B
                                    </button>
                                    <button className="admin-btn admin-btn-primary" style={{fontSize: 12}}
                                            disabled={resolving === dup.id}
                                            onClick={() => handleResolve(dup.id, 'keep_both')}>
                                        <i className="bi bi-check2-circle"/> Keep Both
                                    </button>
                                    <button className="admin-btn" style={{fontSize: 12}}
                                            disabled={resolving === dup.id}
                                            onClick={() => handleResolve(dup.id, 'ignore')}>
                                        <i className="bi bi-eye-slash"/> Ignore
                                    </button>
                                    {resolving === dup.id && (
                                        <span style={{fontSize: 12, color: 'var(--text-muted)', alignSelf: 'center'}}>
                                            <i className="bi bi-hourglass-split"/> Working…
                                        </span>
                                    )}
                                </div>
                            )}
                        </div>
                    ))}
                </div>
            )}
        </div>
    )
}

export function SourcesTab() {
    const [sub, setSub] = useState('hls')
    const {data: stats} = useQuery<ReceiverStats>({
        queryKey: ['admin-receiver-stats'],
        queryFn: () => adminApi.getReceiverStats(),
        refetchInterval: 30000,
        retry: false,
    })

    const dupCount = stats?.duplicate_count ?? 0

    return (<>
        <SubTabs items={[
            {id: 'hls', label: 'HLS'},
            {id: 'remote', label: 'Remote'},
            {id: 'slaves', label: 'Slaves'},
            {id: 'extractor', label: 'HLS Streams'},
            {id: 'crawler', label: 'Crawler'},
            {id: 'duplicates', label: dupCount > 0 ? `Duplicates (${dupCount})` : 'Duplicates'},
        ]} active={sub} onChange={setSub}/>
        {sub === 'hls' && <StreamingTab/>}
        {sub === 'remote' && <RemoteTab/>}
        {sub === 'slaves' && <ReceiverTab/>}
        {sub === 'extractor' && <ExtractorTab/>}
        {sub === 'crawler' && <CrawlerTab/>}
        {sub === 'duplicates' && <DuplicatesTab/>}
    </>)
}

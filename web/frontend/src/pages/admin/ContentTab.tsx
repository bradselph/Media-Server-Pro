import {useEffect, useState} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import {adminApi} from '@/api/endpoints'
import type {CategorizedItem, CategoryStats, DiscoverySuggestion} from '@/api/types'
import {errMsg} from './adminUtils'
import {SubTabs} from './helpers'

// Shape of huggingface block from GET /api/admin/config (nested under config)
type HuggingFaceConfigBlock = {
    enabled?: boolean
    api_key_set?: boolean
    model?: string
    endpoint_url?: string
    max_frames?: number
    timeout_secs?: number
    rate_limit?: number
    max_concurrent?: number
}

// ── Tab: Content Review ───────────────────────────────────────────────────────

type ReviewSortKey = 'name' | 'detected_at' | 'confidence'
export function ContentReviewTab() {
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

// ── Tab: Categorizer (Feature 11) ─────────────────────────────────────────────

export function CategorizerTab() {
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

export function DiscoveryTab() {
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

// ── Tab: Hugging Face (visual classification) ───────────────────────────────

export function HuggingFaceTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [classifyPath, setClassifyPath] = useState('')
    const [classifyDirPath, setClassifyDirPath] = useState('')
    const [classifyFileLoading, setClassifyFileLoading] = useState(false)
    const [classifyDirLoading, setClassifyDirLoading] = useState(false)
    const [saving, setSaving] = useState(false)
    const [apiKeyInput, setApiKeyInput] = useState('')

    const {data: status} = useQuery({
        queryKey: ['classify-status'],
        queryFn: () => adminApi.getClassifyStatus(),
    })

    const {data: config} = useQuery({
        queryKey: ['admin-config'],
        queryFn: () => adminApi.getConfig(),
    })

    const hf = (config?.huggingface as HuggingFaceConfigBlock | undefined) ?? {}
    const [enabled, setEnabled] = useState(hf.enabled ?? false)
    const [model, setModel] = useState(hf.model ?? '')
    const [maxFrames, setMaxFrames] = useState(hf.max_frames ?? 3)
    const [rateLimit, setRateLimit] = useState(hf.rate_limit ?? 30)
    const [timeoutSecs, setTimeoutSecs] = useState(hf.timeout_secs ?? 30)
    const [maxConcurrent, setMaxConcurrent] = useState(hf.max_concurrent ?? 2)
    const [endpointUrl, setEndpointUrl] = useState(hf.endpoint_url ?? '')

    useEffect(() => {
        if (!config?.huggingface) return
        const c = config.huggingface as HuggingFaceConfigBlock
        setEnabled(!!c.enabled)
        setModel(c.model ?? '')
        setMaxFrames(c.max_frames ?? 3)
        setRateLimit(c.rate_limit ?? 30)
        setTimeoutSecs(c.timeout_secs ?? 30)
        setMaxConcurrent(c.max_concurrent ?? 2)
        setEndpointUrl(c.endpoint_url ?? '')
    }, [config?.huggingface])

    async function handleClassifyFile() {
        if (!classifyPath.trim()) return
        setClassifyFileLoading(true)
        setMsg(null)
        try {
            const result = await adminApi.classifyFile(classifyPath.trim())
            setMsg({ type: 'success', text: `Classified: ${result.tags?.length ?? 0} tags added.` })
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        } finally {
            setClassifyFileLoading(false)
        }
    }

    async function handleClassifyDirectory() {
        if (!classifyDirPath.trim()) return
        setClassifyDirLoading(true)
        setMsg(null)
        try {
            await adminApi.classifyDirectory(classifyDirPath.trim())
            setMsg({ type: 'success', text: 'Directory classification completed.' })
            void queryClient.invalidateQueries({ queryKey: ['classify-status'] })
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        } finally {
            setClassifyDirLoading(false)
        }
    }

    async function handleSaveSettings(e: React.FormEvent) {
        e.preventDefault()
        setSaving(true)
        setMsg(null)
        try {
            const updates: Record<string, unknown> = {
                'huggingface.enabled': enabled,
                'huggingface.model': model || 'Salesforce/blip-image-captioning-large',
                'huggingface.max_frames': maxFrames,
                'huggingface.rate_limit': rateLimit,
                'huggingface.timeout_secs': timeoutSecs,
                'huggingface.max_concurrent': maxConcurrent,
                'features.enable_huggingface': enabled,
            }
            updates['huggingface.endpoint_url'] = endpointUrl.trim()
            if (apiKeyInput.trim()) updates['huggingface.api_key'] = apiKeyInput.trim()
            await adminApi.updateConfig(updates as Record<string, string | number | boolean>)
            setMsg({ type: 'success', text: 'Settings saved. Some changes may require a restart.' })
            setApiKeyInput('')
            void queryClient.invalidateQueries({ queryKey: ['admin-config', 'classify-status'] })
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        } finally {
            setSaving(false)
        }
    }

    return (
        <div>
            {msg && (
                <div className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}>
                    {msg.text}
                </div>
            )}

            <div className="admin-card" style={{ marginBottom: 20 }}>
                <h2>Hugging Face Visual Classification</h2>
                <p style={{ color: 'var(--text-muted)', marginBottom: 16 }}>
                    Uses the Hugging Face Inference API to analyze video frames and images for content, then adds suggested tags to mature-flagged media. Requires an API key and FFmpeg for video frame extraction.
                </p>
                {status && (
                    <div className="admin-stats-grid" style={{ marginBottom: 12 }}>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{status.configured ? 'Yes' : 'No'}</span>
                            <span className="admin-stat-label">Configured</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{status.enabled ? 'On' : 'Off'}</span>
                            <span className="admin-stat-label">Enabled</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value" style={{ fontSize: 14 }}>{status.model || '—'}</span>
                            <span className="admin-stat-label">Model</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{status.rate_limit}</span>
                            <span className="admin-stat-label">Rate limit/min</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{status.max_frames}</span>
                            <span className="admin-stat-label">Max frames</span>
                        </div>
                        <div className="admin-stat-card">
                            <span className="admin-stat-value">{status.max_concurrent}</span>
                            <span className="admin-stat-label">Max concurrent</span>
                        </div>
                    </div>
                )}
            </div>

            <div className="admin-card" style={{ marginBottom: 20 }}>
                <h3>Run classification</h3>
                <p style={{ color: 'var(--text-muted)', marginBottom: 12 }}>
                    Classify a single file or all mature-flagged files in a directory. Tags are merged with existing ones.
                </p>
                <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                    <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
                        <input
                            type="text"
                            value={classifyPath}
                            onChange={e => setClassifyPath(e.target.value)}
                            placeholder="Absolute path to media file..."
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
                        <button
                            type="button"
                            className="admin-btn admin-btn-primary"
                            onClick={handleClassifyFile}
                            disabled={classifyFileLoading || !status?.configured}
                        >
                            {classifyFileLoading ? 'Classifying…' : 'Classify file'}
                        </button>
                    </div>
                    <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
                        <input
                            type="text"
                            value={classifyDirPath}
                            onChange={e => setClassifyDirPath(e.target.value)}
                            placeholder="Absolute path to directory..."
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
                        <button
                            type="button"
                            className="admin-btn admin-btn-primary"
                            onClick={handleClassifyDirectory}
                            disabled={classifyDirLoading || !status?.configured}
                        >
                            {classifyDirLoading ? 'Classifying…' : 'Classify directory'}
                        </button>
                    </div>
                </div>
                {status && !status.configured && (
                    <p style={{ color: 'var(--text-muted)', fontSize: 13, marginTop: 8 }}>
                        Set an API key below and save to enable classification.
                    </p>
                )}
            </div>

            <div className="admin-card">
                <h3>Settings</h3>
                <form onSubmit={handleSaveSettings}>
                    <div className="admin-form-group">
                        <label>
                            <input
                                type="checkbox"
                                checked={enabled}
                                onChange={e => setEnabled(e.target.checked)}
                            />
                            {' '}Enable Hugging Face classification
                        </label>
                    </div>
                    <div className="admin-form-group">
                        <label>API key</label>
                        <input
                            type="password"
                            className="admin-input"
                            value={apiKeyInput}
                            onChange={e => setApiKeyInput(e.target.value)}
                            placeholder={hf.api_key_set ? '•••••••• (leave blank to keep current)' : 'Enter Hugging Face API token'}
                            autoComplete="off"
                        />
                        {hf.api_key_set && !apiKeyInput && (
                            <span style={{ fontSize: 12, color: 'var(--text-muted)' }}> Current key is set.</span>
                        )}
                    </div>
                    <div className="admin-form-group">
                        <label>Model</label>
                        <input
                            type="text"
                            className="admin-input"
                            value={model}
                            onChange={e => setModel(e.target.value)}
                            placeholder="Salesforce/blip-image-captioning-large"
                        />
                    </div>
                    <div className="admin-form-group">
                        <label>Endpoint URL (optional)</label>
                        <input
                            type="text"
                            className="admin-input"
                            value={endpointUrl}
                            onChange={e => setEndpointUrl(e.target.value)}
                            placeholder="https://api-inference.huggingface.co"
                        />
                    </div>
                    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))', gap: 12 }}>
                        <div className="admin-form-group">
                            <label>Max frames</label>
                            <input
                                type="number"
                                className="admin-input"
                                min={1}
                                max={20}
                                value={maxFrames}
                                onChange={e => setMaxFrames(Number(e.target.value) || 1)}
                            />
                        </div>
                        <div className="admin-form-group">
                            <label>Rate limit (req/min)</label>
                            <input
                                type="number"
                                className="admin-input"
                                min={1}
                                max={120}
                                value={rateLimit}
                                onChange={e => setRateLimit(Number(e.target.value) || 1)}
                            />
                        </div>
                        <div className="admin-form-group">
                            <label>Timeout (sec)</label>
                            <input
                                type="number"
                                className="admin-input"
                                min={5}
                                max={120}
                                value={timeoutSecs}
                                onChange={e => setTimeoutSecs(Number(e.target.value) || 30)}
                            />
                        </div>
                        <div className="admin-form-group">
                            <label>Max concurrent</label>
                            <input
                                type="number"
                                className="admin-input"
                                min={1}
                                max={10}
                                value={maxConcurrent}
                                onChange={e => setMaxConcurrent(Number(e.target.value) || 1)}
                            />
                        </div>
                    </div>
                    <button type="submit" className="admin-btn admin-btn-primary" disabled={saving}>
                        {saving ? 'Saving…' : 'Save settings'}
                    </button>
                </form>
            </div>
        </div>
    )
}

// ── Consolidated tabs ────────────────────────────────────────────────────────

export function ContentTab() {
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

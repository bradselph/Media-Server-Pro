import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/api/endpoints'
import type { ScanResultItem } from '@/api/types'
import { errMsg } from './adminUtils'

type ReviewSortKey = 'name' | 'detected_at' | 'confidence'

function sortQueue(
    queue: ScanResultItem[],
    sortBy: ReviewSortKey,
    order: 'asc' | 'desc',
    search: string
) {
    return queue
        .filter((i) => !search || i.name.toLowerCase().includes(search.toLowerCase()))
        .sort((a, b) => {
            let cmp = 0
            switch (sortBy) {
                case 'name':
                    cmp = a.name.localeCompare(b.name)
                    break
                case 'detected_at':
                    cmp = (a.detected_at || '').localeCompare(b.detected_at || '')
                    break
                case 'confidence':
                    cmp = a.confidence - b.confidence
                    break
            }
            return order === 'desc' ? -cmp : cmp
        })
}

const thStyle: React.CSSProperties = { cursor: 'pointer', userSelect: 'none', whiteSpace: 'nowrap' }

function ReviewStatsCards({
    scanStats,
}: {
    scanStats: { total_scanned: number; mature_count: number; pending_review: number }
}) {
    return (
        <div className="admin-stats-grid" style={{ marginBottom: 16 }}>
            <div className="admin-stat-card">
                <span className="admin-stat-value">{scanStats.total_scanned.toLocaleString()}</span>
                <span className="admin-stat-label">Scanned</span>
            </div>
            <div className="admin-stat-card">
                <span className="admin-stat-value" style={{ color: '#ef4444' }}>
                    {scanStats.mature_count.toLocaleString()}
                </span>
                <span className="admin-stat-label">Flagged</span>
            </div>
            <div className="admin-stat-card">
                <span className="admin-stat-value" style={{ color: '#10b981' }}>
                    {(scanStats.total_scanned - scanStats.mature_count).toLocaleString()}
                </span>
                <span className="admin-stat-label">Clean</span>
            </div>
            <div className="admin-stat-card">
                <span className="admin-stat-value" style={{ color: '#f59e0b' }}>
                    {scanStats.pending_review.toLocaleString()}
                </span>
                <span className="admin-stat-label">Pending</span>
            </div>
        </div>
    )
}

function ReviewQueueRow({
    item,
    selected,
    onToggleSelect,
    processingPaths,
    onReject,
    onApprove,
    setMsg,
    onInvalidate,
}: {
    item: ScanResultItem
    selected: boolean
    onToggleSelect: () => void
    processingPaths: Set<string>
    onReject: () => void
    onApprove: () => void
    setMsg: (m: { type: 'success' | 'error'; text: string } | null) => void
    onInvalidate: () => void
}) {
    const processing = processingPaths.has(item.id)

    return (
        <tr>
            <td>
                <input type="checkbox" checked={selected} onChange={onToggleSelect} />
            </td>
            <td style={{ maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {item.name}
            </td>
            <td style={{ fontSize: 11, color: 'var(--text-muted)' }}>
                {item.detected_at ? new Date(item.detected_at).toLocaleDateString() : '—'}
            </td>
            <td>
                <div style={{ fontSize: 12 }}>{Math.round(item.confidence * 100)}%</div>
                <div className="admin-progress-bg" style={{ marginTop: 3 }}>
                    <div
                        className="admin-progress-fill"
                        style={{
                            width: `${item.confidence * 100}%`,
                            background:
                                item.confidence > 0.8 ? '#ef4444' : item.confidence > 0.5 ? '#f59e0b' : '#10b981',
                        }}
                    />
                </div>
            </td>
            <td style={{ maxWidth: 160, fontSize: 11, color: 'var(--text-muted)' }}>
                {(item.reasons ?? []).join(', ') || '—'}
            </td>
            <td>
                <div style={{ display: 'flex', gap: 6 }}>
                    <button
                        className="admin-btn admin-btn-success"
                        style={{ padding: '3px 7px' }}
                        title="Not mature"
                        disabled={processing}
                        onClick={() => {
                            onReject()
                            adminApi
                                .batchReview('reject', [item.id])
                                .then(onInvalidate)
                                .catch((err) => setMsg({ type: 'error', text: errMsg(err) }))
                                .finally(onApprove)
                        }}
                    >
                        {processing ? <i className="bi bi-arrow-repeat" /> : <i className="bi bi-check-lg" />}
                    </button>
                    <button
                        className="admin-btn admin-btn-warning"
                        style={{ padding: '3px 7px' }}
                        title="Confirm mature"
                        disabled={processing}
                        onClick={() => {
                            onReject()
                            adminApi
                                .batchReview('approve', [item.id])
                                .then(onInvalidate)
                                .catch((err) => setMsg({ type: 'error', text: errMsg(err) }))
                                .finally(onApprove)
                        }}
                    >
                        <i className="bi bi-exclamation-triangle-fill" />
                    </button>
                    <button
                        className="admin-btn admin-btn-danger"
                        style={{ padding: '3px 7px' }}
                        title="Reject and remove from library"
                        disabled={processing}
                        onClick={() => {
                            if (!window.confirm(`Remove "${item.name}" from library?`)) return
                            onReject()
                            adminApi
                                .rejectContent(item.id)
                                .then(onInvalidate)
                                .catch((err) => setMsg({ type: 'error', text: errMsg(err) }))
                                .finally(onApprove)
                        }}
                    >
                        <i className="bi bi-trash-fill" />
                    </button>
                </div>
            </td>
        </tr>
    )
}

function ReviewQueueTable({
    sortedQueue,
    selected,
    setSelected,
    processingPaths,
    setProcessingPaths,
    setMsg,
    onInvalidate,
    sortBy,
    setSortBy,
    sortOrder,
    setSortOrder,
    search,
    setSearch,
}: {
    sortedQueue: ScanResultItem[]
    selected: Set<string>
    setSelected: (s: Set<string> | ((prev: Set<string>) => Set<string>)) => void
    processingPaths: Set<string>
    setProcessingPaths: (fn: (prev: Set<string>) => Set<string>) => void
    setMsg: (m: { type: 'success' | 'error'; text: string } | null) => void
    onInvalidate: () => void
    sortBy: ReviewSortKey
    setSortBy: (k: ReviewSortKey) => void
    sortOrder: 'asc' | 'desc'
    setSortOrder: (o: 'asc' | 'desc' | ((p: 'asc' | 'desc') => 'asc' | 'desc')) => void
    search: string
    setSearch: (s: string) => void
}) {
    const handleSort = (col: ReviewSortKey) => {
        if (sortBy === col) setSortOrder((p) => (p === 'asc' ? 'desc' : 'asc'))
        else {
            setSortBy(col)
            setSortOrder('asc')
        }
    }
    const sortIndicator = (col: ReviewSortKey) =>
        sortBy !== col ? (
            <span style={{ opacity: 0.3, marginLeft: 4 }}>&#x21C5;</span>
        ) : (
            <span style={{ marginLeft: 4 }}>{sortOrder === 'asc' ? '\u25B2' : '\u25BC'}</span>
        )

    return (
        <>
            <div style={{ marginBottom: 8 }}>
                <input
                    type="text"
                    placeholder="Search files..."
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    style={{
                        padding: '6px 10px',
                        border: '1px solid var(--border-color)',
                        borderRadius: 6,
                        background: 'var(--input-bg)',
                        color: 'var(--text-color)',
                        fontSize: 13,
                        width: '100%',
                        maxWidth: 300,
                    }}
                />
            </div>
            <div className="admin-table-wrapper">
                <table className="admin-table">
                    <thead>
                        <tr>
                            <th>
                                <input
                                    type="checkbox"
                                    onChange={(e) =>
                                        setSelected(e.target.checked ? new Set(sortedQueue.map((i) => i.id)) : new Set())
                                    }
                                />
                            </th>
                            <th style={thStyle} onClick={() => handleSort('name')}>
                                File{sortIndicator('name')}
                            </th>
                            <th style={thStyle} onClick={() => handleSort('detected_at')}>
                                Detected{sortIndicator('detected_at')}
                            </th>
                            <th style={thStyle} onClick={() => handleSort('confidence')}>
                                Confidence{sortIndicator('confidence')}
                            </th>
                            <th>Reasons</th>
                            <th>Actions</th>
                        </tr>
                    </thead>
                    <tbody>
                        {sortedQueue.map((item) => (
                            <ReviewQueueRow
                                key={item.id}
                                item={item}
                                selected={selected.has(item.id)}
                                onToggleSelect={() => {
                                    setSelected((prev) => {
                                        const next = new Set(prev)
                                        if (next.has(item.id)) next.delete(item.id)
                                        else next.add(item.id)
                                        return next
                                    })
                                }}
                                processingPaths={processingPaths}
                                onReject={() => setProcessingPaths((prev) => new Set(prev).add(item.id))}
                                onApprove={() =>
                                    setProcessingPaths((prev) => {
                                        const next = new Set(prev)
                                        next.delete(item.id)
                                        return next
                                    })
                                }
                                setMsg={setMsg}
                                onInvalidate={onInvalidate}
                            />
                        ))}
                    </tbody>
                </table>
            </div>
        </>
    )
}

function MessageAlert({ msg }: { msg: { type: 'success' | 'error'; text: string } | null }) {
    if (!msg) return null
    const variant = msg.type === 'success' ? 'success' : 'danger'
    return <div className={`admin-alert admin-alert-${variant}`}>{msg.text}</div>
}

function ReviewActions({
    scanning,
    onRunScan,
    queueLength,
    selectedCount,
    onBatchReject,
    onBatchApprove,
    onClearQueue,
}: {
    scanning: boolean
    onRunScan: () => void
    queueLength: number
    selectedCount: number
    onBatchReject: () => void
    onBatchApprove: () => void
    onClearQueue: () => void
}) {
    return (
        <div className="admin-action-row">
            <button className="admin-btn admin-btn-primary" onClick={onRunScan} disabled={scanning}>
                {scanning ? (
                    <>
                        <i className="bi bi-arrow-repeat" /> Scanning...
                    </>
                ) : (
                    <>
                        <i className="bi bi-search" /> Run Scan
                    </>
                )}
            </button>
            {queueLength > 0 && (
                <>
                    <button className="admin-btn admin-btn-success" onClick={onBatchReject}>
                        <i className="bi bi-check-circle" /> Not Mature {selectedCount > 0 ? `${selectedCount}` : 'All'}
                    </button>
                    <button className="admin-btn admin-btn-warning" onClick={onBatchApprove}>
                        <i className="bi bi-exclamation-triangle-fill" /> Confirm Mature{' '}
                        {selectedCount > 0 ? `${selectedCount}` : 'All'}
                    </button>
                    <button className="admin-btn admin-btn-danger" onClick={onClearQueue}>
                        <i className="bi bi-trash-fill" /> Clear Queue
                    </button>
                </>
            )}
        </div>
    )
}

function ReviewMainContent({
    isLoading,
    queueLength,
    sortedQueue,
    selected,
    setSelected,
    processingPaths,
    setProcessingPaths,
    setMsg,
    onInvalidate,
    sortBy,
    setSortBy,
    sortOrder,
    setSortOrder,
    search,
    setSearch,
}: {
    isLoading: boolean
    queueLength: number
    sortedQueue: ScanResultItem[]
    selected: Set<string>
    setSelected: (s: Set<string> | ((prev: Set<string>) => Set<string>)) => void
    processingPaths: Set<string>
    setProcessingPaths: (fn: (prev: Set<string>) => Set<string>) => void
    setMsg: (m: { type: 'success' | 'error'; text: string } | null) => void
    onInvalidate: () => void
    sortBy: ReviewSortKey
    setSortBy: (k: ReviewSortKey) => void
    sortOrder: 'asc' | 'desc'
    setSortOrder: (o: 'asc' | 'desc' | ((p: 'asc' | 'desc') => 'asc' | 'desc')) => void
    search: string
    setSearch: (s: string) => void
}) {
    if (isLoading) {
        return <p style={{ color: 'var(--text-muted)' }}>Loading queue...</p>
    }
    if (queueLength === 0) {
        return (
            <div className="admin-card" style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>
                <div style={{ fontSize: 40, marginBottom: 12 }}>
                    <i className="bi bi-check-circle" />
                </div>
                <h3>Queue Empty</h3>
                <p>No content pending review.</p>
            </div>
        )
    }
    return (
        <ReviewQueueTable
            sortedQueue={sortedQueue}
            selected={selected}
            setSelected={setSelected}
            processingPaths={processingPaths}
            setProcessingPaths={setProcessingPaths}
            setMsg={setMsg}
            onInvalidate={onInvalidate}
            sortBy={sortBy}
            setSortBy={setSortBy}
            sortOrder={sortOrder}
            setSortOrder={setSortOrder}
            search={search}
            setSearch={setSearch}
        />
    )
}

export function ContentReviewTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [selected, setSelected] = useState<Set<string>>(new Set())
    const [scanning, setScanning] = useState(false)
    const [processingPaths, setProcessingPaths] = useState<Set<string>>(new Set())
    const [reviewSortBy, setReviewSortBy] = useState<ReviewSortKey>('confidence')
    const [reviewSortOrder, setReviewSortOrder] = useState<'asc' | 'desc'>('desc')
    const [reviewSearch, setReviewSearch] = useState('')

    const { data: scanStats } = useQuery({
        queryKey: ['scanner-stats'],
        queryFn: () => adminApi.getScannerStats(),
    })

    const { data: queue = [], isLoading } = useQuery({
        queryKey: ['review-queue'],
        queryFn: () => adminApi.getReviewQueue(),
    })

    const sortedQueue = sortQueue(queue, reviewSortBy, reviewSortOrder, reviewSearch)

    async function handleBatchAction(action: 'approve' | 'reject') {
        const ids = selected.size > 0 ? Array.from(selected) : queue.map((i) => i.id)
        if (!ids.length || !window.confirm(`Apply "${action}" to ${ids.length} item(s)?`)) return
        try {
            await adminApi.batchReview(action, ids)
            setSelected(new Set())
            setMsg({ type: 'success', text: `"${action}" applied to ${ids.length} item(s).` })
            await queryClient.invalidateQueries({ queryKey: ['review-queue'] })
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        }
    }

    async function handleRunScan() {
        setScanning(true)
        try {
            await adminApi.runScan()
            setMsg({ type: 'success', text: 'Content scan triggered.' })
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        } finally {
            setScanning(false)
        }
    }

    function handleClearQueue() {
        if (!window.confirm('Clear review queue?')) return
        adminApi
            .clearReviewQueue()
            .then(() => {
                void queryClient.invalidateQueries({ queryKey: ['review-queue'] })
                setMsg({ type: 'success', text: 'Queue cleared.' })
            })
            .catch((err) => setMsg({ type: 'error', text: errMsg(err) }))
    }

    return (
        <div>
            <MessageAlert msg={msg} />
            {scanStats && <ReviewStatsCards scanStats={scanStats} />}
            <ReviewActions
                scanning={scanning}
                onRunScan={handleRunScan}
                queueLength={queue.length}
                selectedCount={selected.size}
                onBatchReject={() => handleBatchAction('reject')}
                onBatchApprove={() => handleBatchAction('approve')}
                onClearQueue={handleClearQueue}
            />
            <ReviewMainContent
                isLoading={isLoading}
                queueLength={queue.length}
                sortedQueue={sortedQueue}
                selected={selected}
                setSelected={setSelected}
                processingPaths={processingPaths}
                setProcessingPaths={setProcessingPaths}
                setMsg={setMsg}
                onInvalidate={() => queryClient.invalidateQueries({ queryKey: ['review-queue'] })}
                sortBy={reviewSortBy}
                setSortBy={setReviewSortBy}
                sortOrder={reviewSortOrder}
                setSortOrder={setReviewSortOrder}
                search={reviewSearch}
                setSearch={setReviewSearch}
            />
        </div>
    )
}

import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/api/endpoints'
import type { DiscoverySuggestion } from '@/api/types'
import { errMsg } from './adminUtils'

const inputStyle = {
    flex: 1,
    padding: '6px 10px',
    border: '1px solid var(--border-color)',
    borderRadius: 6,
    background: 'var(--input-bg)',
    color: 'var(--text-color)',
    fontSize: 13,
} as const

export function DiscoveryTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [scanDir, setScanDir] = useState('')
    const [scanning, setScanning] = useState(false)

    const { data: suggestions = [] } = useQuery<DiscoverySuggestion[]>({
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
            setMsg({ type: 'success', text: 'Discovery scan complete. Refresh suggestions below.' })
            await queryClient.invalidateQueries({ queryKey: ['admin-discovery-suggestions'] })
            setScanDir('')
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        } finally {
            setScanning(false)
        }
    }

    async function handleApply(originalPath: string) {
        try {
            await adminApi.applyDiscoverySuggestion(originalPath)
            setMsg({ type: 'success', text: 'Suggestion applied.' })
            await queryClient.invalidateQueries({ queryKey: ['admin-discovery-suggestions'] })
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        }
    }

    async function handleDismiss(originalPath: string) {
        try {
            await adminApi.dismissDiscoverySuggestion(originalPath)
            await queryClient.invalidateQueries({ queryKey: ['admin-discovery-suggestions'] })
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        }
    }

    return (
        <div>
            {msg && (
                <div className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}>
                    {msg.text}
                </div>
            )}

            <div className="admin-card">
                <h2>Scan for New Media</h2>
                <p style={{ fontSize: 13, color: 'var(--text-muted)', marginBottom: 12 }}>
                    Scan a directory for media files and get suggestions for organizing them.
                </p>
                <form onSubmit={handleScan} style={{ display: 'flex', gap: 8 }}>
                    <input
                        type="text"
                        value={scanDir}
                        onChange={(e) => setScanDir(e.target.value)}
                        placeholder="Directory path to scan..."
                        style={inputStyle}
                    />
                    <button
                        type="submit"
                        className="admin-btn admin-btn-primary"
                        disabled={scanning || !scanDir.trim()}
                    >
                        <i className="bi bi-search" /> {scanning ? 'Scanning...' : 'Scan Directory'}
                    </button>
                </form>
            </div>

            <div className="admin-card">
                <h2>
                    Pending Suggestions{' '}
                    <span style={{ fontSize: 13, fontWeight: 400, color: 'var(--text-muted)' }}>
                        ({suggestions.length})
                    </span>
                </h2>
                {suggestions.length === 0 ? (
                    <div style={{ textAlign: 'center', padding: '40px 0', color: 'var(--text-muted)' }}>
                        <i className="bi bi-check-circle" style={{ fontSize: 32 }} />
                        <p style={{ marginTop: 8 }}>No pending suggestions. Run a directory scan to get started.</p>
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
                                {[...suggestions]
                                    .sort((a, b) => b.confidence - a.confidence)
                                    .map((s) => (
                                        <tr key={s.original_path}>
                                            <td
                                                style={{
                                                    maxWidth: 180,
                                                    overflow: 'hidden',
                                                    textOverflow: 'ellipsis',
                                                    whiteSpace: 'nowrap',
                                                }}
                                                title={s.original_path}
                                            >
                                                {s.original_path.split(/[\\/]/).pop()}
                                            </td>
                                            <td style={{ fontWeight: 500 }}>{s.suggested_name}</td>
                                            <td>{s.type}</td>
                                            <td>{(s.confidence * 100).toFixed(0)}%</td>
                                            <td>
                                                <div style={{ display: 'flex', gap: 6 }}>
                                                    <button
                                                        className="admin-btn admin-btn-success"
                                                        style={{ padding: '3px 8px' }}
                                                        onClick={() => handleApply(s.original_path)}
                                                    >
                                                        <i className="bi bi-check-lg" /> Apply
                                                    </button>
                                                    <button
                                                        className="admin-btn"
                                                        style={{ padding: '3px 8px' }}
                                                        onClick={() => handleDismiss(s.original_path)}
                                                    >
                                                        <i className="bi bi-x-lg" /> Dismiss
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

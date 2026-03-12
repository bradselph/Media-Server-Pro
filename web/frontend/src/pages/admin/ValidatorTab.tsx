import {useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {adminApi} from '@/api/endpoints'
import type {ValidationResult, ValidatorStats} from '@/api/types'
import {errMsg} from './adminUtils'

export function ValidatorTab() {
    const [mediaId, setMediaId] = useState('')
    const [validateLoading, setValidateLoading] = useState(false)
    const [fixLoading, setFixLoading] = useState(false)
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [lastResult, setLastResult] = useState<ValidationResult | null>(null)

    const {data: stats} = useQuery<ValidatorStats>({
        queryKey: ['admin-validator-stats'],
        queryFn: () => adminApi.getValidatorStats(),
    })

    async function handleValidate(e: React.FormEvent) {
        e.preventDefault()
        if (!mediaId.trim()) return
        setValidateLoading(true)
        setMsg(null)
        setLastResult(null)
        try {
            const result = await adminApi.validateMedia(mediaId.trim())
            setLastResult(result)
            setMsg({type: 'success', text: result.error ? `Validation completed with issues` : 'Validation passed.'})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setValidateLoading(false)
        }
    }

    async function handleFix(e: React.FormEvent) {
        e.preventDefault()
        if (!mediaId.trim()) return
        setFixLoading(true)
        setMsg(null)
        setLastResult(null)
        try {
            const result = await adminApi.fixMedia(mediaId.trim())
            setLastResult(result)
            setMsg({type: 'success', text: result.error ? `Fix attempted: ${result.error}` : 'Fix completed.'})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setFixLoading(false)
        }
    }

    return (
        <div className="admin-card">
            <h2>Media Validator</h2>
            <p style={{color: 'var(--text-muted)', marginBottom: 16, fontSize: 14}}>
                Validate or attempt to fix media files by ID (codec/container checks via FFprobe).
            </p>
            {stats && (
                <div className="admin-stats-grid" style={{marginBottom: 16}}>
                    <div className="admin-stat-card">
                        <span className="admin-stat-value">{stats.total.toLocaleString()}</span>
                        <span className="admin-stat-label">Total</span>
                    </div>
                    <div className="admin-stat-card">
                        <span className="admin-stat-value">{stats.validated.toLocaleString()}</span>
                        <span className="admin-stat-label">Validated</span>
                    </div>
                    <div className="admin-stat-card">
                        <span className="admin-stat-value">{stats.needs_fix.toLocaleString()}</span>
                        <span className="admin-stat-label">Needs fix</span>
                    </div>
                    <div className="admin-stat-card">
                        <span className="admin-stat-value">{stats.fixed.toLocaleString()}</span>
                        <span className="admin-stat-label">Fixed</span>
                    </div>
                    <div className="admin-stat-card">
                        <span className="admin-stat-value"
                              style={{color: (stats.failed + stats.unsupported) > 0 ? '#ef4444' : 'inherit'}}>
                            {(stats.failed + stats.unsupported).toLocaleString()}
                        </span>
                        <span className="admin-stat-label">Failed / Unsupported</span>
                    </div>
                </div>
            )}
            {msg && (
                <div className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}
                     style={{marginBottom: 12}}>
                    {msg.text}
                </div>
            )}
            <form onSubmit={handleValidate} style={{display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center', marginBottom: 16}}>
                <input
                    type="text"
                    value={mediaId}
                    onChange={e => setMediaId(e.target.value)}
                    placeholder="Media ID..."
                    style={{
                        flex: '1 1 200px',
                        minWidth: 180,
                        padding: '6px 10px',
                        border: '1px solid var(--border-color)',
                        borderRadius: 6,
                        background: 'var(--input-bg)',
                        color: 'var(--text-color)',
                        fontSize: 13,
                    }}
                />
                <button type="submit" className="admin-btn admin-btn-primary" disabled={validateLoading || !mediaId.trim()}>
                    <i className="bi bi-check2-circle"/> {validateLoading ? 'Validating...' : 'Validate'}
                </button>
                <button type="button" className="admin-btn admin-btn-warning" disabled={fixLoading || !mediaId.trim()} onClick={handleFix}>
                    <i className="bi bi-wrench"/> {fixLoading ? 'Fixing...' : 'Fix'}
                </button>
            </form>
            {lastResult && (
                <div style={{
                    padding: 12,
                    background: 'var(--surface-alt)',
                    borderRadius: 8,
                    fontSize: 13,
                    border: '1px solid var(--border-color)',
               }}>
                    <div><strong>Status:</strong> {lastResult.status}</div>
                    {lastResult.error && <div style={{color: 'var(--danger)'}}>{lastResult.error}</div>}
                    {lastResult.issues && lastResult.issues.length > 0 && (
                        <ul style={{margin: '8px 0 0', paddingLeft: 20}}>{lastResult.issues.map((i, k) => <li key={k}>{i}</li>)}</ul>
                    )}
                    {(lastResult.video_codec ?? lastResult.audio_codec) && (
                        <div style={{marginTop: 8}}>
                            Video: {lastResult.video_codec ?? '—'} / Audio: {lastResult.audio_codec ?? '—'}
                        </div>
                    )}
                </div>
            )}
        </div>
    )
}

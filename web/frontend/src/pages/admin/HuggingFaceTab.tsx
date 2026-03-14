import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/api/endpoints'
import type { ClassifyStatus, ClassifyStats, ClassifiedItem } from '@/api/types'
import { errMsg } from './adminUtils'

const TEXT_MUTED = TEXT_MUTED

interface HuggingFaceConfigBlock {
    enabled?: boolean
    api_key_set?: boolean
    model?: string
    endpoint_url?: string
    max_frames?: number
    timeout_secs?: number
    rate_limit?: number
    max_concurrent?: number
}

/* ─── Status cards ────────────────────────────────────────────── */

function HuggingFaceStatusCards({ status }: { status: ClassifyStatus }) {
    return (
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
                <span className="admin-stat-value" style={{ fontSize: 14 }}>
                    {status.model ?? '\u2014'}
                </span>
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
    )
}

/* ─── Classification progress ─────────────────────────────────── */

function ClassificationProgress({ stats }: { stats: ClassifyStats }) {
    const pct = stats.mature_total > 0
        ? Math.round((stats.mature_classified / stats.mature_total) * 100)
        : 0

    return (
        <div className="admin-card" style={{ marginBottom: 20 }}>
            <h3>Classification progress</h3>
            <div className="admin-stats-grid" style={{ marginBottom: 16 }}>
                <div className="admin-stat-card">
                    <span className="admin-stat-value">{stats.total_media}</span>
                    <span className="admin-stat-label">Total media</span>
                </div>
                <div className="admin-stat-card">
                    <span className="admin-stat-value">{stats.mature_total}</span>
                    <span className="admin-stat-label">Mature items</span>
                </div>
                <div className="admin-stat-card">
                    <span className="admin-stat-value">{stats.mature_classified}</span>
                    <span className="admin-stat-label">Classified</span>
                </div>
                <div className="admin-stat-card">
                    <span className="admin-stat-value">{stats.mature_pending}</span>
                    <span className="admin-stat-label">Pending</span>
                </div>
            </div>
            {stats.mature_total > 0 && (
                <div>
                    <div style={{
                        display: 'flex',
                        justifyContent: 'space-between',
                        fontSize: 12,
                        color: TEXT_MUTED,
                        marginBottom: 4,
                    }}>
                        <span>{stats.mature_classified} of {stats.mature_total} classified</span>
                        <span>{pct}%</span>
                    </div>
                    <div style={{
                        height: 8,
                        borderRadius: 4,
                        background: 'var(--border-color)',
                        overflow: 'hidden',
                    }}>
                        <div style={{
                            height: '100%',
                            width: `${pct}%`,
                            borderRadius: 4,
                            background: pct === 100
                                ? 'var(--success-color, #22c55e)'
                                : 'var(--primary-color, #3b82f6)',
                            transition: 'width 0.3s ease',
                        }} />
                    </div>
                </div>
            )}
        </div>
    )
}

/* ─── Background task control ─────────────────────────────────── */

function formatTime(iso?: string): string {
    if (!iso) return '\u2014'
    const d = new Date(iso)
    if (isNaN(d.getTime()) || d.getFullYear() <= 1) return '\u2014'
    return d.toLocaleString()
}

function BackgroundTaskControl({
    status,
    onRunTask,
    onRunAllPending,
    taskLoading,
    allPendingLoading,
    pendingCount,
}: {
    status: ClassifyStatus
    onRunTask: () => void
    onRunAllPending: () => void
    taskLoading: boolean
    allPendingLoading: boolean
    pendingCount: number
}) {
    let runTaskLabel: string
    if (taskLoading) runTaskLabel = 'Starting...'
    else if (status.task_running) runTaskLabel = 'Task running...'
    else runTaskLabel = 'Run scheduled task now'

    return (
        <div className="admin-card" style={{ marginBottom: 20 }}>
            <h3>Background task</h3>
            <p style={{ color: TEXT_MUTED, marginBottom: 12, fontSize: 13 }}>
                The <strong>hf-classification</strong> task runs every 12 hours and classifies
                all mature items that have no tags yet.
            </p>
            <div style={{ display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '6px 16px', fontSize: 13, marginBottom: 16 }}>
                <span style={{ color: TEXT_MUTED }}>Status:</span>
                <span>{status.task_running
                    ? <span style={{ color: 'var(--warning-color, #f59e0b)', fontWeight: 600 }}>Running</span>
                    : <span style={{ color: 'var(--text-color)' }}>Idle</span>}
                </span>
                <span style={{ color: TEXT_MUTED }}>Last run:</span>
                <span>{formatTime(status.task_last_run)}</span>
                <span style={{ color: TEXT_MUTED }}>Next run:</span>
                <span>{formatTime(status.task_next_run)}</span>
                {status.task_last_error && (
                    <>
                        <span style={{ color: TEXT_MUTED }}>Last error:</span>
                        <span style={{ color: 'var(--danger-color, #ef4444)' }}>{status.task_last_error}</span>
                    </>
                )}
                <span style={{ color: TEXT_MUTED }}>Enabled:</span>
                <span>{status.task_enabled ? 'Yes' : 'No'}</span>
            </div>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                <button
                    type="button"
                    className="admin-btn admin-btn-primary"
                    onClick={onRunTask}
                    disabled={taskLoading || status.task_running === true}
                >
                    {runTaskLabel}
                </button>
                <button
                    type="button"
                    className="admin-btn admin-btn-primary"
                    onClick={onRunAllPending}
                    disabled={allPendingLoading || pendingCount === 0}
                    title={pendingCount === 0 ? 'No pending items' : `Classify ${pendingCount} pending items`}
                >
                    {allPendingLoading ? 'Starting...' : `Classify all pending (${pendingCount})`}
                </button>
            </div>
        </div>
    )
}

/* ─── Recently classified items ───────────────────────────────── */

function RecentlyClassifiedTable({
    items,
    onClearTags,
    clearingId,
}: {
    items: ClassifiedItem[]
    onClearTags: (id: string) => void
    clearingId: string | null
}) {
    if (items.length === 0) {
        return (
            <div className="admin-card" style={{ marginBottom: 20 }}>
                <h3>Recently classified</h3>
                <p style={{ color: TEXT_MUTED, fontSize: 13 }}>
                    No classified items yet. Run classification on mature content to see results here.
                </p>
            </div>
        )
    }

    return (
        <div className="admin-card" style={{ marginBottom: 20 }}>
            <h3>Recently classified ({items.length})</h3>
            <div style={{ overflowX: 'auto' }}>
                <table className="admin-table" style={{ width: '100%', fontSize: 13 }}>
                    <thead>
                        <tr>
                            <th style={{ textAlign: 'left' }}>Name</th>
                            <th style={{ textAlign: 'left' }}>Tags</th>
                            <th style={{ textAlign: 'right' }}>Score</th>
                            <th style={{ textAlign: 'right' }}>Actions</th>
                        </tr>
                    </thead>
                    <tbody>
                        {items.map((item) => (
                            <tr key={item.id}>
                                <td style={{ maxWidth: 250, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                    {item.name}
                                </td>
                                <td>
                                    <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
                                        {item.tags.map((tag) => (
                                            <span
                                                key={tag}
                                                style={{
                                                    display: 'inline-block',
                                                    padding: '1px 6px',
                                                    borderRadius: 4,
                                                    background: 'var(--tag-bg, rgba(59,130,246,0.15))',
                                                    color: 'var(--tag-color, #60a5fa)',
                                                    fontSize: 11,
                                                }}
                                            >
                                                {tag}
                                            </span>
                                        ))}
                                    </div>
                                </td>
                                <td style={{ textAlign: 'right', fontFamily: 'monospace' }}>
                                    {(item.mature_score * 100).toFixed(0)}%
                                </td>
                                <td style={{ textAlign: 'right' }}>
                                    <button
                                        type="button"
                                        className="admin-btn"
                                        style={{ fontSize: 11, padding: '2px 8px' }}
                                        onClick={() => { onClearTags(item.id); }}
                                        disabled={clearingId === item.id}
                                    >
                                        {clearingId === item.id ? 'Clearing...' : 'Clear tags'}
                                    </button>
                                </td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
        </div>
    )
}

/* ─── Settings form (unchanged logic) ─────────────────────────── */

const inputBaseStyle = {
    padding: '6px 10px',
    border: '1px solid var(--border-color)',
    borderRadius: 6,
    background: 'var(--input-bg)',
    color: 'var(--text-color)',
    fontSize: 13,
}

function buildHuggingFaceConfigUpdates(params: {
    enabled: boolean
    model: string
    maxFrames: number
    rateLimit: number
    timeoutSecs: number
    maxConcurrent: number
    endpointUrl: string
    apiKeyInput: string
}): Record<string, string | number | boolean> {
    const frames = Math.max(1, Math.min(20, Number(params.maxFrames) || 3))
    const rate = Math.max(1, Math.min(120, Number(params.rateLimit) || 30))
    const timeout = Math.max(5, Math.min(300, Number(params.timeoutSecs) || 30))
    const concurrent = Math.max(1, Math.min(10, Number(params.maxConcurrent) || 2))
    const updates: Record<string, string | number | boolean> = {
        'huggingface.enabled': params.enabled,
        'huggingface.model': (params.model?.trim()) || 'Salesforce/blip-image-captioning-large',
        'huggingface.max_frames': frames,
        'huggingface.rate_limit': rate,
        'huggingface.timeout_secs': timeout,
        'huggingface.max_concurrent': concurrent,
        'features.enable_huggingface': params.enabled,
        'huggingface.endpoint_url': params.endpointUrl.trim(),
    }
    if (params.apiKeyInput.trim()) updates['huggingface.api_key'] = params.apiKeyInput.trim()
    return updates
}

function HuggingFaceSettingsForm({
    enabled,
    setEnabled,
    apiKeyInput,
    setApiKeyInput,
    apiKeySet,
    model,
    setModel,
    endpointUrl,
    setEndpointUrl,
    maxFrames,
    setMaxFrames,
    rateLimit,
    setRateLimit,
    timeoutSecs,
    setTimeoutSecs,
    maxConcurrent,
    setMaxConcurrent,
    onSave,
    saving,
}: {
    enabled: boolean
    setEnabled: (v: boolean) => void
    apiKeyInput: string
    setApiKeyInput: (v: string) => void
    apiKeySet: boolean
    model: string
    setModel: (v: string) => void
    endpointUrl: string
    setEndpointUrl: (v: string) => void
    maxFrames: number
    setMaxFrames: (v: number) => void
    rateLimit: number
    setRateLimit: (v: number) => void
    timeoutSecs: number
    setTimeoutSecs: (v: number) => void
    maxConcurrent: number
    setMaxConcurrent: (v: number) => void
    onSave: (e: React.FormEvent) => void
    saving: boolean
}) {
    return (
        <form onSubmit={onSave}>
            <div className="admin-form-group">
                <label>
                    <input type="checkbox" checked={enabled} onChange={(e) => { setEnabled(e.target.checked); }} /> Enable
                    Hugging Face classification
                </label>
            </div>
            <div className="admin-form-group">
                <label>API key</label>
                <input
                    type="password"
                    className="admin-input"
                    value={apiKeyInput}
                    onChange={(e) => { setApiKeyInput(e.target.value); }}
                    placeholder={
                        apiKeySet ? '\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022 (leave blank to keep current)' : 'Enter Hugging Face API token'
                    }
                    autoComplete="off"
                />
                {apiKeySet && !apiKeyInput && (
                    <span style={{ fontSize: 12, color: TEXT_MUTED }}> Current key is set.</span>
                )}
            </div>
            <div className="admin-form-group">
                <label>Model</label>
                <input
                    type="text"
                    className="admin-input"
                    value={model}
                    onChange={(e) => { setModel(e.target.value); }}
                    placeholder="Salesforce/blip-image-captioning-large"
                />
            </div>
            <div className="admin-form-group">
                <label>Endpoint URL (optional)</label>
                <input
                    type="text"
                    className="admin-input"
                    value={endpointUrl}
                    onChange={(e) => { setEndpointUrl(e.target.value); }}
                    placeholder="https://api-inference.huggingface.co"
                />
            </div>
            <div
                style={{
                    display: 'grid',
                    gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))',
                    gap: 12,
                }}
            >
                <div className="admin-form-group">
                    <label>Max frames</label>
                    <input
                        type="number"
                        className="admin-input"
                        min={1}
                        max={20}
                        value={maxFrames}
                        onChange={(e) => { setMaxFrames(Number(e.target.value) || 1); }}
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
                        onChange={(e) => { setRateLimit(Number(e.target.value) || 1); }}
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
                        onChange={(e) => { setTimeoutSecs(Number(e.target.value) || 30); }}
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
                        onChange={(e) => { setMaxConcurrent(Number(e.target.value) || 1); }}
                    />
                </div>
            </div>
            <button type="submit" className="admin-btn admin-btn-primary" disabled={saving}>
                {saving ? 'Saving\u2026' : 'Save settings'}
            </button>
        </form>
    )
}

/* ─── Run classification section ──────────────────────────────── */

function ClassifyPathRow({
    value,
    onChange,
    placeholder,
    onAction,
    loading,
    disabled,
    actionLabel,
}: {
    value: string
    onChange: (v: string) => void
    placeholder: string
    onAction: () => void
    loading: boolean
    disabled: boolean
    actionLabel: string
}) {
    return (
        <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
            <input
                type="text"
                value={value}
                onChange={(e) => { onChange(e.target.value); }}
                placeholder={placeholder}
                style={{ flex: 1, minWidth: 200, ...inputBaseStyle }}
            />
            <button
                type="button"
                className="admin-btn admin-btn-primary"
                onClick={onAction}
                disabled={disabled}
            >
                {loading ? 'Classifying\u2026' : actionLabel}
            </button>
        </div>
    )
}

function HuggingFaceRunClassification({
    classifyPath,
    setClassifyPath,
    classifyDirPath,
    setClassifyDirPath,
    onClassifyFile,
    onClassifyDirectory,
    fileLoading,
    dirLoading,
    status,
}: {
    classifyPath: string
    setClassifyPath: (v: string) => void
    classifyDirPath: string
    setClassifyDirPath: (v: string) => void
    onClassifyFile: () => void
    onClassifyDirectory: () => void
    fileLoading: boolean
    dirLoading: boolean
    status: ClassifyStatus | undefined
}) {
    const configured = status?.configured
    return (
        <div className="admin-card" style={{ marginBottom: 20 }}>
            <h3>Manual classification</h3>
            <p style={{ color: TEXT_MUTED, marginBottom: 12 }}>
                Classify a single file or all mature-flagged files in a directory. Path must be under your
                configured media directories (Videos, Music, Uploads). Tags are merged with existing ones.
            </p>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                <ClassifyPathRow
                    value={classifyPath}
                    onChange={setClassifyPath}
                    placeholder="File path (absolute or relative to media dirs)..."
                    onAction={onClassifyFile}
                    loading={fileLoading}
                    disabled={fileLoading || !configured || !classifyPath.trim()}
                    actionLabel="Classify file"
                />
                <ClassifyPathRow
                    value={classifyDirPath}
                    onChange={setClassifyDirPath}
                    placeholder="Directory path (absolute or relative to media dirs)..."
                    onAction={onClassifyDirectory}
                    loading={dirLoading}
                    disabled={dirLoading || !configured || !classifyDirPath.trim()}
                    actionLabel="Classify directory"
                />
            </div>
            {configured === false && (
                <p style={{ color: TEXT_MUTED, marginTop: 8, fontSize: 13 }}>
                    Set an API key below and save to enable classification.
                </p>
            )}
            {configured && (
                <p style={{ color: TEXT_MUTED, marginTop: 8, fontSize: 12 }}>
                    Supported: video (mp4, mkv, avi, etc.) and image (jpg, png, webp) files.
                </p>
            )}
        </div>
    )
}

/* ─── Main hook ───────────────────────────────────────────────── */

function useHuggingFaceTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [classifyPath, setClassifyPath] = useState('')
    const [classifyDirPath, setClassifyDirPath] = useState('')
    const [classifyFileLoading, setClassifyFileLoading] = useState(false)
    const [classifyDirLoading, setClassifyDirLoading] = useState(false)
    const [taskLoading, setTaskLoading] = useState(false)
    const [allPendingLoading, setAllPendingLoading] = useState(false)
    const [clearingId, setClearingId] = useState<string | null>(null)
    const [saving, setSaving] = useState(false)
    const [apiKeyInput, setApiKeyInput] = useState('')

    const { data: status } = useQuery({
        queryKey: ['classify-status'],
        queryFn: () => adminApi.getClassifyStatus(),
        refetchInterval: (query) => query.state.data?.task_running ? 5000 : false,
    })

    const { data: stats } = useQuery({
        queryKey: ['classify-stats'],
        queryFn: () => adminApi.getClassifyStats(),
    })

    const { data: config } = useQuery({
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

    function invalidateAll() {
        queryClient.invalidateQueries({ queryKey: ['classify-status'] })
        queryClient.invalidateQueries({ queryKey: ['classify-stats'] })
    }

    async function handleClassifyFile() {
        if (!classifyPath.trim()) return
        setClassifyFileLoading(true)
        setMsg(null)
        try {
            const result = await adminApi.classifyFile(classifyPath.trim())
            setMsg({ type: 'success', text: `Classified: ${result.tags?.length ?? 0} tags added.` })
            invalidateAll()
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
            const result = await adminApi.classifyDirectory(classifyDirPath.trim())
            setMsg({
                type: 'success',
                text: result?.message ?? 'Directory classification started in background.',
            })
            invalidateAll()
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        } finally {
            setClassifyDirLoading(false)
        }
    }

    async function handleRunTask() {
        setTaskLoading(true)
        setMsg(null)
        try {
            const result = await adminApi.classifyRunTask()
            setMsg({ type: 'success', text: result.message })
            invalidateAll()
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        } finally {
            setTaskLoading(false)
        }
    }

    async function handleRunAllPending() {
        setAllPendingLoading(true)
        setMsg(null)
        try {
            const result = await adminApi.classifyAllPending()
            setMsg({
                type: 'success',
                text: result.count > 0
                    ? `Classification started for ${result.count} pending items.`
                    : 'No pending items to classify.',
            })
            invalidateAll()
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        } finally {
            setAllPendingLoading(false)
        }
    }

    async function handleClearTags(id: string) {
        setClearingId(id)
        try {
            await adminApi.classifyClearTags(id)
            setMsg({ type: 'success', text: 'Tags cleared.' })
            invalidateAll()
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        } finally {
            setClearingId(null)
        }
    }

    async function handleSaveSettings(e: React.FormEvent) {
        e.preventDefault()
        setSaving(true)
        setMsg(null)
        try {
            const updates = buildHuggingFaceConfigUpdates({
                enabled,
                model,
                maxFrames,
                rateLimit,
                timeoutSecs,
                maxConcurrent,
                endpointUrl,
                apiKeyInput,
            })
            await adminApi.updateConfig(updates)
            setMsg({ type: 'success', text: 'Settings saved. Some changes may require a restart.' })
            setApiKeyInput('')
            queryClient.invalidateQueries({ queryKey: ['admin-config'] })
            queryClient.invalidateQueries({ queryKey: ['classify-status'] })
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        } finally {
            setSaving(false)
        }
    }

    return {
        msg, status, stats,
        classifyPath, setClassifyPath,
        classifyDirPath, setClassifyDirPath,
        handleClassifyFile, handleClassifyDirectory,
        classifyFileLoading, classifyDirLoading,
        handleRunTask, taskLoading,
        handleRunAllPending, allPendingLoading,
        handleClearTags, clearingId,
        saving, apiKeyInput, setApiKeyInput,
        enabled, setEnabled,
        model, setModel,
        endpointUrl, setEndpointUrl,
        maxFrames, setMaxFrames,
        rateLimit, setRateLimit,
        timeoutSecs, setTimeoutSecs,
        maxConcurrent, setMaxConcurrent,
        handleSaveSettings, hf,
    }
}

/* ─── Main component ──────────────────────────────────────────── */

export function HuggingFaceTab() {
    const state = useHuggingFaceTab()

    return (
        <div>
            {state.msg && (
                <div className={`admin-alert admin-alert-${state.msg.type === 'success' ? 'success' : 'danger'}`}>
                    {state.msg.text}
                </div>
            )}

            <div className="admin-card" style={{ marginBottom: 20 }}>
                <h2>Hugging Face Visual Classification</h2>
                <p style={{ color: TEXT_MUTED, marginBottom: 16 }}>
                    Uses the Hugging Face Inference API to analyze video frames and images for content, then adds
                    suggested tags to mature-flagged media. Requires an API key and FFmpeg for video frame extraction.
                </p>
                {state.status && <HuggingFaceStatusCards status={state.status} />}
            </div>

            {state.stats && <ClassificationProgress stats={state.stats} />}

            {state.status && (
                <BackgroundTaskControl
                    status={state.status}
                    onRunTask={state.handleRunTask}
                    onRunAllPending={state.handleRunAllPending}
                    taskLoading={state.taskLoading}
                    allPendingLoading={state.allPendingLoading}
                    pendingCount={state.stats?.mature_pending ?? 0}
                />
            )}

            {state.stats && (
                <RecentlyClassifiedTable
                    items={state.stats.recent_items ?? []}
                    onClearTags={state.handleClearTags}
                    clearingId={state.clearingId}
                />
            )}

            <HuggingFaceRunClassification
                classifyPath={state.classifyPath}
                setClassifyPath={state.setClassifyPath}
                classifyDirPath={state.classifyDirPath}
                setClassifyDirPath={state.setClassifyDirPath}
                onClassifyFile={state.handleClassifyFile}
                onClassifyDirectory={state.handleClassifyDirectory}
                fileLoading={state.classifyFileLoading}
                dirLoading={state.classifyDirLoading}
                status={state.status}
            />

            <div className="admin-card">
                <h3>Settings</h3>
                <HuggingFaceSettingsForm
                    enabled={state.enabled}
                    setEnabled={state.setEnabled}
                    apiKeyInput={state.apiKeyInput}
                    setApiKeyInput={state.setApiKeyInput}
                    apiKeySet={!!state.hf.api_key_set}
                    model={state.model}
                    setModel={state.setModel}
                    endpointUrl={state.endpointUrl}
                    setEndpointUrl={state.setEndpointUrl}
                    maxFrames={state.maxFrames}
                    setMaxFrames={state.setMaxFrames}
                    rateLimit={state.rateLimit}
                    setRateLimit={state.setRateLimit}
                    timeoutSecs={state.timeoutSecs}
                    setTimeoutSecs={state.setTimeoutSecs}
                    maxConcurrent={state.maxConcurrent}
                    setMaxConcurrent={state.setMaxConcurrent}
                    onSave={state.handleSaveSettings}
                    saving={state.saving}
                />
            </div>
        </div>
    )
}

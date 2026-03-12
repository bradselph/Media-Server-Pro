import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { adminApi } from '@/api/endpoints'
import { errMsg } from './adminUtils'

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
                    {status.model ?? '—'}
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

const inputBaseStyle = {
    padding: '6px 10px',
    border: '1px solid var(--border-color)',
    borderRadius: 6,
    background: 'var(--input-bg)',
    color: 'var(--text-color)',
    fontSize: 13,
}

// TODO: Duplicate type — this local `ClassifyStatus` interface duplicates the exported
// `ClassifyStatus` in `@/api/types.ts` (line 488). The local version has `model?` optional
// while types.ts has `model` required, which is an inconsistency.
// WHY: If the backend shape changes, only one copy may be updated, causing silent type
// drift. The optional vs required `model` difference could also mask bugs.
// FIX: Remove this local interface and import `ClassifyStatus` from `@/api/types.ts`.
// Verify which optionality (`model` vs `model?`) is correct per the backend response.
interface ClassifyStatus {
    configured: boolean
    enabled: boolean
    model?: string
    rate_limit: number
    max_frames: number
    max_concurrent: number
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

function HuggingFaceFormApiKey({
    apiKeyInput,
    setApiKeyInput,
    apiKeySet,
}: {
    apiKeyInput: string
    setApiKeyInput: (v: string) => void
    apiKeySet: boolean
}) {
    return (
        <div className="admin-form-group">
            <label>API key</label>
            <input
                type="password"
                className="admin-input"
                value={apiKeyInput}
                onChange={(e) => { setApiKeyInput(e.target.value); }}
                placeholder={
                    apiKeySet ? '•••••••• (leave blank to keep current)' : 'Enter Hugging Face API token'
                }
                autoComplete="off"
            />
            {apiKeySet && !apiKeyInput && (
                <span style={{ fontSize: 12, color: 'var(--text-muted)' }}> Current key is set.</span>
            )}
        </div>
    )
}

function HuggingFaceFormModelAndEndpoint({
    model,
    setModel,
    endpointUrl,
    setEndpointUrl,
}: {
    model: string
    setModel: (v: string) => void
    endpointUrl: string
    setEndpointUrl: (v: string) => void
}) {
    return (
        <>
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
        </>
    )
}

function HuggingFaceFormNumericOptions({
    maxFrames,
    setMaxFrames,
    rateLimit,
    setRateLimit,
    timeoutSecs,
    setTimeoutSecs,
    maxConcurrent,
    setMaxConcurrent,
}: {
    maxFrames: number
    setMaxFrames: (v: number) => void
    rateLimit: number
    setRateLimit: (v: number) => void
    timeoutSecs: number
    setTimeoutSecs: (v: number) => void
    maxConcurrent: number
    setMaxConcurrent: (v: number) => void
}) {
    return (
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
    )
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
            <HuggingFaceFormApiKey
                apiKeyInput={apiKeyInput}
                setApiKeyInput={setApiKeyInput}
                apiKeySet={apiKeySet}
            />
            <HuggingFaceFormModelAndEndpoint
                model={model}
                setModel={setModel}
                endpointUrl={endpointUrl}
                setEndpointUrl={setEndpointUrl}
            />
            <HuggingFaceFormNumericOptions
                maxFrames={maxFrames}
                setMaxFrames={setMaxFrames}
                rateLimit={rateLimit}
                setRateLimit={setRateLimit}
                timeoutSecs={timeoutSecs}
                setTimeoutSecs={setTimeoutSecs}
                maxConcurrent={maxConcurrent}
                setMaxConcurrent={setMaxConcurrent}
            />
            <button type="submit" className="admin-btn admin-btn-primary" disabled={saving}>
                {saving ? 'Saving…' : 'Save settings'}
            </button>
        </form>
    )
}

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
                {loading ? 'Classifying…' : actionLabel}
            </button>
        </div>
    )
}

function ClassifyStatusHint({ configured }: { configured: boolean | undefined }) {
    if (configured === undefined) return null
    const style = { color: 'var(--text-muted)', marginTop: 8 } as const
    if (!configured) {
        return <p style={{ ...style, fontSize: 13 }}>Set an API key below and save to enable classification.</p>
    }
    return (
        <p style={{ ...style, fontSize: 12 }}>
            Supported: video (mp4, mkv, avi, etc.) and image (jpg, png, webp) files.
        </p>
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
            <h3>Run classification</h3>
            <p style={{ color: 'var(--text-muted)', marginBottom: 12 }}>
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
            <ClassifyStatusHint configured={configured} />
        </div>
    )
}

function useHuggingFaceTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [classifyPath, setClassifyPath] = useState('')
    const [classifyDirPath, setClassifyDirPath] = useState('')
    const [classifyFileLoading, setClassifyFileLoading] = useState(false)
    const [classifyDirLoading, setClassifyDirLoading] = useState(false)
    const [saving, setSaving] = useState(false)
    const [apiKeyInput, setApiKeyInput] = useState('')

    const { data: status } = useQuery({
        queryKey: ['classify-status'],
        queryFn: () => adminApi.getClassifyStatus(),
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
            const result = await adminApi.classifyDirectory(classifyDirPath.trim())
            setMsg({
                type: 'success',
                text: result?.message ?? 'Directory classification started in background.',
            })
            queryClient.invalidateQueries({ queryKey: ['classify-status'] })
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
            // TODO: Bug — this passes ['admin-config', 'classify-status'] as a single queryKey,
            // which only matches queries whose key is exactly that two-element array. It does NOT
            // invalidate both the 'admin-config' and 'classify-status' queries separately.
            // WHY: TanStack Query uses prefix matching on queryKey arrays. A query registered with
            // queryKey: ['admin-config'] won't match ['admin-config', 'classify-status'].
            // FIX: Call invalidateQueries twice with separate keys:
            //   queryClient.invalidateQueries({ queryKey: ['admin-config'] })
            //   queryClient.invalidateQueries({ queryKey: ['classify-status'] })
            queryClient.invalidateQueries({ queryKey: ['admin-config', 'classify-status'] })
        } catch (err) {
            setMsg({ type: 'error', text: errMsg(err) })
        } finally {
            setSaving(false)
        }
    }

    return {
        msg,
        status,
        classifyPath,
        setClassifyPath,
        classifyDirPath,
        setClassifyDirPath,
        handleClassifyFile,
        handleClassifyDirectory,
        classifyFileLoading,
        classifyDirLoading,
        saving,
        apiKeyInput,
        setApiKeyInput,
        enabled,
        setEnabled,
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
        handleSaveSettings,
        hf,
    }
}

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
                <p style={{ color: 'var(--text-muted)', marginBottom: 16 }}>
                    Uses the Hugging Face Inference API to analyze video frames and images for content, then adds
                    suggested tags to mature-flagged media. Requires an API key and FFmpeg for video frame extraction.
                </p>
                {state.status && <HuggingFaceStatusCards status={state.status} />}
            </div>

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

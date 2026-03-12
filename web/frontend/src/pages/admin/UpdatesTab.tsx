import {useEffect, useRef, useState} from 'react'
import {useQuery, useQueryClient} from '@tanstack/react-query'
import {adminApi} from '@/api/endpoints'
import {errMsg} from './adminUtils'

// ── Tab: Updates ──────────────────────────────────────────────────────────────

type BuildProgress = {
    inProgress: boolean
    stage: string
    progress: number
    error?: string
    done: boolean   // completed (success or error)
    success: boolean
}

export function UpdatesTab() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [checking, setChecking] = useState(false)
    const [applying, setApplying] = useState(false)
    const [updateApplied, setUpdateApplied] = useState(false)
    const [checkingSource, setCheckingSource] = useState(false)
    const [sourceStatus, setSourceStatus] = useState<{ updates_available: boolean; remote_commit: string } | null>(null)
    const [build, setBuild] = useState<BuildProgress | null>(null)
    const [savingConfig, setSavingConfig] = useState(false)
    const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)

    // Fetch update config (method + branch)
    const {data: updateCfg} = useQuery({
        queryKey: ['admin-update-config'],
        queryFn: () => adminApi.getUpdateConfig(),
        staleTime: 60_000,
    })

    const activeMethod = updateCfg?.update_method || 'source'
    const activeBranch = updateCfg?.branch || 'main'

    async function handleSaveConfig(method: 'source' | 'binary', branch: string) {
        setSavingConfig(true)
        setMsg(null)
        try {
            await adminApi.setUpdateConfig({update_method: method, branch})
            await queryClient.invalidateQueries({queryKey: ['admin-update-config']})
            setMsg({type: 'success', text: `Update settings saved: method=${method}, branch=${branch}`})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setSavingConfig(false)
        }
    }

    // Poll /api/admin/update/source/progress every 2s while a build is running
    // TODO: Stale closure risk — the `build?.inProgress` dependency means the effect
    // re-runs whenever that boolean changes, but `setBuild` inside the interval callback
    // captures the initial `build` reference. This works because it only reads `p` (the
    // API response), not `build` from closure. However, the `clearInterval(pollRef.current!)`
    // inside the callback uses a non-null assertion that could fail if the ref was already
    // cleared by the cleanup function racing with the callback.
    // FIX: Guard with `if (pollRef.current) clearInterval(pollRef.current)` instead of `!`.
    useEffect(() => {
        if (!build?.inProgress) {
            if (pollRef.current) {
                clearInterval(pollRef.current)
                pollRef.current = null
            }
            return
        }
        pollRef.current = setInterval(async () => {
            try {
                const p = await adminApi.getSourceUpdateProgress()
                const done = !p.in_progress
                const success = done && !p.error
                setBuild({
                    inProgress: p.in_progress,
                    stage: p.stage,
                    progress: p.progress,
                    error: p.error,
                    done,
                    success,
                })
                if (done) {
                    clearInterval(pollRef.current!)
                    pollRef.current = null
                    if (p.error) {
                        setMsg({type: 'error', text: `Build failed at "${p.stage}": ${p.error}`})
                    } else if (p.stage.includes('up to date')) {
                        setMsg({type: 'success', text: 'Already up to date — no changes applied.'})
                    } else {
                        setMsg({type: 'success', text: `Build complete (${p.stage}). Restart the service to apply.`})
                    }
                }
            } catch {
                // network blip — keep polling
            }
        }, 2000)
        return () => {
            if (pollRef.current) clearInterval(pollRef.current)
        }
    }, [build?.inProgress])

    const {data: status, refetch} = useQuery({
        queryKey: ['admin-update-status'],
        queryFn: () => adminApi.getUpdateStatus(),
        staleTime: 60_000,
    })

    async function handleCheck() {
        setChecking(true)
        setMsg(null)
        try {
            await adminApi.checkUpdates()
            await refetch()
            setMsg({type: 'success', text: 'Update check complete.'})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setChecking(false)
        }
    }

    async function handleApply() {
        if (!window.confirm(
            'Download and install the new binary now?\n\n' +
            'You will need to restart the service afterwards to run the new version.'
        )) return
        setApplying(true)
        setUpdateApplied(false)
        setMsg(null)
        try {
            await adminApi.applyUpdate()
            setUpdateApplied(true)
            setMsg({type: 'success', text: 'Update installed. Click "Restart Server" to run the new version.'})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setApplying(false)
        }
    }

    async function handleCheckSource() {
        setCheckingSource(true)
        setMsg(null)
        try {
            const result = await adminApi.checkSourceUpdates()
            setSourceStatus(result)
            setMsg({
                type: 'success',
                text: result.updates_available
                    ? `New commits available (remote: ${result.remote_commit})`
                    : 'Already up to date with the remote branch.',
            })
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setCheckingSource(false)
        }
    }

    async function handleApplySource() {
        if (!window.confirm(
            'This will run git pull + go build on the server and replace the running binary.\n\n' +
            'You will need to restart the service afterwards.\n\n' +
            'Proceed?'
        )) return
        setMsg(null)
        setBuild({inProgress: true, stage: 'starting', progress: 0, done: false, success: false})
        try {
            const result = await adminApi.applySourceUpdate()
            setBuild({
                inProgress: result.in_progress,
                stage: result.stage,
                progress: result.progress,
                error: result.error,
                done: !result.in_progress,
                success: !result.in_progress && !result.error,
            })
            setSourceStatus(null)
        } catch (err) {
            setBuild(null)
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    async function handleRestart() {
        if (!window.confirm('Restart the server now?')) return
        try {
            await adminApi.restartServer()
            setMsg({type: 'success', text: 'Restart initiated. The page will reload shortly…'})
            setTimeout(() => { window.location.reload(); }, 5000)
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    const buildRunning = build?.inProgress === true

    return (
        <div>
            {msg && (
                <div className={`admin-alert admin-alert-${msg.type === 'success' ? 'success' : 'danger'}`}>
                    {msg.text}
                </div>
            )}

            {/* Update Settings */}
            <div className="admin-card" style={{maxWidth: 640, marginBottom: 20}}>
                <h2>Update Settings</h2>
                <p style={{fontSize: 13, color: 'var(--text-muted)', margin: '0 0 16px 0'}}>
                    Configure how updates are applied. For <strong>main</strong> branch releases,
                    you can choose between downloading a pre-built binary or building from source.
                    The <strong>development</strong> branch always builds from source.
                </p>

                <div style={{display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, marginBottom: 16}}>
                    <div>
                        <label style={{fontSize: 12, color: 'var(--text-muted)', display: 'block', marginBottom: 4}}>
                            Update Method
                        </label>
                        <select
                            aria-label="Update method"
                            value={activeMethod}
                            onChange={e => handleSaveConfig(e.target.value as 'source' | 'binary', activeBranch)}
                            disabled={savingConfig || activeBranch === 'development'}
                            style={{
                                width: '100%',
                                padding: '8px 12px',
                                borderRadius: 6,
                                border: '1px solid var(--border-color)',
                                background: 'var(--bg-color)',
                                color: 'var(--text-color)',
                                fontSize: 14,
                            }}
                        >
                            <option value="source">Source Build (git pull + go build)</option>
                            <option value="binary">Binary Download (GitHub Release)</option>
                        </select>
                        {activeBranch === 'development' && (
                            <p style={{fontSize: 11, color: 'var(--text-muted)', marginTop: 4}}>
                                Development branch always uses source builds.
                            </p>
                        )}
                    </div>
                    <div>
                        <label style={{fontSize: 12, color: 'var(--text-muted)', display: 'block', marginBottom: 4}}>
                            Branch
                        </label>
                        <select
                            aria-label="Branch"
                            value={activeBranch}
                            onChange={e => {
                                const newBranch = e.target.value
                                const method = newBranch === 'development' ? 'source' : activeMethod
                                handleSaveConfig(method, newBranch)
                            }}
                            disabled={savingConfig}
                            style={{
                                width: '100%',
                                padding: '8px 12px',
                                borderRadius: 6,
                                border: '1px solid var(--border-color)',
                                background: 'var(--bg-color)',
                                color: 'var(--text-color)',
                                fontSize: 14,
                            }}
                        >
                            <option value="main">main (stable releases)</option>
                            <option value="development">development (latest features)</option>
                        </select>
                    </div>
                </div>

                <div style={{
                    background: 'var(--hover-bg)',
                    border: '1px solid var(--border-color)',
                    borderRadius: 6,
                    padding: '10px 14px',
                    fontSize: 12,
                    color: 'var(--text-muted)',
                }}>
                    <strong style={{color: 'var(--text-color)'}}>Current config:</strong>{' '}
                    {activeMethod === 'binary' ? 'Binary download' : 'Source build'} from <code>{activeBranch}</code> branch
                </div>
            </div>

            {/* GitHub Releases update — shown when method is "binary" or always for release checks */}
            <div className="admin-card" style={{maxWidth: 640, marginBottom: 20}}>
                <h2>Software Updates <span style={{fontSize: 13, fontWeight: 400, color: 'var(--text-muted)'}}>— GitHub Releases</span>
                </h2>

                <div style={{display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, marginBottom: 20}}>
                    <div>
                        <div style={{fontSize: 12, color: 'var(--text-muted)', marginBottom: 4}}>Current Version</div>
                        <div style={{fontWeight: 600, fontSize: 18}}>{status?.current_version || '—'}</div>
                    </div>
                    {status?.checked_at && (
                        <div>
                            <div style={{fontSize: 12, color: 'var(--text-muted)', marginBottom: 4}}>Latest Version
                            </div>
                            <div style={{fontWeight: 600, fontSize: 18}}>
                                {status.latest_version || '—'}
                                {status.update_available && (
                                    <span style={{
                                        marginLeft: 8,
                                        fontSize: 12,
                                        background: '#22c55e',
                                        color: '#fff',
                                        borderRadius: 4,
                                        padding: '2px 6px'
                                    }}>New!</span>
                                )}
                            </div>
                        </div>
                    )}
                </div>

                {status?.checked_at && (
                    <p style={{fontSize: 12, color: 'var(--text-muted)', margin: '0 0 16px 0'}}>
                        Last checked: {new Date(status.checked_at).toLocaleString()}
                        {status.error && <span style={{color: '#ef4444', marginLeft: 8}}>— {status.error}</span>}
                    </p>
                )}

                {!status?.checked_at && (
                    <p style={{fontSize: 13, color: 'var(--text-muted)', marginBottom: 16}}>
                        No update check has been performed yet.
                    </p>
                )}

                {status?.update_available && status.release_notes && (
                    <div style={{
                        background: 'var(--hover-bg)',
                        border: '1px solid var(--border-color)',
                        borderRadius: 6,
                        padding: '10px 14px',
                        marginBottom: 16,
                        fontSize: 13,
                        maxHeight: 160,
                        overflowY: 'auto',
                        whiteSpace: 'pre-wrap',
                    }}>
                        <strong>Release Notes</strong>
                        <div style={{marginTop: 6, color: 'var(--text-muted)'}}>{status.release_notes}</div>
                    </div>
                )}

                <div style={{display: 'flex', gap: 10, flexWrap: 'wrap'}}>
                    <button className="admin-btn admin-btn-primary" onClick={handleCheck} disabled={checking}>
                        <i className="bi bi-arrow-repeat"/> {checking ? 'Checking…' : 'Check for Updates'}
                    </button>
                    {status?.update_available && activeMethod === 'binary' && !updateApplied && (
                        <button className="admin-btn admin-btn-success" onClick={handleApply} disabled={applying}>
                            <i className="bi bi-download"/>
                            {applying ? 'Downloading…' : `Download ${status.latest_version}`}
                        </button>
                    )}
                    {updateApplied && (
                        <button className="admin-btn admin-btn-warning" onClick={handleRestart}>
                            <i className="bi bi-arrow-clockwise"/> Restart Server
                        </button>
                    )}
                    {status?.release_url && (
                        <a href={status.release_url} target="_blank" rel="noopener noreferrer"
                           className="admin-btn">
                            <i className="bi bi-box-arrow-up-right"/> View Release
                        </a>
                    )}
                </div>

                {activeMethod === 'source' && status?.update_available && (
                    <p style={{fontSize: 12, color: 'var(--text-muted)', marginTop: 12}}>
                        Update method is set to <strong>Source Build</strong> — use the Source Update
                        section below to pull and build the new version.
                    </p>
                )}
            </div>

            {/* Source-based update (git pull + go build) */}
            <div className="admin-card" style={{maxWidth: 640}}>
                <h2>Source Update <span style={{fontSize: 13, fontWeight: 400, color: 'var(--text-muted)'}}>
                    — git pull + build ({activeBranch})
                </span></h2>
                <p style={{fontSize: 13, color: 'var(--text-muted)', margin: '0 0 16px 0'}}>
                    Pull the latest code from the <code>{activeBranch}</code> branch, rebuild the frontend and
                    server binary in-place. Requires build tools (git, npm, go) and the GitHub
                    token (UPDATER_GITHUB_TOKEN) to be configured on the server.
                </p>

                {sourceStatus && !build && (
                    <div style={{
                        background: sourceStatus.updates_available ? 'rgba(34,197,94,0.1)' : 'var(--hover-bg)',
                        border: `1px solid ${sourceStatus.updates_available ? '#22c55e' : 'var(--border-color)'}`,
                        borderRadius: 6,
                        padding: '8px 14px',
                        marginBottom: 16,
                        fontSize: 13,
                    }}>
                        {sourceStatus.updates_available
                            ? <>New commits available on <code>{activeBranch}</code> — remote
                                HEAD: <code>{sourceStatus.remote_commit}</code></>
                            : <>Repository is up to date with the <code>{activeBranch}</code> branch.</>}
                    </div>
                )}

                {/* Live build progress */}
                {build && (
                    <div style={{marginBottom: 16}}>
                        <div style={{
                            display: 'flex',
                            justifyContent: 'space-between',
                            fontSize: 13,
                            marginBottom: 6,
                        }}>
                            <span style={{
                                color: build.error ? '#ef4444' : build.done ? '#22c55e' : 'var(--text-color)',
                                fontWeight: 500,
                            }}>
                                {build.error
                                    ? `Failed: ${build.stage}`
                                    : build.done
                                        ? `Done: ${build.stage}`
                                        : build.stage || 'starting…'}
                            </span>
                            <span style={{color: 'var(--text-muted)'}}>{Math.round(build.progress)}%</span>
                        </div>
                        <div style={{
                            height: 8,
                            background: 'var(--hover-bg)',
                            borderRadius: 4,
                            overflow: 'hidden',
                        }}>
                            <div style={{
                                height: '100%',
                                width: `${build.progress}%`,
                                background: build.error ? '#ef4444' : build.done ? '#22c55e' : '#3b82f6',
                                borderRadius: 4,
                                transition: 'width 0.4s ease',
                            }}/>
                        </div>
                    </div>
                )}

                <div style={{display: 'flex', gap: 10, flexWrap: 'wrap'}}>
                    <button className="admin-btn admin-btn-primary" onClick={handleCheckSource}
                            disabled={checkingSource || buildRunning}>
                        <i className="bi bi-git"/> {checkingSource ? 'Checking…' : `Check ${activeBranch}`}
                    </button>
                    <button
                        className="admin-btn admin-btn-success"
                        onClick={handleApplySource}
                        disabled={buildRunning}
                        title={`git pull origin ${activeBranch} + npm build + go build, then replace binary`}
                    >
                        <i className="bi bi-arrow-up-circle"/>
                        {buildRunning ? 'Building…' : 'Pull & Build'}
                    </button>
                    {build?.success && (
                        <button className="admin-btn admin-btn-warning" onClick={handleRestart}>
                            <i className="bi bi-arrow-clockwise"/> Restart Server
                        </button>
                    )}
                    {build?.done && (
                        <button className="admin-btn" onClick={() => { setBuild(null); }}>
                            Dismiss
                        </button>
                    )}
                </div>
                {!build && (
                    <p style={{fontSize: 12, color: 'var(--text-muted)', marginTop: 12}}>
                        After a successful build, use the{' '}
                        <strong>Restart Server</strong> button or run:{' '}
                        <code style={{background: 'var(--hover-bg)', padding: '2px 6px', borderRadius: 4}}>
                            systemctl restart mediaserver
                        </code>
                    </p>
                )}
            </div>
        </div>
    )
}

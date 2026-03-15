import {type FormEvent, useState} from 'react'
import {useMutation, useQuery, useQueryClient} from '@tanstack/react-query'
import {downloaderApi} from '@/api/endpoints'
import type {
    DownloaderDetectResult,
    DownloaderDownloadFile,
    DownloaderHealth,
    DownloaderSettings,
    DownloaderStreamInfo,
    ImportableFile,
} from '@/api/types'
import {useDownloaderWebSocket} from '@/hooks/useDownloaderWebSocket'
import {errMsg, formatBytes, formatUptime} from './adminUtils'
import {SubTabs} from './helpers'

export function DownloaderTab() {
    const [sub, setSub] = useState('download')

    const {data: health} = useQuery<DownloaderHealth>({
        queryKey: ['downloader-health'],
        queryFn: () => downloaderApi.getHealth(),
        refetchInterval: 15000,
    })

    const online = health?.online ?? false

    return (<>
        <SubTabs items={[
            {id: 'download', label: 'Download'},
            {id: 'files', label: 'Server Files'},
            {id: 'import', label: 'Import to Library'},
            {id: 'status', label: 'Status'},
        ]} active={sub} onChange={setSub}/>

        {sub === 'download' && <DownloadSection online={online}/>}
        {sub === 'files' && <FilesSection online={online}/>}
        {sub === 'import' && <ImportSection/>}
        {sub === 'status' && <StatusSection health={health}/>}
    </>)
}

// ── Download Section ──────────────────────────────────────────────────────────

function DownloadSection({online}: { online: boolean }) {
    const [url, setUrl] = useState('')
    const [detecting, setDetecting] = useState(false)
    const [detected, setDetected] = useState<DownloaderDetectResult | null>(null)
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const {connected, clientId, activeDownloads} = useDownloaderWebSocket()

    async function handleDetect(e: FormEvent) {
        e.preventDefault()
        if (!url.trim()) return
        setDetecting(true)
        setDetected(null)
        setMsg(null)
        try {
            const result = await downloaderApi.detect(url.trim())
            setDetected(result)
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        } finally {
            setDetecting(false)
        }
    }

    async function handleDownload(stream?: DownloaderStreamInfo) {
        if (!detected || !clientId) return
        setMsg(null)
        try {
            await downloaderApi.download({
                url: stream?.url ?? detected.url,
                title: detected.title,
                clientId,
                isYouTube: detected.isYouTube,
                isYouTubeMusic: detected.isYouTubeMusic,
                relayId: detected.relayId,
            })
            setMsg({type: 'success', text: 'Download started'})
        } catch (err) {
            setMsg({type: 'error', text: errMsg(err)})
        }
    }

    return (
        <div className="admin-section">
            <div style={{display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '16px'}}>
                <span style={{
                    width: 10, height: 10, borderRadius: '50%',
                    background: online ? '#22c55e' : '#ef4444',
                    display: 'inline-block',
                }}/>
                <span style={{color: 'var(--text-muted)', fontSize: '0.85rem'}}>
                    {online ? 'Downloader online' : 'Downloader offline'}
                    {connected && clientId ? ' | WS connected' : ' | WS disconnected'}
                </span>
            </div>

            <form onSubmit={handleDetect} style={{display: 'flex', gap: '8px', marginBottom: '16px'}}>
                <input
                    type="url"
                    className="admin-input"
                    placeholder="Enter URL to detect streams..."
                    value={url}
                    onChange={e => { setUrl(e.target.value); }}
                    style={{flex: 1}}
                    disabled={!online || detecting}
                />
                <button type="submit" className="admin-btn admin-btn-primary"
                        disabled={!online || detecting || !url.trim()}>
                    {detecting ? 'Detecting...' : 'Detect'}
                </button>
            </form>

            {msg && (
                <div className={`admin-msg admin-msg-${msg.type}`} style={{marginBottom: '12px'}}>
                    {msg.text}
                </div>
            )}

            {detected && (
                <div className="admin-card" style={{marginBottom: '16px'}}>
                    <h4 style={{margin: '0 0 8px'}}>{detected.title || 'Detected Streams'}</h4>
                    {detected.streams.length === 0 ? (
                        <p style={{color: 'var(--text-muted)'}}>No streams detected</p>
                    ) : (
                        <div style={{display: 'flex', flexDirection: 'column', gap: '6px'}}>
                            {detected.streams.map((s, i) => (
                                <div key={i} style={{
                                    display: 'flex', alignItems: 'center', gap: '8px',
                                    padding: '6px 8px', background: 'var(--bg-secondary)', borderRadius: '4px',
                                }}>
                                    <span style={{flex: 1, fontSize: '0.85rem'}}>
                                        {s.quality || s.resolution || s.format || s.type}
                                        {s.label ? ` - ${s.label}` : ''}
                                        {s.size ? ` (${formatBytes(s.size)})` : ''}
                                    </span>
                                    <button className="admin-btn admin-btn-sm admin-btn-primary"
                                            disabled={!clientId}
                                            onClick={() => { handleDownload(s); }}>
                                        Download
                                    </button>
                                </div>
                            ))}
                        </div>
                    )}
                    {detected.isYouTube && (
                        <button className="admin-btn admin-btn-primary" style={{marginTop: '8px'}}
                                disabled={!clientId}
                                onClick={() => { handleDownload(); }}>
                            Download (yt-dlp best)
                        </button>
                    )}
                </div>
            )}

            {activeDownloads.size > 0 && (
                <div>
                    <h4 style={{marginBottom: '8px'}}>Active Downloads</h4>
                    {Array.from(activeDownloads.values()).map(dl => (
                        <div key={dl.downloadId} className="admin-card" style={{marginBottom: '8px'}}>
                            <div style={{display: 'flex', justifyContent: 'space-between', marginBottom: '4px'}}>
                                <span style={{fontSize: '0.85rem', fontWeight: 500}}>
                                    {dl.title || dl.filename || dl.downloadId}
                                </span>
                                <span style={{fontSize: '0.8rem', color: 'var(--text-muted)'}}>
                                    {dl.status}{dl.speed ? ` | ${dl.speed}` : ''}{dl.eta ? ` | ETA ${dl.eta}` : ''}
                                </span>
                            </div>
                            <div style={{
                                height: '6px', borderRadius: '3px',
                                background: 'var(--bg-secondary)', overflow: 'hidden',
                            }}>
                                <div style={{
                                    height: '100%', borderRadius: '3px',
                                    width: `${Math.min(dl.progress ?? 0, 100)}%`,
                                    background: dl.status === 'error' ? '#ef4444'
                                        : dl.status === 'complete' ? '#22c55e' : '#3b82f6',
                                    transition: 'width 0.3s',
                                }}/>
                            </div>
                            {dl.error && (
                                <p style={{color: '#ef4444', fontSize: '0.8rem', margin: '4px 0 0'}}>
                                    {dl.error}
                                </p>
                            )}
                        </div>
                    ))}
                </div>
            )}
        </div>
    )
}

// ── Server Files Section ──────────────────────────────────────────────────────

function FilesSection({online}: { online: boolean }) {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)

    const {data: files, isLoading} = useQuery<DownloaderDownloadFile[]>({
        queryKey: ['downloader-files'],
        queryFn: () => downloaderApi.listDownloads(),
        enabled: online,
        refetchInterval: 10000,
    })

    const deleteMutation = useMutation({
        mutationFn: (filename: string) => downloaderApi.deleteDownload(filename),
        onSuccess: () => {
            queryClient.invalidateQueries({queryKey: ['downloader-files']})
            setMsg({type: 'success', text: 'File deleted'})
        },
        onError: (err) => { setMsg({type: 'error', text: errMsg(err)}); },
    })

    if (!online) return <p style={{color: 'var(--text-muted)', padding: '16px'}}>Downloader is offline</p>
    if (isLoading) return <p style={{color: 'var(--text-muted)', padding: '16px'}}>Loading...</p>

    return (
        <div className="admin-section">
            {msg && <div className={`admin-msg admin-msg-${msg.type}`} style={{marginBottom: '12px'}}>{msg.text}</div>}

            {!files?.length ? (
                <p style={{color: 'var(--text-muted)'}}>No files on the downloader server</p>
            ) : (
                <table className="admin-table">
                    <thead>
                    <tr>
                        <th>Filename</th>
                        <th>Size</th>
                        <th>Created</th>
                        <th style={{width: '80px'}}/>
                    </tr>
                    </thead>
                    <tbody>
                    {files.map(f => (
                        <tr key={f.filename}>
                            <td style={{wordBreak: 'break-all'}}>{f.filename}</td>
                            <td>{formatBytes(f.size)}</td>
                            <td>{new Date(f.created * 1000).toLocaleString()}</td>
                            <td>
                                <button className="admin-btn admin-btn-sm admin-btn-danger"
                                        disabled={deleteMutation.isPending}
                                        onClick={() => { deleteMutation.mutate(f.filename); }}>
                                    Delete
                                </button>
                            </td>
                        </tr>
                    ))}
                    </tbody>
                </table>
            )}
        </div>
    )
}

// ── Import Section ────────────────────────────────────────────────────────────

function ImportSection() {
    const queryClient = useQueryClient()
    const [msg, setMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null)
    const [deleteSource, setDeleteSource] = useState(true)
    const [triggerScan, setTriggerScan] = useState(true)

    const {data: files, isLoading} = useQuery<ImportableFile[]>({
        queryKey: ['downloader-importable'],
        queryFn: () => downloaderApi.listImportable(),
        refetchInterval: 10000,
    })

    const importMutation = useMutation({
        mutationFn: (filename: string) => downloaderApi.importFile(filename, deleteSource, triggerScan),
        onSuccess: (result) => {
            queryClient.invalidateQueries({queryKey: ['downloader-importable']})
            queryClient.invalidateQueries({queryKey: ['downloader-files']})
            setMsg({type: 'success', text: `Imported to ${result.destination}`})
        },
        onError: (err) => { setMsg({type: 'error', text: errMsg(err)}); },
    })

    if (isLoading) return <p style={{color: 'var(--text-muted)', padding: '16px'}}>Loading...</p>

    return (
        <div className="admin-section">
            <div style={{display: 'flex', gap: '16px', marginBottom: '16px'}}>
                <label style={{display: 'flex', alignItems: 'center', gap: '6px', fontSize: '0.85rem'}}>
                    <input type="checkbox" checked={deleteSource}
                           onChange={e => { setDeleteSource(e.target.checked); }}/>
                    Delete source after import
                </label>
                <label style={{display: 'flex', alignItems: 'center', gap: '6px', fontSize: '0.85rem'}}>
                    <input type="checkbox" checked={triggerScan}
                           onChange={e => { setTriggerScan(e.target.checked); }}/>
                    Trigger media scan after import
                </label>
            </div>

            {msg && <div className={`admin-msg admin-msg-${msg.type}`} style={{marginBottom: '12px'}}>{msg.text}</div>}

            {!files?.length ? (
                <p style={{color: 'var(--text-muted)'}}>No files ready to import</p>
            ) : (
                <table className="admin-table">
                    <thead>
                    <tr>
                        <th>Filename</th>
                        <th>Type</th>
                        <th>Size</th>
                        <th>Modified</th>
                        <th style={{width: '80px'}}/>
                    </tr>
                    </thead>
                    <tbody>
                    {files.map(f => (
                        <tr key={f.name}>
                            <td style={{wordBreak: 'break-all'}}>{f.name}</td>
                            <td>{f.isAudio ? 'Audio' : 'Video'}</td>
                            <td>{formatBytes(f.size)}</td>
                            <td>{new Date(f.modified * 1000).toLocaleString()}</td>
                            <td>
                                <button className="admin-btn admin-btn-sm admin-btn-primary"
                                        disabled={importMutation.isPending}
                                        onClick={() => { importMutation.mutate(f.name); }}>
                                    Import
                                </button>
                            </td>
                        </tr>
                    ))}
                    </tbody>
                </table>
            )}
        </div>
    )
}

// ── Status Section ────────────────────────────────────────────────────────────

function StatusSection({health}: { health?: DownloaderHealth }) {
    const {data: settings} = useQuery<DownloaderSettings>({
        queryKey: ['downloader-settings'],
        queryFn: () => downloaderApi.getSettings(),
        enabled: health?.online ?? false,
    })

    return (
        <div className="admin-section">
            <h4 style={{marginBottom: '12px'}}>Service Status</h4>
            <div className="admin-card" style={{marginBottom: '16px'}}>
                <table className="admin-kv-table">
                    <tbody>
                    <tr>
                        <td>Status</td>
                        <td>
                            <span style={{
                                color: health?.online ? '#22c55e' : '#ef4444',
                                fontWeight: 600,
                            }}>
                                {health?.online ? 'Online' : 'Offline'}
                            </span>
                        </td>
                    </tr>
                    {health?.online && health.activeDownloads != null && (
                        <tr>
                            <td>Active Downloads</td>
                            <td>{health.activeDownloads}</td>
                        </tr>
                    )}
                    {health?.online && health.queuedDownloads != null && (
                        <tr>
                            <td>Queued Downloads</td>
                            <td>{health.queuedDownloads}</td>
                        </tr>
                    )}
                    {health?.online && health.uptime != null && (
                        <tr>
                            <td>Uptime</td>
                            <td>{formatUptime(health.uptime)}</td>
                        </tr>
                    )}
                    {health?.error && (
                        <tr>
                            <td>Error</td>
                            <td style={{color: '#ef4444'}}>{health.error}</td>
                        </tr>
                    )}
                    </tbody>
                </table>
            </div>

            {health?.online && health.dependencies && (
                <>
                    <h4 style={{marginBottom: '8px'}}>Dependencies</h4>
                    <div className="admin-card" style={{marginBottom: '16px'}}>
                        <table className="admin-kv-table">
                            <tbody>
                            {Object.entries(health.dependencies).map(([name, version]) => (
                                <tr key={name}>
                                    <td>{name}</td>
                                    <td>{version}</td>
                                </tr>
                            ))}
                            </tbody>
                        </table>
                    </div>
                </>
            )}

            {settings && (
                <>
                    <h4 style={{marginBottom: '8px'}}>Settings</h4>
                    <div className="admin-card" style={{marginBottom: '16px'}}>
                        <table className="admin-kv-table">
                            <tbody>
                            <tr>
                                <td>Max Concurrent</td>
                                <td>{settings.maxConcurrent}</td>
                            </tr>
                            <tr>
                                <td>Audio Format</td>
                                <td>{settings.audioFormat}</td>
                            </tr>
                            <tr>
                                <td>Server Storage</td>
                                <td>{settings.allowServerStorage ? 'Allowed' : 'Browser only'}</td>
                            </tr>
                            <tr>
                                <td>Proxy</td>
                                <td>{settings.proxy?.enabled ? 'Enabled' : 'Disabled'}</td>
                            </tr>
                            </tbody>
                        </table>
                    </div>

                    {settings.supportedSites?.length > 0 && (
                        <>
                            <h4 style={{marginBottom: '8px'}}>Supported Sites</h4>
                            <div className="admin-card">
                                <div style={{display: 'flex', flexWrap: 'wrap', gap: '6px'}}>
                                    {settings.supportedSites.map(site => (
                                        <span key={site} style={{
                                            padding: '2px 8px', borderRadius: '4px',
                                            background: 'var(--bg-secondary)', fontSize: '0.8rem',
                                        }}>
                                            {site}
                                        </span>
                                    ))}
                                </div>
                            </div>
                        </>
                    )}
                </>
            )}
        </div>
    )
}

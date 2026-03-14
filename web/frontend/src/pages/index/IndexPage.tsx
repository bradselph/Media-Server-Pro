import React, {type ChangeEvent, type CSSProperties, type DragEvent, useCallback, useEffect, useMemo, useRef, useState,} from 'react'
import {keepPreviousData, useQuery, useQueryClient} from '@tanstack/react-query'
import {Link, useNavigate, useSearchParams} from 'react-router-dom'
import {decode} from 'blurhash'
import {useAuthStore} from '@/stores/authStore'
import {useThemeStore} from '@/stores/themeStore'
import {useSettingsStore} from '@/stores/settingsStore'
import {usePlaylistStore} from '@/stores/playlistStore'
import {ApiError} from '@/api/client'
import {analyticsApi, mediaApi, playlistApi, suggestionsApi, versionApi} from '@/api/endpoints'
import type {AnalyticsSummary, MediaCategory, MediaItem, Playlist, Suggestion} from '@/api/types'
import {useEqualizer} from '@/hooks/useEqualizer'
import {EqualizerPanel} from '@/components/EqualizerPanel'
import {formatDuration, formatFileSize, formatTitle} from '@/utils/formatters'
import '@/styles/index.css'

// Allowed pagination limits — used to normalize URL/API values so 48/96 etc. work consistently
const PAGINATION_LIMITS = [12, 24, 48, 96] as const

const FONT_SIZE_SMALL = 13
const COLOR_TEXT_MUTED = 'var(--text-muted)'
const MUTED_TEXT_STYLE: React.CSSProperties = {color: COLOR_TEXT_MUTED, fontSize: FONT_SIZE_SMALL}

function normalizeLimit(value: number, fallback: number): number {
    const n = Number.isFinite(value) && value > 0 ? Math.floor(value) : fallback
    if (PAGINATION_LIMITS.includes(n as (typeof PAGINATION_LIMITS)[number])) return n
    const next = PAGINATION_LIMITS.find(m => m >= n)
    return next ?? PAGINATION_LIMITS[PAGINATION_LIMITS.length - 1]
}

// BlurHashPlaceholder renders a decoded BlurHash as a canvas for LQIP
function BlurHashPlaceholder({hash, className, style}: { hash: string; className?: string; style?: CSSProperties }) {
    const canvasRef = useRef<HTMLCanvasElement>(null)

    useEffect(() => {
        if (!hash || !canvasRef.current) return
        try {
            const pixels = decode(hash, 32, 18)
            const canvas = canvasRef.current
            const ctx = canvas.getContext('2d')
            if (!ctx) return
            canvas.width = 32
            canvas.height = 18
            const imageData = ctx.createImageData(32, 18)
            imageData.data.set(pixels)
            ctx.putImageData(imageData, 0, 0)
        } catch {
            // Invalid hash — ignore
        }
    }, [hash])

    return <canvas ref={canvasRef} className={className} style={{...style, display: 'block', width: '100%', height: '100%', objectFit: 'cover'}} aria-hidden />
}

// Skeleton placeholder for loading cards
function MediaCardSkeleton() {
    return (
        <div className="media-card media-card-skeleton" aria-hidden>
            <div className="media-thumbnail-placeholder media-skeleton-thumb"/>
            <div className="media-card-body">
                <div className="media-skeleton-title"/>
                <div className="media-skeleton-meta"/>
            </div>
        </div>
    )
}

// ── Upload Modal ──────────────────────────────────────────────────────────────

interface UploadFile {
    file: File
    name: string
    size: number
}

interface UploadResult {
    filename: string
    size?: number
    error?: string
}

function UploadModal({onClose, onDone, maxFileSize, categories = []}: {
    onClose: () => void;
    onDone: () => void;
    maxFileSize?: number;
    categories?: MediaCategory[];
}) {
    const [files, setFiles] = useState<UploadFile[]>([])
    const [selectedCategory, setSelectedCategory] = useState('')
    const [phase, setPhase] = useState<'select' | 'uploading' | 'done'>('select')
    const [progress, setProgress] = useState(0)
    const [statusText, setStatusText] = useState('')
    const [results, setResults] = useState<{ uploaded: UploadResult[]; errors: UploadResult[] } | null>(null)
    const [dragOver, setDragOver] = useState(false)
    const [sizeError, setSizeError] = useState('')
    const fileInputRef = useRef<HTMLInputElement>(null)
    const xhrRef = useRef<XMLHttpRequest | null>(null)

    function addFiles(fileList: FileList) {
        setSizeError('')
        if (maxFileSize && maxFileSize > 0) {
            const oversized = Array.from(fileList).filter(f => f.size > maxFileSize)
            if (oversized.length > 0) {
                setSizeError(`${oversized.map(f => f.name).join(', ')} exceed${oversized.length === 1 ? 's' : ''} the ${formatFileSize({ bytes: maxFileSize }, '0 B')} limit`)
                return
            }
        }
        const newFiles = Array.from(fileList).map(f => ({file: f, name: f.name, size: f.size}))
        setFiles(prev => [...prev, ...newFiles])
    }

    function removeFile(idx: number) {
        setFiles(prev => prev.filter((_, i) => i !== idx))
    }

    function handleDrop(e: DragEvent<HTMLDivElement>) {
        e.preventDefault()
        setDragOver(false)
        if (e.dataTransfer.files.length) addFiles(e.dataTransfer.files)
    }

    function handleFileChange(e: ChangeEvent<HTMLInputElement>) {
        if (e.target.files?.length) addFiles(e.target.files)
    }

    function startUpload() {
        if (!files.length) return
        setPhase('uploading')
        setProgress(0)
        setStatusText('Uploading...')

        const formData = new FormData()
        files.forEach(f => { formData.append('files', f.file); })
        if (selectedCategory) formData.append('category', selectedCategory)

        const xhr = new XMLHttpRequest()
        xhrRef.current = xhr

        xhr.upload.addEventListener('progress', (e) => {
            if (e.lengthComputable) {
                const pct = Math.round((e.loaded / e.total) * 100)
                setProgress(pct)
                setStatusText(`Uploading... ${pct}% (${formatFileSize({ bytes: e.loaded }, '0 B')} / ${formatFileSize({ bytes: e.total }, '0 B')})`)
            }
        })

        xhr.addEventListener('load', () => {
            if (xhr.status >= 200 && xhr.status < 300) {
                try {
                    const raw = JSON.parse(xhr.responseText)
                    const data = raw.data ?? raw
                    setResults({uploaded: data.uploaded ?? [], errors: data.errors ?? []})
                    setPhase('done')
                    if ((data.uploaded ?? []).length > 0) onDone()
                } catch {
                    setResults({uploaded: [], errors: [{filename: 'Upload', error: 'Failed to parse response'}]})
                    setPhase('done')
                }
            } else if (xhr.status === 401) {
                setResults({uploaded: [], errors: [{filename: 'Auth', error: 'Authentication required'}]})
                setPhase('done')
            } else {
                setResults({uploaded: [], errors: [{filename: 'Upload', error: `Server error ${xhr.status}`}]})
                setPhase('done')
            }
        })

        xhr.addEventListener('error', () => {
            setResults({uploaded: [], errors: [{filename: 'Upload', error: 'Network error'}]})
            setPhase('done')
        })

        xhr.addEventListener('abort', () => {
            setPhase('select')
        })

        xhr.withCredentials = true
        xhr.open('POST', '/api/upload', true)
        xhr.send(formData)
    }

    function handleCancel() {
        xhrRef.current?.abort()
        onClose()
    }

    function handleClose() {
        xhrRef.current?.abort()
        onClose()
    }

    return (
        <div className="modal-overlay" onClick={e => e.target === e.currentTarget && handleClose()} role="presentation">
            <div className="modal-box" role="dialog" aria-modal="true" aria-labelledby="upload-modal-title">
                <div className="modal-header">
                    <h2 id="upload-modal-title"><i className="bi bi-cloud-upload-fill"/> Upload Media Files</h2>
                    <button type="button" className="modal-close" onClick={handleClose} aria-label="Close">×</button>
                </div>
                <div className="modal-body">
                    {phase === 'select' && (
                        <>
                            <div
                                className={`upload-drop-zone ${dragOver ? 'drag-over' : ''}`}
                                onDragOver={e => {
                                    e.preventDefault();
                                    setDragOver(true)
                                }}
                                onDragLeave={() => { setDragOver(false); }}
                                onDrop={handleDrop}
                                onClick={() => fileInputRef.current?.click()}
                            >
                                <div className="upload-drop-zone-icon"><i className="bi bi-cloud-upload"/></div>
                                <p>Drag and drop files here or click to browse</p>
                                <button className="controls-btn controls-btn-primary" onClick={e => {
                                    e.stopPropagation();
                                    fileInputRef.current?.click()
                                }}>
                                    Browse Files
                                </button>
                                <input
                                    ref={fileInputRef}
                                    type="file"
                                    multiple
                                    accept="video/*,audio/*"
                                    style={{display: 'none'}}
                                    onChange={handleFileChange}
                                />
                            </div>

                            {sizeError && (
                                <div style={{color: '#ef4444', fontSize: FONT_SIZE_SMALL, marginTop: 8}}>
                                    <i className="bi bi-exclamation-triangle"/> {sizeError}
                                </div>
                            )}

                            {categories.length > 0 && (
                                <div className="filter-group" style={{marginTop: 12}}>
                                    <label htmlFor="upload-category">Category</label>
                                    <select id="upload-category" className="filter-select" value={selectedCategory}
                                            onChange={e => setSelectedCategory(e.target.value)}>
                                        <option value="">Default</option>
                                        {categories.map(c => (
                                            <option key={c.name} value={c.name}>{c.display_name || c.name}</option>
                                        ))}
                                    </select>
                                </div>
                            )}

                            {files.length > 0 && (
                                <>
                                    <h3 style={{margin: '0 0 8px 0', fontSize: 15}}>Files to Upload
                                        ({files.length})</h3>
                                    {files.map((f, i) => (
                                        <div key={`${f.name}-${f.size}-${i}`} className="upload-file-item">
                                            <span className="upload-file-name">{f.name}</span>
                                            <span className="upload-file-size">{formatFileSize({ bytes: f.size }, '0 B')}</span>
                                            <button className="upload-remove-btn" onClick={() => { removeFile(i); }}>×
                                            </button>
                                        </div>
                                    ))}
                                    <div className="upload-actions">
                                        <button className="controls-btn controls-btn-success" onClick={startUpload}>
                                            Start Upload
                                        </button>
                                        <button className="controls-btn" onClick={() => { setFiles([]); }}>
                                            Clear All
                                        </button>
                                    </div>
                                </>
                            )}
                        </>
                    )}

                    {phase === 'uploading' && (
                        <>
                            <h3 style={{margin: '0 0 8px 0', fontSize: 15}}>Upload Progress</h3>
                            <div className="upload-progress-bar">
                                <div className="upload-progress-fill" style={{width: `${progress}%`}}/>
                            </div>
                            <p className="upload-status">{statusText}</p>
                            <div className="upload-actions">
                                <button className="controls-btn" onClick={handleCancel}>Cancel</button>
                            </div>
                        </>
                    )}

                    {phase === 'done' && results && (
                        <>
                            <h3 style={{margin: '0 0 8px 0', fontSize: 15}}>Upload Results</h3>
                            {results.uploaded.map((r, i) => (
                                <div key={i} className="upload-success"><i
                                    className="bi bi-check-circle-fill"/> {r.filename} ({formatFileSize({ bytes: r.size ?? 0 }, '0 B')})
                                </div>
                            ))}
                            {results.errors.map((r, i) => (
                                <div key={i} className="upload-error"><i
                                    className="bi bi-x-circle-fill"/> {r.filename}: {r.error}</div>
                            ))}
                            <div className="upload-actions" style={{marginTop: 16}}>
                                <button className="controls-btn controls-btn-primary" onClick={handleClose}>Done
                                </button>
                            </div>
                        </>
                    )}
                </div>
            </div>
        </div>
    )
}

// ── MediaCard Component ───────────────────────────────────────────────────────

const THUMBNAIL_RETRY_DELAY_MS = 2500
const THUMBNAIL_MAX_RETRIES = 3
const THUMBNAIL_LAZY_MARGIN_PX = 200

function usePreviewHover(item: MediaItem, restricted: boolean) {
    const [previewUrls, setPreviewUrls] = useState<string[] | null>(null)
    const [previewIndex, setPreviewIndex] = useState(0)
    const hoveringRef = useRef(false)
    const fetchedRef = useRef(false)
    const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

    function startCycling(urls: string[]) {
        if (intervalRef.current) clearInterval(intervalRef.current)
        if (urls.length > 1) {
            setPreviewIndex(0)
            intervalRef.current = setInterval(() => setPreviewIndex(i => i + 1), 800)
        }
    }

    function handleMouseEnter() {
        if (restricted || item.type !== 'video' || !item.thumbnail_url) return
        hoveringRef.current = true
        if (!fetchedRef.current) {
            fetchedRef.current = true
            mediaApi.getThumbnailPreviews(item.id).then(data => {
                if (data.previews?.length > 1) {
                    setPreviewUrls(data.previews)
                    if (hoveringRef.current) startCycling(data.previews)
                }
            }).catch(() => {})
        } else if (previewUrls && previewUrls.length > 1) {
            startCycling(previewUrls)
        }
    }

    function handleMouseLeave() {
        hoveringRef.current = false
        if (intervalRef.current) {
            clearInterval(intervalRef.current)
            intervalRef.current = null
        }
        setPreviewIndex(0)
    }

    useEffect(() => () => {
        if (intervalRef.current) clearInterval(intervalRef.current)
    }, [])

    const currentThumbnail =
        previewUrls && previewUrls.length > 0
            ? previewUrls[previewIndex % previewUrls.length]
            : null

    return {previewUrls, previewIndex, currentThumbnail, handleMouseEnter, handleMouseLeave}
}

/** Append cache-buster for mature thumbnails when user can view them, so the browser
 * fetches the real image instead of serving the cached censored placeholder after login. */
function thumbnailUrlForMatureAccess(
    url: string | undefined,
    isMature: boolean,
    canViewMature: boolean,
): string | undefined {
    if (!url) return undefined
    if (isMature && canViewMature) {
        const sep = url.includes('?') ? '&' : '?'
        return `${url}${sep}_m=1`
    }
    return url
}

function MediaCardThumbnailBlock({
    item,
    restricted,
    thumbnailError,
    imgLoaded,
    inView,
    thumbnailSrc,
    baseThumbnailUrl,
    previewUrls,
    onError,
    onLoad,
}: {
    item: MediaItem
    restricted: boolean
    thumbnailError: boolean
    imgLoaded: boolean
    inView: boolean
    thumbnailSrc: string | null
    baseThumbnailUrl: string | undefined
    previewUrls: string[] | null
    onError: () => void
    onLoad: () => void
}) {
    if (item.thumbnail_url && !thumbnailError) {
        return (
            <>
                {item.blur_hash && (
                    <BlurHashPlaceholder
                        hash={item.blur_hash}
                        className="media-thumbnail media-thumbnail-blurhash"
                        style={{ position: 'absolute', inset: 0, opacity: imgLoaded ? 0 : 1, transition: 'opacity 0.2s ease' }}
                    />
                )}
                {!item.blur_hash && !imgLoaded && (
                    <div className="media-thumbnail-placeholder" style={{ position: 'absolute', inset: 0, zIndex: 0 }}>
                        <i className={item.type === 'video' ? 'bi bi-play-circle' : 'bi bi-music-note-beamed'} />
                    </div>
                )}
                <img
                    className="media-thumbnail"
                    src={inView ? (thumbnailSrc || baseThumbnailUrl || item.thumbnail_url) : undefined}
                    srcSet={(!previewUrls || previewUrls.length === 0) && baseThumbnailUrl
                        ? [160, 320, 640].map(w => `${baseThumbnailUrl}${baseThumbnailUrl.includes('?') ? '&' : '?'}w=${w} ${w}w`).join(', ')
                        : undefined}
                    sizes={(!previewUrls || previewUrls.length === 0) ? '(max-width: 640px) 160px, (max-width: 1024px) 320px, 640px' : undefined}
                    alt={formatTitle({ value: item.name })}
                    loading={inView ? 'eager' : 'lazy'}
                    style={{
                        ...(restricted ? { filter: 'blur(16px)', pointerEvents: 'none' } : {}),
                        opacity: !imgLoaded && item.blur_hash ? 0 : 1,
                        transition: 'opacity 0.2s ease',
                        position: 'relative',
                        zIndex: 1,
                    }}
                    onError={onError}
                    onLoad={onLoad}
                />
            </>
        )
    }
    // Thumbnail failed or not available — show blur hash or gradient placeholder
    if (item.blur_hash) {
        return <BlurHashPlaceholder hash={item.blur_hash} className="media-thumbnail" />
    }
    return (
        <div className="media-thumbnail-placeholder">
            <i className={item.type === 'video' ? 'bi bi-play-circle' : 'bi bi-music-note-beamed'} />
        </div>
    )
}

function getRestrictedPlayTitle(restricted: boolean, isAuthenticated: boolean): string | undefined {
    if (!restricted) return undefined
    return isAuthenticated ? 'Enable mature content in profile settings' : 'Sign in to play 18+ content'
}

function MediaCardMatureOverlay({ item, isAuthenticated }: { item: MediaItem; isAuthenticated: boolean }) {
    return (
        <div className="mature-gate-overlay">
            <i className="bi bi-shield-lock-fill" />
            <span>18+ Content</span>
            {isAuthenticated
                ? (
                    <Link
                        to={`/profile?mature_redirect=${encodeURIComponent(`/player?id=${item.id}`)}`}
                        className="mature-gate-login"
                        onClick={(e) => e.stopPropagation()}
                    >
                        Enable in profile settings
                    </Link>
                )
                : (
                    <Link
                        to={`/login?redirect=${encodeURIComponent(`/player?id=${item.id}`)}`}
                        className="mature-gate-login"
                        onClick={(e) => e.stopPropagation()}
                    >
                        Sign in to view
                    </Link>
                )}
        </div>
    )
}

function MediaCard({
                       item,
                       isPlaying,
                       onPlay,
                       canDownload,
                       canViewMature,
                       isAuthenticated,
                   }: {
    item: MediaItem
    isPlaying: boolean
    onPlay: (item: MediaItem) => void
    canDownload: boolean
    canViewMature: boolean
    isAuthenticated: boolean
}) {
    const navigate = useNavigate()
    const restricted = item.is_mature && !canViewMature
    const previewHover = usePreviewHover(item, restricted)
    const baseThumbnailUrl = thumbnailUrlForMatureAccess(
        item.thumbnail_url ?? undefined,
        item.is_mature,
        canViewMature,
    )
    const currentThumbnail = previewHover.currentThumbnail ?? baseThumbnailUrl

    const [thumbnailSrc, setThumbnailSrc] = useState<string | null>(() =>
        thumbnailUrlForMatureAccess(item.thumbnail_url ?? undefined, item.is_mature, canViewMature) ?? null)
    const [thumbnailError, setThumbnailError] = useState(false)
    const [imgLoaded, setImgLoaded] = useState(false)
    const [inView, setInView] = useState(false)
    const containerRef = useRef<HTMLDivElement>(null)
    const retryCountRef = useRef(0)
    const retryTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

    useEffect(() => {
        if (!currentThumbnail) return
        const thumbnail = currentThumbnail
        queueMicrotask(() => {
            setThumbnailError(false)
            setThumbnailSrc(thumbnail)
            setImgLoaded(false)
            retryCountRef.current = 0
        })
    }, [currentThumbnail])

    useEffect(() => () => {
        if (retryTimeoutRef.current) clearTimeout(retryTimeoutRef.current)
    }, [])

    useEffect(() => {
        const el = containerRef.current
        if (!el) return
        const obs = new IntersectionObserver(
            ([entry]) => { if (entry?.isIntersecting) setInView(true) },
            { rootMargin: `${THUMBNAIL_LAZY_MARGIN_PX}px` }
        )
        obs.observe(el)
        return () => obs.disconnect()
    }, [])

    function handleThumbnailError() {
        const baseUrl = baseThumbnailUrl ?? item.thumbnail_url
        if (!baseUrl || retryCountRef.current >= THUMBNAIL_MAX_RETRIES) {
            setThumbnailError(true)
            return
        }
        retryCountRef.current += 1
        retryTimeoutRef.current = setTimeout(() => {
            retryTimeoutRef.current = null
            const sep = baseUrl.includes('?') ? '&' : '?'
            setThumbnailSrc(`${baseUrl}${sep}_=${Date.now()}`)
        }, THUMBNAIL_RETRY_DELAY_MS)
    }

    function goToPlayer() {
        if (restricted) return
        navigate(`/player?id=${encodeURIComponent(item.id)}`)
    }

    return (
        <div className={`media-card ${isPlaying ? 'playing' : ''} ${restricted ? 'mature-restricted' : ''}`}>
            <div
                ref={containerRef}
                onClick={goToPlayer}
                style={{ cursor: restricted ? 'default' : 'pointer', position: 'relative' }}
                onMouseEnter={previewHover.handleMouseEnter}
                onMouseLeave={previewHover.handleMouseLeave}
            >
                <MediaCardThumbnailBlock
                    item={item}
                    restricted={restricted}
                    thumbnailError={thumbnailError}
                    imgLoaded={imgLoaded}
                    inView={inView}
                    thumbnailSrc={thumbnailSrc}
                    baseThumbnailUrl={baseThumbnailUrl}
                    previewUrls={previewHover.previewUrls}
                    onError={handleThumbnailError}
                    onLoad={() => { retryCountRef.current = 0; setImgLoaded(true) }}
                />
                {restricted && <MediaCardMatureOverlay item={item} isAuthenticated={isAuthenticated} />}
            </div>
            <div className="media-card-body">
                <div className="media-card-title">{formatTitle({ value: item.name })}</div>
                <div className="media-card-meta">
                    <span>
                        <span className={`media-card-type-badge badge-${item.type}`}>{item.type}</span>
                    </span>
                    {item.duration > 0 && <span><i className="bi bi-clock" /> {formatDuration({ seconds: item.duration })}</span>}
                    {item.views > 0 && <span><i className="bi bi-eye" /> {item.views}</span>}
                    {item.is_mature && <span className="media-card-type-badge badge-mature">18+</span>}
                </div>
                <div className="media-card-actions">
                    <button
                        className="media-card-btn media-card-btn-play"
                        onClick={() => !restricted && onPlay(item)}
                        disabled={restricted}
                        title={getRestrictedPlayTitle(restricted, isAuthenticated)}
                    >
                        <i className="bi bi-play-fill" /> Play
                    </button>
                    {canDownload && !restricted && (
                        <a
                            href={mediaApi.getDownloadUrl(item.id)}
                            download
                            className="media-card-btn"
                            style={{ flex: 1, width: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', textDecoration: 'none', color: 'inherit' }}
                            title="Download"
                        >
                            <i className="bi bi-download" />
                        </a>
                    )}
                    {!restricted && (
                        <button className="media-card-btn" onClick={goToPlayer} title="Full player"><i className="bi bi-box-arrow-up-right" /></button>
                    )}
                </div>
            </div>
        </div>
    )
}

// ── Inline Player ─────────────────────────────────────────────────────────────

function InlinePlayer({
                          nowPlaying,
                          playlist,
                          onEnded,
                      }: {
    nowPlaying: MediaItem | null
    playlist: MediaItem[]
    onEnded: (next: MediaItem | null) => void
}) {
    const audioRef = useRef<HTMLAudioElement>(null)
    const videoRef = useRef<HTMLVideoElement>(null)
    const [isPlaying, setIsPlaying] = useState(false)
    const [currentTime, setCurrentTime] = useState(0)
    const [duration, setDuration] = useState(0)
    const [volume, setVolume] = useState(1)
    const [muted, setMuted] = useState(false)
    const [audioReady, setAudioReady] = useState(false)
    const [showEq, setShowEq] = useState(false)

    const isVideo = nowPlaying?.type === 'video'
    const activeRef = isVideo ? videoRef : audioRef

    // Wire up equalizer to the audio element (EQ only applies to audio, not video)
    const eq = useEqualizer(audioRef, audioReady)

    // Mark audio element as ready after mount (defer to avoid setState-in-effect lint)
    useEffect(() => {
        if (audioRef.current) queueMicrotask(() => { setAudioReady(true); })
    }, [])

    const handleEqToggle = useCallback(() => { setShowEq(v => !v); }, [])

    // Load new media when nowPlaying changes (volume/muted applied so new track respects current settings).
    // volume and muted are intentionally omitted — adding them would restart the track on every volume change.
    // A separate effect below syncs volume/muted independently.
    useEffect(() => {
        if (!nowPlaying) return
        const el = activeRef.current
        if (!el) return
        el.src = mediaApi.getStreamUrl(nowPlaying.id)
        el.volume = volume
        el.muted = muted
        el.play().then(() => { setIsPlaying(true); }).catch(() => setIsPlaying(false))
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [nowPlaying, activeRef])

    useEffect(() => {
        const el = activeRef.current
        if (el) {
            el.volume = volume
            el.muted = muted
        }
    }, [volume, muted, activeRef])

    function togglePlay() {
        const el = activeRef.current
        if (!el || !nowPlaying) return
        if (el.paused) {
            el.play().then(() => { setIsPlaying(true); }).catch(() => {
            })
        } else {
            el.pause()
            setIsPlaying(false)
        }
    }

    function handlePrev() {
        if (!nowPlaying || !playlist.length) return
        const idx = playlist.findIndex(i => i.id === nowPlaying.id)
        const prev = idx > 0 ? playlist[idx - 1] : playlist[playlist.length - 1]
        onEnded(prev)
    }

    function handleNext() {
        if (!nowPlaying || !playlist.length) return
        const idx = playlist.findIndex(i => i.id === nowPlaying.id)
        const next = idx < playlist.length - 1 ? playlist[idx + 1] : null
        onEnded(next)
    }

    function handleTimeUpdate() {
        const el = activeRef.current
        if (el) setCurrentTime(el.currentTime)
    }

    function handleLoadedMetadata() {
        const el = activeRef.current
        if (el) setDuration(el.duration)
    }

    function handleProgressClick(e: React.MouseEvent<HTMLDivElement>) {
        const el = activeRef.current
        if (!el || !duration) return
        const rect = e.currentTarget.getBoundingClientRect()
        const ratio = (e.clientX - rect.left) / rect.width
        el.currentTime = ratio * duration
    }

    function handleVolumeChange(e: ChangeEvent<HTMLInputElement>) {
        const v = parseFloat(e.target.value)
        setVolume(v)
        if (audioRef.current) audioRef.current.volume = v
        if (videoRef.current) videoRef.current.volume = v
    }

    function handleEnded() {
        handleNext()
    }

    if (!nowPlaying) return null

    const progress = duration > 0 ? (currentTime / duration) * 100 : 0

    return (
        <div className="inline-player">
            {/* EQ panel floats above the player bar (audio only) */}
            {!isVideo && (
                <EqualizerPanel
                    visible={showEq}
                    onClose={handleEqToggle}
                    {...eq}
                />
            )}
            <audio
                ref={audioRef}
                style={{display: 'none'}}
                onTimeUpdate={handleTimeUpdate}
                onLoadedMetadata={handleLoadedMetadata}
                onEnded={handleEnded}
                onPlay={() => { setIsPlaying(true); }}
                onPause={() => { setIsPlaying(false); }}
            />
            <video
                ref={videoRef}
                style={{display: 'none'}}
                onTimeUpdate={handleTimeUpdate}
                onLoadedMetadata={handleLoadedMetadata}
                onEnded={handleEnded}
                onPlay={() => { setIsPlaying(true); }}
                onPause={() => { setIsPlaying(false); }}
                playsInline
            />
            <div className="player-content">
                <div className="player-info">
                    <div className="player-title">{nowPlaying.name}</div>
                    <div className="player-meta">
                        <i className={isVideo ? 'bi bi-play-fill' : 'bi bi-music-note-beamed'}/> {isVideo ? 'Video' : 'Audio'} · {formatDuration({ seconds: duration })}
                    </div>
                </div>

                <div className="player-center">
                    <div className="player-controls">
                        <button className="player-btn" onClick={handlePrev} title="Previous"><i
                            className="bi bi-skip-start-fill"/></button>
                        <button className="player-btn player-btn-play" onClick={togglePlay} title="Play/Pause">
                            {isPlaying ? <i className="bi bi-pause-fill"/> : <i className="bi bi-play-fill"/>}
                        </button>
                        <button className="player-btn" onClick={handleNext} title="Next"><i
                            className="bi bi-skip-end-fill"/></button>
                    </div>
                    <div className="player-progress-row">
                        <span>{formatDuration({ seconds: currentTime })}</span>
                        <div
                            className="player-progress-bar"
                            onClick={handleProgressClick}
                            onTouchStart={(e) => {
                                e.preventDefault()
                                const touch = e.touches[0]
                                if (!touch) return
                                const el = activeRef.current
                                if (!el || !duration) return
                                const rect = e.currentTarget.getBoundingClientRect()
                                const ratio = Math.max(0, Math.min(1, (touch.clientX - rect.left) / rect.width))
                                el.currentTime = ratio * duration
                            }}
                            onTouchMove={(e) => {
                                e.preventDefault()
                                const touch = e.touches[0]
                                if (!touch) return
                                const el = activeRef.current
                                if (!el || !duration) return
                                const rect = e.currentTarget.getBoundingClientRect()
                                const ratio = Math.max(0, Math.min(1, (touch.clientX - rect.left) / rect.width))
                                el.currentTime = ratio * duration
                            }}
                            style={{ touchAction: 'none' }}
                        >
                            <div className="player-progress-fill" style={{width: `${progress}%`}}/>
                        </div>
                        <span>{formatDuration({ seconds: duration })}</span>
                    </div>
                </div>

                <div className="player-right">
                    <div className="player-volume-row">
                        <button
                            type="button"
                            className="player-btn"
                            onClick={() => {
                                const el = activeRef.current
                                if (el) {
                                    el.muted = !el.muted
                                    setMuted(el.muted)
                                }
                            }}
                            title={muted ? 'Unmute' : 'Mute'}
                            aria-label={muted ? 'Unmute' : 'Mute'}
                        >
                            <i className={muted ? 'bi bi-volume-mute-fill' : 'bi bi-volume-up-fill'} />
                        </button>
                        <input
                            type="range"
                            className="player-volume-slider"
                            min="0"
                            max="1"
                            step="0.05"
                            value={volume}
                            onChange={handleVolumeChange}
                        />
                    </div>
                    {!isVideo && (
                        <button
                            className={`player-btn${showEq ? ' player-btn--active' : ''}`}
                            onClick={handleEqToggle}
                            title="Equalizer"
                        >
                            <i className="bi bi-sliders"/>
                        </button>
                    )}
                    <Link to={`/player?id=${encodeURIComponent(nowPlaying.id)}`} className="player-btn"
                          title="Open full player">
                        <i className="bi bi-box-arrow-up-right"/>
                    </Link>
                </div>
            </div>
        </div>
    )
}

// ── UserMenu Component ────────────────────────────────────────────────────────

function UserMenu() {
    const navigate = useNavigate()
    const user = useAuthStore((s) => s.user)
    const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
    const isAdmin = useAuthStore((s) => s.isAdmin)
    const allowGuests = useAuthStore((s) => s.allowGuests)
    const logout = useAuthStore((s) => s.logout)
    const [open, setOpen] = useState(false)

    async function handleLogout() {
        await logout()
        // If guests can browse, stay on index; otherwise go to login
        navigate(allowGuests ? '/' : '/login')
    }

    if (!isAuthenticated) {
        return (
            <div className="user-auth-section">
                <button className="controls-btn" onClick={() => { navigate('/login'); }}>Login</button>
                <button className="controls-btn" onClick={() => { navigate('/signup'); }}>Sign Up</button>
            </div>
        )
    }

    return (
        <div className="user-auth-section">
            <div className="user-dropdown-wrapper">
                <button className="controls-btn" onClick={() => { setOpen(o => !o); }}>
                    <i className="bi bi-person-fill"/> {user?.username}
                    {isAdmin && <span className="admin-badge">Admin</span>}
                </button>
                {open && (
                    <>
                        <div style={{position: 'fixed', inset: 0, zIndex: 999}} onClick={() => { setOpen(false); }}/>
                        <div className="user-dropdown-menu" style={{zIndex: 1000}}>
                            <div className="user-dropdown-header">{user?.username} · {user?.role}</div>
                            <div className="user-dropdown-divider"/>
                            <div className="user-dropdown-item" onClick={() => {
                                navigate('/profile');
                                setOpen(false)
                            }}>
                                <i className="bi bi-gear-fill"/> Settings & Profile
                            </div>
                            {isAdmin && (
                                <>
                                    <div className="user-dropdown-divider"/>
                                    <div className="user-dropdown-item" style={{fontWeight: 600, color: '#667eea'}}>
                                        <i className="bi bi-shield-fill"/> Admin Features
                                    </div>
                                    <div className="user-dropdown-item" onClick={() => {
                                        navigate('/admin');
                                        setOpen(false)
                                    }}>
                                        <i className="bi bi-speedometer2"/> Admin Dashboard
                                    </div>
                                    <div className="user-dropdown-item" onClick={() => {
                                        navigate('/admin', {state: {tab: 'users'}});
                                        setOpen(false)
                                    }}>
                                        <i className="bi bi-people-fill"/> Manage Users
                                    </div>
                                </>
                            )}
                            <div className="user-dropdown-divider"/>
                            <div className="user-dropdown-item" onClick={handleLogout}>
                                <i className="bi bi-box-arrow-right"/> Logout
                            </div>
                        </div>
                    </>
                )}
            </div>
        </div>
    )
}

// ── SuggestionThumbnail ───────────────────────────────────────────────────────
// Renders a suggestion card thumbnail. Falls back to a media-type-appropriate
// placeholder if the URL is absent or fails to load (thumbnail still generating).

const THUMB_STYLE: CSSProperties = {
    width: '100%',
    borderRadius: 4,
    marginBottom: 6,
    aspectRatio: '16/9',
    objectFit: 'cover',
    background: 'var(--card-bg, #1e1e2e)',
}

function SuggestionThumbnail({url, mediaType}: { url?: string; mediaType?: string }) {
    const [failed, setFailed] = useState(false)
    const isAudio = mediaType === 'audio'

    if (!url || failed) {
        // Placeholder: music note for audio, film frame for video
        return (
            <div style={{
                ...THUMB_STYLE,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                fontSize: 32,
                color: 'var(--text-muted, #666)',
            }}>
                <i className={isAudio ? 'bi bi-music-note-beamed' : 'bi bi-film'}/>
            </div>
        )
    }

    return (
        <img
            src={url}
            alt=""
            loading="lazy"
            style={THUMB_STYLE}
            onError={() => { setFailed(true); }}
        />
    )
}

// ── Main IndexPage ────────────────────────────────────────────────────────────
// Cognitive complexity is high due to many URL-driven filters, queries, and conditional sections.
// NOTE: Consider extracting useIndexPageState() + IndexPageContent to reduce complexity.
export function IndexPage() {
    const navigate = useNavigate()
    const queryClient = useQueryClient()
    const isAuthenticated = useAuthStore((s) => s.isAuthenticated)
    const isAdmin = useAuthStore((s) => s.isAdmin)
    const permissions = useAuthStore((s) => s.permissions)
    const user = useAuthStore((s) => s.user)
    const {theme, toggleTheme} = useThemeStore()
    const serverSettings = useSettingsStore((s) => s.serverSettings)
    const uploadsEnabled = serverSettings?.uploads?.enabled ?? true
    const playlistsEnabled = serverSettings?.features?.enablePlaylists !== false
    const suggestionsEnabled = serverSettings?.features?.enableSuggestions !== false

    // Pagination & filter state persisted in URL so back-navigation restores position
    const [searchParams, setSearchParams] = useSearchParams()
    const rawDefault =
        user?.preferences?.items_per_page ?? serverSettings?.ui?.items_per_page ?? 24
    const defaultLimit = normalizeLimit(Number(rawDefault) || 24, 24)

    // Refs so updateParams stays stable even as defaultLimit or setSearchParams changes.
    // In React Router v7, setSearchParams gets a new identity on each location update, so
    // depending on it directly would make updateParams unstable — causing the search debounce
    // effect to re-fire on every page navigation and reset the page back to 1.
    const defaultLimitRef = useRef(defaultLimit)
    const setSearchParamsRef = useRef(setSearchParams)
    useEffect(() => {
        defaultLimitRef.current = defaultLimit
        setSearchParamsRef.current = setSearchParams
    }, [defaultLimit, setSearchParams])

    const page = Math.max(1, Number(searchParams.get('page')) || 1)
    const rawLimit = Number(searchParams.get('limit')) || defaultLimit
    const limit = normalizeLimit(rawLimit, defaultLimit)
    const mediaType = searchParams.get('type') || 'all'
    const sortBy = searchParams.get('sort') || 'date'
    const sortOrder = searchParams.get('order') || 'desc'
    const category = searchParams.get('category') || 'all'
    const search = searchParams.get('q') || ''
    const [searchInput, setSearchInput] = useState(search)

    // Helper: update URL params (replace: true to avoid flooding browser history)
    const updateParams = useCallback((updates: Record<string, string | number | null>) => {
        setSearchParamsRef.current(prev => {
            const next = new URLSearchParams(prev)
            for (const [key, value] of Object.entries(updates)) {
                if (value === null || value === '') {
                    next.delete(key)
                } else {
                    next.set(key, String(value))
                }
            }
            // Clean defaults out of URL to keep it tidy
            if (next.get('page') === '1') next.delete('page')
            if (next.get('type') === 'all') next.delete('type')
            if (next.get('sort') === 'date') next.delete('sort')
            if (next.get('order') === 'desc') next.delete('order')
            if (next.get('category') === 'all') next.delete('category')
            if (next.get('limit') === String(defaultLimitRef.current)) next.delete('limit')
            return next
        }, {replace: true})
    }, []) // truly stable — reads both setSearchParams and defaultLimit via refs

    const setPage = useCallback((v: number | ((prev: number) => number)) => {
        const newPage = typeof v === 'function' ? v(page) : v
        updateParams({page: newPage})
    }, [page, updateParams])

    const setLimit = useCallback(
        (v: number) => {
            updateParams({limit: normalizeLimit(v, defaultLimit), page: null})
        },
        [updateParams, defaultLimit],
    )

    const setMediaType = useCallback((v: string) => {
        updateParams({type: v, page: null})
    }, [updateParams])

    const setSortBy = useCallback((v: string) => {
        updateParams({sort: v, page: null})
    }, [updateParams])

    const setSortOrder = useCallback((v: string) => {
        updateParams({order: v, page: null})
    }, [updateParams])

    const setCategory = useCallback((v: string) => {
        updateParams({category: v, page: null})
    }, [updateParams])

    const [showFilters, setShowFilters] = useState(true)

    // Playlist store — shuffle, repeat, and queue management
    const {shuffleMode, repeatMode, toggleShuffle, toggleRepeat, setPlaylistFromIds} = usePlaylistStore()

    // UI state
    const [showUpload, setShowUpload] = useState(false)
    const [showSidebar, setShowSidebar] = useState(false)
    const [nowPlaying, setNowPlaying] = useState<MediaItem | null>(null)
    const [newPlaylistName, setNewPlaylistName] = useState('')
    const [renameId, setRenameId] = useState<string | null>(null)
    const [renameName, setRenameName] = useState('')
    const [playlistError, setPlaylistError] = useState<string | null>(null)

    // Auto-clear playlist errors after 5 seconds
    useEffect(() => {
        if (!playlistError) return
        const t = setTimeout(() => { setPlaylistError(null); }, 5000)
        return () => { clearTimeout(t); }
    }, [playlistError])

    // Debounced search — syncs typed input to URL param
    useEffect(() => {
        const t = setTimeout(() => {
            updateParams({q: searchInput || null, page: null})
        }, 400)
        return () => clearTimeout(t)
    }, [searchInput, updateParams])

    // Media list query — keepPreviousData prevents the grid from blanking out
    // when changing page/filter/sort; the old results stay visible until the new ones arrive.
    const {data: mediaData, isPending: mediaInitialLoading, isFetching: mediaFetching, isPlaceholderData: mediaStale, error: mediaError} = useQuery({
        queryKey: ['media', {page, limit, type: mediaType, sort: sortBy, order: sortOrder, category, search}],
        queryFn: () => mediaApi.list({
            page,
            limit: limit === 0 ? undefined : limit,
            type: mediaType === 'all' ? undefined : mediaType,
            sort: sortBy,
            sort_order: sortOrder,
            category: category === 'all' ? undefined : category,
            search: search || undefined,
        }),
        placeholderData: keepPreviousData,
    })

    // Categories query — rarely changes, cache for 10 minutes
    const {data: categories = []} = useQuery<MediaCategory[]>({
        queryKey: ['categories'],
        queryFn: () => mediaApi.getCategories(),
        staleTime: 10 * 60 * 1000,
    })

    // Analytics query — admin-only endpoint, only execute when user is admin
    const {data: analytics} = useQuery<AnalyticsSummary>({
        queryKey: ['analytics-summary'],
        queryFn: () => analyticsApi.getSummary(),
        enabled: isAdmin,
    })

    // Section visibility from user preferences — default true so sections show before prefs load
    const showContinueWatching = user?.preferences?.show_continue_watching ?? true
    const showRecommended = user?.preferences?.show_recommended ?? true
    const showTrending = user?.preferences?.show_trending ?? true
    const canViewMature = permissions.can_view_mature && (user?.preferences?.show_mature === true)

    // Retry strategy for suggestion queries: retry on 503 (catalogue not seeded yet)
    const suggestionsRetry = (failureCount: number, error: Error) => {
        if (error instanceof ApiError && error.status === 503) return failureCount < 5
        return failureCount < 1
    }
    const suggestionsRetryDelay = (attempt: number) => Math.min(1000 * 2 ** attempt, 10000)

    // Continue watching query — in-progress items for authenticated users
    const {data: continueWatching = []} = useQuery<Suggestion[]>({
        queryKey: ['continue-watching', canViewMature],
        queryFn: () => suggestionsApi.getContinueWatching(),
        enabled: isAuthenticated && showContinueWatching,
        staleTime: 2 * 60 * 1000,
        retry: suggestionsRetry,
        retryDelay: suggestionsRetryDelay,
        select: data => (data ?? []).slice(0, 8),
    })

    // Personalized suggestions — public, shows genre/history-based picks
    const {
        data: suggestions = [],
        isLoading: suggestionsLoading,
        isError: suggestionsError,
        refetch: suggestionsRefetch,
    } = useQuery<Suggestion[]>({
        queryKey: ['suggestions', canViewMature],
        queryFn: () => suggestionsApi.get(),
        enabled: suggestionsEnabled && showRecommended,
        staleTime: 10 * 60 * 1000,
        retry: suggestionsRetry,
        retryDelay: suggestionsRetryDelay,
        select: data => (data ?? []).slice(0, 8),
    })

    // Trending suggestions — public, most-viewed recently
    const {
        data: trending = [],
        isLoading: trendingLoading,
        isError: trendingError,
        refetch: trendingRefetch,
    } = useQuery<Suggestion[]>({
        queryKey: ['suggestions-trending', canViewMature],
        queryFn: () => suggestionsApi.getTrending(),
        enabled: suggestionsEnabled && showTrending,
        staleTime: 10 * 60 * 1000,
        retry: suggestionsRetry,
        retryDelay: suggestionsRetryDelay,
        select: data => (data ?? []).slice(0, 8),
    })

    // Playlists query — backend may return null for empty list (Go nil slice),
    // so normalize with select to always get an array
    const {data: playlists = []} = useQuery<Playlist[] | null, Error, Playlist[]>({
        queryKey: ['playlists'],
        queryFn: () => playlistApi.list(),
        enabled: isAuthenticated,
        staleTime: 5 * 60 * 1000,
        select: (data) => data ?? [],
    })

    // Server version (from deploy script / VERSION file) — shown in footer
    const {data: versionData} = useQuery({
        queryKey: ['version'],
        queryFn: () => versionApi.get(),
        staleTime: 5 * 60 * 1000,
    })

    const items = useMemo(() => mediaData?.items ?? [], [mediaData?.items])
    const totalPages = mediaData?.total_pages ?? 1
    const hasNextPage = page < totalPages

    // Batch prefetch thumbnails — single API call, then preload images into browser cache.
    // For mature items when canViewMature, add cache-buster so we load real thumbnails after login.
    useEffect(() => {
        if (items.length === 0) return
        const ids = items
            .filter(m => m.thumbnail_url)
            .map(m => m.id)
            .slice(0, 50)
        if (ids.length === 0) return
        const itemMap = new Map(items.map(m => [m.id, m]))
        mediaApi.getThumbnailBatch(ids, 320)
            .then(res => {
                const base = window.location.origin
                Object.entries(res.thumbnails ?? {}).forEach(([id, url]) => {
                    const item = itemMap.get(id)
                    const finalUrl = thumbnailUrlForMatureAccess(
                        url.startsWith('/') ? url : `/${url}`,
                        !!item?.is_mature,
                        canViewMature,
                    ) ?? url
                    const full = finalUrl.startsWith('/') ? base + finalUrl : finalUrl
                    const img = new Image()
                    img.src = full
                })
            })
            .catch(() => {})
    }, [items, canViewMature])

    // Prefetch next page for faster pagination
    useEffect(() => {
        if (!hasNextPage) return
        queryClient.prefetchQuery({
            queryKey: ['media', {page: page + 1, limit, type: mediaType, sort: sortBy, order: sortOrder, category, search}],
            queryFn: () => mediaApi.list({
                page: page + 1,
                limit: limit === 0 ? undefined : limit,
                type: mediaType === 'all' ? undefined : mediaType,
                sort: sortBy,
                sort_order: sortOrder,
                category: category === 'all' ? undefined : category,
                search: search || undefined,
            }),
        })
    }, [page, limit, mediaType, sortBy, sortOrder, category, search, hasNextPage, queryClient])
    const totalItems = mediaData?.total_items ?? 0

    function handlePlay(item: MediaItem) {
        setNowPlaying(item)
        // Track view
        analyticsApi.trackEvent({type: 'play', media_id: item.id}).catch(() => {
        })
        // ST-05: removed zero-position trackPosition call — writing position=0 at play-start
        // overwrites any saved resume position before the player has a chance to read it.
    }

    function handlePlayerEnded(next: MediaItem | null) {
        setNowPlaying(next)
    }

    async function handleCreatePlaylist() {
        const name = newPlaylistName.trim() || `Playlist ${(playlists.length + 1)}`
        setPlaylistError(null)
        try {
            await playlistApi.create(name)
            setNewPlaylistName('')
            queryClient.invalidateQueries({queryKey: ['playlists']})
        } catch (err) {
            setPlaylistError(err instanceof Error ? err.message : 'Failed to create playlist')
        }
    }

    async function handleDeletePlaylist(id: string) {
        if (!window.confirm('Delete this playlist?')) return
        setPlaylistError(null)
        try {
            await playlistApi.delete(id)
            queryClient.invalidateQueries({queryKey: ['playlists']})
        } catch (err) {
            setPlaylistError(err instanceof Error ? err.message : 'Failed to delete playlist')
        }
    }

    async function handleRenamePlaylist(id: string) {
        const name = renameName.trim()
        if (!name) return
        setPlaylistError(null)
        try {
            await playlistApi.update(id, {name})
            setRenameId(null)
            queryClient.invalidateQueries({queryKey: ['playlists']})
        } catch (err) {
            setPlaylistError(err instanceof Error ? err.message : 'Failed to rename playlist')
        }
    }

    async function handleExportPlaylist(id: string, name: string, format: 'json' | 'm3u' | 'm3u8') {
        try {
            const blob = await playlistApi.export(id, format)
            const url = window.URL.createObjectURL(blob)
            const a = document.createElement('a')
            a.href = url
            a.download = `${name}.${format}`
            document.body.appendChild(a)
            a.click()
            document.body.removeChild(a)
            window.URL.revokeObjectURL(url)
        } catch (err) {
            setPlaylistError(err instanceof Error ? err.message : 'Export failed')
        }
    }

    async function handleAddToPlaylist(playlistId: string) {
        if (!nowPlaying) return
        setPlaylistError(null)
        try {
            await playlistApi.addItem(playlistId, {
                media_id: nowPlaying.id,
                title: nowPlaying.name,
            })
            queryClient.invalidateQueries({queryKey: ['playlists']})
        } catch (err) {
            setPlaylistError(err instanceof Error ? err.message : 'Failed to add to playlist')
        }
    }

    function handleRefresh() {
        queryClient.invalidateQueries({queryKey: ['media']})
        queryClient.invalidateQueries({queryKey: ['analytics-summary']})
    }

    function handlePlayAll() {
        const playable = items.filter(i => !i.is_mature || (permissions.can_view_mature && user?.preferences?.show_mature === true))
        if (playable.length === 0) return
        setPlaylistFromIds(playable.map(i => i.id), playable.map(i => i.name))
        setNowPlaying(playable[0])
    }

    // Upload done → refresh media
    function handleUploadDone() {
        queryClient.invalidateQueries({queryKey: ['media']})
    }

    function renderSuggestionSection(
        loading: boolean,
        error: boolean,
        items: Suggestion[],
        onRetry: () => void,
        loadingLabel: string,
        errorMessage: string,
        emptyMessage: string,
        renderMeta: (entry: Suggestion) => React.ReactNode
    ) {
        if (loading) return <p style={MUTED_TEXT_STYLE}>Loading {loadingLabel}…</p>
        if (error) {
            return (
                <p style={MUTED_TEXT_STYLE}>
                    {errorMessage}{' '}
                    <button type="button" className="controls-btn" style={{marginLeft: 4}} onClick={onRetry}>
                        Retry
                    </button>
                </p>
            )
        }
        if (items.length > 0) {
            return (
                <div className="continue-watching-row">
                    {items.map(entry => (
                        <Link
                            key={entry.media_id}
                            className="continue-card"
                            to={`/player?id=${encodeURIComponent(entry.media_id)}`}
                        >
                            <SuggestionThumbnail url={entry.thumbnail_url} mediaType={entry.media_type}/>
                            <div className="continue-card-name">{formatTitle({value: entry.title || entry.media_id})}</div>
                            {renderMeta(entry)}
                        </Link>
                    ))}
                </div>
            )
        }
        return <p style={MUTED_TEXT_STYLE}>{emptyMessage}</p>
    }

    function renderSuggestionsState() {
        return renderSuggestionSection(
            suggestionsLoading,
            suggestionsError,
            suggestions,
            () => suggestionsRefetch(),
            'suggestions',
            'Suggestions are still loading (catalogue may be scanning).',
            'No recommendations yet. Watch some media to get personalized picks.',
            (entry) => entry.score !== null && entry.score !== undefined
                ? <div className="continue-card-meta"><i className="bi bi-stars"/> {Math.round(entry.score * 100)}% match</div>
                : null
        )
    }

    function renderTrendingState() {
        return renderSuggestionSection(
            trendingLoading,
            trendingError,
            trending,
            () => trendingRefetch(),
            'trending',
            'Trending is still loading.',
            'No trending items yet.',
            () => <div className="continue-card-meta"><i className="bi bi-fire"/> Trending</div>
        )
    }

    function getMediaErrorMessage(err: unknown): string {
        return err instanceof Error ? err.message : 'Something went wrong. Please try again.'
    }

    function getEmptyMediaMessage(): string {
        return search
            ? `No results for "${search}". Try a different search term.`
            : 'Add media files to your library or adjust your filters to get started.'
    }

    function renderMediaGridState() {
        if (mediaError) {
            return (
                <div className="empty-state empty-state-error">
                    <h3>We couldn&apos;t load your library</h3>
                    <p>{getMediaErrorMessage(mediaError)}</p>
                    <button className="controls-btn controls-btn-primary" onClick={() => queryClient.invalidateQueries({queryKey: ['media']})}>
                        <i className="bi bi-arrow-clockwise"/> Try again
                    </button>
                </div>
            )
        }
        if (mediaInitialLoading) {
            return (
                <div className="media-grid media-grid-loading">
                    {Array.from({length: 12}, (_, i) => <MediaCardSkeleton key={i}/>)}
                </div>
            )
        }
        if (items.length === 0 && mediaData?.scanning) {
            return (
                <div className="loading-state">
                    <i className="bi bi-arrow-repeat"/> Scanning your library&hellip; give it a moment.
                </div>
            )
        }
        if (items.length === 0) {
            return (
                <div className="empty-state">
                    <h3>No media found</h3>
                    <p>{getEmptyMediaMessage()}</p>
                    {permissions.can_upload && (
                        <button className="controls-btn controls-btn-primary" onClick={() => setShowUpload(true)}>
                            <i className="bi bi-cloud-upload-fill"/> Upload media
                        </button>
                    )}
                </div>
            )
        }
        return (
            <div
                className="media-grid-wrapper"
                style={{
                    ...((mediaFetching && mediaStale) ? {opacity: 0.92, transition: 'opacity 0.25s ease'} : {opacity: 1, transition: 'opacity 0.25s ease'}),
                }}
            >
                {(mediaFetching && mediaStale) && (
                    <div className="media-grid-updating" role="status">
                        <i className="bi bi-arrow-repeat"/> Updating&hellip;
                    </div>
                )}
                <div className="media-grid">
                    {items.map(item => (
                        <MediaCard
                            key={item.id}
                            item={item}
                            isPlaying={nowPlaying?.id === item.id}
                            onPlay={handlePlay}
                            canDownload={permissions.can_download}
                            canViewMature={canViewMature}
                            isAuthenticated={isAuthenticated}
                        />
                    ))}
                </div>
            </div>
        )
    }

    return (
        <div className="index-page" data-theme={theme}>
            {/* Header */}
            <div className="index-header">
                <h1>Media Streamer Pro</h1>
                <p>Video and Music Streaming Server</p>
                {analytics && !analytics.analytics_disabled && (user?.preferences?.show_analytics !== false) && (
                    <div className="analytics-bar">
                        <span><i
                            className="bi bi-play-circle-fill"/> {(analytics.total_events ?? 0).toLocaleString()} plays</span>
                        <span><i
                            className="bi bi-people-fill"/> {(analytics.unique_clients ?? 0).toLocaleString()} listeners</span>
                        <span><i className="bi bi-lightning-fill"/> {analytics.active_sessions ?? 0} active</span>
                        <span><i
                            className="bi bi-eye-fill"/> {(analytics.total_views ?? 0).toLocaleString()} views</span>
                    </div>
                )}
            </div>

            {/* Controls Bar */}
            <div className="controls-bar">
                <button className="controls-btn" onClick={() => { setShowFilters(f => !f); }}>
                    <i className="bi bi-funnel-fill"/> {showFilters ? 'Hide Filters' : 'Filters'}
                </button>

                <input
                    type="search"
                    className="controls-search"
                    placeholder="Search your media library..."
                    value={searchInput}
                    onChange={e => { setSearchInput(e.target.value); }}
                    aria-label="Search media library"
                />

                <button className="controls-btn" onClick={handleRefresh} title="Refresh"><i
                    className={`bi bi-arrow-counterclockwise${mediaFetching ? ' spin' : ''}`}/> Refresh
                </button>
                <button className="controls-btn controls-btn-success" onClick={handlePlayAll}><i
                    className="bi bi-play-fill"/> Play All
                </button>
                <button
                    className={`controls-btn ${shuffleMode ? 'controls-btn-primary' : ''}`}
                    onClick={toggleShuffle}
                    title={shuffleMode ? 'Shuffle: On' : 'Shuffle: Off'}
                >
                    <i className="bi bi-shuffle"/>
                </button>
                <button
                    className={`controls-btn ${repeatMode !== 'none' ? 'controls-btn-primary' : ''}`}
                    onClick={toggleRepeat}
                    title={`Repeat: ${repeatMode}`}
                >
                    <i className={repeatMode === 'one' ? 'bi bi-repeat-1' : 'bi bi-repeat'}/>
                </button>

                {permissions.can_upload && uploadsEnabled && (
                    <button className="controls-btn" onClick={() => { setShowUpload(true); }}><i
                        className="bi bi-cloud-upload-fill"/> Upload</button>
                )}

                {isAdmin && (
                    <button className="controls-btn" onClick={() => navigate('/admin')}><i
                        className="bi bi-gear-fill"/> Settings</button>
                )}

                <button className="controls-btn" onClick={toggleTheme} title="Toggle theme">
                    {theme === 'dark' ? <i className="bi bi-sun-fill"/> : <i className="bi bi-moon-fill"/>}
                </button>

                {playlistsEnabled && permissions.can_create_playlists && (
                    <button className="controls-btn" onClick={() => setShowSidebar(true)}><i
                        className="bi bi-collection-fill"/> Playlists</button>
                )}

                <UserMenu/>
            </div>

            {/* Continue Watching — only for authenticated users who haven't disabled it */}
            {isAuthenticated && showContinueWatching && continueWatching.length > 0 && (
                <div className="continue-watching-section">
                    <h3 className="section-heading"><i className="bi bi-play-circle"/> Continue Watching</h3>
                    <div className="continue-watching-row">
                        {continueWatching.map(entry => (
                            <Link
                                key={entry.media_id}
                                className="continue-card"
                                to={`/player?id=${encodeURIComponent(entry.media_id)}`}
                            >
                                <SuggestionThumbnail url={entry.thumbnail_url} mediaType={entry.media_type}/>
                                <div className="continue-card-name">{formatTitle({ value: entry.title || entry.media_id })}</div>
                                <div className="continue-card-meta"><i className="bi bi-play-circle"/> Continue</div>
                            </Link>
                        ))}
                    </div>
                </div>
            )}

            {/* Recommended For You — shown when enabled; loading/error states with retry */}
            {suggestionsEnabled && showRecommended && (
                <div className="continue-watching-section">
                    <h3 className="section-heading"><i className="bi bi-stars"/> Recommended For You</h3>
                    {renderSuggestionsState()}
                </div>
            )}

            {/* Trending — shown when enabled; loading/error states with retry */}
            {suggestionsEnabled && showTrending && (
                <div className="continue-watching-section">
                    <h3 className="section-heading"><i className="bi bi-fire"/> Trending</h3>
                    {renderTrendingState()}
                </div>
            )}

            {/* Filter Panel */}
            {showFilters && (
                <div className="filter-panel">
                    <div className="filter-group">
                        <label htmlFor="f-type">Media Type</label>
                        <select id="f-type" className="filter-select" value={mediaType}
                                onChange={e => setMediaType(e.target.value)}>
                            <option value="all">All Media</option>
                            <option value="video">Videos Only</option>
                            <option value="audio">Music Only</option>
                        </select>
                    </div>

                    <div className="filter-group">
                        <label htmlFor="f-sort">Sort By</label>
                        <select id="f-sort" className="filter-select" value={sortBy}
                                onChange={e => setSortBy(e.target.value)}>
                            <option value="name">Sort by Name</option>
                            <option value="date">Sort by Date Added</option>
                            <option value="size">Sort by File Size</option>
                            <option value="duration">Sort by Duration</option>
                            <option value="views">Sort by Views</option>
                        </select>
                    </div>

                    <div className="filter-group">
                        <label htmlFor="f-order">Order</label>
                        <select id="f-order" className="filter-select" value={sortOrder}
                                onChange={e => setSortOrder(e.target.value)}>
                            <option value="asc">Ascending</option>
                            <option value="desc">Descending</option>
                        </select>
                    </div>

                    {categories.length > 0 && (
                        <div className="filter-group">
                            <label htmlFor="f-cat">Category</label>
                            <select id="f-cat" className="filter-select" value={category}
                                    onChange={e => setCategory(e.target.value)}>
                                <option value="all">All Categories</option>
                                {categories.map(c => (
                                    <option key={c.name} value={c.name}>{c.display_name || c.name}</option>
                                ))}
                            </select>
                        </div>
                    )}

                    <div className="filter-group">
                        <label>Results: {totalItems.toLocaleString()} items</label>
                    </div>
                </div>
            )}

            {/* Media Grid */}
            <div className="media-section">
                {renderMediaGridState()}

                {/* Pagination */}
                {totalPages > 1 && (
                    <div className="pagination">
                        <button
                            className="pagination-btn"
                            disabled={page <= 1}
                            onClick={() => setPage(p => p - 1)}
                        >
                            <i className="bi bi-chevron-left"/> Previous
                        </button>
                        <div style={{display: 'flex', alignItems: 'center', gap: 12}}>
                            <span className="pagination-info">Page {page} of {totalPages}</span>
                            <div style={{display: 'flex', alignItems: 'center', gap: 6}}>
                                <label htmlFor="per-page" style={{fontSize: FONT_SIZE_SMALL, color: COLOR_TEXT_MUTED}}>Per
                                    page:</label>
                                <select
                                    id="per-page"
                                    className="pagination-select"
                                    value={String(limit)}
                                    onChange={e => setLimit(normalizeLimit(Number(e.target.value), defaultLimit))}
                                >
                                    {PAGINATION_LIMITS.map(n => (
                                        <option key={n} value={String(n)}>{n}</option>
                                    ))}
                                </select>
                            </div>
                        </div>
                        <button
                            className="pagination-btn"
                            disabled={page >= totalPages}
                            onClick={() => setPage(p => p + 1)}
                        >
                            Next <i className="bi bi-chevron-right"/>
                        </button>
                    </div>
                )}
            </div>

            {/* FAB - Playlists */}
            {playlistsEnabled && permissions.can_create_playlists && (
                <button className="fab-btn" onClick={() => setShowSidebar(true)} title="Playlists">
                    <i className="bi bi-collection-fill"/>
                </button>
            )}

            {/* Inline Player */}
            <InlinePlayer
                nowPlaying={nowPlaying}
                playlist={items.filter(i => !i.is_mature || (permissions.can_view_mature && user?.preferences?.show_mature === true))}
                onEnded={handlePlayerEnded}
            />

            {/* Upload Modal */}
            {showUpload && (
                <UploadModal
                    onClose={() => setShowUpload(false)}
                    onDone={handleUploadDone}
                    maxFileSize={serverSettings?.uploads?.maxFileSize}
                    categories={categories}
                />
            )}

            {/* Playlists Sidebar */}
            {playlistsEnabled && showSidebar && (
                <>
                    <div className="sidebar-overlay" onClick={() => setShowSidebar(false)}/>
                    <div className="sidebar">
                        <div className="sidebar-header">
                            <span>Playlists</span>
                            <button className="sidebar-close-btn" onClick={() => setShowSidebar(false)}>×</button>
                        </div>
                        <div className="sidebar-content">
                            {playlistError && (
                                <div style={{
                                    background: '#fee2e2',
                                    color: '#991b1b',
                                    borderRadius: 6,
                                    padding: '6px 10px',
                                    marginBottom: 10,
                                    fontSize: FONT_SIZE_SMALL
                                }}>
                                    {playlistError}
                                </div>
                            )}
                            {/* Create playlist */}
                            <div style={{marginBottom: 12}}>
                                <div style={{display: 'flex', gap: 6}}>
                                    <input
                                        type="text"
                                        value={newPlaylistName}
                                        onChange={e => setNewPlaylistName(e.target.value)}
                                        placeholder="New playlist name..."
                                        style={{
                                            flex: 1,
                                            padding: '6px 10px',
                                            border: '1px solid var(--border-color)',
                                            borderRadius: 6,
                                            background: 'var(--input-bg)',
                                            color: 'var(--text-color)',
                                            fontSize: FONT_SIZE_SMALL,
                                        }}
                                        onKeyDown={e => e.key === 'Enter' && handleCreatePlaylist()}
                                    />
                                    <button className="controls-btn controls-btn-primary"
                                            onClick={handleCreatePlaylist}>
                                        +
                                    </button>
                                </div>
                            </div>

                            {playlists.length === 0 ? (
                                <p style={{textAlign: 'center', color: COLOR_TEXT_MUTED, fontSize: 14}}>
                                    No playlists yet
                                </p>
                            ) : (
                                [...playlists].sort((a, b) => a.name.localeCompare(b.name)).map(pl => (
                                    <div key={pl.id} className="playlist-item">
                                        <div style={{flex: 1, minWidth: 0}}>
                                            {renameId === pl.id ? (
                                                <div style={{display: 'flex', gap: 4}}>
                                                    <input
                                                        autoFocus
                                                        value={renameName}
                                                        onChange={e => setRenameName(e.target.value)}
                                                        onKeyDown={e => {
                                                            if (e.key === 'Enter') handleRenamePlaylist(pl.id)
                                                            if (e.key === 'Escape') setRenameId(null)
                                                        }}
                                                        style={{
                                                            flex: 1,
                                                            padding: '3px 6px',
                                                            border: '1px solid var(--border-color)',
                                                            borderRadius: 4,
                                                            background: 'var(--input-bg)',
                                                            color: 'var(--text-color)',
                                                            fontSize: FONT_SIZE_SMALL,
                                                        }}
                                                    />
                                                    <button
                                                        onClick={() => handleRenamePlaylist(pl.id)}
                                                        style={{
                                                            background: 'none',
                                                            border: 'none',
                                                            color: '#22c55e',
                                                            cursor: 'pointer'
                                                        }}
                                                    >
                                                        <i className="bi bi-check-lg"/>
                                                    </button>
                                                    <button
                                                        onClick={() => setRenameId(null)}
                                                        style={{
                                                            background: 'none',
                                                            border: 'none',
                                                            color: COLOR_TEXT_MUTED,
                                                            cursor: 'pointer'
                                                        }}
                                                    >
                                                        <i className="bi bi-x-lg"/>
                                                    </button>
                                                </div>
                                            ) : (
                                                <div style={{display: 'flex', alignItems: 'center', gap: 4}}>
                                                    <div className="playlist-item-name"
                                                         style={{flex: 1}}>{pl.name}</div>
                                                    <button
                                                        onClick={() => {
                                                            setRenameId(pl.id);
                                                            setRenameName(pl.name)
                                                        }}
                                                        style={{
                                                            background: 'none',
                                                            border: 'none',
                                                            color: COLOR_TEXT_MUTED,
                                                            cursor: 'pointer',
                                                            padding: '2px 3px'
                                                        }}
                                                        title="Rename playlist"
                                                    >
                                                        <i className="bi bi-pencil-fill" style={{fontSize: 11}}/>
                                                    </button>
                                                </div>
                                            )}
                                            <div style={{display: 'flex', alignItems: 'center', gap: 6, marginTop: 2}}>
                                                <div className="playlist-item-count">{pl.items?.length ?? 0} items</div>
                                                {nowPlaying && (
                                                    <button
                                                        onClick={() => handleAddToPlaylist(pl.id)}
                                                        style={{
                                                            background: 'none',
                                                            border: 'none',
                                                            color: '#667eea',
                                                            cursor: 'pointer',
                                                            fontSize: 11,
                                                            padding: 0
                                                        }}
                                                        title={`Add "${nowPlaying.name}" to this playlist`}
                                                    >
                                                        <i className="bi bi-plus-circle"/> Add current
                                                    </button>
                                                )}
                                                <button
                                                    onClick={() => handleExportPlaylist(pl.id, pl.name, 'm3u8')}
                                                    style={{
                                                        background: 'none',
                                                        border: 'none',
                                                        color: COLOR_TEXT_MUTED,
                                                        cursor: 'pointer',
                                                        fontSize: 11,
                                                        padding: 0
                                                    }}
                                                    title="Export as M3U8"
                                                >
                                                    <i className="bi bi-download"/> Export
                                                </button>
                                            </div>
                                        </div>
                                        <button
                                            onClick={() => handleDeletePlaylist(pl.id)}
                                            style={{
                                                background: 'none',
                                                border: 'none',
                                                color: '#ef4444',
                                                cursor: 'pointer',
                                                fontSize: 16,
                                                padding: '2px 4px',
                                                borderRadius: 3,
                                            }}
                                            title="Delete playlist"
                                        >
                                            <i className="bi bi-trash-fill"/>
                                        </button>
                                    </div>
                                ))
                            )}
                        </div>
                    </div>
                </>
            )}

            {/* Version footer — matches deployed version from deploy script (VERSION file → ldflags) */}
            <footer className="index-version-footer" aria-label="Application version">
                {versionData && versionData.version !== null ? (
                    <span>v{versionData.version}</span>
                ) : null}
            </footer>
        </div>
    )
}

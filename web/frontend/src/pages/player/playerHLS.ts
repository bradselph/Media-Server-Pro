/** HLS check, polling, and state. Extracted to reduce usePlayerPageState complexity. */
import { useCallback, useEffect, useMemo, useState } from 'react'
import type { RefObject } from 'react'
import { hlsApi, mediaApi } from '@/api/endpoints'
import type { HLSJob } from '@/api/types'
import { useHLS } from '@/hooks/useHLS'

export type HlsCheckSetters = {
    setHlsAvailable: (v: boolean) => void
    setHlsReadyUrl: (v: string | null) => void
    setHlsJob: (v: HLSJob | null) => void
    setHlsPolling: (v: boolean) => void
    setActiveHlsUrl: (v: string | null) => void
}

function applyHlsCheckResult(
    hls: {
        available?: boolean
        hls_url?: string
        job_id?: string
        status?: string
        progress?: number
        qualities?: unknown[]
        started_at?: string
        error?: string
    },
    setters: HlsCheckSetters,
): void {
    if (hls.available && hls.hls_url) {
        setters.setHlsAvailable(true)
        setters.setHlsReadyUrl(hls.hls_url)
        setters.setActiveHlsUrl(hls.hls_url) // Auto-use HLS when already ready (no prompt)
        return
    }
    if (hls.job_id && hls.status === 'running') {
        setters.setHlsJob({
            id: hls.job_id,
            status: 'running',
            progress: hls.progress ?? 0,
            qualities: (hls.qualities ?? []) as string[],
            started_at: hls.started_at ?? '',
            error: hls.error ?? '',
            available: false,
        })
        setters.setHlsPolling(true)
    }
}

function useHlsCheckEffect(
    mediaId: string,
    media: { type: string } | undefined,
    hlsEnabled: boolean,
    videoRef: RefObject<HTMLVideoElement | null>,
    setters: HlsCheckSetters,
) {
    useEffect(() => {
        if (!mediaId || media?.type !== 'video' || !hlsEnabled) return
        hlsApi.check(mediaId).then((hls) => {
            applyHlsCheckResult(hls, setters)
            // When HLS is not available (job running or error), set direct stream as fallback
            if (!(hls.available && hls.hls_url) && videoRef.current) {
                videoRef.current.src = mediaApi.getStreamUrl(mediaId)
            }
        }).catch(() => {
            if (videoRef.current) videoRef.current.src = mediaApi.getStreamUrl(mediaId)
        })
    // Depend on media?.type instead of the full media object to avoid re-running
    // on every React Query refetch (which creates a new object reference).
    }, [mediaId, media?.type, hlsEnabled, videoRef, setters])
}

function useHlsPollingEffect(
    hlsPolling: boolean,
    hlsJob: HLSJob | null,
    setters: {
        setHlsJob: (v: HLSJob | null) => void
        setHlsPolling: (v: boolean) => void
        setHlsAvailable: (v: boolean) => void
        setHlsReadyUrl: (v: string | null) => void
    },
) {
    useEffect(() => {
        if (!hlsPolling || !hlsJob) return
        const interval = setInterval(async () => {
            try {
                const updated = await hlsApi.getStatus(hlsJob.id)
                setters.setHlsJob(updated)
                if (updated.status === 'completed') {
                    setters.setHlsPolling(false)
                    setters.setHlsAvailable(true)
                    setters.setHlsReadyUrl(hlsApi.getMasterPlaylistUrl(updated.id))
                } else if (updated.status === 'failed') {
                    setters.setHlsPolling(false)
                }
            } catch {
                setters.setHlsPolling(false)
            }
        }, 3000)
        return () => clearInterval(interval)
    // Depend on hlsJob?.id instead of the full object to prevent the interval from
    // being recreated every 3 seconds when setHlsJob replaces the object reference.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [hlsPolling, hlsJob?.id, setters])
}

export function usePlayerHLS(
    mediaId: string,
    media: { type: string; is_mature?: boolean } | undefined,
    hlsEnabled: boolean,
    videoRef: RefObject<HTMLVideoElement | null>,
) {
    const [hlsJob, setHlsJob] = useState<HLSJob | null>(null)
    const [hlsPolling, setHlsPolling] = useState(false)
    const [activeHlsUrl, setActiveHlsUrl] = useState<string | null>(null)
    const [hlsAvailable, setHlsAvailable] = useState(false)
    const [hlsReadyUrl, setHlsReadyUrl] = useState<string | null>(null)

    const onHlsFallback = useCallback(() => setActiveHlsUrl(null), [])

    const {
        qualities: hlsQualities,
        currentQuality,
        autoLevel,
        selectQuality,
        isLoading: hlsIsLoading,
        error: hlsError,
        bandwidth,
    } = useHLS(
        videoRef,
        media?.type === 'video' && hlsEnabled ? activeHlsUrl : null,
        onHlsFallback,
    )

    const hlsCheckSetters = useMemo(
        () => ({
            setHlsAvailable,
            setHlsReadyUrl,
            setHlsJob,
            setHlsPolling,
            setActiveHlsUrl,
        }),
        [],
    )
    useHlsCheckEffect(mediaId, media, hlsEnabled, videoRef, hlsCheckSetters)

    const hlsPollingSetters = useMemo(
        () => ({
            setHlsJob,
            setHlsPolling,
            setHlsAvailable,
            setHlsReadyUrl,
        }),
        [],
    )
    useHlsPollingEffect(hlsPolling, hlsJob, hlsPollingSetters)

    return {
        hlsJob,
        setHlsJob,
        setHlsPolling,
        activeHlsUrl,
        setActiveHlsUrl,
        hlsAvailable,
        setHlsAvailable,
        hlsReadyUrl,
        setHlsReadyUrl,
        hlsQualities,
        currentQuality,
        autoLevel,
        selectQuality,
        hlsIsLoading,
        hlsError,
        bandwidth,
    }
}

/** HLS check, polling, and state. Extracted to reduce usePlayerPageState complexity. */
import { useCallback, useEffect, useMemo, useState } from 'react'
import type { RefObject } from 'react'
import { hlsApi } from '@/api/endpoints'
import type { HLSJob } from '@/api/types'
import { useHLS } from '@/hooks/useHLS'

export type HlsCheckSetters = {
    setHlsAvailable: (v: boolean) => void
    setHlsReadyUrl: (v: string | null) => void
    setHlsJob: (v: HLSJob | null) => void
    setHlsPolling: (v: boolean) => void
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
    setters: HlsCheckSetters,
) {
    useEffect(() => {
        if (!mediaId || media?.type !== 'video' || !hlsEnabled) return
        hlsApi.check(mediaId).then((hls) => applyHlsCheckResult(hls, setters)).catch(() => {})
    }, [mediaId, media, hlsEnabled, setters])
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
    }, [hlsPolling, hlsJob, setters])
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
        }),
        [],
    )
    useHlsCheckEffect(mediaId, media, hlsEnabled, hlsCheckSetters)

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

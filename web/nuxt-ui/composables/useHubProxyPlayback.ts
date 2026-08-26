/**
 * Server-side playback for Hub items.
 *
 * Hub entries are third-party embeds, so the provider normally sees the viewer's
 * IP — and where the provider blocks a region, the embed simply refuses to play.
 * This composable switches a Hub item from the provider's iframe to a <video>
 * element fed by our own server, which fetches the media upstream on the
 * viewer's behalf.
 *
 * The iframe stays mounted underneath at all times. Every failure path here ends
 * with `active` back to false, so a viewer never lands on a dead black box —
 * worst case they get exactly today's behaviour.
 *
 * Modelled on useHLS.ts's attach sequence (Safari-native check, dynamic import,
 * isSupported guard, generation token re-checked after every await) but kept
 * separate: useHLS is bound to local-media job semantics that do not apply here.
 */

import type {Ref} from 'vue'
import {useHubApi} from '~/composables/useApiEndpoints'

/** Fatal hls.js network errors tolerated per attach before falling back. */
const MAX_NETWORK_RETRIES = 3
/** Fatal hls.js media errors tolerated per attach before falling back. */
const MAX_MEDIA_RETRIES = 2

export interface UseHubProxyPlaybackReturn {
    /** Whether the <video> element (rather than the iframe) should be shown. */
    active: Ref<boolean>
    /** A capability check or attach is in flight. */
    loading: Ref<boolean>
    /** Last failure, cleared on the next attempt. */
    error: Ref<string | null>
    /** Transport in use once active: 'hls' or 'mp4'. */
    kind: Ref<'hls' | 'mp4' | null>
    /** Switch this embed to server-side playback. Resolves once settled. */
    activate: (embedId: string) => Promise<void>
    /** Return to the iframe and release the player. */
    deactivate: () => void
}

export function useHubProxyPlayback(
    videoRef: Ref<HTMLVideoElement | null>,
): UseHubProxyPlaybackReturn {
    const hubApi = useHubApi()

    const active = ref(false)
    const loading = ref(false)
    const error = ref<string | null>(null)
    const kind = ref<'hls' | 'mp4' | null>(null)

    let hlsInstance: import('hls.js').default | null = null
    // Bumped on every activate/deactivate. Any async step that resumes with a
    // stale token belongs to a superseded attempt and must not touch the DOM —
    // otherwise switching items mid-load attaches the previous item's stream.
    let gen = 0
    // Where playback had reached, so a mid-stream recovery can resume in place
    // rather than restarting the video.
    let lastPosition = 0

    function destroyPlayer() {
        if (hlsInstance) {
            hlsInstance.destroy()
            hlsInstance = null
        }
        const el = videoRef.value
        if (el) {
            el.removeAttribute('src')
            el.load()
        }
    }

    function deactivate() {
        gen++
        destroyPlayer()
        active.value = false
        loading.value = false
        kind.value = null
        lastPosition = 0
    }

    function fail(message: string) {
        error.value = message
        destroyPlayer()
        active.value = false
        loading.value = false
        kind.value = null
    }

    /** Wait for the <video> element to exist after `active` flips it into the DOM. */
    async function waitForVideoEl(token: number): Promise<HTMLVideoElement | null> {
        for (let attempt = 0; attempt < 10; attempt++) {
            await nextTick()
            if (token !== gen) return null
            if (videoRef.value?.isConnected) return videoRef.value
            await new Promise(r => setTimeout(r, 50 * (attempt + 1)))
        }
        return token === gen ? videoRef.value : null
    }

    async function attachHls(el: HTMLVideoElement, url: string, token: number) {
        // Safari plays HLS natively; loading hls.js there is wasted work.
        if (el.canPlayType('application/vnd.apple.mpegurl')) {
            // Native playback has no hls.js error events and no startLoad() to
            // retry with, so listen on the element itself. Without this the
            // failure path never ran here and a broken stream left the viewer
            // on a frozen <video> instead of falling back to the iframe.
            el.addEventListener('error', () => {
                if (token === gen) fail('Server playback failed')
            })
            el.src = url
            return
        }
        const Hls = (await import('hls.js')).default
        if (token !== gen || !el.isConnected) return
        if (!Hls.isSupported()) {
            fail('This browser cannot play the server stream')
            return
        }

        const hls = new Hls({enableWorker: true, lowLatencyMode: false, backBufferLength: 90})
        hlsInstance = hls
        // Recovery budgets, per attach. Retrying unconditionally meant a
        // persistently broken stream looped forever, so `active` never went
        // back to false and the iframe fallback was unreachable.
        let networkRetries = 0
        let mediaRetries = 0
        hls.on(Hls.Events.ERROR, (_e: unknown, data: import('hls.js').ErrorData) => {
            if (!data.fatal || token !== gen) return
            // Network errors are usually a rotated upstream URL. The server
            // re-resolves on its own, so a reload normally recovers; once the
            // budget is spent, fall back to the iframe.
            if (data.type === Hls.ErrorTypes.NETWORK_ERROR && networkRetries < MAX_NETWORK_RETRIES) {
                networkRetries++
                hls.startLoad()
                return
            }
            if (data.type === Hls.ErrorTypes.MEDIA_ERROR && mediaRetries < MAX_MEDIA_RETRIES) {
                mediaRetries++
                hls.recoverMediaError()
                return
            }
            fail('Server playback failed')
        })
        hls.loadSource(url)
        hls.attachMedia(el)
    }

    /**
     * Attach a progressive file.
     *
     * Unlike HLS there is no manifest to re-request, so a mid-stream failure
     * would normally kill playback outright. We re-point the element at the same
     * proxy URL (the server re-resolves upstream behind it) and restore the
     * position, so a rotated upstream URL costs a blip rather than the video.
     */
    function attachMp4(el: HTMLVideoElement, url: string, token: number) {
        let recovered = false
        el.addEventListener('timeupdate', () => {
            if (token === gen) lastPosition = el.currentTime
        })
        el.addEventListener('error', () => {
            if (token !== gen) return
            if (recovered) {
                fail('Server playback failed')
                return
            }
            recovered = true
            const resumeAt = lastPosition
            el.src = `${url}?r=${Date.now()}`
            el.load()
            el.addEventListener('loadedmetadata', () => {
                if (token === gen && resumeAt > 0) el.currentTime = resumeAt
            }, {once: true})
            void el.play().catch(() => { /* autoplay may be blocked; user can resume */ })
        })
        el.src = url
    }

    async function activate(embedId: string) {
        const token = ++gen
        loading.value = true
        error.value = null
        lastPosition = 0

        let capability
        try {
            capability = await hubApi.checkPlayback(embedId)
        } catch (e: unknown) {
            if (token !== gen) return
            fail(e instanceof Error ? e.message : 'Could not reach the server')
            return
        }
        if (token !== gen) return

        if (!capability?.available) {
            fail(capability?.reason || 'Server playback is not available for this item')
            return
        }

        // Flip first so the <video> element renders, then wait for it.
        kind.value = capability.kind ?? 'hls'
        active.value = true
        const el = await waitForVideoEl(token)
        if (token !== gen) return
        if (!el) {
            fail('Player failed to load')
            return
        }

        try {
            if (kind.value === 'mp4') {
                attachMp4(el, hubApi.getProxyStreamUrl(embedId), token)
            } else {
                await attachHls(el, hubApi.getProxyMasterUrl(embedId), token)
            }
        } catch {
            if (token === gen) fail('Server playback failed to start')
            return
        }
        if (token === gen) loading.value = false
    }

    onUnmounted(deactivate)

    return {active, loading, error, kind, activate, deactivate}
}

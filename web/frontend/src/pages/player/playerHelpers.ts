/** Pure helper functions for the player (no JSX). */

export const PLAYBACK_SPEEDS = [0.5, 0.75, 1, 1.25, 1.5, 2] as const

export function createSeekBack(getActiveEl: () => HTMLMediaElement | null) {
    return () => {
        const el = getActiveEl()
        if (el) el.currentTime = Math.max(0, el.currentTime - 10)
    }
}

export function createSeekForward(getActiveEl: () => HTMLMediaElement | null) {
    return () => {
        const el = getActiveEl()
        if (el) el.currentTime = Math.min(el.duration, el.currentTime + 10)
    }
}

export function cyclePlaybackSpeed(current: number, setSpeed: (s: number) => void) {
    const idx = PLAYBACK_SPEEDS.indexOf(current as (typeof PLAYBACK_SPEEDS)[number])
    setSpeed(PLAYBACK_SPEEDS[(idx + 1) % PLAYBACK_SPEEDS.length])
}

export function getVolumeIconClass(isMuted: boolean, volume: number): string {
    if (isMuted || volume === 0) return 'bi bi-volume-mute-fill'
    if (volume < 0.5) return 'bi bi-volume-down-fill'
    return 'bi bi-volume-up-fill'
}

export function thumbnailUrlWithMatureBuster(url: string | undefined, canViewMature: boolean): string | undefined {
    if (!url) return undefined
    if (canViewMature) {
        const sep = url.includes('?') ? '&' : '?'
        return `${url}${sep}_m=1`
    }
    return url
}

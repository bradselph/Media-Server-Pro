import {describe, it, expect} from 'vitest'
import {
    capturePlaybackState,
    decideNetworkErrorAction,
    resolveTerminalHlsError,
    pollPhaseFor,
} from '~/composables/useHLS'

// Locks in the pure decision logic behind several player-behavior fixes so a
// future refactor of useHLS.ts can't silently reintroduce them:
//  - C02: capturing position/play-state across a direct<->HLS source swap so
//    the handoff doesn't silently rewind the video to 0.
//  - R01: a fatal NETWORK_ERROR before anything has buffered gets at most one
//    quick retry instead of the full ~49s mid-stream reconnect loop, which
//    used to leave the player blank with a misleading "reconnecting" banner.
//  - C11: failed/canceled HLS jobs surface a real error instead of the
//    progress banner silently disappearing.
//  - C12: a slow encode falls back to an infrequent background poll instead
//    of permanently giving up after 30 minutes.

describe('capturePlaybackState', () => {
    it('returns zero/not-playing for a missing element', () => {
        expect(capturePlaybackState(null)).toEqual({time: 0, wasPlaying: false})
        expect(capturePlaybackState(undefined)).toEqual({time: 0, wasPlaying: false})
    })

    it('captures the current time and derives wasPlaying from paused', () => {
        expect(capturePlaybackState({currentTime: 42.5, paused: true})).toEqual({time: 42.5, wasPlaying: false})
        expect(capturePlaybackState({currentTime: 10, paused: false})).toEqual({time: 10, wasPlaying: true})
    })

    it('reports time 0 for a fresh element, which callers treat as "nothing to restore"', () => {
        expect(capturePlaybackState({currentTime: 0, paused: true}).time).toBe(0)
    })
})

describe('decideNetworkErrorAction', () => {
    it('grants only one quick retry before falling back when nothing has buffered yet', () => {
        expect(decideNetworkErrorAction(false, 1)).toEqual({action: 'retry', delayMs: 1000})
        expect(decideNetworkErrorAction(false, 2)).toEqual({action: 'fallback', delayMs: 0})
        expect(decideNetworkErrorAction(false, 3)).toEqual({action: 'fallback', delayMs: 0})
    })

    it('grants the full 8-attempt exponential backoff once something has buffered', () => {
        expect(decideNetworkErrorAction(true, 1)).toEqual({action: 'retry', delayMs: 1000})
        expect(decideNetworkErrorAction(true, 2).action).toBe('retry')
        const eighth = decideNetworkErrorAction(true, 8)
        expect(eighth.action).toBe('retry')
        expect(eighth.delayMs).toBeCloseTo(1000 * 1.5 ** 7) // ~17s — under the 30s safety cap
        expect(decideNetworkErrorAction(true, 9)).toEqual({action: 'fallback', delayMs: 0})
    })
})

describe('resolveTerminalHlsError', () => {
    it('surfaces the backend error text for failed/canceled jobs', () => {
        expect(resolveTerminalHlsError('failed', 'ffmpeg exited 1')).toBe('ffmpeg exited 1')
        expect(resolveTerminalHlsError('canceled', 'stopped by admin')).toBe('stopped by admin')
    })

    it('falls back to a generic message when the backend sends no error text', () => {
        expect(resolveTerminalHlsError('failed', '')).toBe('HLS generation failed')
        expect(resolveTerminalHlsError('failed', undefined)).toBe('HLS generation failed')
        expect(resolveTerminalHlsError('canceled')).toBe('HLS generation failed')
    })

    it('returns null for non-terminal / successful statuses so callers leave hlsError untouched', () => {
        expect(resolveTerminalHlsError('running')).toBeNull()
        expect(resolveTerminalHlsError('pending')).toBeNull()
        expect(resolveTerminalHlsError('completed')).toBeNull()
    })
})

describe('pollPhaseFor', () => {
    it('stays fast within the normal polling window', () => {
        expect(pollPhaseFor(0)).toBe('fast')
        expect(pollPhaseFor(5 * 60 * 1000)).toBe('fast')
    })

    it('switches to a slow background poll instead of giving up on a long encode', () => {
        expect(pollPhaseFor(31 * 60 * 1000)).toBe('slow')
    })

    it('finally expires past the sane outer bound', () => {
        expect(pollPhaseFor(4 * 60 * 60 * 1000)).toBe('expired')
    })
})

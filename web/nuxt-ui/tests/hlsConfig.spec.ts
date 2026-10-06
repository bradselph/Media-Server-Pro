import {describe, it, expect} from 'vitest'
import {HLS_MOBILE_OVERRIDES, HLS_TUNING_CONFIG, hlsTuningForDevice} from '~/utils/hlsConfig'

// C18: the Hub server-proxy player (useHubProxyPlayback.ts) used to construct
// its own bare `new Hls({enableWorker, lowLatencyMode, backBufferLength})`
// with none of the buffer overrides below, silently reverting to hls.js's low
// stock defaults (maxBufferLength 30s, maxBufferSize 60MB) and reintroducing
// the rebuffering bug the main player (useHLS.ts) was already fixed for.
// Both composables now build their `Hls` instance from this shared constant,
// so locking in its values here catches either one drifting away from it.
describe('HLS_TUNING_CONFIG', () => {
    it('buffers generously ahead instead of falling back to hls.js\'s low defaults', () => {
        expect(HLS_TUNING_CONFIG.maxBufferLength).toBe(90)
        expect(HLS_TUNING_CONFIG.maxMaxBufferLength).toBe(600)
        expect(HLS_TUNING_CONFIG.maxBufferSize).toBe(200 * 1000 * 1000)
        expect(HLS_TUNING_CONFIG.maxBufferHole).toBe(0.5)
        expect(HLS_TUNING_CONFIG.backBufferLength).toBe(90)
    })

    it('keeps the worker/low-latency settings both players rely on', () => {
        expect(HLS_TUNING_CONFIG.enableWorker).toBe(true)
        expect(HLS_TUNING_CONFIG.lowLatencyMode).toBe(false)
    })

    it('keeps the manifest/level/fragment retry budget both players inherit', () => {
        expect(HLS_TUNING_CONFIG.manifestLoadingMaxRetry).toBe(4)
        expect(HLS_TUNING_CONFIG.levelLoadingMaxRetry).toBe(4)
        expect(HLS_TUNING_CONFIG.fragLoadingMaxRetry).toBe(8)
    })
})

// hls.js sizes its forward buffer from maxBufferSize / bitrate, so the desktop
// 200MB cap asked for up to the full 600s on mobile renditions — well past
// what phone browsers let a SourceBuffer hold, and a lot of metered data.
describe('hlsTuningForDevice', () => {
    it('keeps the desktop tuning on fine-pointer devices', () => {
        expect(hlsTuningForDevice(false)).toEqual(HLS_TUNING_CONFIG)
    })

    it('caps buffering on touch devices while inheriting everything else', () => {
        const mobile = hlsTuningForDevice(true)
        expect(mobile.maxBufferSize).toBe(40 * 1000 * 1000)
        expect(mobile.maxMaxBufferLength).toBe(120)
        expect(mobile.maxBufferLength).toBe(45)
        expect(mobile.backBufferLength).toBe(20)
        expect(mobile.maxBufferSize!).toBeLessThan(HLS_TUNING_CONFIG.maxBufferSize!)
        expect(mobile.maxMaxBufferLength!).toBeLessThan(HLS_TUNING_CONFIG.maxMaxBufferLength!)
        // Retry budgets and worker settings are shared, not re-tuned.
        for (const key of Object.keys(HLS_TUNING_CONFIG) as Array<keyof typeof HLS_TUNING_CONFIG>) {
            if (key in HLS_MOBILE_OVERRIDES) continue
            expect(mobile[key]).toEqual(HLS_TUNING_CONFIG[key])
        }
    })
})

import {describe, it, expect} from 'vitest'
import {HLS_TUNING_CONFIG} from '~/utils/hlsConfig'

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

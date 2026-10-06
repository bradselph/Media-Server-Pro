/**
 * hls.js tuning shared by every player that constructs an `Hls` instance —
 * the main player (composables/useHLS.ts) and the Hub server-proxy player
 * (composables/useHubProxyPlayback.ts) — so the two can't silently drift
 * apart. Imported explicitly to avoid Nuxt auto-import TDZ issues.
 *
 * Buffer targets are generous so playback doesn't stall on marginal networks
 * and so buffering keeps filling ahead while PAUSED (hls.js loads up to
 * these caps regardless of play/pause — it never stops on pause). A
 * previous maxMaxBufferLength=120 (well under hls.js's 600 default) and
 * maxBufferSize=60MB capped high-bitrate streams to only ~20s of lookahead,
 * causing repeated `waiting` underruns (stuck buffer spinner) and making
 * the buffer appear to stop growing the moment you paused.
 *
 * The loading timeouts/retries below are hls.js's own internal per-request
 * retry budget (manifest/level/fragment fetches) — independent of any
 * higher-level reconnect/fallback logic a caller layers on top in its own
 * `Hls.Events.ERROR` handler (each caller keeps its own error budget there).
 */
export const HLS_TUNING_CONFIG: Partial<import('hls.js').HlsConfig> = {
    debug: false,
    enableWorker: true,
    lowLatencyMode: false,
    backBufferLength: 90,
    maxBufferLength: 90,              // target forward buffer (s)
    maxMaxBufferLength: 600,          // hard cap (s) — hls.js default; allows deep buffering while paused
    maxBufferSize: 200 * 1000 * 1000, // byte cap (~48s @25Mbps 4K, ~320s @5Mbps) — was 60MB
    maxBufferHole: 0.5,
    manifestLoadingTimeOut: 20000,
    manifestLoadingMaxRetry: 4,
    manifestLoadingRetryDelay: 1000,
    levelLoadingTimeOut: 20000,
    levelLoadingMaxRetry: 4,
    levelLoadingRetryDelay: 1000,
    fragLoadingTimeOut: 30000,
    fragLoadingMaxRetry: 8,
    fragLoadingRetryDelay: 1000,
    fragLoadingMaxRetryTimeout: 16000,
    startFragPrefetch: true,
    testBandwidth: true,
}

/**
 * Buffer caps for phones and tablets. hls.js derives its forward-buffer
 * target from maxBufferSize / bitrate, so the desktop 200MB cap means up to
 * the full 600s on a 2-3 Mbps mobile rendition — several times what mobile
 * browsers allow a SourceBuffer to hold (tens of MB) and a lot of data to
 * pull on a metered connection. hls.js does recover from the resulting
 * QuotaExceededErrors, but each one aborts an append and shrinks the target
 * on the fly; staying under the quota keeps playback steady instead.
 * 45–120s of lookahead is still far more than a stall needs.
 */
export const HLS_MOBILE_OVERRIDES: Partial<import('hls.js').HlsConfig> = {
    backBufferLength: 20,
    maxBufferLength: 45,
    maxMaxBufferLength: 120,
    maxBufferSize: 40 * 1000 * 1000,
}

/** Touch-first device (phone/tablet) — the same probe the share sheet uses. */
export function isCoarsePointerDevice(): boolean {
    return typeof globalThis.matchMedia === 'function'
        && globalThis.matchMedia('(pointer: coarse)').matches
}

/** The tuning to construct an `Hls` instance with on this device. */
export function hlsTuningForDevice(coarsePointer: boolean = isCoarsePointerDevice()): Partial<import('hls.js').HlsConfig> {
    return coarsePointer ? {...HLS_TUNING_CONFIG, ...HLS_MOBILE_OVERRIDES} : {...HLS_TUNING_CONFIG}
}

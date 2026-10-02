import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'
import {defineComponent, h, ref} from 'vue'
import {mount} from '@vue/test-utils'
import {
    classifyProbe,
    DIRECT_MAX_RETRIES,
    DIRECT_STALL_TIMEOUT_MS,
    directRetryDelayMs,
    parseRetryAfterMs,
    triggerForMediaError,
    useDirectPlayRecovery,
    type ProbeOutcome,
} from '~/composables/useDirectPlayRecovery'

// A plain <video src> stream has no retry logic of its own: before this
// composable, a dropped connection stopped playback until a page reload, a
// hung one spun forever, and a transient server error at startup (503 while
// the library loads after a restart) was reported as "format not supported".

describe('parseRetryAfterMs', () => {
    it('reads delta-seconds and HTTP-dates, capped at a minute', () => {
        expect(parseRetryAfterMs('3')).toBe(3000)
        expect(parseRetryAfterMs('600')).toBe(60_000)
        const now = Date.parse('2026-10-02T12:00:00Z')
        expect(parseRetryAfterMs('Fri, 02 Oct 2026 12:00:10 GMT', now)).toBe(10_000)
        expect(parseRetryAfterMs('Fri, 02 Oct 2026 11:00:00 GMT', now)).toBe(0)
    })

    it('ignores a missing or unparseable value', () => {
        expect(parseRetryAfterMs(null)).toBeUndefined()
        expect(parseRetryAfterMs('')).toBeUndefined()
        expect(parseRetryAfterMs('soon')).toBeUndefined()
    })
})

describe('classifyProbe', () => {
    it('treats a served byte as ok', () => {
        expect(classifyProbe(206)).toEqual({kind: 'ok'})
        expect(classifyProbe(200)).toEqual({kind: 'ok'})
    })

    it('treats server errors, rate limits and timeouts as transient, keeping Retry-After', () => {
        expect(classifyProbe(503, '3')).toEqual({kind: 'transient', retryAfterMs: 3000})
        expect(classifyProbe(429).kind).toBe('transient')
        expect(classifyProbe(502).kind).toBe('transient')
        expect(classifyProbe(408).kind).toBe('transient')
    })

    it('treats auth, permission and missing-file answers as terminal, preferring the server message', () => {
        expect(classifyProbe(401)).toEqual({kind: 'terminal', message: 'Sign in to watch this'})
        expect(classifyProbe(403, null, 'This content is marked as mature (18+).'))
            .toEqual({kind: 'terminal', message: 'This content is marked as mature (18+).'})
        expect(classifyProbe(404)).toEqual({kind: 'terminal', message: 'This media is no longer available'})
        expect(classifyProbe(418)).toEqual({kind: 'terminal', message: 'Playback failed (HTTP 418)'})
    })
})

describe('directRetryDelayMs', () => {
    it('backs off exponentially and honors a longer Retry-After up to a minute', () => {
        expect(directRetryDelayMs(1)).toBe(1000)
        expect(directRetryDelayMs(2)).toBe(2000)
        expect(directRetryDelayMs(6)).toBe(30_000)
        expect(directRetryDelayMs(99)).toBe(30_000)
        expect(directRetryDelayMs(1, 45_000)).toBe(45_000)
        expect(directRetryDelayMs(1, 500)).toBe(1000)
        expect(directRetryDelayMs(1, 120_000)).toBe(60_000)
    })
})

describe('triggerForMediaError', () => {
    it('maps MediaError codes, leaving deliberate aborts alone', () => {
        expect(triggerForMediaError(1)).toBeNull()
        expect(triggerForMediaError(2)).toBe('network')
        expect(triggerForMediaError(3)).toBe('decode')
        expect(triggerForMediaError(4)).toBe('unsupported')
        expect(triggerForMediaError(undefined)).toBe('network')
    })
})

// ---------------------------------------------------------------------------
// Behavior against a scripted media element
// ---------------------------------------------------------------------------

interface FakeMedia extends EventTarget {
    currentTime: number
    duration: number
    paused: boolean
    ended: boolean
    readyState: number
    error: { code: number } | null
    load: ReturnType<typeof vi.fn>
    play: ReturnType<typeof vi.fn>
}

function fakeMedia(): FakeMedia {
    const el = new EventTarget() as FakeMedia
    Object.assign(el, {
        currentTime: 0,
        duration: 600,
        paused: true,
        ended: false,
        readyState: 0,
        error: null,
        load: vi.fn(),
        play: vi.fn(() => Promise.resolve()),
    })
    return el
}

function setup(probeResults: ProbeOutcome[], onUnplayable?: () => boolean) {
    const el = fakeMedia()
    const mediaRef = ref<HTMLMediaElement | null>(el as unknown as HTMLMediaElement)
    const url = ref<string | null>('/media?id=abc')
    const probe = vi.fn(async () => probeResults.length > 1 ? probeResults.shift()! : probeResults[0]!)
    let api!: ReturnType<typeof useDirectPlayRecovery>
    const wrapper = mount(defineComponent({
        setup() {
            api = useDirectPlayRecovery({mediaRef, directUrl: () => url.value, probe, onUnplayable})
            return () => h('div')
        },
    }))
    return {el, url, probe, api, wrapper}
}

/** Put the element into "playing at `time`" and then fail it with `code`. */
function playThenFail(el: FakeMedia, time: number, code: number) {
    el.readyState = 4
    el.currentTime = time
    el.paused = false
    el.dispatchEvent(new Event('play'))
    el.error = {code}
}

/** The element finished re-loading after el.load(). */
function finishReload(el: FakeMedia) {
    el.error = null
    el.currentTime = 0
    el.readyState = 1
    el.dispatchEvent(new Event('loadedmetadata'))
}

describe('useDirectPlayRecovery', () => {
    beforeEach(() => {
        vi.useFakeTimers()
    })
    afterEach(() => {
        vi.useRealTimers()
    })

    it('reloads a dropped stream right away and resumes where it was, playing', async () => {
        const {el, api} = setup([{kind: 'ok'}])
        playThenFail(el, 42, 2)

        expect(api.handleError()).toBe(true)
        expect(api.reconnecting.value).toBe(true)
        await vi.advanceTimersByTimeAsync(0)
        expect(el.load).toHaveBeenCalledTimes(1)

        finishReload(el)
        expect(el.currentTime).toBe(42)
        expect(el.play).toHaveBeenCalled()

        el.dispatchEvent(new Event('playing'))
        expect(api.reconnecting.value).toBe(false)
        expect(api.failure.value).toBeNull()
    })

    it('backs off while the server is erroring, honoring Retry-After', async () => {
        const {el, api} = setup([{kind: 'transient', retryAfterMs: 5000}])
        playThenFail(el, 10, 2)

        api.handleError()
        await vi.advanceTimersByTimeAsync(4900)
        expect(el.load).not.toHaveBeenCalled()
        await vi.advanceTimersByTimeAsync(200)
        expect(el.load).toHaveBeenCalledTimes(1)
    })

    it('stops on a terminal answer and reports the reason instead of retrying', async () => {
        const {el, api} = setup([{kind: 'terminal', message: 'Sign in to watch this'}])
        playThenFail(el, 10, 2)

        api.handleError()
        await vi.advanceTimersByTimeAsync(60_000)
        expect(el.load).not.toHaveBeenCalled()
        expect(api.failure.value).toBe('Sign in to watch this')
        expect(api.reconnecting.value).toBe(false)
    })

    it('gives up after the retry budget with a connection message', async () => {
        const {el, api} = setup([{kind: 'transient'}])
        playThenFail(el, 10, 2)

        for (let i = 0; i < DIRECT_MAX_RETRIES; i++) {
            api.handleError()
            await vi.advanceTimersByTimeAsync(60_000)
            expect(el.load).toHaveBeenCalledTimes(i + 1)
            el.error = {code: 2} // the reload failed too
        }
        api.handleError()
        await vi.advanceTimersByTimeAsync(60_000)
        expect(el.load).toHaveBeenCalledTimes(DIRECT_MAX_RETRIES)
        expect(api.failure.value).toBe('Lost connection to the server')
    })

    it('retries an "unsupported" startup failure once before blaming the format, then hands off to HLS', async () => {
        const onUnplayable = vi.fn(() => true)
        const {el, api} = setup([{kind: 'ok'}], onUnplayable)
        el.error = {code: 4} // failed before any metadata loaded

        api.handleError()
        await vi.advanceTimersByTimeAsync(0)
        expect(el.load).toHaveBeenCalledTimes(1) // maybe it was a transient 503
        expect(onUnplayable).not.toHaveBeenCalled()

        api.handleError() // same failure again
        await vi.advanceTimersByTimeAsync(0)
        expect(onUnplayable).toHaveBeenCalledTimes(1)
        expect(el.load).toHaveBeenCalledTimes(1)
        expect(api.failure.value).toBeNull()
        expect(api.reconnecting.value).toBe(false)
    })

    it('reports an unplayable format when nothing can take over', async () => {
        const {el, api} = setup([{kind: 'ok'}], () => false)
        el.error = {code: 4}

        api.handleError()
        await vi.advanceTimersByTimeAsync(0)
        api.handleError()
        await vi.advanceTimersByTimeAsync(0)
        expect(api.failure.value).toMatch(/format isn't supported/)
    })

    it('reloads a stream that stalls with no download progress', async () => {
        const {el, probe} = setup([{kind: 'ok'}])
        playThenFail(el, 30, 2)
        el.error = null
        el.readyState = 2 // HAVE_CURRENT_DATA: waiting on data
        el.dispatchEvent(new Event('waiting'))

        await vi.advanceTimersByTimeAsync(DIRECT_STALL_TIMEOUT_MS - 3000)
        expect(probe).not.toHaveBeenCalled()
        await vi.advanceTimersByTimeAsync(6000)
        expect(probe).toHaveBeenCalledTimes(1)
        expect(el.load).toHaveBeenCalledTimes(1)
    })

    it('leaves a slow-but-flowing stall to the browser', async () => {
        const {el, probe} = setup([{kind: 'ok'}])
        playThenFail(el, 30, 2)
        el.error = null
        el.readyState = 2
        el.dispatchEvent(new Event('waiting'))

        for (let i = 0; i < 30; i++) {
            await vi.advanceTimersByTimeAsync(1000)
            el.dispatchEvent(new Event('progress'))
        }
        expect(probe).not.toHaveBeenCalled()
        expect(el.load).not.toHaveBeenCalled()
    })

    it('starts fresh when the source changes', async () => {
        const {el, url, api} = setup([{kind: 'terminal', message: 'Sign in to watch this'}])
        playThenFail(el, 10, 2)
        api.handleError()
        await vi.advanceTimersByTimeAsync(0)
        expect(api.failure.value).not.toBeNull()

        url.value = null // e.g. HLS took over
        await vi.advanceTimersByTimeAsync(0)
        expect(api.failure.value).toBeNull()
        expect(api.handleError()).toBe(false) // not direct play any more
    })
})

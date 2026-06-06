import {type MaybeRefOrGetter, onBeforeUnmount, ref, toValue} from 'vue'

// Cycle through the scrub-thumbnail previews while the pointer is on a
// media card. Coarse pointers (touch) skip the animation per the design
// brief. Previews are fetched lazily on first hover and cached for the
// life of the composable instance.
//
// Usage:
//   const { previewSrc, onEnter, onLeave } = useHoverFrames(() => item.id)
//   <img :src="previewSrc ?? fallback" @mouseenter="onEnter" @mouseleave="onLeave" />
const INTERVAL_MS = 750
const MAX_FRAMES = 6

export function useHoverFrames(mediaId: MaybeRefOrGetter<string>) {
    const previewSrc = ref<string | null>(null)

    let cachedFor: string | null = null
    let frames: string[] | null = null
    let timer: ReturnType<typeof setInterval> | null = null
    let hovered = false
    let pending = false

    function isCoarse(): boolean {
        if (typeof window === 'undefined') return false
        const mm = window.matchMedia?.('(pointer: coarse)')
        return !!mm?.matches
    }

    function stopTimer() {
        if (timer) {
            clearInterval(timer)
            timer = null
        }
    }

    function startCycle() {
        if (!frames || frames.length === 0) return
        stopTimer()
        let idx = 0
        previewSrc.value = frames[0]
        timer = setInterval(() => {
            if (!hovered || !frames) return
            idx = (idx + 1) % frames.length
            previewSrc.value = frames[idx]
        }, INTERVAL_MS)
    }

    async function onEnter() {
        if (isCoarse()) return
        hovered = true
        const id = toValue(mediaId)
        if (!id) return
        if (cachedFor === id && frames) {
            startCycle()
            return
        }
        if (pending) return
        pending = true
        try {
            const mediaApi = useMediaApi()
            const r = await mediaApi.getThumbnailPreviews(id)
            if (!hovered) return
            cachedFor = id
            frames = (r?.previews ?? []).slice(0, MAX_FRAMES)
            if (frames.length > 0) startCycle()
        } catch {
            cachedFor = id
            frames = []
        } finally {
            pending = false
        }
    }

    function onLeave() {
        hovered = false
        stopTimer()
        previewSrc.value = null
    }

    onBeforeUnmount(() => {
        hovered = false
        stopTimer()
    })

    return {previewSrc, onEnter, onLeave}
}

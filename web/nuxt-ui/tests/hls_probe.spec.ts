import {describe, it, expect, vi} from 'vitest'
import {ref, nextTick} from 'vue'

vi.mock('hls.js', () => {
    throw new Error('chunk load failed')
})

describe('probe', () => {
    it('resumes after hls.js import failure', async () => {
        const {useHLS} = await import('~/composables/useHLS')
        const video = document.createElement('video')
        document.body.appendChild(video)
        video.currentTime = 30
        await video.play()

        const videoRef = ref<HTMLVideoElement | null>(video)
        const mediaId = ref('')
        const hls = useHLS(videoRef, mediaId)
        hls.hlsUrl.value = 'https://example.test/master.m3u8'

        await hls.activateHLS()
        await vi.waitFor(() => {
            expect(hls.hlsActivated.value).toBe(false)
        })

        expect(hls.hlsError.value).toBe('Failed to load HLS player')

        video.currentTime = 0
        video.pause()

        video.dispatchEvent(new Event('loadedmetadata'))

        await vi.waitFor(() => {
            expect(video.currentTime).toBe(30)
        })
        expect(video.paused).toBe(false)
    })
})

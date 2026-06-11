import {defineStore} from 'pinia'
import {isPrivateSession} from '~/stores/auth'

export interface PlaybackMediaInfo {
    id: string
    name: string
    type: 'video' | 'audio' | 'unknown'
    thumbnail_url?: string
    duration: number
}

export const usePlaybackStore = defineStore('playback', () => {
    const currentMediaId = ref<string | null>(null)
    const position = ref(0)
    const duration = ref(0)
    const isPlaying = ref(false)
    const mediaInfo = ref<PlaybackMediaInfo | null>(null)

    // Initialize composable once at store creation, not inside interval callbacks
    const playbackApi = usePlaybackApi()

    let saveInterval: ReturnType<typeof setInterval> | null = null

    function setMedia(id: string, info?: PlaybackMediaInfo) {
        currentMediaId.value = id
        position.value = 0
        duration.value = 0
        isPlaying.value = false
        if (info) mediaInfo.value = info
    }

    function updatePosition(pos: number, dur?: number) {
        position.value = pos
        if (dur !== undefined) duration.value = dur
    }

    async function savePosition() {
        if (!currentMediaId.value || position.value <= 0) return
        // Private session (B.2 retention plan): defense-in-depth — the
        // backend already drops the write when X-MSP-Private is set, but
        // skipping the request entirely avoids the round-trip and keeps
        // network panel quiet so users can see their toggle is working.
        if (isPrivateSession()) return
        try {
            await playbackApi.savePosition(currentMediaId.value, position.value, duration.value)
        } catch {
        }
    }

    function startAutoSave() {
        stopAutoSave()
        saveInterval = setInterval(savePosition, 15_000)
    }

    function stopAutoSave() {
        if (saveInterval !== null) {
            clearInterval(saveInterval)
            saveInterval = null
        }
    }

    return {
        currentMediaId, position, duration, isPlaying, mediaInfo,
        setMedia, updatePosition, savePosition,
        startAutoSave, stopAutoSave,
    }
})

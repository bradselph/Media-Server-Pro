import {create} from 'zustand'
import {persist} from 'zustand/middleware'
import {hlsApi, mediaApi, watchHistoryApi} from '@/api/endpoints'

interface PlaybackState {
    isPlaying: boolean
    currentMediaId: string | null
    currentMediaTitle: string
    currentMediaType: 'video' | 'audio' | null
    currentVolume: number
    isMuted: boolean
    currentTime: number
    duration: number
    playbackSpeed: number
    hlsEnabled: boolean
    hlsUrl: string | null

    playMedia: (id: string, title?: string, type?: 'video' | 'audio') => Promise<void>
    togglePlayPause: () => void
    setPlaying: (playing: boolean) => void
    setVolume: (volume: number) => void
    toggleMute: () => void
    setCurrentTime: (time: number) => void
    setDuration: (duration: number) => void
    setPlaybackSpeed: (speed: number) => void
    setHLSUrl: (url: string | null) => void
    stopPlayback: () => void
}

// We store volume/speed in localStorage via persist
export const usePlaybackStore = create<PlaybackState>()(
    persist(
        (set, get) => ({
            isPlaying: false,
            currentMediaId: null,
            currentMediaTitle: '',
            currentMediaType: null,
            currentVolume: 0.8,
            isMuted: false,
            currentTime: 0,
            duration: 0,
            playbackSpeed: 1,
            hlsEnabled: false,
            hlsUrl: null,

            playMedia: async (id: string, title?: string, type?: 'video' | 'audio') => {
                const displayTitle = title || id

                set({
                    currentMediaId: id,
                    currentMediaTitle: displayTitle,
                    currentMediaType: type ?? 'video',
                    isPlaying: true,
                    currentTime: 0,
                    duration: 0,
                    hlsUrl: null,
                })

                // Check HLS availability for video files
                const isVideo = type ? type === 'video' : true
                if (isVideo) {
                    try {
                        const hlsInfo = await hlsApi.check(id)
                        if (hlsInfo.available && hlsInfo.hls_url) {
                            set({hlsEnabled: true, hlsUrl: hlsInfo.hls_url})
                        } else {
                            set({hlsEnabled: false, hlsUrl: null})
                            // Trigger background HLS generation
                            hlsApi.generate(id).catch(() => {
                            })
                        }
                    } catch {
                        set({hlsEnabled: false, hlsUrl: null})
                    }
                } else {
                    set({hlsEnabled: false, hlsUrl: null})
                }

                // Track playback
                // TODO: Redundant try/catch — `watchHistoryApi.trackPosition()` returns a Promise,
                // and `.catch(() => {})` already swallows rejections. The outer try/catch never
                // fires because the Promise constructor itself won't throw synchronously.
                // FIX: Remove the outer try/catch; the `.catch()` is sufficient.
                try {
                    watchHistoryApi.trackPosition(id, 0, 0).catch(() => {
                    })
                } catch {
                    // Tracking is non-critical
                }
            },

            togglePlayPause: () => {
                set(s => ({isPlaying: !s.isPlaying}))
            },

            setPlaying: (playing: boolean) => set({isPlaying: playing}),

            setVolume: (volume: number) => {
                set({currentVolume: Math.max(0, Math.min(1, volume)), isMuted: false})
            },

            toggleMute: () => set(s => ({isMuted: !s.isMuted})),

            setCurrentTime: (time: number) => set({currentTime: time}),

            setDuration: (duration: number) => set({duration}),

            setPlaybackSpeed: (speed: number) => set({playbackSpeed: speed}),

            setHLSUrl: (url: string | null) => set({hlsUrl: url, hlsEnabled: url !== null}),

            stopPlayback: () => {
                // Save position before stopping
                const state = get()
                if (state.currentMediaId && state.currentTime > 0) {
                    watchHistoryApi.trackPosition(state.currentMediaId, state.currentTime, state.duration).catch(() => {
                    })
                }
                set({
                    isPlaying: false,
                    currentMediaId: null,
                    currentMediaTitle: '',
                    currentMediaType: null,
                    currentTime: 0,
                    duration: 0,
                    hlsUrl: null,
                    hlsEnabled: false,
                })
            },
        }),
        {
            name: 'media-server-playback',
            partialize: (state: PlaybackState) => ({
                currentVolume: state.currentVolume,
                playbackSpeed: state.playbackSpeed,
            }),
        },
    ),
)

// Helper: get the streaming URL for a media ID
export function getStreamUrl(id: string): string {
    return mediaApi.getStreamUrl(id)
}

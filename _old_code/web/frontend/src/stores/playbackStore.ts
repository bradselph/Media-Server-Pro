import {create} from 'zustand'
import {persist} from 'zustand/middleware'
import {hlsApi, mediaApi, watchHistoryApi} from '@/api/endpoints'

interface PlaybackState {
    isPlaying: boolean
    currentMediaPath: string | null
    currentMediaTitle: string
    currentMediaType: 'video' | 'audio' | null
    currentVolume: number
    isMuted: boolean
    currentTime: number
    duration: number
    playbackSpeed: number
    hlsEnabled: boolean
    hlsUrl: string | null

    playMedia: (path: string, type?: 'video' | 'audio') => Promise<void>
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
            currentMediaPath: null,
            currentMediaTitle: '',
            currentMediaType: null,
            currentVolume: 0.8,
            isMuted: false,
            currentTime: 0,
            duration: 0,
            playbackSpeed: 1,
            hlsEnabled: false,
            hlsUrl: null,

            playMedia: async (path: string, type?: 'video' | 'audio') => {
                // Use caller-supplied type when available; fall back to extension heuristic for prev/next nav
                const isVideo = type ? type === 'video' : /\.(mp4|mkv|avi|webm|mov|wmv)$/i.test(path)
                const title = path.split('/').pop()?.split('\\').pop()?.replace(/\.[^.]+$/, '').replace(/[_-]/g, ' ') || path

                set({
                    currentMediaPath: path,
                    currentMediaTitle: title,
                    currentMediaType: type ?? (isVideo ? 'video' : 'audio'),
                    isPlaying: true,
                    currentTime: 0,
                    duration: 0,
                    hlsUrl: null,
                })

                // Check HLS availability for video files
                if (isVideo) {
                    try {
                        const hlsInfo = await hlsApi.check(path)
                        if (hlsInfo.available && hlsInfo.hls_url) {
                            set({hlsEnabled: true, hlsUrl: hlsInfo.hls_url})
                        } else {
                            set({hlsEnabled: false, hlsUrl: null})
                            // Trigger background HLS generation
                            hlsApi.generate(path).catch(() => {
                            })
                        }
                    } catch {
                        set({hlsEnabled: false, hlsUrl: null})
                    }
                } else {
                    set({hlsEnabled: false, hlsUrl: null})
                }

                // Track playback
                try {
                    watchHistoryApi.trackPosition(path, 0, 0).catch(() => {
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
                if (state.currentMediaPath && state.currentTime > 0) {
                    watchHistoryApi.trackPosition(state.currentMediaPath, state.currentTime, state.duration).catch(() => {
                    })
                }
                set({
                    isPlaying: false,
                    currentMediaPath: null,
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

// Helper: get the streaming URL for a media path
export function getStreamUrl(path: string): string {
    return mediaApi.getStreamUrl(path)
}

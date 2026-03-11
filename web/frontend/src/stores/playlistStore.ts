import {create} from 'zustand'
import {persist} from 'zustand/middleware'
import {playlistApi} from '@/api/endpoints'
import type {Playlist, PlaylistItem} from '@/api/types'

/** Random index in [0, max) using crypto for linter/safety (shuffle is non-security but avoids PRNG warning). */
function randomIndex(max: number): number {
    if (max <= 0) return 0
    const arr = new Uint32Array(1)
    crypto.getRandomValues(arr)
    return Math.floor((arr[0] / (0x100000000)) * max)
}

interface PlaylistState {
    currentPlaylist: PlaylistItem[]
    currentIndex: number
    currentPlaylistId: string | null
    currentPlaylistName: string
    repeatMode: 'none' | 'one' | 'all'
    shuffleMode: boolean
    savedPlaylists: Playlist[]
    playlistVisible: boolean
    playlistError: string | null // IC-09: surfaced on loadPlaylists failure

    // Playlist CRUD
    loadPlaylists: () => Promise<void>
    createPlaylist: (name: string) => Promise<void>
    deletePlaylist: (id: string) => Promise<void>
    loadPlaylist: (id: string) => Promise<void>

    // Current playlist management
    addItem: (item: PlaylistItem) => void
    removeItem: (index: number) => void
    clearPlaylist: () => void
    setCurrentIndex: (index: number) => void
    reorderItems: (fromIndex: number, toIndex: number) => void

    // Playback control
    playNext: () => string | null
    playPrevious: () => string | null
    toggleShuffle: () => void
    toggleRepeat: () => void
    setPlaylistVisible: (visible: boolean) => void
    togglePlaylistVisible: () => void

    // Batch operations
    setPlaylistFromIds: (ids: string[], titles?: string[]) => void
}


export const usePlaylistStore = create<PlaylistState>()(
    persist(
        (set, get) => ({
            currentPlaylist: [],
            currentIndex: -1,
            currentPlaylistId: null,
            currentPlaylistName: 'Now Playing',
            repeatMode: 'none' as const,
            shuffleMode: false,
            savedPlaylists: [],
            playlistVisible: false,
            playlistError: null,

            loadPlaylists: async () => {
                try {
                    const playlists = await playlistApi.list()
                    set({savedPlaylists: Array.isArray(playlists) ? playlists : [], playlistError: null})
                } catch (err) {
                    // IC-09: surface error instead of silently returning empty list
                    const msg = (err as Error)?.message ?? 'Failed to load playlists'
                    set({playlistError: msg})
                }
            },

            createPlaylist: async (name: string) => {
                const playlist = await playlistApi.create(name)
                set(s => ({savedPlaylists: [...s.savedPlaylists, playlist]}))
            },

            deletePlaylist: async (id: string) => {
                await playlistApi.delete(id)
                set(s => ({
                    savedPlaylists: s.savedPlaylists.filter(p => p.id !== id),
                    currentPlaylistId: s.currentPlaylistId === id ? null : s.currentPlaylistId,
                }))
            },

            loadPlaylist: async (id: string) => {
                const playlist = await playlistApi.get(id)
                // IC-10: only reset index when loading a different playlist
                set(s => ({
                    currentPlaylist: playlist.items || [],
                    currentPlaylistId: playlist.id,
                    currentPlaylistName: playlist.name,
                    currentIndex: s.currentPlaylistId === playlist.id ? s.currentIndex : 0,
                }))
            },

            addItem: (item: PlaylistItem) => {
                set(s => ({
                    currentPlaylist: [...s.currentPlaylist, item],
                    currentIndex: s.currentPlaylist.length === 0 ? 0 : s.currentIndex,
                }))
            },

            removeItem: (index: number) => {
                set(s => {
                    const newList = s.currentPlaylist.filter((_, i) => i !== index)
                    let newIndex = s.currentIndex
                    if (index < s.currentIndex) newIndex--
                    if (index === s.currentIndex) newIndex = Math.min(newIndex, newList.length - 1)
                    return {currentPlaylist: newList, currentIndex: Math.max(0, newIndex)}
                })
            },

            clearPlaylist: () => {
                set({
                    currentPlaylist: [],
                    currentIndex: -1,
                    currentPlaylistId: null,
                    currentPlaylistName: 'Now Playing'
                })
            },

            setCurrentIndex: (index: number) => set({currentIndex: index}),

            reorderItems: (fromIndex: number, toIndex: number) => {
                set(s => {
                    const items = [...s.currentPlaylist]
                    const [moved] = items.splice(fromIndex, 1)
                    items.splice(toIndex, 0, moved)
                    let newIndex = s.currentIndex
                    if (s.currentIndex === fromIndex) newIndex = toIndex
                    else if (fromIndex < s.currentIndex && toIndex >= s.currentIndex) newIndex--
                    else if (fromIndex > s.currentIndex && toIndex <= s.currentIndex) newIndex++
                    return {currentPlaylist: items, currentIndex: newIndex}
                })
            },

            playNext: () => {
                const state = get()
                const {currentPlaylist, currentIndex, repeatMode, shuffleMode} = state
                if (currentPlaylist.length === 0) return null

                let nextIndex: number
                if (repeatMode === 'one') {
                    nextIndex = currentIndex
                } else if (shuffleMode) {
                    nextIndex = randomIndex(currentPlaylist.length)
                } else {
                    nextIndex = currentIndex + 1
                    if (nextIndex >= currentPlaylist.length) {
                        if (repeatMode === 'all') nextIndex = 0
                        else return null
                    }
                }
                set({currentIndex: nextIndex})
                return currentPlaylist[nextIndex]?.media_id || null
            },

            playPrevious: () => {
                const state = get()
                const {currentPlaylist, currentIndex, repeatMode} = state
                if (currentPlaylist.length === 0) return null

                let prevIndex = currentIndex - 1
                if (prevIndex < 0) {
                    if (repeatMode === 'all') prevIndex = currentPlaylist.length - 1
                    else prevIndex = 0
                }
                set({currentIndex: prevIndex})
                return currentPlaylist[prevIndex]?.media_id || null
            },

            toggleShuffle: () => set(s => ({shuffleMode: !s.shuffleMode})),

            toggleRepeat: () => {
                set(s => {
                    const modes: Array<'none' | 'one' | 'all'> = ['none', 'one', 'all']
                    const idx = modes.indexOf(s.repeatMode)
                    return {repeatMode: modes[(idx + 1) % modes.length]}
                })
            },

            setPlaylistVisible: (visible: boolean) => set({playlistVisible: visible}),
            togglePlaylistVisible: () => set(s => ({playlistVisible: !s.playlistVisible})),

            setPlaylistFromIds: (ids: string[], titles?: string[]) => {
                const items: PlaylistItem[] = ids.map((id, i) => ({
                    media_id: id,
                    title: titles?.[i] || id,
                    position: i,
                    added_at: new Date().toISOString(),
                }))
                set({
                    currentPlaylist: items,
                    currentIndex: 0,
                    currentPlaylistId: null,
                    currentPlaylistName: 'Now Playing',
                })
            },
        }),
        {
            name: 'media-server-playlist',
            partialize: (state: PlaylistState) => ({
                repeatMode: state.repeatMode,
                shuffleMode: state.shuffleMode,
            }),
        },
    ),
)

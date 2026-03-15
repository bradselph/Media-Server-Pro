import {describe, expect, it, vi, beforeEach} from 'vitest'
import {usePlaylistStore} from './playlistStore'
import {playlistApi} from '@/api/endpoints'
import type {PlaylistItem} from '@/api/types'

vi.mock('@/api/endpoints')

function makeItem(mediaId: string, position = 0): PlaylistItem {
    return {
        media_id: mediaId,
        title: mediaId,
        position,
        added_at: '2026-01-01T00:00:00Z',
    }
}

const initialState = {
    currentPlaylist: [] as PlaylistItem[],
    currentIndex: -1,
    currentPlaylistId: null,
    currentPlaylistName: 'Now Playing',
    repeatMode: 'none' as const,
    shuffleMode: false,
    savedPlaylists: [],
    playlistVisible: false,
    playlistError: null,
}

beforeEach(() => {
    vi.resetAllMocks()
    usePlaylistStore.setState(initialState)
})

describe('playlistStore', () => {
    it('initial state defaults', () => {
        const state = usePlaylistStore.getState()
        expect(state.currentPlaylist).toEqual([])
        expect(state.currentIndex).toBe(-1)
        expect(state.repeatMode).toBe('none')
        expect(state.shuffleMode).toBe(false)
    })

    it('addItem appends and sets index if first item', () => {
        const item = makeItem('a')
        usePlaylistStore.getState().addItem(item)

        const state = usePlaylistStore.getState()
        expect(state.currentPlaylist).toHaveLength(1)
        expect(state.currentPlaylist[0].media_id).toBe('a')
        expect(state.currentIndex).toBe(0)
    })

    it('addItem appends without changing index for subsequent items', () => {
        usePlaylistStore.setState({
            currentPlaylist: [makeItem('a')],
            currentIndex: 0,
        })

        usePlaylistStore.getState().addItem(makeItem('b'))

        const state = usePlaylistStore.getState()
        expect(state.currentPlaylist).toHaveLength(2)
        expect(state.currentIndex).toBe(0)
    })

    it('removeItem adjusts currentIndex correctly', () => {
        usePlaylistStore.setState({
            currentPlaylist: [makeItem('a'), makeItem('b'), makeItem('c')],
            currentIndex: 1,
        })

        // Remove item before current index
        usePlaylistStore.getState().removeItem(0)

        const state = usePlaylistStore.getState()
        expect(state.currentPlaylist).toHaveLength(2)
        expect(state.currentPlaylist[0].media_id).toBe('b')
        expect(state.currentIndex).toBe(0) // decremented since removed item was before
    })

    it('clearPlaylist resets everything', () => {
        usePlaylistStore.setState({
            currentPlaylist: [makeItem('a'), makeItem('b')],
            currentIndex: 1,
            currentPlaylistId: 'pl-1',
            currentPlaylistName: 'My List',
        })

        usePlaylistStore.getState().clearPlaylist()

        const state = usePlaylistStore.getState()
        expect(state.currentPlaylist).toEqual([])
        expect(state.currentIndex).toBe(-1)
        expect(state.currentPlaylistId).toBeNull()
        expect(state.currentPlaylistName).toBe('Now Playing')
    })

    it('reorderItems moves item and updates index', () => {
        usePlaylistStore.setState({
            currentPlaylist: [makeItem('a', 0), makeItem('b', 1), makeItem('c', 2)],
            currentIndex: 0,
        })

        // Move item 0 to position 2
        usePlaylistStore.getState().reorderItems(0, 2)

        const state = usePlaylistStore.getState()
        expect(state.currentPlaylist.map(i => i.media_id)).toEqual(['b', 'c', 'a'])
        // currentIndex was 0 (the moved item), so it should now be 2
        expect(state.currentIndex).toBe(2)
    })

    it('playNext advances index', () => {
        usePlaylistStore.setState({
            currentPlaylist: [makeItem('a'), makeItem('b'), makeItem('c')],
            currentIndex: 0,
        })

        const result = usePlaylistStore.getState().playNext()
        expect(result).toBe('b')
        expect(usePlaylistStore.getState().currentIndex).toBe(1)
    })

    it('playNext returns null at end without repeat', () => {
        usePlaylistStore.setState({
            currentPlaylist: [makeItem('a'), makeItem('b')],
            currentIndex: 1,
            repeatMode: 'none',
        })

        const result = usePlaylistStore.getState().playNext()
        expect(result).toBeNull()
    })

    it('playNext wraps with repeat all', () => {
        usePlaylistStore.setState({
            currentPlaylist: [makeItem('a'), makeItem('b')],
            currentIndex: 1,
            repeatMode: 'all',
        })

        const result = usePlaylistStore.getState().playNext()
        expect(result).toBe('a')
        expect(usePlaylistStore.getState().currentIndex).toBe(0)
    })

    it('playPrevious decrements index', () => {
        usePlaylistStore.setState({
            currentPlaylist: [makeItem('a'), makeItem('b'), makeItem('c')],
            currentIndex: 2,
        })

        const result = usePlaylistStore.getState().playPrevious()
        expect(result).toBe('b')
        expect(usePlaylistStore.getState().currentIndex).toBe(1)
    })

    it('toggleShuffle toggles shuffleMode', () => {
        expect(usePlaylistStore.getState().shuffleMode).toBe(false)

        usePlaylistStore.getState().toggleShuffle()
        expect(usePlaylistStore.getState().shuffleMode).toBe(true)

        usePlaylistStore.getState().toggleShuffle()
        expect(usePlaylistStore.getState().shuffleMode).toBe(false)
    })

    it('toggleRepeat cycles none -> one -> all', () => {
        expect(usePlaylistStore.getState().repeatMode).toBe('none')

        usePlaylistStore.getState().toggleRepeat()
        expect(usePlaylistStore.getState().repeatMode).toBe('one')

        usePlaylistStore.getState().toggleRepeat()
        expect(usePlaylistStore.getState().repeatMode).toBe('all')

        usePlaylistStore.getState().toggleRepeat()
        expect(usePlaylistStore.getState().repeatMode).toBe('none')
    })

    it('setPlaylistFromIds creates PlaylistItems from ids', () => {
        usePlaylistStore.getState().setPlaylistFromIds(
            ['id-1', 'id-2', 'id-3'],
            ['Title 1', 'Title 2', 'Title 3'],
        )

        const state = usePlaylistStore.getState()
        expect(state.currentPlaylist).toHaveLength(3)
        expect(state.currentPlaylist[0].media_id).toBe('id-1')
        expect(state.currentPlaylist[0].title).toBe('Title 1')
        expect(state.currentPlaylist[1].position).toBe(1)
        expect(state.currentIndex).toBe(0)
        expect(state.currentPlaylistName).toBe('Now Playing')
    })

    it('loadPlaylists surfaces error on failure', async () => {
        vi.mocked(playlistApi.list).mockRejectedValue(new Error('Network error'))

        await usePlaylistStore.getState().loadPlaylists()

        expect(usePlaylistStore.getState().playlistError).toBe('Network error')
    })

    it('loadPlaylists clears error on success', async () => {
        usePlaylistStore.setState({playlistError: 'previous error'})
        vi.mocked(playlistApi.list).mockResolvedValue([])

        await usePlaylistStore.getState().loadPlaylists()

        expect(usePlaylistStore.getState().playlistError).toBeNull()
        expect(usePlaylistStore.getState().savedPlaylists).toEqual([])
    })
})

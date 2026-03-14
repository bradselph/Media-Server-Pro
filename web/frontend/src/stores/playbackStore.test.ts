import {describe, expect, it, vi, beforeEach} from 'vitest'
import {usePlaybackStore} from './playbackStore'
import {watchHistoryApi} from '@/api/endpoints'

vi.mock('@/api/endpoints')

const initialState = {
    isPlaying: false,
    currentMediaId: null,
    currentMediaTitle: '',
    currentMediaType: null as 'video' | 'audio' | null,
    currentVolume: 0.8,
    isMuted: false,
    currentTime: 0,
    duration: 0,
    playbackSpeed: 1,
    hlsEnabled: false,
    hlsUrl: null,
}

beforeEach(() => {
    vi.resetAllMocks()
    usePlaybackStore.setState(initialState)
})

describe('playbackStore', () => {
    it('initial state defaults', () => {
        const state = usePlaybackStore.getState()
        expect(state.isPlaying).toBe(false)
        expect(state.currentMediaId).toBeNull()
        expect(state.currentVolume).toBe(0.8)
        expect(state.playbackSpeed).toBe(1)
        expect(state.isMuted).toBe(false)
    })

    it('togglePlayPause toggles isPlaying', () => {
        usePlaybackStore.setState({isPlaying: true})
        usePlaybackStore.getState().togglePlayPause()
        expect(usePlaybackStore.getState().isPlaying).toBe(false)

        usePlaybackStore.getState().togglePlayPause()
        expect(usePlaybackStore.getState().isPlaying).toBe(true)
    })

    it('setVolume clamps between 0 and 1', () => {
        usePlaybackStore.getState().setVolume(1.5)
        expect(usePlaybackStore.getState().currentVolume).toBe(1)

        usePlaybackStore.getState().setVolume(-0.5)
        expect(usePlaybackStore.getState().currentVolume).toBe(0)

        usePlaybackStore.getState().setVolume(0.5)
        expect(usePlaybackStore.getState().currentVolume).toBe(0.5)
    })

    it('setVolume unmutes', () => {
        usePlaybackStore.setState({isMuted: true})
        usePlaybackStore.getState().setVolume(0.5)
        expect(usePlaybackStore.getState().isMuted).toBe(false)
    })

    it('toggleMute toggles isMuted state', () => {
        expect(usePlaybackStore.getState().isMuted).toBe(false)

        usePlaybackStore.getState().toggleMute()
        expect(usePlaybackStore.getState().isMuted).toBe(true)

        usePlaybackStore.getState().toggleMute()
        expect(usePlaybackStore.getState().isMuted).toBe(false)
    })

    it('stopPlayback saves position and resets state', () => {
        vi.mocked(watchHistoryApi.trackPosition).mockResolvedValue(undefined as void)

        usePlaybackStore.setState({
            isPlaying: true,
            currentMediaId: 'media-1',
            currentMediaTitle: 'Test Video',
            currentMediaType: 'video',
            currentTime: 120,
            duration: 300,
            hlsEnabled: true,
            hlsUrl: '/hls/media-1/master.m3u8',
        })

        usePlaybackStore.getState().stopPlayback()

        // Should have called trackPosition with current state
        expect(watchHistoryApi.trackPosition).toHaveBeenCalledWith('media-1', 120, 300)

        // State should be reset
        const state = usePlaybackStore.getState()
        expect(state.isPlaying).toBe(false)
        expect(state.currentMediaId).toBeNull()
        expect(state.currentMediaTitle).toBe('')
        expect(state.currentMediaType).toBeNull()
        expect(state.currentTime).toBe(0)
        expect(state.duration).toBe(0)
        expect(state.hlsUrl).toBeNull()
        expect(state.hlsEnabled).toBe(false)
    })

    it('stopPlayback does not track position when currentTime is 0', () => {
        usePlaybackStore.setState({
            currentMediaId: 'media-1',
            currentTime: 0,
        })

        usePlaybackStore.getState().stopPlayback()
        expect(watchHistoryApi.trackPosition).not.toHaveBeenCalled()
    })

    it('setPlaybackSpeed updates speed', () => {
        usePlaybackStore.getState().setPlaybackSpeed(2)
        expect(usePlaybackStore.getState().playbackSpeed).toBe(2)
    })
})

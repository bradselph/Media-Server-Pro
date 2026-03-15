import {describe, expect, it, vi, beforeEach} from 'vitest'
import {useSettingsStore} from './settingsStore'
import {settingsApi} from '@/api/endpoints'
import type {ServerSettings} from '@/api/types'

vi.mock('@/api/endpoints')

const initialState = {
    serverSettings: null,
    isLoading: false,
    error: null,
}

const mockSettings: ServerSettings = {
    thumbnails: {
        enabled: true,
        autoGenerate: true,
        width: 320,
        height: 180,
        video_preview_count: 5,
    },
    streaming: {
        mobileOptimization: false,
    },
    analytics: {
        enabled: true,
    },
    features: {
        enableThumbnails: true,
        enableHLS: true,
        enableAnalytics: true,
        enablePlaylists: true,
        enableUserAuth: true,
        enableAdminPanel: true,
    },
} as ServerSettings

beforeEach(() => {
    vi.resetAllMocks()
    useSettingsStore.setState(initialState)
})

describe('settingsStore', () => {
    it('initial state', () => {
        const state = useSettingsStore.getState()
        expect(state.serverSettings).toBeNull()
        expect(state.isLoading).toBe(false)
        expect(state.error).toBeNull()
    })

    it('loadServerSettings fetches and stores settings', async () => {
        vi.mocked(settingsApi.getServerSettings).mockResolvedValue(mockSettings)

        await useSettingsStore.getState().loadServerSettings()

        const state = useSettingsStore.getState()
        expect(state.serverSettings).toEqual(mockSettings)
        expect(state.isLoading).toBe(false)
        expect(state.error).toBeNull()
    })

    it('loadServerSettings sets error on failure', async () => {
        vi.mocked(settingsApi.getServerSettings).mockRejectedValue(
            new Error('Connection refused'),
        )

        await useSettingsStore.getState().loadServerSettings()

        const state = useSettingsStore.getState()
        expect(state.serverSettings).toBeNull()
        expect(state.isLoading).toBe(false)
        expect(state.error).toBe('Connection refused')
    })
})

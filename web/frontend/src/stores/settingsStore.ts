import {create} from 'zustand'
import {settingsApi} from '@/api/endpoints'
import type {ServerSettings} from '@/api/types'

interface SettingsState {
    serverSettings: ServerSettings | null
    isLoading: boolean
    error: string | null
    loadServerSettings: () => Promise<void>
}

export const useSettingsStore = create<SettingsState>((set) => ({
    serverSettings: null,
    isLoading: false,
    error: null,

    loadServerSettings: async () => {
        set({isLoading: true, error: null})
        try {
            const settings = await settingsApi.getServerSettings()
            set({serverSettings: settings, isLoading: false})
        } catch (err) {
            set({
                error: err instanceof Error ? err.message : 'Failed to load settings',
                isLoading: false,
            })
        }
    },
}))

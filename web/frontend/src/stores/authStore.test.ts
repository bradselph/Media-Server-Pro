import {describe, expect, it, vi, beforeEach} from 'vitest'
import {useAuthStore} from './authStore'
import {authApi} from '@/api/endpoints'
import {ApiError} from '@/api/client'

vi.mock('@/api/endpoints')

const defaultPermissions = {
    can_stream: false,
    can_download: false,
    can_upload: false,
    can_delete: false,
    can_manage: false,
    can_view_mature: false,
    can_create_playlists: false,
}

const mockUser = {
    id: 'u1',
    username: 'testuser',
    role: 'viewer' as const,
    type: 'standard',
    enabled: true,
    permissions: {
        can_stream: true,
        can_download: true,
        can_upload: false,
        can_delete: false,
        can_manage: false,
        can_view_mature: false,
        can_create_playlists: true,
    },
    preferences: {
        theme: 'dark' as const,
        default_quality: 'auto',
        show_mature: false,
        mature_preference_set: false,
        auto_play: true,
        equalizer_preset: 'flat',
        resume_playback: true,
        show_analytics: false,
        items_per_page: 20,
        view_mode: 'grid',
        playback_speed: 1,
        volume: 0.8,
        language: 'en',
        sort_by: 'title',
        sort_order: 'asc',
        filter_category: '',
        filter_media_type: '',
        show_continue_watching: true,
        show_recommended: true,
        show_trending: true,
    },
    storage_used: 0,
    active_streams: 0,
    created_at: '2026-01-01T00:00:00Z',
}

const initialState = {
    user: null,
    isAuthenticated: false,
    isAdmin: false,
    allowGuests: false,
    permissions: defaultPermissions,
    isLoading: true,
}

beforeEach(() => {
    vi.resetAllMocks()
    useAuthStore.setState(initialState)
})

describe('authStore', () => {
    it('initial state has correct defaults', () => {
        const state = useAuthStore.getState()
        expect(state.isAuthenticated).toBe(false)
        expect(state.isAdmin).toBe(false)
        expect(state.isLoading).toBe(true)
        expect(state.user).toBeNull()
        expect(state.allowGuests).toBe(false)
        expect(state.permissions).toEqual(defaultPermissions)
    })

    it('checkSession sets user when authenticated', async () => {
        vi.mocked(authApi.getSession).mockResolvedValue({
            authenticated: true,
            allow_guests: false,
            user: mockUser,
        })

        await useAuthStore.getState().checkSession()

        const state = useAuthStore.getState()
        expect(state.isAuthenticated).toBe(true)
        expect(state.user).toEqual(mockUser)
        expect(state.isAdmin).toBe(false)
        expect(state.allowGuests).toBe(false)
        expect(state.permissions).toEqual(mockUser.permissions)
        expect(state.isLoading).toBe(false)
    })

    it('checkSession clears state on 401', async () => {
        // First set some user state
        useAuthStore.setState({user: mockUser, isAuthenticated: true, isAdmin: false})

        vi.mocked(authApi.getSession).mockRejectedValue(
            new ApiError('Unauthorized', 401),
        )

        await useAuthStore.getState().checkSession()

        const state = useAuthStore.getState()
        expect(state.isAuthenticated).toBe(false)
        expect(state.user).toBeNull()
        expect(state.isAdmin).toBe(false)
        expect(state.isLoading).toBe(false)
    })

    it('checkSession preserves state on transient error', async () => {
        // Set up an authenticated user
        useAuthStore.setState({user: mockUser, isAuthenticated: true, isAdmin: false})

        vi.mocked(authApi.getSession).mockRejectedValue(
            new Error('Network error'),
        )

        await useAuthStore.getState().checkSession()

        const state = useAuthStore.getState()
        // User state should be preserved on non-401 errors
        expect(state.user).toEqual(mockUser)
        expect(state.isAuthenticated).toBe(true)
        expect(state.isLoading).toBe(false)
    })

    it('login calls authApi.login and checkSession', async () => {
        vi.mocked(authApi.login).mockResolvedValue({
            session_id: 's1',
            is_admin: false,
            username: 'testuser',
            role: 'viewer',
            expires_at: '2026-12-31T00:00:00Z',
        })
        vi.mocked(authApi.getSession).mockResolvedValue({
            authenticated: true,
            allow_guests: false,
            user: mockUser,
        })

        const result = await useAuthStore.getState().login('testuser', 'password')

        expect(authApi.login).toHaveBeenCalledWith('testuser', 'password')
        expect(authApi.getSession).toHaveBeenCalled()
        expect(result.isAdmin).toBe(false)
        expect(useAuthStore.getState().isAuthenticated).toBe(true)
    })

    it('logout clears user state but preserves allowGuests', async () => {
        // Set up state with allowGuests true
        useAuthStore.setState({
            user: mockUser,
            isAuthenticated: true,
            isAdmin: false,
            allowGuests: true,
        })

        vi.mocked(authApi.logout).mockResolvedValue(undefined as void)

        await useAuthStore.getState().logout()

        const state = useAuthStore.getState()
        expect(state.user).toBeNull()
        expect(state.isAuthenticated).toBe(false)
        expect(state.isAdmin).toBe(false)
        expect(state.allowGuests).toBe(true)
    })

    it('setUser updates auth state', () => {
        useAuthStore.getState().setUser(mockUser)

        const state = useAuthStore.getState()
        expect(state.user).toEqual(mockUser)
        expect(state.isAuthenticated).toBe(true)
        expect(state.isAdmin).toBe(false)
        expect(state.permissions).toEqual(mockUser.permissions)
    })

    it('setUser with admin user sets isAdmin true', () => {
        const adminUser = {...mockUser, role: 'admin' as const}
        useAuthStore.getState().setUser(adminUser)

        expect(useAuthStore.getState().isAdmin).toBe(true)
    })

    it('setUser with null clears state', () => {
        useAuthStore.setState({user: mockUser, isAuthenticated: true})
        useAuthStore.getState().setUser(null)

        const state = useAuthStore.getState()
        expect(state.user).toBeNull()
        expect(state.isAuthenticated).toBe(false)
        expect(state.isAdmin).toBe(false)
        expect(state.permissions).toEqual(defaultPermissions)
    })
})

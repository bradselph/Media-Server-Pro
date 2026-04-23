import type {LoginResponse, SessionCheckResponse, User, UserPermissions, UserPreferences} from '~/types/api'
import {asRecord} from '~/utils/typeGuards'

function asString(value: unknown, fallback = ''): string {
    return typeof value === 'string' ? value : fallback
}

function asBoolean(value: unknown, fallback = false): boolean {
    return typeof value === 'boolean' ? value : fallback
}

function asNumber(value: unknown, fallback: number): number {
    return typeof value === 'number' && Number.isFinite(value) ? value : fallback
}

export function normalizePermissions(input: unknown): UserPermissions {
    const src = asRecord(input) ?? {}
    return {
        can_stream: asBoolean(src.can_stream, true),
        can_download: asBoolean(src.can_download, false),
        can_upload: asBoolean(src.can_upload, false),
        can_delete: asBoolean(src.can_delete, false),
        can_manage: asBoolean(src.can_manage, false),
        can_view_mature: asBoolean(src.can_view_mature, false),
        can_create_playlists: asBoolean(src.can_create_playlists, true),
    }
}

export function normalizePreferences(input: unknown): UserPreferences {
    const src = asRecord(input) ?? {}
    const showMature = src.show_mature ?? src.show_mature_content
    const showAnalytics = src.show_analytics ?? src.collect_analytics
    const showContinue = src.show_continue_watching ?? src.show_home_continue_watching
    const showRecommended = src.show_recommended ?? src.show_home_suggestions
    const showTrending = src.show_trending ?? src.show_home_recently_added

    return {
        theme: asString(src.theme, 'dark'),
        view_mode: (asString(src.view_mode, 'grid') as UserPreferences['view_mode']),
        default_quality: asString(src.default_quality, 'auto'),
        auto_play: asBoolean(src.auto_play, false),
        playback_speed: asNumber(src.playback_speed, 1),
        volume: asNumber(src.volume, 1),
        show_mature: asBoolean(showMature, false),
        mature_preference_set: asBoolean(src.mature_preference_set, false),
        language: asString(src.language, 'en'),
        equalizer_preset: asString(src.equalizer_preset, ''),
        resume_playback: asBoolean(src.resume_playback, true),
        show_analytics: asBoolean(showAnalytics, true),
        items_per_page: asNumber(src.items_per_page, 20),
        sort_by: asString(src.sort_by, 'date_added'),
        sort_order: asString(src.sort_order, 'desc'),
        filter_category: asString(src.filter_category, ''),
        filter_media_type: asString(src.filter_media_type, ''),
        custom_eq_presets: (asRecord(src.custom_eq_presets) ?? undefined) as UserPreferences['custom_eq_presets'],
        show_continue_watching: asBoolean(showContinue, true),
        show_recommended: asBoolean(showRecommended, true),
        show_trending: asBoolean(showTrending, true),
        skip_interval: asNumber(src.skip_interval, 10),
        shuffle_enabled: asBoolean(src.shuffle_enabled, false),
        show_buffer_bar: asBoolean(src.show_buffer_bar, true),
        download_prompt: asBoolean(src.download_prompt, true),
    }
}

// Only send user-settable preference fields to prevent sending internal flags
const PREF_PATCH_KEYS: (keyof UserPreferences)[] = [
    'theme', 'view_mode', 'default_quality', 'auto_play', 'playback_speed',
    'volume', 'show_mature', 'language', 'equalizer_preset', 'resume_playback',
    'show_analytics', 'items_per_page', 'sort_by', 'sort_order',
    'filter_category', 'filter_media_type', 'custom_eq_presets',
    'show_continue_watching', 'show_recommended', 'show_trending',
    'skip_interval', 'shuffle_enabled', 'show_buffer_bar', 'download_prompt',
]

export function toPreferencesPatch(input: Partial<UserPreferences>): Record<string, unknown> {
    const out: Record<string, unknown> = {}
    for (const k of PREF_PATCH_KEYS) {
        if (k in input) out[k] = input[k]
    }
    return out
}

export function normalizeUser(input: unknown): User | null {
    const src = asRecord(input)
    if (!src) return null

    const username = asString(src.username)
    if (!username) return null

    return {
        id: asString(src.id),
        username,
        email: asString(src.email) || undefined,
        role: (['admin', 'viewer'].includes(asString(src.role)) ? asString(src.role) : 'viewer') as User['role'],
        type: asString(src.type, 'standard'),
        enabled: asBoolean(src.enabled, true),
        created_at: asString(src.created_at),
        last_login: asString(src.last_login) || undefined,
        previous_last_login: asString(src.previous_last_login) || undefined,
        storage_used: asNumber(src.storage_used, 0),
        active_streams: asNumber(src.active_streams, 0),
        watch_history: Array.isArray(src.watch_history) ? (src.watch_history as User['watch_history']) : undefined,
        permissions: normalizePermissions(src.permissions),
        preferences: normalizePreferences(src.preferences),
        metadata: (asRecord(src.metadata) ?? undefined) as User['metadata'],
    }
}

export function normalizeSession(input: unknown): SessionCheckResponse {
    const src = asRecord(input) ?? {}
    return {
        authenticated: asBoolean(src.authenticated, false),
        allow_guests: asBoolean(src.allow_guests, false),
        user: normalizeUser(src.user) ?? undefined,
    }
}

export function normalizeLogin(input: unknown): LoginResponse {
    const src = asRecord(input) ?? {}
    return {
        session_id: asString(src.session_id),
        username: asString(src.username),
        role: (['admin', 'viewer'].includes(asString(src.role)) ? asString(src.role) : 'viewer') as LoginResponse['role'],
        is_admin: asBoolean(src.is_admin, asString(src.role) === 'admin'),
        expires_at: asString(src.expires_at),
    }
}

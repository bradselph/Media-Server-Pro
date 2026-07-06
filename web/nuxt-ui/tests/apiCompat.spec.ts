import {describe, it, expect} from 'vitest'
import {
    normalizePermissions,
    normalizePreferences,
    normalizeUser,
    normalizeLogin,
    toPreferencesPatch,
} from '~/utils/apiCompat'

// apiCompat normalizes raw API payloads at the FE<->BE boundary. These tests lock
// in the safe defaults, the legacy field aliases, and the accent_hue clamp so a
// backend field rename or a missing field can't silently change client behavior.

describe('normalizePermissions', () => {
    it('applies safe defaults for missing/invalid input', () => {
        const p = normalizePermissions(undefined)
        expect(p.can_stream).toBe(true)
        expect(p.can_create_playlists).toBe(true)
        expect(p.can_upload).toBe(false)
        expect(p.can_manage).toBe(false)
        expect(p.can_view_mature).toBe(false)
    })

    it('reads provided booleans and ignores non-booleans', () => {
        const p = normalizePermissions({can_view_mature: true, can_stream: false, can_upload: 'yes'})
        expect(p.can_view_mature).toBe(true)
        expect(p.can_stream).toBe(false)
        expect(p.can_upload).toBe(false) // non-boolean falls back to the default
    })
})

describe('normalizePreferences', () => {
    it('defaults accent_hue to 220 and clamps out-of-range / rounds fractional values', () => {
        expect(normalizePreferences({}).accent_hue).toBe(220)
        expect(normalizePreferences({accent_hue: -50}).accent_hue).toBe(0)
        expect(normalizePreferences({accent_hue: 999}).accent_hue).toBe(360)
        expect(normalizePreferences({accent_hue: 137.6}).accent_hue).toBe(138)
    })

    it('falls back to legacy field aliases when the canonical key is absent', () => {
        expect(normalizePreferences({show_mature_content: true}).show_mature).toBe(true)
        expect(normalizePreferences({show_home_continue_watching: false}).show_continue_watching).toBe(false)
        expect(normalizePreferences({show_home_suggestions: false}).show_recommended).toBe(false)
        expect(normalizePreferences({show_home_recently_added: false}).show_trending).toBe(false)
    })

    it('prefers the canonical key over the legacy alias (nullish-coalescing, not OR)', () => {
        // canonical false must win over legacy true — proves `??` semantics, so a
        // user who turned mature off isn't overridden by a stale legacy field.
        expect(normalizePreferences({show_mature: false, show_mature_content: true}).show_mature).toBe(false)
    })
})

describe('toPreferencesPatch', () => {
    it('emits only user-settable keys present in the input (drops internal flags)', () => {
        const patch = toPreferencesPatch({auto_play: true, mature_preference_set: true} as never)
        expect(patch.auto_play).toBe(true)
        expect('mature_preference_set' in patch).toBe(false)
    })
})

describe('normalizeUser', () => {
    it('returns null when username is missing', () => {
        expect(normalizeUser(null)).toBeNull()
        expect(normalizeUser({id: 'x'})).toBeNull()
    })

    it('coerces an unknown role to viewer and preserves valid roles', () => {
        expect(normalizeUser({username: 'a', role: 'superadmin'})?.role).toBe('viewer')
        expect(normalizeUser({username: 'a', role: 'admin'})?.role).toBe('admin')
        expect(normalizeUser({username: 'a', role: 'viewer'})?.role).toBe('viewer')
    })

    it('nests normalized permissions and preferences', () => {
        const u = normalizeUser({username: 'a'})
        expect(u?.permissions.can_stream).toBe(true)
        expect(u?.preferences.accent_hue).toBe(220)
    })
})

describe('normalizeLogin', () => {
    it('derives is_admin from role when the flag is absent', () => {
        expect(normalizeLogin({role: 'admin', session_id: 's'}).is_admin).toBe(true)
        expect(normalizeLogin({role: 'viewer'}).is_admin).toBe(false)
    })

    it('honors an explicit is_admin flag', () => {
        expect(normalizeLogin({role: 'viewer', is_admin: true}).is_admin).toBe(true)
    })
})

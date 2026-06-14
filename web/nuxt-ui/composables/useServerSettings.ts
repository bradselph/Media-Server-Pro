import type {ServerSettings} from '~/types/api'

/**
 * Cached access to the public /api/server-settings payload, shared across
 * components via useState so the endpoint is fetched once per app session.
 *
 * `settings` stays null until `load()` resolves (and on fetch failure), so
 * callers gating UI on a flag should fail open: treat null as "allowed" and
 * let the backend's own enforcement be the final word.
 */
export function useServerSettings() {
    const settings = useState<ServerSettings | null>('server-settings', () => null)
    const loaded = useState<boolean>('server-settings-loaded', () => false)

    async function load(): Promise<ServerSettings | null> {
        if (loaded.value) return settings.value
        try {
            settings.value = await useSettingsApi().get() as ServerSettings
            loaded.value = true
        } catch {
            // Non-critical: leave settings null (callers fail open) and allow
            // a later call to retry.
        }
        return settings.value
    }

    return {settings, load}
}

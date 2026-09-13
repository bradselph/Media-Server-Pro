import {beforeEach, describe, expect, it, vi} from 'vitest'
import {createPinia, setActivePinia} from 'pinia'

// A 401 does not prove the session is gone. The backend's sessionAuth middleware
// only attaches the session to a request when ValidateSession succeeds, and a
// transient failure there (DB blip, exhausted connection pool) takes the same
// path as a missing cookie — so every requireAuth route answers 401 while the
// session is still perfectly valid.
//
// Redirecting on that produced a hard-reload loop: the home page fires a dozen
// session-scoped requests right after login, one 401s, useApi bounces to /login,
// /login sees a valid session and bounces straight back, and round it goes. The
// user saw a blank page until they interrupted it with a manual reload.
//
// These lock in both halves of the contract: don't bounce when the session is
// still good, DO bounce when it really is gone.

interface FetchLog {
    session: number
    protectedCalls: number
}

function jsonResponse(body: unknown, status = 200): Response {
    return {
        ok: status >= 200 && status < 300,
        status,
        headers: {get: () => 'application/json'},
        json: async () => body,
        text: async () => JSON.stringify(body),
    } as unknown as Response
}

/**
 * Installs a fetch stub. `sessionAuthenticated` is what /api/auth/session
 * reports; every other route answers 401 to model the outage.
 */
function stubFetch(sessionAuthenticated: () => boolean): FetchLog {
    const log: FetchLog = {session: 0, protectedCalls: 0}
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
        const url = String(typeof input === 'string' ? input : (input as Request).url ?? input)
        if (url.includes('/api/auth/session')) {
            log.session++
            const authed = sessionAuthenticated()
            return jsonResponse({
                success: true,
                data: {
                    authenticated: authed,
                    allow_guests: true,
                    user: authed ? {id: 'u1', username: 'demo', role: 'viewer'} : undefined,
                },
            })
        }
        log.protectedCalls++
        return jsonResponse({success: false, error: 'Unauthorized'}, 401)
    }) as unknown as typeof fetch
    return log
}

/**
 * Fresh module registry per test: useApi keeps module-scoped redirect guards,
 * and the auth store keeps a module-scoped logged-in mirror.
 */
async function loadModules() {
    vi.resetModules()
    const authModule = await import('~/stores/auth')
    const apiModule = await import('~/composables/useApi')
    return {authModule, apiModule}
}

/** Lets the fire-and-forget 401 handler finish its session re-check. */
async function flush() {
    for (let i = 0; i < 10; i++) await Promise.resolve()
    await new Promise(resolve => setTimeout(resolve, 0))
}

describe('401 handling', () => {
    let replace: ReturnType<typeof vi.fn>

    beforeEach(() => {
        setActivePinia(createPinia())
        window.sessionStorage.clear()
        window.localStorage.clear()
        replace = vi.fn()
        // happy-dom's location.replace would actually navigate; intercept it.
        vi.spyOn(window.location, 'replace').mockImplementation(replace as unknown as (url: string | URL) => void)
    })

    it('does not redirect when the server still reports an active session', async () => {
        const {authModule, apiModule} = await loadModules()
        const log = stubFetch(() => true)

        const store = authModule.useAuthStore()
        await store.fetchSession()
        expect(authModule.isAuthenticated()).toBe(true)

        // A session-scoped call 401s even though the session is fine.
        await expect(apiModule.useApi().get('/api/favorites')).rejects.toThrow()
        await flush()

        expect(replace).not.toHaveBeenCalled()
        // It should have asked the server before deciding, rather than assuming.
        expect(log.session).toBeGreaterThan(1)
    })

    it('redirects to login when the server confirms the session is gone', async () => {
        const {authModule, apiModule} = await loadModules()
        let authed = true
        stubFetch(() => authed)

        const store = authModule.useAuthStore()
        await store.fetchSession()
        expect(authModule.isAuthenticated()).toBe(true)

        authed = false // session expired / revoked server-side
        await expect(apiModule.useApi().get('/api/favorites')).rejects.toThrow()
        await flush()

        expect(replace).toHaveBeenCalledTimes(1)
        expect(String(replace.mock.calls[0][0])).toContain('/login')
    })

    it('stops redirecting once the reload-loop budget is spent', async () => {
        const {authModule, apiModule} = await loadModules()
        stubFetch(() => false)

        // Two redirects already happened in this tab moments ago (they survive a
        // reload via sessionStorage, which is the only reason a loop is detectable).
        const now = Date.now()
        window.sessionStorage.setItem('msp-auth-redirects', JSON.stringify([now - 500, now - 200]))

        apiModule.redirectToLogin()
        expect(replace).not.toHaveBeenCalled()
    })

    it('allows a redirect when the loop budget has aged out', async () => {
        const {apiModule} = await loadModules()
        stubFetch(() => false)

        // Same two redirects, but long enough ago to be outside the window.
        const stale = Date.now() - 60_000
        window.sessionStorage.setItem('msp-auth-redirects', JSON.stringify([stale, stale + 10]))

        apiModule.redirectToLogin()
        expect(replace).toHaveBeenCalledTimes(1)
    })

    it('guests are never redirected by a 401 from an optional auth-only call', async () => {
        const {authModule, apiModule} = await loadModules()
        stubFetch(() => false)

        expect(authModule.isAuthenticated()).toBe(false)
        await expect(apiModule.useApi().get('/api/playback')).rejects.toThrow()
        await flush()

        expect(replace).not.toHaveBeenCalled()
    })
})

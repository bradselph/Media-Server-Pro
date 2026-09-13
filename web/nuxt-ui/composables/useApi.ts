/**
 * Typed API client that unwraps the Go JSON envelope { success, data, message }.
 */
import {isAuthenticated, isPrivateSession} from '~/stores/auth'

interface GoEnvelope<T> {
    success: boolean
    data?: T
    message?: string
    error?: string
}

/**
 * Build the headers for an outgoing API request. Always sets Content-Type
 * for JSON requests; when the user has the private-session toggle on, also
 * attaches X-MSP-Private so the backend knows to skip history / analytics
 * writes for this request.
 */
function buildHeaders(base: Record<string, string>): Record<string, string> {
    if (isPrivateSession()) base['X-MSP-Private'] = '1'
    return base
}

class ApiError extends Error {
    constructor(
        message: string,
        public status: number,
        public body?: unknown,
    ) {
        super(message)
        this.name = 'ApiError'
    }
}

let _redirecting = false
let _revalidating = false

// A forced-logout redirect is a full page load, so an in-memory flag cannot see
// what the page before it did. These bound how many times we are willing to
// bounce a browser to /login before concluding we are in a loop.
const REDIRECT_LOG_KEY = 'msp-auth-redirects'
const REDIRECT_WINDOW_MS = 15_000
const REDIRECT_MAX = 2

/**
 * Record this redirect and report whether it should go ahead.
 *
 * The counter lives in sessionStorage so it survives the reload. Without it,
 * /login sending an already-authenticated user straight back to the page that
 * 401'd is an unbounded hard-reload loop, and all the user sees is a blank,
 * flashing screen.
 */
function redirectAllowed(): boolean {
    try {
        const now = Date.now()
        const raw = globalThis.sessionStorage.getItem(REDIRECT_LOG_KEY)
        const recent = (raw ? JSON.parse(raw) as number[] : [])
            .filter(t => typeof t === 'number' && now - t < REDIRECT_WINDOW_MS)
        if (recent.length >= REDIRECT_MAX) {
            console.warn('[auth] suppressing repeated redirects to /login — a session-scoped request keeps returning 401. Staying put instead of reload-looping.')
            return false
        }
        recent.push(now)
        globalThis.sessionStorage.setItem(REDIRECT_LOG_KEY, JSON.stringify(recent))
        return true
    } catch {
        // sessionStorage blocked (private mode / cookies off) — keep the old behaviour.
        return true
    }
}

/** Clears the loop guard. Called once a request succeeds, so an ordinary
 *  expiry later in the session still gets its full redirect budget. */
function clearRedirectGuard(): void {
    try {
        globalThis.sessionStorage.removeItem(REDIRECT_LOG_KEY)
    } catch { /* ignore */
    }
}

export function redirectToLogin(): void {
    if (_redirecting || !import.meta.client) return
    if (!redirectAllowed()) return
    _redirecting = true
    setTimeout(() => {
        _redirecting = false
    }, 3000)
    const redirect = globalThis.location.pathname + globalThis.location.search
    const isAuthPage = ['/login', '/signup', '/admin-login'].some(p => redirect.startsWith(p))
    const target = redirect && !isAuthPage
        ? `/login?redirect=${encodeURIComponent(redirect)}`
        : '/login'
    globalThis.location.replace(target)
}

/**
 * Ask the server whether it still recognises our session.
 *
 * Plain fetch on purpose: useApi is imported at module level by
 * useApiEndpoints, so it must not reach for Nuxt composables (same reason
 * redirectToLogin uses location.replace rather than navigateTo).
 */
async function sessionStillValid(): Promise<boolean> {
    try {
        const res = await fetch('/api/auth/session', {
            credentials: 'include',
            headers: {Accept: 'application/json'},
        })
        // Only a definitive answer may log someone out. A 401 is one; a 5xx
        // (e.g. the backend's "session store temporarily unavailable") is the
        // server saying it could not tell us, and an outage must not read as a
        // logout.
        if (res.status === 401) return false
        if (!res.ok) return true
        const envelope = await res.json() as GoEnvelope<{ authenticated?: boolean }>
        return (envelope.data ?? envelope as { authenticated?: boolean })?.authenticated === true
    } catch {
        // An unreachable server proves nothing about the session — don't log
        // someone out over a dropped connection.
        return true
    }
}

/**
 * Decide what a 401 means for a request made while a session was believed active.
 *
 * A 401 on its own does NOT prove the session is gone. The backend's sessionAuth
 * middleware only attaches the session to the request when ValidateSession
 * succeeds, and a *transient* failure there — a database blip, an exhausted
 * connection pool — takes the same path as a missing cookie. Every requireAuth
 * route then answers 401 while the session is in fact still valid.
 *
 * Redirecting on that turned a hiccup into a hard-reload loop: the home page
 * fires a dozen session-scoped requests at once, one 401s, we bounce to /login,
 * /login sees a valid session and bounces straight back, and round it goes. The
 * user sees a blank page until they interrupt it with a manual reload.
 *
 * So confirm with the server first. /api/auth/session reads the same request
 * context requireAuth does and is the same endpoint the login page checks, so
 * only redirecting when it reports "not authenticated" guarantees /login will
 * not bounce us back.
 */
async function handleUnauthorized(): Promise<void> {
    if (_revalidating || !import.meta.client) return
    _revalidating = true
    try {
        if (!await sessionStillValid()) redirectToLogin()
    } finally {
        _revalidating = false
    }
}

async function parseEnvelope<T>(res: Response): Promise<T> {
    const contentType = res.headers.get('content-type') ?? ''
    if (!contentType.includes('application/json')) {
        if (!res.ok) {
            // Try to capture error body for a more actionable message (e.g. nginx 502 HTML).
            const text = await res.text().catch(() => '')
            const detail = text.replace(/<[^>]+>/g, '').trim().slice(0, 120)
            throw new ApiError(detail ? `HTTP ${res.status}: ${detail}` : `HTTP ${res.status}`, res.status)
        }
        return undefined as T
    }

    const envelope = await res.json() as GoEnvelope<T>
    if (!res.ok || envelope.success === false) {
        // On 401, consider redirecting to login ONLY when a session was actually
        // active — i.e. a logged-in user whose session expired or was revoked.
        // Guests on public pages (player, browse, …) routinely touch auth-only
        // optional endpoints (HLS availability, playback position); those 401s
        // must be handled by the caller's catch and fall back gracefully, NOT
        // bounce the guest to /login.
        //
        // handleUnauthorized re-checks with the server before bouncing, because a
        // 401 can also mean "the backend could not load the session this time"
        // rather than "you are logged out" — see its doc comment.
        if (res.status === 401 && isAuthenticated()) void handleUnauthorized()
        throw new ApiError(
            envelope.message ?? envelope.error ?? `HTTP ${res.status}`,
            res.status,
            envelope,
        )
    }
    // A successful session-scoped call means we are not in a redirect loop, so
    // give a genuine expiry later in this session its full redirect budget back.
    if (isAuthenticated()) clearRedirectGuard()
    return (envelope.data ?? envelope) as T
}

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
    const opts: RequestInit = {
        method,
        credentials: 'include',
        headers: buildHeaders({'Content-Type': 'application/json'}),
    }
    if (body !== undefined) {
        opts.body = JSON.stringify(body)
    }
    const res = await fetch(url, opts)
    return parseEnvelope<T>(res)
}

async function requestForm<T>(method: string, url: string, form: FormData): Promise<T> {
    // Do NOT set Content-Type — the browser must set it with the multipart boundary.
    const res = await fetch(url, {
        method,
        credentials: 'include',
        body: form,
        headers: buildHeaders({}),
    })
    return parseEnvelope<T>(res)
}

function requestFormWithProgress<T>(
    method: string,
    url: string,
    form: FormData,
    onProgress: (pct: number) => void,
): Promise<T> {
    return new Promise<T>((resolve, reject) => {
        const xhr = new XMLHttpRequest()
        xhr.open(method, url)
        xhr.withCredentials = true
        if (isPrivateSession()) xhr.setRequestHeader('X-MSP-Private', '1')
        xhr.upload.addEventListener('progress', (e) => {
            if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100))
        })
        xhr.addEventListener('load', async () => {
            try {
                // Reconstruct a minimal Response so parseEnvelope can handle it.
                const res = new Response(xhr.responseText, {
                    status: xhr.status,
                    headers: {'content-type': xhr.getResponseHeader('content-type') ?? 'application/json'},
                })
                resolve(await parseEnvelope<T>(res))
            } catch (err) {
                reject(err)
            }
        })
        xhr.addEventListener('error', () => reject(new ApiError('Network error', 0)))
        xhr.addEventListener('abort', () => reject(new ApiError('Upload aborted', 0)))
        xhr.send(form)
    })
}

export function useApi() {
    return {
        get: <T>(url: string) => request<T>('GET', url),
        post: <T>(url: string, body?: unknown) => request<T>('POST', url, body),
        postForm: <T>(url: string, form: FormData) => requestForm<T>('POST', url, form),
        postFormWithProgress: <T>(url: string, form: FormData, onProgress: (pct: number) => void) =>
            requestFormWithProgress<T>('POST', url, form, onProgress),
        put: <T>(url: string, body?: unknown) => request<T>('PUT', url, body),
        patch: <T>(url: string, body?: unknown) => request<T>('PATCH', url, body),
        delete: <T>(url: string, body?: unknown) => request<T>('DELETE', url, body),
    }
}

export {ApiError}

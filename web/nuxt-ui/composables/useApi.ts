/**
 * Typed API client that unwraps the Go JSON envelope { success, data, message }.
 */

interface GoEnvelope<T> {
    success: boolean
    data?: T
    message?: string
    error?: string
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

export function redirectToLogin(): void {
    if (_redirecting || !import.meta.client) return
    _redirecting = true
    setTimeout(() => { _redirecting = false }, 3000)
    const redirect = globalThis.location.pathname + globalThis.location.search
    const isAuthPage = ['/login', '/signup', '/admin-login'].some(p => redirect.startsWith(p))
    const target = redirect && !isAuthPage
        ? `/login?redirect=${encodeURIComponent(redirect)}`
        : '/login'
    globalThis.location.replace(target)
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
        // On 401, redirect to login so stale sessions are cleared automatically.
        // NOTE: use window.location.replace (not navigateTo) — useApi is imported at module
        // level in useApiEndpoints.ts so it must not reference Nuxt composables which require
        // the Nuxt app context; doing so creates a TDZ error in the production bundle.
        if (res.status === 401) redirectToLogin()
        throw new ApiError(
            envelope.message ?? envelope.error ?? `HTTP ${res.status}`,
            res.status,
            envelope,
        )
    }
    return (envelope.data ?? envelope) as T
}

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
    const opts: RequestInit = {
        method,
        credentials: 'include',
        headers: {'Content-Type': 'application/json'},
    }
    if (body !== undefined) {
        opts.body = JSON.stringify(body)
    }
    const res = await fetch(url, opts)
    return parseEnvelope<T>(res)
}

async function requestForm<T>(method: string, url: string, form: FormData): Promise<T> {
    // Do NOT set Content-Type — the browser must set it with the multipart boundary.
    const res = await fetch(url, { method, credentials: 'include', body: form })
    return parseEnvelope<T>(res)
}

export function useApi() {
    return {
        get: <T>(url: string) => request<T>('GET', url),
        post: <T>(url: string, body?: unknown) => request<T>('POST', url, body),
        postForm: <T>(url: string, form: FormData) => requestForm<T>('POST', url, form),
        put: <T>(url: string, body?: unknown) => request<T>('PUT', url, body),
        patch: <T>(url: string, body?: unknown) => request<T>('PATCH', url, body),
        delete: <T>(url: string, body?: unknown) => request<T>('DELETE', url, body),
    }
}

export {ApiError}
